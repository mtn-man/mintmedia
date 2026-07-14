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
	"time"

	"github.com/mtn-man/mintmedia/internal/clipboard"
	"github.com/mtn-man/mintmedia/internal/console"
	"github.com/mtn-man/mintmedia/internal/jobrunner"
	"github.com/mtn-man/mintmedia/internal/logging"
	"github.com/mtn-man/mintmedia/internal/magnet"
	"github.com/mtn-man/mintmedia/internal/notify"
	"github.com/mtn-man/mintmedia/internal/processor"
	"github.com/mtn-man/mintmedia/internal/shutdown"
	"github.com/mtn-man/mintmedia/internal/transmission"
	"github.com/mtn-man/mintmedia/internal/watch"
)

var ErrShutdownTimedOut = errors.New("daemon shutdown timed out")

var newDaemonCaffeinate = func() notify.CaffeinateController {
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

	// internal: tracks in-flight paths to suppress duplicate processing
	inFlightMu sync.Mutex
	inFlight   map[string]struct{}

	// internal test seam; defaults to notify.PlaySound when nil.
	playSoundFn func(context.Context, string) error

	// internal: ensures "Track progress here" is logged at most once per session.
	trackProgressOnce sync.Once
}

// workItem is a unit of work queued for the single processing worker.
type workItem struct {
	path        string
	inFlightKey string
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
		d.logConsoleWarn(logging.EventSystemStartup, fmt.Sprintf("WARNING  caffeinate: %v", err), err, nil)
	}
	defer func() {
		cancelCaff()
		if err := caff.Stop(); err != nil {
			d.logConsoleWarn(logging.EventSystemShutdownComplete, fmt.Sprintf("WARNING  caffeinate stop: %v", err), err, nil)
		}
	}()

	// Defaults
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

	hooks := shutdown.Hooks{
		OnWaitStart: func(grace time.Duration) {
			d.logConsoleWarn(
				logging.EventSystemShutdownRequested,
				fmt.Sprintf(
					"\nWARNING  shutdown requested. Waiting up to %s for in-flight jobs.",
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
					"WARNING  shutdown grace elapsed. Canceling in-flight jobs (timeout=%s).",
					shutdown.FormatDurationCompact(force),
				),
				nil,
				logging.Fields{"force": shutdown.FormatDurationCompact(force)},
			)
			d.logHistoryWarn(logging.EventSystemShutdownGraceElapsed, nil, logging.Fields{
				"force": shutdown.FormatDurationCompact(force),
			})
		},
	}

	d.inFlightMu.Lock()
	d.inFlight = make(map[string]struct{})
	d.inFlightMu.Unlock()

	// Wire media-aware ordering into the watcher's settle batch. The closure
	// captures ctx so sorting respects daemon shutdown cancellation.
	d.Watcher.SetSortFunc(func(paths []string) []string {
		sorted, errs, sortErr := processor.SortCandidates(ctx, d.Proc, paths)
		if sortErr != nil {
			return paths // context canceled; preserve original order
		}
		for _, se := range errs {
			d.logSortParseError(se.Path, se.Err)
		}
		return sorted
	})

	// Start watcher + poller (safe to call even if already running in your design).
	if err := d.Watcher.Start(ctx); err != nil {
		return fmt.Errorf("start watcher: %w", err)
	}
	defer func() {
		if err := d.Watcher.Close(); err != nil {
			d.logConsoleWarn(logging.EventSystemShutdownComplete, fmt.Sprintf("WARNING  watcher close: %v", err), err, nil)
		}
	}()

	var pollerEvents <-chan string
	var pollerErrors <-chan error
	if d.Poller != nil {
		d.Poller.Start(ctx)
		pollerEvents = d.Poller.Events()
		pollerErrors = d.Poller.Errors()
	}

	workQueue := make(chan workItem, 128)
	outcome := make(chan workerOutcome, 1)
	go d.runWorker(ctx, policy, hooks, workQueue, outcome)

	d.logConsoleInfo(logging.EventSystemStartup, "STARTED  mintmedia daemon", nil)
	switch {
	case d.Poller == nil:
		d.logConsoleInfo(logging.EventSystemStartup, "Clipboard polling disabled.", nil)
	case d.Tx != nil:
		d.logConsoleInfo(logging.EventSystemStartup, "Polling clipboard for magnet links (Transmission enabled).", nil)
	default:
		d.logConsoleInfo(logging.EventSystemStartup, "Polling clipboard for magnet links (logging only).", nil)
	}
	d.logConsoleInfo(logging.EventSystemStartup, "Press Ctrl+C to stop.\n", nil)
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

			pendingPaths := make([]string, 0, len(pending))
			for pth := range pending {
				pendingPaths = append(pendingPaths, pth)
			}
			sortedPaths, sortErrs, sortErr := processor.SortCandidates(ctx, d.Proc, pendingPaths)
			if sortErr != nil {
				sortedPaths = pendingPaths // context canceled; fall back to arbitrary order
			}
			for _, se := range sortErrs {
				// Leave parse-error paths in pending; they will be retried on the next tick.
				d.logSortParseError(se.Path, se.Err)
			}

			fileCount := 0
			for _, pth := range sortedPaths {
				if ctx.Err() != nil {
					break
				}
				plans, planErr := d.Proc.Plan(ctx, processor.Request{InputPath: pth})
				if planErr != nil {
					continue
				}
				fileCount += len(plans)
			}
			noun := "files"
			if fileCount == 1 {
				noun = "file"
			}
			d.logConsoleInfo(
				logging.EventSystemDestinationsReady,
				fmt.Sprintf("INFO     destinations ready; processing %d pending %s.", fileCount, noun),
				logging.Fields{"pending": fileCount},
			)
			d.logHistoryInfo(logging.EventSystemDestinationsReady, logging.Fields{
				"pending": fileCount,
			})
			for _, pth := range sortedPaths {
				delete(pending, pth)
				key := d.inFlightKey(pth)
				if key == "" {
					return fmt.Errorf("empty in-flight key for path: %s", pth)
				}
				if !d.tryMarkInFlight(key) {
					d.logConsoleWarn(
						logging.EventDaemonPathDuplicate,
						fmt.Sprintf("WARNING  already in-flight, skipping: %s", pth),
						nil,
						logging.Fields{"path": pth},
					)
					d.logHistoryWarn(logging.EventDaemonPathDuplicate, nil, logging.Fields{"path": pth})
					continue
				}
				select {
				case workQueue <- workItem{path: pth, inFlightKey: key}:
				case <-ctx.Done():
					d.clearInFlight(key)
				}
			}

		// --- Watcher errors ---
		case err, ok := <-d.Watcher.Errors():
			if !ok {
				return nil
			}
			if err != nil {
				d.logConsoleError(logging.EventDaemonWatchError, fmt.Sprintf("ERROR    watcher: %v", err), err, nil)
				d.logHistoryError(logging.EventDaemonWatchError, err, nil)
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
						fmt.Sprintf("WARNING  already in-flight, skipping: %s", path),
						nil,
						logging.Fields{"path": path},
					)
					d.logHistoryWarn(logging.EventDaemonPathDuplicate, nil, logging.Fields{"path": path})
					continue
				}
				pending[path] = time.Now()
				if lastWaitLog.IsZero() || time.Since(lastWaitLog) > time.Minute {
					d.logConsoleInfo(
						logging.EventSystemDestinationsWaiting,
						"INFO     destination library unavailable; waiting...",
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
					fmt.Sprintf("WARNING  already in-flight, skipping: %s", path),
					nil,
					logging.Fields{"path": path},
				)
				d.logHistoryWarn(logging.EventDaemonPathDuplicate, nil, logging.Fields{"path": path})
				continue
			}
			select {
			case workQueue <- workItem{path: path, inFlightKey: key}:
			case <-ctx.Done():
				d.clearInFlight(key)
				break runLoop
			}

		// --- Clipboard errors ---
		case err, ok := <-pollerErrors:
			if !ok {
				// poller shuts down with ctx; watcher may still be running
				continue
			}
			if err != nil {
				d.logConsoleError(logging.EventDaemonClipboardError, fmt.Sprintf("ERROR    clipboard: %v", err), err, nil)
				d.logHistoryError(logging.EventDaemonClipboardError, err, nil)
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
				fmt.Sprintf("TORRENT  %q  (btih=%s, %d trackers)", truncateForLog(dn, 80), btih, tr),
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
					d.logConsoleError(logging.EventDaemonTxAddError, fmt.Sprintf("ERROR    torrent: could not add — %v", err), err, logging.Fields{
						"btih": btihShort,
					})
					d.logHistoryError(logging.EventDaemonTxAddError, err, logging.Fields{
						"btih": btihShort,
					})
					return
				}

				if strings.TrimSpace(d.TransmissionHost) != "" {
					d.trackProgressOnce.Do(func() {
						d.logConsoleInfo(
							logging.EventDaemonMagnetAdded,
							fmt.Sprintf("TORRENT  Track progress here: http://%s/transmission/web/", d.TransmissionHost),
							logging.Fields{"host": d.TransmissionHost},
						)
					})
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

	// Wait for runWorker to fully stop. jobrunner.Run (invoked per item inside
	// processPath) guarantees runWorker returns within policy.Grace+policy.Force
	// of ctx being canceled, even if the underlying processor ignores
	// cancellation entirely, so this wait is bounded in practice despite having
	// no explicit timeout here.
	result := <-outcome

	if !result.lastItemTimedOut {
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

// workerOutcome reports how runWorker's item processing ended.
type workerOutcome struct {
	// lastItemTimedOut is true when the item runWorker was processing gave up
	// per its shutdown.Policy (see jobrunner.Run).
	lastItemTimedOut bool
}

func (d *Daemon) runWorker(runCtx context.Context, policy shutdown.Policy, hooks shutdown.Hooks, queue <-chan workItem, outcome chan<- workerOutcome) {
	for {
		// Give runCtx cancellation priority over dequeuing another item: once
		// shutdown has been observed, stop pulling from queue entirely rather
		// than racing the two select cases below. Without this, a canceled
		// runCtx and a non-empty queue are both permanently ready, so plain
		// select could keep "choosing" queue across iterations and run
		// jobrunner.Run's grace/force drain (and its hooks) again for every
		// additional item, instead of bounding the whole shutdown to one
		// grace+force window.
		select {
		case <-runCtx.Done():
			outcome <- workerOutcome{}
			return
		default:
		}

		select {
		case <-runCtx.Done():
			outcome <- workerOutcome{}
			return
		case item, ok := <-queue:
			if !ok {
				outcome <- workerOutcome{}
				return
			}
			if d.processPath(runCtx, policy, hooks, item.path, item.inFlightKey) {
				outcome <- workerOutcome{lastItemTimedOut: true}
				return
			}
		}
	}
}

// processPath runs one item through jobrunner.Run, applying policy/hooks so
// shutdown of the daemon's run context (ctx) is handled with a bounded
// graceful-then-forced drain. It reports timedOut=true when the item was
// abandoned per policy (see jobrunner.Run's late-callback-dropping guarantee).
func (d *Daemon) processPath(ctx context.Context, policy shutdown.Policy, hooks shutdown.Hooks, pth string, inFlightKey string) (timedOut bool) {
	defer d.clearInFlight(inFlightKey)

	start := time.Now()
	jobCtx := context.WithoutCancel(ctx)
	planner := notify.NewDoneSoundPlanner(d.DoneNotificationMode)
	emit := func(r processor.Result) {
		dur := time.Since(start).Round(time.Second)
		if r.Applied {
			durSuffix := ""
			if dur >= time.Second {
				durSuffix = fmt.Sprintf("  (%s)", dur)
			}
			d.logConsoleInfo(
				logging.EventProcessorMoveMainApplied,
				fmt.Sprintf("SORTED   %s\n    %s   %s%s", filepath.Base(r.Plan.MainSourcePath), console.Colorize("->", console.Green), r.Plan.DestMainPath, durSuffix),
				logging.Fields{"path": pth, "dest_path": r.Plan.DestMainPath, "duration": dur.String()},
			)
			playCount := planner.OnAppliedMain()
			for i := 0; i < playCount; i++ {
				d.playSoundAsync(jobCtx, d.SoundDone)
			}
			return
		}
		if processor.IsSuppressedResult(r) {
			return
		}
		d.logConsoleInfo(
			logging.EventProcessorInputSkippedParseError,
			fmt.Sprintf("SKIPPED  %s — %s", filepath.Base(pth), r.Reason),
			logging.Fields{"path": pth, "reason": r.Reason, "duration": dur.String()},
		)
	}

	err, _ := jobrunner.Run(ctx, policy, hooks, d.Proc, pth, emit)
	if errors.Is(err, jobrunner.ErrAbandoned) {
		return true
	}

	dur := time.Since(start).Round(time.Second)

	if err != nil {
		d.logConsoleError(
			logging.EventDaemonProcessError,
			fmt.Sprintf("ERROR    %s — %v  (%s)", pth, err, dur),
			err,
			logging.Fields{"path": pth, "duration": dur.String()},
		)
		d.logHistoryError(logging.EventDaemonProcessError, err, logging.Fields{
			"path":     pth,
			"duration": dur.String(),
		})
		return false
	}

	playCount := planner.OnJobComplete()
	for i := 0; i < playCount; i++ {
		d.playSoundAsync(jobCtx, d.SoundDone)
	}

	if planner.HasAppliedMain() {
		d.cleanupCompletedTorrents(jobCtx)
	}
	return false
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
		d.logConsoleError(logging.EventDaemonTxCleanupError, fmt.Sprintf("ERROR    torrent cleanup: %v", err), err, nil)
		d.logHistoryError(logging.EventDaemonTxCleanupError, err, nil)
		return
	}
	if removed > 0 {
		noun := "torrents"
		if removed == 1 {
			noun = "torrent"
		}
		d.logConsoleInfo(
			logging.EventDaemonTxCleanupRemoved,
			fmt.Sprintf("REMOVED  %d completed %s", removed, noun),
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

