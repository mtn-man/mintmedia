package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mtn-man/mintmedia/internal/paths"
)

// IsDestinationUnavailable reports whether err indicates the destination
// filesystem itself is the problem (out of space, over quota, or permission
// denied) rather than an ordinary one-off move failure. Callers can use this
// to distinguish a systemic destination outage -- worth pausing further
// writes until it clears -- from a transient or item-specific error.
func IsDestinationUnavailable(err error) bool {
	return errors.Is(err, syscall.ENOSPC) ||
		errors.Is(err, syscall.EDQUOT) ||
		errors.Is(err, fs.ErrPermission)
}

// LibraryFileMode is the mode every file mintmedia moves into the library ends
// up with: group and other need read so a media server running as a different
// user can read it.
//
// Move applies it once, after the move, rather than each path applying its own.
// os.Rename preserves the source file's mode while the copy path builds a fresh
// temp, so without a single shared step the resulting permission would depend on
// whether src and dst happen to share a filesystem.
const LibraryFileMode = 0o644

// ErrDestinationExists is wrapped into the error Move returns when it
// refuses to overwrite an existing destination file.
var ErrDestinationExists = errors.New("destination already exists")

// IsDestinationExists reports whether err indicates Move refused to
// overwrite an existing destination file, as opposed to some other move
// failure. Callers can use this to treat the collision as an expected,
// gracefully-handled outcome rather than an operational error.
func IsDestinationExists(err error) bool {
	return errors.Is(err, ErrDestinationExists)
}

// Options configures transfer behavior.
type Options struct {
	// Reporter receives structured progress snapshots (preferred for progress bars).
	// If nil, structured reporting is disabled.
	Reporter Reporter

	// UpdateEvery controls how often structured progress is sampled.
	// If <= 0, defaults to 250 milliseconds.
	UpdateEvery time.Duration
}

// CleanupError indicates the destination is finalized but source cleanup failed.
// Callers may treat this as a warning and continue.
type CleanupError struct {
	Src string
	Dst string
	Err error
}

func (e *CleanupError) Error() string {
	if e == nil {
		return "cleanup source failed"
	}
	if e.Err == nil {
		return fmt.Sprintf("cleanup source %s after move to %s failed", e.Src, e.Dst)
	}
	return fmt.Sprintf("cleanup source %s after move to %s failed: %v", e.Src, e.Dst, e.Err)
}

func (e *CleanupError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// RenameOrCopy implements rename fast-path and copy fallback.
type RenameOrCopy struct {
	opts      Options
	newTicker func(time.Duration) ticker
	copyFn    func(io.Writer, io.Reader) (int64, error)
}

type ticker interface {
	C() <-chan time.Time
	Stop()
}

type realTicker struct {
	t *time.Ticker
}

func (rt *realTicker) C() <-chan time.Time { return rt.t.C }
func (rt *realTicker) Stop()               { rt.t.Stop() }

// NewRenameOrCopy creates a transferer that attempts os.Rename, and falls back to copy+atomic finalize.
func NewRenameOrCopy(opts Options) *RenameOrCopy {
	return &RenameOrCopy{opts: opts}
}

// tickInterval, tickerFactory, and copier are the single source of truth for
// RenameOrCopy's defaults, each falling back only when the corresponding
// field/Options value is zero. This makes a zero-value &RenameOrCopy{} (used
// directly by one test, and implicitly by any caller that skips
// NewRenameOrCopy) behave identically to a constructed one, so the
// constructor doesn't need to duplicate these fallbacks itself.

func (t *RenameOrCopy) tickInterval() time.Duration {
	if t.opts.UpdateEvery > 0 {
		return t.opts.UpdateEvery
	}
	return 250 * time.Millisecond
}

func (t *RenameOrCopy) tickerFactory() func(time.Duration) ticker {
	if t.newTicker != nil {
		return t.newTicker
	}
	return func(d time.Duration) ticker {
		return &realTicker{t: time.NewTicker(d)}
	}
}

func (t *RenameOrCopy) copier() func(io.Writer, io.Reader) (int64, error) {
	if t.copyFn != nil {
		return t.copyFn
	}
	return io.Copy
}

// Move relocates src to dst, attempting os.Rename first and falling back to
// copy+atomic finalize (with progress reporting) when rename isn't possible,
// e.g. across filesystems/devices.
func (t *RenameOrCopy) Move(ctx context.Context, src, dst string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := paths.MkdirShared(filepath.Dir(dst)); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}

	// Fail-safe: do not overwrite an existing destination.
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("%w: %s", ErrDestinationExists, dst)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat destination: %w", err)
	}

	same, err := paths.SameDevice(src, filepath.Dir(dst))
	if err != nil {
		return err
	}
	if !same {
		if err := t.copyThenReplace(ctx, src, dst); err != nil {
			return err
		}
	} else {
		// Same device: try rename, return any error without fallback.
		if err := os.Rename(src, dst); err != nil {
			return err
		}
	}

	// Best-effort: the file is already in the library, so a failed chmod is not
	// worth failing an otherwise-successful move over.
	_ = os.Chmod(dst, LibraryFileMode) //nolint:gosec // deliberate: see LibraryFileMode
	return nil
}

func (t *RenameOrCopy) copyThenReplace(ctx context.Context, src, dst string) (retErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Determine total size for progress
	var total int64 = -1
	if st, err := os.Stat(src); err == nil && st.Mode().IsRegular() {
		total = st.Size()
	}

	dir := filepath.Dir(dst)
	base := filepath.Base(dst)

	// Fail-safe: do not overwrite an existing destination.
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("%w: %s", ErrDestinationExists, dst)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat destination: %w", err)
	}

	// Create a unique temp file on the destination filesystem.
	tmpFile, err := os.CreateTemp(dir, base+".partial.*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmp := tmpFile.Name()

	in, err := os.Open(src) //nolint:gosec // src is always built internally from resolved config/plan paths, never external input
	if err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("open source file: %w", err)
	}
	defer func() {
		if err := in.Close(); err != nil && retErr == nil {
			retErr = fmt.Errorf("close source file: %w", err)
		}
	}()

	out := tmpFile
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmp)
		}
	}()

	var copied int64
	start := time.Now()

	var stopReport chan struct{}
	var reportWG sync.WaitGroup
	var stopReportOnce sync.Once

	stopReporter := func() {
		if stopReport == nil {
			return
		}
		stopReportOnce.Do(func() {
			close(stopReport)
			reportWG.Wait()
		})
	}
	defer stopReporter()

	// Structured reporter ticker (intended for progress bars).
	if t.opts.Reporter != nil {
		stopReport = make(chan struct{})
		reportWG.Go(func() {
			progressTicker := t.tickerFactory()(t.tickInterval())
			defer progressTicker.Stop()

			var lastBytes int64
			lastTime := start
			baseName := filepath.Base(dst)

			for {
				select {
				case <-ctx.Done():
					return
				case <-stopReport:
					return
				case now := <-progressTicker.C():
					c := atomic.LoadInt64(&copied)
					if c == lastBytes {
						continue
					}

					dBytes := c - lastBytes
					dt := now.Sub(lastTime).Seconds()
					mbps := 0.0
					if dt > 0 {
						mbps = (float64(dBytes) / (1024 * 1024)) / dt
					}

					t.opts.Reporter.Update(Snapshot{
						Name:     baseName,
						Copied:   c,
						Total:    total,
						RateMBps: mbps,
						Elapsed:  now.Sub(start),
					})

					lastBytes = c
					lastTime = now
				}
			}
		})
	}

	// Counting reader updates "copied" atomically
	cr := &countReader{
		r:      in,
		ctx:    ctx,
		copied: &copied,
	}

	_, copyErr := t.copier()(out, cr)
	syncErr := out.Sync()
	closeErr := out.Close()

	if copyErr != nil {
		return fmt.Errorf("copy file: %w", copyErr)
	}
	if syncErr != nil {
		return fmt.Errorf("sync temp file: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close temp file: %w", closeErr)
	}

	// Publish the temp at the library mode: os.CreateTemp makes it 0o600 and the
	// rename below makes it visible under its final name at once. Move's chmod
	// normalizes the mode across both transfer paths, but it runs after this
	// returns -- and the CleanupError below means dst is already finalized, so
	// the mode has to be right before the rename rather than after it.
	_ = os.Chmod(tmp, LibraryFileMode) //nolint:gosec // deliberate: see LibraryFileMode

	// Atomic finalize on destination filesystem
	if err := os.Rename(tmp, dst); err != nil {
		return fmt.Errorf("rename temp file to destination: %w", err)
	}

	// Only remove source after destination is finalized
	if err := os.Remove(src); err != nil {
		return &CleanupError{
			Src: src,
			Dst: dst,
			Err: err,
		}
	}

	// Success: disable deferred temp cleanup.
	cleanupTmp = false

	// Ensure no further updates can be emitted before the final callback.
	stopReporter()

	// Clear any in-place progress line.
	if t.opts.Reporter != nil {
		t.opts.Reporter.Done()
	}

	return nil
}

type countReader struct {
	r      io.Reader
	ctx    context.Context
	copied *int64
}

func (cr *countReader) Read(p []byte) (int, error) {
	if err := cr.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := cr.r.Read(p)
	if n > 0 {
		atomic.AddInt64(cr.copied, int64(n))
	}
	return n, err
}
