package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Mtn-Man/mintmedia/internal/clipboard"
	"github.com/Mtn-Man/mintmedia/internal/logging"
	"github.com/Mtn-Man/mintmedia/internal/notify"
	"github.com/Mtn-Man/mintmedia/internal/processor"
	"github.com/Mtn-Man/mintmedia/internal/shutdown"
	"github.com/Mtn-Man/mintmedia/internal/transmission"
	"github.com/Mtn-Man/mintmedia/internal/watch"
)

var ErrShutdownTimedOut = errors.New("daemon shutdown timed out")

type caffeinateController interface {
	Start(context.Context) error
	Stop() error
}

var newDaemonCaffeinate = func() caffeinateController {
	return notify.NewCaffeinate()
}

// Daemon wires together the watcher + clipboard poller + processor + optional Transmission client.
type Daemon struct {
	Watcher *watch.DropFolderWatcher
	// Optional: if nil, clipboard polling is disabled.
	Poller *clipboard.Poller
	Proc   processor.Processor

	// Optional: if nil, magnets are logged only.
	Tx *transmission.Client

	// Optional: unified operational logger.
	Logger logging.Logger

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
	SoundDone  string // played after successful APPLIED processing based on DoneNotificationMode
	// done notification policy: per_file | per_job | off
	DoneNotificationMode string

	// Time to wait for in-flight processing jobs to finish after shutdown is requested.
	ShutdownGraceDuration time.Duration

	// Additional time to wait after force-canceling in-flight jobs.
	ShutdownForceTimeout time.Duration

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
	// internal: current number of in-flight media processing jobs
	jobsInFlight int64

	// internal: tracks in-flight paths to suppress duplicate processing
	inFlightMu sync.Mutex
	inFlight   map[string]struct{}

	// internal test seam; defaults to notify.PlaySound when nil.
	playSoundFn func(context.Context, string) error
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
	caffCtx, cancelCaff := context.WithCancel(context.Background())
	caff := newDaemonCaffeinate()
	if err := caff.Start(caffCtx); err != nil {
		fmt.Fprintf(os.Stderr, "caffeinate warning: %v\n", err)
	}
	defer func() {
		cancelCaff()
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
	if strings.TrimSpace(d.DoneNotificationMode) == "" {
		d.DoneNotificationMode = notify.DoneNotificationPerFile
	}
	policy := shutdown.ResolvePolicy(d.ShutdownGraceDuration, d.ShutdownForceTimeout)
	d.ShutdownGraceDuration = policy.Grace
	d.ShutdownForceTimeout = policy.Force

	// jobsCtx is intentionally independent from the run context so shutdown can
	// first attempt a graceful drain, then force-cancel only if grace expires.
	jobsCtx, cancelJobs := context.WithCancel(context.Background())
	defer cancelJobs()

	d.inFlightMu.Lock()
	d.inFlight = make(map[string]struct{})
	d.inFlightMu.Unlock()

	// Start watcher + poller (safe to call even if already running in your design).
	if err := d.Watcher.Start(ctx); err != nil {
		return fmt.Errorf("start watcher: %w", err)
	}
	defer func() {
		if err := d.Watcher.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "watcher close warning: %v\n", err)
		}
	}()

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
	d.logHistoryInfo(logging.EventSystemStartup, logging.Fields{
		"mode": "daemon",
	})

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
			d.logHistoryInfo(logging.EventSystemDestinationsReady, logging.Fields{
				"pending": len(pending),
			})
			for pth := range pending {
				delete(pending, pth)
				key := d.inFlightKey(pth)
				if key == "" {
					return fmt.Errorf("empty in-flight key for path: %s", pth)
				}
				if !d.tryMarkInFlight(key) {
					fmt.Fprintf(os.Stderr, "WARN: suppressed duplicate path already in-flight: %s\n", pth)
					d.logHistoryWarn(logging.EventDaemonPathDuplicate, nil, logging.Fields{"path": pth})
					continue
				}
				d.jobsWg.Add(1)
				atomic.AddInt64(&d.jobsInFlight, 1)
				go d.processPathAsync(ctx, jobsCtx, sem, pth, key)
			}

		// --- Watcher errors ---
		case err, ok := <-d.Watcher.Errors():
			if !ok {
				return nil
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "watch error: %v\n", err)
				d.logHistoryWarn(logging.EventDaemonWatchError, err, nil)
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
					d.logHistoryWarn(logging.EventDaemonPathDuplicate, nil, logging.Fields{"path": path})
					continue
				}
				pending[path] = time.Now()
				if lastWaitLog.IsZero() || time.Since(lastWaitLog) > time.Minute {
					fmt.Fprintf(os.Stderr, "Destinations not available/writable; waiting (pending=%d)\n", len(pending))
					d.logHistoryInfo(logging.EventSystemDestinationsWaiting, logging.Fields{
						"pending": len(pending),
					})
					lastWaitLog = time.Now()
				}
				continue
			}

			if !d.tryMarkInFlight(key) {
				fmt.Fprintf(os.Stderr, "WARN: suppressed duplicate path already in-flight: %s\n", path)
				d.logHistoryWarn(logging.EventDaemonPathDuplicate, nil, logging.Fields{"path": path})
				continue
			}
			d.jobsWg.Add(1)
			atomic.AddInt64(&d.jobsInFlight, 1)
			go d.processPathAsync(ctx, jobsCtx, sem, path, key)

		// --- Clipboard errors ---
		case err, ok := <-pollerErrors:
			if !ok {
				// poller shuts down with ctx; watcher may still be running
				continue
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "clipboard error: %v\n", err)
				d.logHistoryWarn(logging.EventDaemonClipboardError, err, nil)
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
					d.logHistoryWarn(logging.EventDaemonTxAddError, err, logging.Fields{
						"btih": btihShort,
					})
					return
				}

				fmt.Printf("TRANSMISSION: added (btih=%s)\n", btihShort)
				if strings.TrimSpace(d.TransmissionHost) != "" {
					fmt.Printf("Track progress here: http://%s/transmission/web/\n", d.TransmissionHost)
				}
				base := context.WithoutCancel(ctx)
				_ = notify.PlaySound(base, d.SoundInput)
				d.logHistoryInfo(logging.EventDaemonMagnetAdded, logging.Fields{
					"btih": btihShort,
					"dn":   dn,
				})
			}(magnet, btih, dn)
		}
	}

	// Drain: wait for in-flight processing jobs with a bounded graceful shutdown.
	drain := shutdown.Drain(
		shutdown.Policy{
			Grace: d.ShutdownGraceDuration,
			Force: d.ShutdownForceTimeout,
		},
		atomic.LoadInt64(&d.jobsInFlight) > 0,
		func(timeout time.Duration) bool {
			done := make(chan struct{})
			go func() {
				defer close(done)
				d.jobsWg.Wait()
			}()

			if timeout <= 0 {
				<-done
				return true
			}

			timer := time.NewTimer(timeout)
			defer timer.Stop()

			select {
			case <-done:
				return true
			case <-timer.C:
				return false
			}
		},
		cancelJobs,
		shutdown.Hooks{
			OnWaitStart: func(grace time.Duration) {
				fmt.Fprintf(
					os.Stderr,
					"\nShutdown requested; waiting up to %s for in-flight jobs\n",
					shutdown.FormatDurationCompact(grace),
				)
				d.logHistoryInfo(logging.EventSystemShutdownRequested, logging.Fields{
					"grace": shutdown.FormatDurationCompact(grace),
				})
			},
			OnGraceElapsed: func(force time.Duration) {
				fmt.Fprintf(
					os.Stderr,
					"Shutdown grace elapsed; canceling in-flight jobs (timeout=%s)\n",
					shutdown.FormatDurationCompact(force),
				)
				d.logHistoryWarn(logging.EventSystemShutdownGraceElapsed, nil, logging.Fields{
					"force": shutdown.FormatDurationCompact(force),
				})
			},
		},
	)
	if !drain.TimedOut {
		fmt.Println()
		fmt.Println("Shutdown complete.")
		d.logHistoryInfo(logging.EventSystemShutdownComplete, nil)
		return nil
	}
	d.logHistoryError(logging.EventSystemShutdownTimeout, ErrShutdownTimedOut, logging.Fields{
		"grace": shutdown.FormatDurationCompact(d.ShutdownGraceDuration),
		"force": shutdown.FormatDurationCompact(d.ShutdownForceTimeout),
	})

	return fmt.Errorf(
		"%w (grace=%s force=%s)",
		ErrShutdownTimedOut,
		shutdown.FormatDurationCompact(d.ShutdownGraceDuration),
		shutdown.FormatDurationCompact(d.ShutdownForceTimeout),
	)
}

func (d *Daemon) processPathAsync(runCtx context.Context, procCtx context.Context, sem chan struct{}, pth string, inFlightKey string) {
	defer d.jobsWg.Done()
	defer atomic.AddInt64(&d.jobsInFlight, -1)
	defer d.clearInFlight(inFlightKey)

	// Acquire a processing slot without blocking the main event loop.
	select {
	case sem <- struct{}{}:
		defer func() { <-sem }()
	case <-runCtx.Done():
		return
	}

	start := time.Now()
	jobCtx := context.WithoutCancel(runCtx)
	planner := notify.NewDoneSoundPlanner(d.DoneNotificationMode)
	streamed := false
	emit := func(r processor.Result, dur time.Duration) {
		if r.Applied {
			fmt.Printf("APPLIED:\n")
			fmt.Printf("  Source:   %s\n", pth)
			fmt.Printf("  Dest:     %s\n", r.Plan.DestMainPath)
			fmt.Printf("  Duration: %s\n", dur)
			playCount := planner.OnAppliedMain()
			for i := 0; i < playCount; i++ {
				d.playSoundAsync(jobCtx, d.SoundDone)
			}
			return
		}
		if r.Reason == processor.ErrNotMedia.Error() ||
			r.Reason == processor.ErrNoMainMediaFound.Error() ||
			r.Reason == processor.ErrInputMissing.Error() {
			return
		}
		fmt.Printf("IGNORED (%s): %s (duration=%s)\n", pth, r.Reason, dur)
	}
	req := processor.Request{
		InputPath: pth,
		OnResult: func(r processor.Result) {
			streamed = true
			emit(r, time.Since(start).Round(time.Second))
		},
	}
	results, err := d.Proc.Process(procCtx, req)

	dur := time.Since(start).Round(time.Second)

	if err != nil {
		fmt.Fprintf(os.Stderr, "PROCESS ERROR (%s): %v (duration=%s)\n", pth, err, dur)
		d.logHistoryError(logging.EventDaemonProcessError, err, logging.Fields{
			"path":     pth,
			"duration": dur.String(),
		})
		return
	}

	if !streamed {
		for _, r := range results {
			emit(r, dur)
		}
	}

	playCount := planner.OnJobComplete()
	for i := 0; i < playCount; i++ {
		d.playSoundAsync(jobCtx, d.SoundDone)
	}

	if planner.HasAppliedMain() {
		d.cleanupCompletedTorrents(jobCtx)
	}
}

func (d *Daemon) playSoundAsync(ctx context.Context, soundPath string) {
	soundPath = strings.TrimSpace(soundPath)
	if soundPath == "" {
		return
	}
	play := d.playSoundFn
	if play == nil {
		play = notify.PlaySound
	}
	base := context.WithoutCancel(ctx)
	go func() { _ = play(base, soundPath) }()
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
		d.logHistoryWarn(logging.EventDaemonTxCleanupError, err, nil)
		return
	}
	if removed > 0 {
		fmt.Printf("TRANSMISSION: removed %d completed torrent(s)\n", removed)
		d.logHistoryInfo(logging.EventDaemonTxCleanupRemoved, logging.Fields{
			"removed": removed,
		})
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

func (d *Daemon) logHistoryInfo(event logging.Event, fields logging.Fields) {
	if d == nil || d.Logger == nil {
		return
	}
	d.Logger.Log(logging.Entry{
		Level:     logging.LevelInfo,
		Component: componentForEvent(event),
		Event:     event,
		Fields:    fields,
		ToConsole: logging.BoolPtr(false),
	})
}

func (d *Daemon) logHistoryWarn(event logging.Event, err error, fields logging.Fields) {
	if d == nil || d.Logger == nil {
		return
	}
	d.Logger.Log(logging.Entry{
		Level:     logging.LevelWarn,
		Component: componentForEvent(event),
		Event:     event,
		Fields:    fields,
		Err:       logging.ErrorFieldFrom(err),
		ToConsole: logging.BoolPtr(false),
	})
}

func (d *Daemon) logHistoryError(event logging.Event, err error, fields logging.Fields) {
	if d == nil || d.Logger == nil {
		return
	}
	d.Logger.Log(logging.Entry{
		Level:     logging.LevelError,
		Component: componentForEvent(event),
		Event:     event,
		Fields:    fields,
		Err:       logging.ErrorFieldFrom(err),
		ToConsole: logging.BoolPtr(false),
	})
}

func componentForEvent(event logging.Event) string {
	if strings.HasPrefix(string(event), "system.") {
		return "system"
	}
	return "daemon"
}
