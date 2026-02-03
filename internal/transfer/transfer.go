package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"
)

// ProgressSink receives human-readable progress lines.
// If nil, no progress output is produced.
type ProgressSink func(line string)

// Options configures transfer behavior.
type Options struct {
	// ProgressEvery controls how often progress is sampled.
	// If <= 0, defaults to 5 seconds.
	ProgressEvery time.Duration

	// Progress receives progress updates (best-effort).
	Progress ProgressSink

	// Reporter receives structured progress snapshots (preferred for progress bars).
	// If nil, structured reporting is disabled.
	Reporter Reporter

	// ReportEvery controls how often structured progress is sampled.
	// If <= 0, defaults to 250 milliseconds.
	ReportEvery time.Duration

	// PrintDone controls whether a final "COPY DONE" line is emitted via Progress.
	// If Progress is nil, this has no effect.
	PrintDone bool
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

// Transferer moves a file from src -> dst.
// If src/dst are on different devices, we copy+replace. If same device, we attempt os.Rename and return any error without fallback.
type Transferer interface {
	Move(ctx context.Context, src, dst string) error
}

// RenameOrCopy implements rename fast-path and copy fallback.
type RenameOrCopy struct {
	opts Options
}

// NewRenameOrCopy creates a transferer that attempts os.Rename, and falls back to copy+atomic finalize.
func NewRenameOrCopy(opts Options) *RenameOrCopy {
	if opts.ProgressEvery <= 0 {
		opts.ProgressEvery = 5 * time.Second
	}
	if opts.ReportEvery <= 0 {
		opts.ReportEvery = 250 * time.Millisecond
	}
	return &RenameOrCopy{opts: opts}
}

func sameDevice(srcPath, dstDir string) (bool, error) {
	srcInfo, err := os.Lstat(srcPath)
	if err != nil {
		return false, fmt.Errorf("lstat source %q: %w", srcPath, err)
	}
	dstInfo, err := os.Stat(dstDir)
	if err != nil {
		return false, fmt.Errorf("stat destination dir %q: %w", dstDir, err)
	}

	srcStat, ok := srcInfo.Sys().(*syscall.Stat_t)
	if !ok || srcStat == nil {
		return false, fmt.Errorf("stat source %q: missing syscall.Stat_t", srcPath)
	}
	dstStat, ok := dstInfo.Sys().(*syscall.Stat_t)
	if !ok || dstStat == nil {
		return false, fmt.Errorf("stat destination dir %q: missing syscall.Stat_t", dstDir)
	}

	return srcStat.Dev == dstStat.Dev, nil
}

func (t *RenameOrCopy) Move(ctx context.Context, src, dst string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}

	// Fail-safe: do not overwrite an existing destination.
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("destination already exists: %s", dst)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat destination: %w", err)
	}

	same, err := sameDevice(src, filepath.Dir(dst))
	if err != nil {
		return err
	}
	if !same {
		return t.copyThenReplace(ctx, src, dst)
	}

	// Same device: try rename, return any error without fallback.
	if err := os.Rename(src, dst); err != nil {
		return err
	}
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
		return fmt.Errorf("destination already exists: %s", dst)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat destination: %w", err)
	}

	// Create a unique temp file on the destination filesystem.
	tmpFile, err := os.CreateTemp(dir, base+".partial.*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmp := tmpFile.Name()

	in, err := os.Open(src)
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

	var stopProgress chan struct{}
	var stopReport chan struct{}

	// Progress ticker (only emits when progress advances)
	if t.opts.Progress != nil {
		stopProgress = make(chan struct{})
		go func() {
			tick := t.opts.ProgressEvery
			if tick <= 0 {
				tick = 5 * time.Second
			}
			ticker := time.NewTicker(tick)
			defer ticker.Stop()

			var lastBytes int64
			lastTime := start
			baseName := filepath.Base(dst)

			for {
				select {
				case <-ctx.Done():
					return
				case <-stopProgress:
					return
				case now := <-ticker.C:
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

					if total > 0 {
						pct := (float64(c) / float64(total)) * 100
						t.opts.Progress(fmt.Sprintf(
							"COPYING: %s %.1f%% (%s / %s) %.1f MB/s",
							baseName,
							pct,
							humanBytes(c),
							humanBytes(total),
							mbps,
						))
					} else {
						t.opts.Progress(fmt.Sprintf(
							"COPYING: %s (%s copied) %.1f MB/s",
							baseName,
							humanBytes(c),
							mbps,
						))
					}

					lastBytes = c
					lastTime = now
				}
			}
		}()
		defer close(stopProgress)
	}

	// Structured reporter ticker (intended for progress bars).
	if t.opts.Reporter != nil {
		stopReport = make(chan struct{})
		go func() {
			tick := t.opts.ReportEvery
			if tick <= 0 {
				tick = 250 * time.Millisecond
			}
			ticker := time.NewTicker(tick)
			defer ticker.Stop()

			var lastBytes int64
			lastTime := start
			baseName := filepath.Base(dst)

			for {
				select {
				case <-ctx.Done():
					return
				case <-stopReport:
					return
				case now := <-ticker.C:
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
		}()
		defer close(stopReport)
	}

	// Counting reader updates "copied" atomically
	cr := &countReader{
		r:      in,
		ctx:    ctx,
		copied: &copied,
	}

	_, copyErr := io.Copy(out, cr)
	syncErr := out.Sync()
	closeErr := out.Close()

	if copyErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("copy file: %w", copyErr)
	}
	if syncErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("sync temp file: %w", syncErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close temp file: %w", closeErr)
	}

	// Set final file permissions before atomic rename.
	_ = os.Chmod(tmp, 0o644)

	// Atomic finalize on destination filesystem
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
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

	// Structured completion callback
	if t.opts.Reporter != nil && t.opts.PrintDone {
		t.opts.Reporter.Done(Snapshot{
			Name:     filepath.Base(dst),
			Copied:   total,
			Total:    total,
			RateMBps: 0,
			Elapsed:  time.Since(start),
		})
	}

	// Optional final completion line
	if t.opts.Progress != nil && t.opts.PrintDone {
		base := filepath.Base(dst)
		elapsed := time.Since(start).Round(time.Second)
		if total > 0 {
			t.opts.Progress(fmt.Sprintf("COPY DONE: %s (%s) in %s", base, humanBytes(total), elapsed))
		} else {
			t.opts.Progress(fmt.Sprintf("COPY DONE: %s in %s", base, elapsed))
		}
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

func humanBytes(n int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
		TB = 1024 * GB
	)
	switch {
	case n >= TB:
		return fmt.Sprintf("%.2f TB", float64(n)/float64(TB))
	case n >= GB:
		return fmt.Sprintf("%.2f GB", float64(n)/float64(GB))
	case n >= MB:
		return fmt.Sprintf("%.2f MB", float64(n)/float64(MB))
	case n >= KB:
		return fmt.Sprintf("%.2f KB", float64(n)/float64(KB))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
