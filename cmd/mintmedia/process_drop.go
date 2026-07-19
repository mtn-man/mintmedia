package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/mtn-man/mintmedia/internal/console"
	"github.com/mtn-man/mintmedia/internal/jobrunner"
	"github.com/mtn-man/mintmedia/internal/notify"
	"github.com/mtn-man/mintmedia/internal/processor"
	"github.com/mtn-man/mintmedia/internal/resultformat"
	"github.com/mtn-man/mintmedia/internal/shutdown"
)

type ProcessDropOutcome struct {
	ErrorCount  int
	Interrupted bool
	TimedOut    bool
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
	caffCtx, cancelCaff := context.WithCancel(context.Background())
	caff := newProcessDropCaffeinate()
	if err := caff.Start(caffCtx); err != nil {
		if errors.Is(err, notify.ErrInhibitUnsupported) {
			fmt.Println(console.ColorizePrefixOut("INFO     caffeinate: sleep inhibition not available on this platform"))
		} else {
			fmt.Fprintln(os.Stderr, console.ColorizePrefixErr(fmt.Sprintf("WARNING  caffeinate: %v", err)))
		}
	}
	defer func() {
		cancelCaff()
		if err := caff.Stop(); err != nil {
			fmt.Fprintln(os.Stderr, console.ColorizePrefixErr(fmt.Sprintf("WARNING  caffeinate stop: %v", err)))
		}
	}()

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
		st, err := os.Stat(dir)
		if err != nil || !st.IsDir() {
			PrintProcessDropDestinationError(dir)
			return ProcessDropOutcome{ErrorCount: 1}
		}
	}

	summary := ProcessDropSummary{}

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
	playDoneCount := func(count int) {
		for i := 0; i < count; i++ {
			if !soundDebounce.Allow(time.Now(), doneSoundCooldown) {
				continue
			}
			// Capture playDoneSound synchronously here rather than inside the
			// goroutine: it's a package-level var swapped out by tests, and
			// reading it lazily in the goroutine could race with a test's
			// cleanup restoring it after processDropFolder has returned.
			play := playDoneSound
			go func() { _ = play(context.WithoutCancel(ctx), soundDone) }()
		}
	}

	for _, path := range candidates {
		if ctx.Err() != nil {
			if !interrupted {
				interrupted = true
			}
			break
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
				playDoneCount(planner.OnAppliedMain())
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

		if runErr != nil {
			PrintProcessDropItemError(path, runErr, time.Since(itemStart).Round(time.Second))
			errCount++
		}

		playCount := planner.OnJobComplete()
		playDoneCount(playCount)

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
