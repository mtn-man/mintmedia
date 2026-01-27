package daemon

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Mtn-Man/mintmedia/internal/clipboard"
	"github.com/Mtn-Man/mintmedia/internal/notify"
	"github.com/Mtn-Man/mintmedia/internal/processor"
	"github.com/Mtn-Man/mintmedia/internal/state"
	"github.com/Mtn-Man/mintmedia/internal/transmission"
	"github.com/Mtn-Man/mintmedia/internal/watch"
)

// Daemon wires together the watcher + clipboard poller + processor + optional Transmission client.
type Daemon struct {
	Watcher *watch.DropFolderWatcher
	// Optional: if nil, clipboard polling is disabled.
	Poller *clipboard.Poller
	Proc   processor.Processor

	// Optional: if nil, magnets are logged only.
	Tx *transmission.Client

	// Optional: unified history log. If nil, daemon will not record magnet/tx cleanup events.
	History state.History

	// Host used for "Track progress here" line (e.g., "localhost:9091").
	TransmissionHost string

	// Destination directories (resolved absolute paths)
	MoviesDir string
	ShowsDir  string

	// If true, daemon will defer processing until destination
	// directories exist and are writable.
	DeferDestinationChecks bool

	// Max concurrent media processing jobs.
	MaxConcurrent int

	// Sounds (best-effort; empty disables)
	SoundInput string // played on successful Transmission add
	SoundDone  string // played on successful APPLIED processing

	// Transmission add timeout
	MagnetTimeout time.Duration

	// If true, prints full magnet lines; if false, prints summary.
	VerboseMagnets bool

	// If true, after any successful APPLIED processing, attempt to remove all completed torrents from Transmission.
	AutoCleanupCompletedTorrents bool

	// Cooldown between Transmission cleanup attempts. If <= 0, defaults to 120s.
	CleanupCooldown time.Duration

	// internal: last cleanup time (guarded by cleanupMu)
	cleanupMu     sync.Mutex
	lastCleanupAt time.Time

	// internal: tracks in-flight media processing jobs so we can drain on shutdown
	jobsWg sync.WaitGroup

	// internal: tracks in-flight paths to suppress duplicate processing
	inFlightMu sync.Mutex
	inFlight   map[string]struct{}
}

// Run starts the daemon loop. The caller is responsible for creating and starting the Watcher and Poller.
// However, for convenience and symmetry with the current approach, Run() will Start() them if not started yet.
//
// Recommended usage from main:
//
//	w := watch.NewDropFolderWatcher(...)
//	p := clipboard.NewPoller(...)
//	d := &daemon.Daemon{Watcher:w, Poller:p, Proc:proc, Tx:tx, ...}
//	return d.Run(ctx)
func (d *Daemon) Run(ctx context.Context) error {
	if d.Watcher == nil {
		return fmt.Errorf("daemon: Watcher is nil")
	}
	if d.Proc == nil {
		return fmt.Errorf("daemon: Proc is nil")
	}

	// Prevent macOS idle sleep for the lifetime of the daemon (best-effort).
	caff := notify.NewCaffeinate()
	if err := caff.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "caffeinate warning: %v\n", err)
	}
	defer func() {
		if err := caff.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "caffeinate stop warning: %v\n", err)
		}
	}()

	// Defaults
	if d.MaxConcurrent < 1 {
		d.MaxConcurrent = 1
	}
	if d.MagnetTimeout <= 0 {
		d.MagnetTimeout = 10 * time.Second
	}
	if d.CleanupCooldown <= 0 {
		d.CleanupCooldown = 120 * time.Second
	}

	d.inFlightMu.Lock()
	d.inFlight = make(map[string]struct{})
	d.inFlightMu.Unlock()

	// Start watcher + poller (safe to call even if already running in your design).
	if err := d.Watcher.Start(ctx); err != nil {
		return fmt.Errorf("start watcher: %w", err)
	}
	defer d.Watcher.Close()

	var pollerEvents <-chan string
	var pollerErrors <-chan error
	if d.Poller != nil {
		d.Poller.Start(ctx)
		pollerEvents = d.Poller.Events()
		pollerErrors = d.Poller.Errors()
	}

	sem := make(chan struct{}, d.MaxConcurrent)

	fmt.Println("Mintmedia daemon started.")
	switch {
	case d.Poller == nil:
		fmt.Println("Clipboard polling disabled.")
	case d.Tx != nil:
		fmt.Println("Polling clipboard for magnet links (Transmission enabled).")
	default:
		fmt.Println("Polling clipboard for magnet links (logging only).")
	}
	fmt.Println("Press Ctrl+C to stop.")

	pending := make(map[string]time.Time)
	var lastWaitLog time.Time

	retryTick := time.NewTicker(5 * time.Second)
	defer retryTick.Stop()

runLoop:
	for {
		select {
		case <-ctx.Done():
			// Stop accepting new work; exit the event loop and drain below.
			break runLoop

		case <-retryTick.C:
			if len(pending) == 0 {
				continue
			}
			if !d.DeferDestinationChecks || !d.destinationsReady() {
				continue
			}

			fmt.Fprintf(os.Stderr, "Destinations ready; processing %d pending item(s)\n", len(pending))
			for pth := range pending {
				delete(pending, pth)
				key := d.inFlightKey(pth)
				if key == "" {
					return fmt.Errorf("empty in-flight key for path: %s", pth)
				}
				if !d.tryMarkInFlight(key) {
					fmt.Fprintf(os.Stderr, "WARN: suppressed duplicate path already in-flight: %s\n", pth)
					continue
				}
				d.jobsWg.Add(1)
				go d.processPathAsync(ctx, sem, pth, key)
			}

		// --- Watcher errors ---
		case err, ok := <-d.Watcher.Errors():
			if !ok {
				return nil
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "watch error: %v\n", err)
			}

		// --- Stable filesystem events ---
		case path, ok := <-d.Watcher.Events():
			if !ok {
				return nil
			}
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			path = filepath.Clean(path)
			key := d.inFlightKey(path)
			if key == "" {
				return fmt.Errorf("empty in-flight key for path: %s", path)
			}

			if d.DeferDestinationChecks && !d.destinationsReady() {
				if d.isInFlight(key) {
					fmt.Fprintf(os.Stderr, "WARN: suppressed duplicate path already in-flight: %s\n", path)
					continue
				}
				pending[path] = time.Now()
				if lastWaitLog.IsZero() || time.Since(lastWaitLog) > time.Minute {
					fmt.Fprintf(os.Stderr, "Destinations not available/writable; waiting (pending=%d)\n", len(pending))
					lastWaitLog = time.Now()
				}
				continue
			}

			if !d.tryMarkInFlight(key) {
				fmt.Fprintf(os.Stderr, "WARN: suppressed duplicate path already in-flight: %s\n", path)
				continue
			}
			d.jobsWg.Add(1)
			go d.processPathAsync(ctx, sem, path, key)

		// --- Clipboard errors ---
		case err, ok := <-pollerErrors:
			if !ok {
				// poller shuts down with ctx; watcher may still be running
				continue
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "clipboard error: %v\n", err)
			}

		// --- Clipboard magnet events ---
		case magnet, ok := <-pollerEvents:
			if !ok {
				continue
			}
			magnet = strings.TrimSpace(magnet)
			if magnet == "" {
				continue
			}

			btih, dn, tr, okMag := magnetSummary(magnet)
			if !okMag {
				// Not a valid magnet URI; ignore silently.
				continue
			}
			if dn == "" {
				dn = "(no dn)"
			}

			if d.VerboseMagnets {
				fmt.Printf("MAGNET: %s\n", magnet)
			} else {
				fmt.Printf("MAGNET: btih=%s dn=%q tr=%d\n", btih, truncateForLog(dn, 80), tr)
			}

			// If Transmission not enabled, just log.
			if d.Tx == nil {
				continue
			}

			// Non-blocking add
			go func(m string, btihShort string, dn string) {
				tctx, cancel := context.WithTimeout(ctx, d.MagnetTimeout)
				defer cancel()

				if err := d.Tx.AddMagnet(tctx, m); err != nil {
					fmt.Fprintf(os.Stderr, "TRANSMISSION ERROR: %v\n", err)
					return
				}

				fmt.Printf("TRANSMISSION: added (btih=%s)\n", btihShort)
				if strings.TrimSpace(d.TransmissionHost) != "" {
					fmt.Printf("Track progress here: http://%s/transmission/web/\n", d.TransmissionHost)
				}
				base := context.WithoutCancel(ctx)
				_ = notify.PlaySound(base, d.SoundInput)
				if d.History != nil {
					ev := state.MagnetAdded(btihShort, dn)
					if err := d.History.Record(context.WithoutCancel(ctx), ev); err != nil {
						fmt.Fprintf(os.Stderr, "HISTORY ERROR: %v\n", err)
					}
				}
			}(magnet, btih, dn)
		}
	}

	// Drain: wait for in-flight processing jobs to finish.
	d.jobsWg.Wait()
	fmt.Println()
	fmt.Println("Shutdown complete.")
	return nil
}

func (d *Daemon) processPathAsync(ctx context.Context, sem chan struct{}, pth string, inFlightKey string) {
	defer d.jobsWg.Done()
	defer d.clearInFlight(inFlightKey)

	// Acquire a processing slot without blocking the main event loop.
	select {
	case sem <- struct{}{}:
		defer func() { <-sem }()
	case <-ctx.Done():
		return
	}

	start := time.Now()
	// Use a background context so in-flight transfers finish even after shutdown is requested.
	results, err := d.Proc.Process(context.Background(), processor.Request{InputPath: pth})

	dur := time.Since(start).Round(time.Second)

	if err != nil {
		fmt.Fprintf(os.Stderr, "PROCESS ERROR (%s): %v (duration=%s)\n", pth, err, dur)
		return
	}

	var anyApplied bool
	for _, r := range results {
		if r.Applied {
			anyApplied = true
			fmt.Printf("APPLIED:\n")
			fmt.Printf("  Source:   %s\n", pth)
			fmt.Printf("  Dest:     %s\n", r.Plan.DestMainPath)
			fmt.Printf("  Duration: %s\n", dur)
			continue
		}
		if r.Reason == processor.ErrNotMedia.Error() ||
			r.Reason == processor.ErrNoMainMediaFound.Error() ||
			r.Reason == processor.ErrInputMissing.Error() {
			continue
		}
		fmt.Printf("IGNORED (%s): %s (duration=%s)\n", pth, r.Reason, dur)
	}

	if anyApplied {
		jobCtx := context.WithoutCancel(ctx)
		d.playSoundAsync(jobCtx, d.SoundDone)
		d.cleanupCompletedTorrents(jobCtx)
	}
}

func (d *Daemon) playSoundAsync(ctx context.Context, soundPath string) {
	soundPath = strings.TrimSpace(soundPath)
	if soundPath == "" {
		return
	}
	base := context.WithoutCancel(ctx)
	go func() { _ = notify.PlaySound(base, soundPath) }()
}

func (d *Daemon) cleanupCompletedTorrents(ctx context.Context) {
	// Only run when Transmission is enabled and the feature is turned on.
	if d.Tx == nil || !d.AutoCleanupCompletedTorrents {
		return
	}

	// Cooldown gate
	now := time.Now()
	d.cleanupMu.Lock()
	if !d.lastCleanupAt.IsZero() && now.Sub(d.lastCleanupAt) < d.CleanupCooldown {
		d.cleanupMu.Unlock()
		return
	}
	d.lastCleanupAt = now
	d.cleanupMu.Unlock()

	// Bound cleanup time so it can't hang forever, but do not cancel on shutdown.
	base := context.WithoutCancel(ctx)
	tctx, cancel := context.WithTimeout(base, 30*time.Second)
	defer cancel()

	removed, err := d.Tx.RemoveCompleted(tctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "TRANSMISSION CLEANUP ERROR: %v\n", err)
		return
	}
	if removed > 0 {
		fmt.Printf("TRANSMISSION: removed %d completed torrent(s)\n", removed)
		if d.History != nil {
			if err := d.History.Record(context.WithoutCancel(ctx), state.TransmissionCleanup(removed)); err != nil {
				fmt.Fprintf(os.Stderr, "HISTORY ERROR: %v\n", err)
			}
		}
	}
}

func (d *Daemon) tryMarkInFlight(path string) bool {
	d.inFlightMu.Lock()
	defer d.inFlightMu.Unlock()
	if d.inFlight == nil {
		d.inFlight = make(map[string]struct{})
	}
	if _, ok := d.inFlight[path]; ok {
		return false
	}
	d.inFlight[path] = struct{}{}
	return true
}

func (d *Daemon) isInFlight(path string) bool {
	d.inFlightMu.Lock()
	defer d.inFlightMu.Unlock()
	if d.inFlight == nil {
		return false
	}
	_, ok := d.inFlight[path]
	return ok
}

func (d *Daemon) clearInFlight(path string) {
	d.inFlightMu.Lock()
	defer d.inFlightMu.Unlock()
	if d.inFlight == nil {
		return
	}
	delete(d.inFlight, path)
}

func (d *Daemon) inFlightKey(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = filepath.Clean(path)
	if realPath, err := filepath.EvalSymlinks(path); err == nil {
		path = filepath.Clean(realPath)
	}
	if isCaseInsensitiveFS() {
		path = strings.ToLower(path)
	}
	return path
}

func isCaseInsensitiveFS() bool {
	switch runtime.GOOS {
	case "darwin", "windows":
		return true
	default:
		return false
	}
}

// destinationsReady returns true when both destination directories are present and writable.
func (d *Daemon) destinationsReady() bool {
	if strings.TrimSpace(d.MoviesDir) == "" || strings.TrimSpace(d.ShowsDir) == "" {
		return false
	}
	return dirWritable(d.MoviesDir) && dirWritable(d.ShowsDir)
}

func dirWritable(dir string) bool {
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return false
	}
	f, err := os.CreateTemp(dir, ".mintmedia-writetest-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

// --- Magnet formatting helpers ---------------------------------------------

func magnetSummary(m string) (btihShort string, dn string, trackers int, ok bool) {
	m = strings.TrimSpace(m)
	if m == "" {
		return "", "", 0, false
	}

	u, err := url.Parse(m)
	if err != nil {
		return "", "", 0, false
	}
	if strings.ToLower(u.Scheme) != "magnet" {
		return "", "", 0, false
	}

	q := u.Query()

	xt := q.Get("xt")
	const prefix = "urn:btih:"
	if !strings.HasPrefix(xt, prefix) {
		return "", "", 0, false
	}
	h := strings.TrimSpace(strings.TrimPrefix(xt, prefix))
	// Require at least a small hash fragment; full hashes are typically 40 (hex) or 32 (base32)
	if len(h) < 8 {
		return "", "", 0, false
	}

	dn = q.Get("dn")
	trackers = len(q["tr"])

	if len(h) > 12 {
		btihShort = h[:12]
	} else {
		btihShort = h
	}

	return btihShort, dn, trackers, true
}

func truncateForLog(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 {
		return s
	}
	rs := []rune(s)
	if len(rs) <= max {
		return s
	}
	if max <= 3 {
		return string(rs[:max])
	}
	return string(rs[:max-3]) + "..."
}
