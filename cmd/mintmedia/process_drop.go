package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mtn-man/mintmedia/internal/console"
	"github.com/mtn-man/mintmedia/internal/jobrunner"
	"github.com/mtn-man/mintmedia/internal/notify"
	"github.com/mtn-man/mintmedia/internal/processor"
	"github.com/mtn-man/mintmedia/internal/resultformat"
	"github.com/mtn-man/mintmedia/internal/shutdown"
	"github.com/mtn-man/mintmedia/internal/watch"
)

type dropCandidate struct {
	path string
}

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
		fmt.Fprintln(os.Stderr, console.ColorizePrefixErr(fmt.Sprintf("WARNING  caffeinate: %v", err)))
	}
	defer func() {
		cancelCaff()
		if err := caff.Stop(); err != nil {
			fmt.Fprintln(os.Stderr, console.ColorizePrefixErr(fmt.Sprintf("WARNING  caffeinate stop: %v", err)))
		}
	}()

	entries, err := os.ReadDir(dropRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, console.ColorizePrefixErr(fmt.Sprintf("ERROR    %v", err)))
		return ProcessDropOutcome{ErrorCount: 1}
	}

	candidates := make([]dropCandidate, 0, len(entries))
	errCount := 0

	for _, ent := range entries {
		name := ent.Name()
		if watch.IsIgnorableDropEntry(name) {
			continue
		}
		path := filepath.Join(dropRoot, name)

		info, err := ent.Info()
		if err != nil {
			PrintProcessDropStatError(path, err)
			errCount++
			continue
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			continue
		}

		candidates = append(candidates, dropCandidate{path: path})
	}

	{
		paths := make([]string, len(candidates))
		for i, c := range candidates {
			paths[i] = c.path
		}
		sortedPaths, sortErrs, sortErr := processor.SortCandidates(ctx, proc, paths)
		if sortErr != nil {
			return ProcessDropOutcome{ErrorCount: errCount, Interrupted: true}
		}
		for _, se := range sortErrs {
			PrintProcessDropSortError(se.Path, se.Err)
			errCount++
		}
		// Build a lookup so we can reconstruct candidates in sorted order.
		idx := make(map[string]dropCandidate, len(candidates))
		for _, c := range candidates {
			idx[c.path] = c
		}
		ordered := make([]dropCandidate, 0, len(sortedPaths))
		for _, p := range sortedPaths {
			if c, ok := idx[p]; ok {
				ordered = append(ordered, c)
			}
		}
		candidates = ordered
	}

	if len(candidates) == 0 && errCount == 0 {
		PrintProcessDropNoFiles()
		return ProcessDropOutcome{}
	}
	if len(candidates) == 0 {
		return ProcessDropOutcome{ErrorCount: errCount}
	}

	// Count actual media files via a planning pass so the discovery message
	// reflects the real number of files to process rather than the number of
	// top-level drop entries (e.g. a season pack directory counts as 8, not 1).
	candidatePaths := make([]string, len(candidates))
	for i, c := range candidates {
		candidatePaths[i] = c.path
	}
	fileCount, countInterrupted := processor.CountPlans(ctx, proc, candidatePaths)
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

	for _, item := range candidates {
		if ctx.Err() != nil {
			if !interrupted {
				interrupted = true
			}
			break
		}

		planner := notify.NewDoneSoundPlanner(doneNotificationMode)
		playDoneCount := func(count int) {
			for i := 0; i < count; i++ {
				_ = playDoneSound(context.WithoutCancel(ctx), soundDone)
			}
		}
		itemStart := time.Now()
		recordResult := func(r processor.Result) {
			if processor.IsSuppressedResult(r) {
				return
			}
			dur := time.Since(itemStart).Round(time.Second)
			PrintProcessDropResults([]processor.Result{r}, verbose, dur)
			summary.Results++
			if r.Applied {
				summary.Applied++
				playDoneCount(planner.OnAppliedMain())
				return
			}
			summary.Skipped++
		}

		_, runErr := jobrunner.Run(ctx, policy, hooks, proc, item.path, recordResult)

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
			PrintProcessDropItemError(item.path, runErr, time.Since(itemStart).Round(time.Second))
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
