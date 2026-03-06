package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Mtn-Man/mintmedia/internal/clipboard"
	"github.com/Mtn-Man/mintmedia/internal/logging"
	"github.com/Mtn-Man/mintmedia/internal/magnet"
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
		d.logConsoleWarn(logging.EventSystemStartup, fmt.Sprintf("caffeinate warning: %v", err), err, nil)
	}
	defer func() {
		cancelCaff()
		if err := caff.Stop(); err != nil {
			d.logConsoleWarn(logging.EventSystemShutdownComplete, fmt.Sprintf("caffeinate stop warning: %v", err), err, nil)
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
			d.logConsoleWarn(logging.EventSystemShutdownComplete, fmt.Sprintf("watcher close warning: %v", err), err, nil)
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

	d.logConsoleInfo(logging.EventSystemStartup, "Mintmedia daemon started.", nil)
	switch {
	case d.Poller == nil:
		d.logConsoleInfo(logging.EventSystemStartup, "Clipboard polling disabled.", nil)
	case d.Tx != nil:
		d.logConsoleInfo(logging.EventSystemStartup, "Polling clipboard for magnet links (Transmission enabled).", nil)
	default:
		d.logConsoleInfo(logging.EventSystemStartup, "Polling clipboard for magnet links (logging only).", nil)
	}
	d.logConsoleInfo(logging.EventSystemStartup, "Press Ctrl+C to stop.", nil)
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

			d.logConsoleWarn(
				logging.EventSystemDestinationsReady,
				fmt.Sprintf("Destinations ready; processing %d pending item(s)", len(pending)),
				nil,
				logging.Fields{"pending": len(pending)},
			)
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
					d.logConsoleWarn(
						logging.EventDaemonPathDuplicate,
						fmt.Sprintf("WARN: suppressed duplicate path already in-flight: %s", pth),
						nil,
						logging.Fields{"path": pth},
					)
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
				d.logConsoleWarn(logging.EventDaemonWatchError, fmt.Sprintf("watch error: %v", err), err, nil)
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
					d.logConsoleWarn(
						logging.EventDaemonPathDuplicate,
						fmt.Sprintf("WARN: suppressed duplicate path already in-flight: %s", path),
						nil,
						logging.Fields{"path": path},
					)
					d.logHistoryWarn(logging.EventDaemonPathDuplicate, nil, logging.Fields{"path": path})
					continue
				}
				pending[path] = time.Now()
				if lastWaitLog.IsZero() || time.Since(lastWaitLog) > time.Minute {
					d.logConsoleWarn(
						logging.EventSystemDestinationsWaiting,
						fmt.Sprintf("Destinations not available/writable; waiting (pending=%d)", len(pending)),
						nil,
						logging.Fields{"pending": len(pending)},
					)
					d.logHistoryInfo(logging.EventSystemDestinationsWaiting, logging.Fields{
						"pending": len(pending),
					})
					lastWaitLog = time.Now()
				}
				continue
			}

			if !d.tryMarkInFlight(key) {
				d.logConsoleWarn(
					logging.EventDaemonPathDuplicate,
					fmt.Sprintf("WARN: suppressed duplicate path already in-flight: %s", path),
					nil,
					logging.Fields{"path": path},
				)
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
				d.logConsoleWarn(logging.EventDaemonClipboardError, fmt.Sprintf("clipboard error: %v", err), err, nil)
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

			d.logConsoleInfo(
				logging.EventDaemonMagnetAdded,
				fmt.Sprintf("MAGNET: btih=%s dn=%q tr=%d", btih, truncateForLog(dn, 80), tr),
				logging.Fields{"btih": btih, "dn": dn, "trackers": tr},
			)

			// If Transmission not enabled, just log.
			if d.Tx == nil {
				continue
			}

			// Non-blocking add
			go func(m string, btihShort string, dn string) {
				tctx, cancel := context.WithTimeout(ctx, d.MagnetTimeout)
				defer cancel()

				if err := d.Tx.AddMagnet(tctx, m); err != nil {
					d.logConsoleWarn(logging.EventDaemonTxAddError, fmt.Sprintf("TRANSMISSION ERROR: %v", err), err, logging.Fields{
						"btih": btihShort,
					})
					d.logHistoryWarn(logging.EventDaemonTxAddError, err, logging.Fields{
						"btih": btihShort,
					})
					return
				}

				d.logConsoleInfo(
					logging.EventDaemonMagnetAdded,
					fmt.Sprintf("TRANSMISSION: added (btih=%s)", btihShort),
					logging.Fields{"btih": btihShort, "dn": dn},
				)
				if strings.TrimSpace(d.TransmissionHost) != "" {
					d.logConsoleInfo(
						logging.EventDaemonMagnetAdded,
						fmt.Sprintf("Track progress here: http://%s/transmission/web/", d.TransmissionHost),
						logging.Fields{"host": d.TransmissionHost},
					)
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
				d.logConsoleWarn(
					logging.EventSystemShutdownRequested,
					fmt.Sprintf(
						"\nShutdown requested; waiting up to %s for in-flight jobs",
						shutdown.FormatDurationCompact(grace),
					),
					nil,
					logging.Fields{"grace": shutdown.FormatDurationCompact(grace)},
				)
				d.logHistoryInfo(logging.EventSystemShutdownRequested, logging.Fields{
					"grace": shutdown.FormatDurationCompact(grace),
				})
			},
			OnGraceElapsed: func(force time.Duration) {
				d.logConsoleWarn(
					logging.EventSystemShutdownGraceElapsed,
					fmt.Sprintf(
						"Shutdown grace elapsed; canceling in-flight jobs (timeout=%s)",
						shutdown.FormatDurationCompact(force),
					),
					nil,
					logging.Fields{"force": shutdown.FormatDurationCompact(force)},
				)
				d.logHistoryWarn(logging.EventSystemShutdownGraceElapsed, nil, logging.Fields{
					"force": shutdown.FormatDurationCompact(force),
				})
			},
		},
	)
	if !drain.TimedOut {
		d.logConsoleInfo(logging.EventSystemShutdownComplete, "\nShutdown complete.", nil)
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
			d.logConsoleInfo(
				logging.EventProcessorMoveMainApplied,
				fmt.Sprintf("APPLIED:\n  Source:   %s\n  Dest:     %s\n  Duration: %s", pth, r.Plan.DestMainPath, dur),
				logging.Fields{"path": pth, "dest_path": r.Plan.DestMainPath, "duration": dur.String()},
			)
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
		d.logConsoleInfo(
			logging.EventProcessorInputSkippedParseError,
			fmt.Sprintf("IGNORED (%s): %s (duration=%s)", pth, r.Reason, dur),
			logging.Fields{"path": pth, "reason": r.Reason, "duration": dur.String()},
		)
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
		d.logConsoleError(
			logging.EventDaemonProcessError,
			fmt.Sprintf("PROCESS ERROR (%s): %v (duration=%s)", pth, err, dur),
			err,
			logging.Fields{"path": pth, "duration": dur.String()},
		)
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

	// RemoveCompleted obeys caller context. Detach from shutdown cancellation,
	// but keep a hard timeout so cleanup cannot hang indefinitely.
	base := context.WithoutCancel(ctx)
	tctx, cancel := context.WithTimeout(base, 30*time.Second)
	defer cancel()

	removed, err := d.Tx.RemoveCompleted(tctx)
	if err != nil {
		d.logConsoleWarn(logging.EventDaemonTxCleanupError, fmt.Sprintf("TRANSMISSION CLEANUP ERROR: %v", err), err, nil)
		d.logHistoryWarn(logging.EventDaemonTxCleanupError, err, nil)
		return
	}
	if removed > 0 {
		d.logConsoleInfo(
			logging.EventDaemonTxCleanupRemoved,
			fmt.Sprintf("TRANSMISSION: removed %d completed torrent(s)", removed),
			logging.Fields{"removed": removed},
		)
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
	info, err := magnet.Parse(m)
	if err != nil {
		return "", "", 0, false
	}
	return magnet.ShortBTIH(info.BTIH, 12), info.DN, info.Trackers, true
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
	d.Logger.HistoryInfo(componentForEvent(event), event, fields)
}

func (d *Daemon) logConsoleInfo(event logging.Event, msg string, fields logging.Fields) {
	if d == nil || d.Logger == nil {
		return
	}
	d.Logger.ConsoleInfo(componentForEvent(event), event, msg, fields)
}

func (d *Daemon) logHistoryWarn(event logging.Event, err error, fields logging.Fields) {
	if d == nil || d.Logger == nil {
		return
	}
	d.Logger.HistoryWarn(componentForEvent(event), event, err, fields)
}

func (d *Daemon) logConsoleWarn(event logging.Event, msg string, err error, fields logging.Fields) {
	if d == nil || d.Logger == nil {
		return
	}
	d.Logger.ConsoleWarn(componentForEvent(event), event, msg, err, fields)
}

func (d *Daemon) logHistoryError(event logging.Event, err error, fields logging.Fields) {
	if d == nil || d.Logger == nil {
		return
	}
	d.Logger.HistoryError(componentForEvent(event), event, err, fields)
}

func (d *Daemon) logConsoleError(event logging.Event, msg string, err error, fields logging.Fields) {
	if d == nil || d.Logger == nil {
		return
	}
	d.Logger.ConsoleError(componentForEvent(event), event, msg, err, fields)
}

func componentForEvent(event logging.Event) string {
	if strings.HasPrefix(string(event), "system.") {
		return "system"
	}
	if strings.HasPrefix(string(event), "processor.") {
		return "processor"
	}
	return "daemon"
}
