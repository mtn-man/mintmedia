package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/mtn-man/mintmedia/internal/console"
	"github.com/mtn-man/mintmedia/internal/jobrunner"
	"github.com/mtn-man/mintmedia/internal/notify"
	"github.com/mtn-man/mintmedia/internal/paths"
	"github.com/mtn-man/mintmedia/internal/processor"
	"github.com/mtn-man/mintmedia/internal/resultformat"
	"github.com/mtn-man/mintmedia/internal/shutdown"
)

type ProcessDropOutcome struct {
	ErrorCount  int
	Interrupted bool
	TimedOut    bool
}

// destDegradedSet tracks destination categories (Movies/Shows) that have
// gone bad during this one-shot run. Unlike daemon.Daemon's degraded-state
// tracking, this needs no mutex: processDropFolder's candidate loop is
// single-threaded and sequential, with no concurrent worker goroutine to
// race against.
type destDegradedSet map[processor.Category]struct{}

// mark records cat as degraded, returning true only on the first
// healthy->degraded transition, so callers can print the one-time notice
// exactly once instead of on every subsequent occurrence.
func (s destDegradedSet) mark(cat processor.Category) bool {
	if _, ok := s[cat]; ok {
		return false
	}
	s[cat] = struct{}{}
	return true
}

func (s destDegradedSet) isDegraded(cat processor.Category) bool {
	_, ok := s[cat]
	return ok
}

func (s destDegradedSet) any() bool {
	return len(s) > 0
}

// destDirFor maps a processor.Category to its configured destination directory.
func destDirFor(cat processor.Category, moviesDir, showsDir string) string {
	if cat == processor.CategoryShow {
		return showsDir
	}
	return moviesDir
}

var playDoneSound = notify.PlaySound
var newProcessDropCaffeinate = func() notify.CaffeinateController {
	return notify.NewCaffeinate()
}

func processDropFolder(
	ctx context.Context,
	proc processor.Processor,
	dropRoot string,
	moviesDir string,
	showsDir string,
	soundDone string,
	doneNotificationMode string,
	verbose bool,
	shutdownGrace time.Duration,
	shutdownForce time.Duration,
) ProcessDropOutcome {
	fmt.Println(console.ColorizePrefixOut("STARTED  mintmedia"))
	fmt.Println()

	start := time.Now()
	policy := shutdown.ResolvePolicy(shutdownGrace, shutdownForce)

	// Prevent macOS idle sleep for the lifetime of this process-drop run (best-effort).
	stop := notify.StartCaffeinate(newProcessDropCaffeinate, cliCaffeinateHooks())
	defer stop()

	candidates, errCount, readErr, sortErr := discoverDropPaths(ctx, proc, dropRoot)
	if readErr != nil {
		fmt.Fprintln(os.Stderr, console.ColorizePrefixErr(fmt.Sprintf("ERROR    %v", readErr)))
		return ProcessDropOutcome{ErrorCount: 1}
	}
	if sortErr != nil {
		return ProcessDropOutcome{ErrorCount: errCount, Interrupted: true}
	}

	if len(candidates) == 0 && errCount == 0 {
		PrintProcessDropNoFiles()
		return ProcessDropOutcome{}
	}
	if len(candidates) == 0 {
		return ProcessDropOutcome{ErrorCount: errCount}
	}

	// Count expected media files via a cheap extension-only walk (no naming/
	// hint resolution) so the discovery message reflects the real number of
	// files to process rather than the number of top-level drop entries (e.g.
	// a season pack directory counts as 8, not 1), without paying for a full
	// second Plan pass over the batch. This is an estimate, not an exact
	// count: Plan may still reject a file this count includes (unparseable or
	// ambiguous name), which is why it's labeled "expected" rather than
	// "discovered".
	fileCount, countInterrupted := processor.CountMainMedia(ctx, proc, candidates)
	if countInterrupted {
		return ProcessDropOutcome{ErrorCount: errCount, Interrupted: true}
	}
	if fileCount == 0 && errCount == 0 {
		PrintProcessDropNoFiles()
		return ProcessDropOutcome{}
	}
	if fileCount == 0 {
		return ProcessDropOutcome{ErrorCount: errCount}
	}

	PrintProcessDropCandidates(fileCount)

	for _, dir := range []string{moviesDir, showsDir} {
		if !paths.DirWritable(dir) {
			PrintProcessDropDestinationError(dir)
			return ProcessDropOutcome{ErrorCount: 1}
		}
	}

	summary := ProcessDropSummary{}
	degraded := make(destDegradedSet)

	interrupted := false
	timedOut := false

	hooks := shutdown.Hooks{
		OnWaitStart: func(grace time.Duration) {
			fmt.Fprint(os.Stderr, "\n"+console.ColorizePrefixErr(resultformat.ShutdownWaitMessage("item", grace))+"\n")
		},
		OnGraceElapsed: func(force time.Duration) {
			fmt.Fprint(os.Stderr, "\n"+console.ColorizePrefixErr(resultformat.ShutdownGraceElapsedMessage("item", force))+"\n")
		},
	}

	// Done-sound plays are spaced at least doneSoundCooldown apart: afplay on
	// the default done sound runs ~2s, and a fast, same-filesystem batch can
	// stream many applied results within milliseconds of each other, so
	// without coalescing they'd overlap into a cacophony rather than
	// distinct dings. Declared above the candidate loop (not per-job, like
	// planner below) since overlap can happen across jobs too, e.g. two
	// small top-level candidates finishing back-to-back.
	const doneSoundCooldown = 3 * time.Second
	var soundDebounce notify.Debouncer
	var soundWG sync.WaitGroup
	// playDoneSound is read once here rather than inside DoneSoundPlayer's
	// goroutines: it's a package-level var swapped out by tests, and reading
	// it lazily could race with a test's cleanup restoring it after
	// processDropFolder has returned.
	player := notify.DoneSoundPlayer{
		Debounce: &soundDebounce,
		Cooldown: doneSoundCooldown,
		Play:     playDoneSound,
		Wait:     &soundWG,
	}
	// The caller (main) exits the process immediately after this function
	// returns, so any in-flight done-sound playback must be joined here --
	// otherwise the last file's sound can be killed mid-playback or never
	// scheduled at all.
	defer soundWG.Wait()

	for _, path := range candidates {
		if ctx.Err() != nil {
			if !interrupted {
				interrupted = true
			}
			break
		}

		// Fast path: if a destination category is already known degraded,
		// learn this item's category via a speculative Plan() and skip it
		// outright rather than attempting a move that can only fail the same
		// way -- a known-full disk still costs a real write if attempted,
		// since RenameOrCopy's cross-device fallback copies the whole file
		// into a temp file on the destination before Sync/Rename would hit
		// ENOSPC. This only pays for the extra Plan() call once something is
		// actually degraded; the common (healthy) case is a single
		// map-length check.
		if degraded.any() {
			if cat, known := processor.CategoryForPath(ctx, proc, path); known && degraded.isDegraded(cat) {
				errCount++
				summary.DestDegraded++
				PrintProcessDropDestinationDegradedSkip(path, cat)
				if ctx.Err() != nil && !interrupted {
					interrupted = true
				}
				if interrupted {
					break
				}
				continue
			}
		}

		planner := notify.NewDoneSoundPlanner(doneNotificationMode)
		itemStart := time.Now()
		recordResult := func(r processor.Result) {
			if processor.IsSuppressedResult(r) {
				return
			}
			dur := time.Since(itemStart).Round(time.Second)
			itemStart = time.Now()
			PrintProcessDropResults([]processor.Result{r}, verbose, dur)
			summary.Results++
			if r.Applied {
				summary.Applied++
				player.PlayCount(ctx, soundDone, planner.OnAppliedMain())
				return
			}
			summary.Skipped++
		}

		_, runErr := jobrunner.Run(ctx, policy, hooks, proc, path, recordResult)

		if ctx.Err() != nil && !interrupted {
			interrupted = true
		}
		if errors.Is(runErr, jobrunner.ErrAbandoned) {
			timedOut = true
			errCount++
			fmt.Fprintln(os.Stderr, console.ColorizePrefixErr("ERROR    shutdown timed out while waiting for in-flight item."))
			break
		}

		var destErr *processor.DestinationUnavailableError
		switch {
		case errors.As(runErr, &destErr) && degraded.mark(destErr.Category):
			// First occurrence for this category: recordResult above already
			// reported this item's own Result correctly (including the
			// Applied:true partial-move cases where the main file moved but
			// an associated file's move triggered this error) -- there is
			// nothing to roll back here. This only adds the one-time notice
			// and marks the category degraded for the rest of this run.
			errCount++
			summary.DestDegraded++
			PrintProcessDropDestinationDegraded(destErr.Category, destDirFor(destErr.Category, moviesDir, showsDir), destErr.Err, time.Since(itemStart).Round(time.Second))
		case runErr != nil:
			PrintProcessDropItemError(path, runErr, time.Since(itemStart).Round(time.Second))
			errCount++
		}

		player.PlayCount(ctx, soundDone, planner.OnJobComplete())

		if interrupted {
			break
		}
	}

	if interrupted && !timedOut {
		fmt.Fprint(os.Stderr, "\n"+console.ColorizePrefixErr("WARNING  shutdown requested. Stopping.")+"\n")
	}

	summary.Errors = errCount
	summary.Elapsed = time.Since(start)

	PrintProcessDropSummary(summary)

	return ProcessDropOutcome{
		ErrorCount:  errCount,
		Interrupted: interrupted,
		TimedOut:    timedOut,
	}
}
