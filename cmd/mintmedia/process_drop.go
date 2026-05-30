package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mtn-man/mintmedia/internal/console"
	"github.com/mtn-man/mintmedia/internal/notify"
	"github.com/mtn-man/mintmedia/internal/processor"
	"github.com/mtn-man/mintmedia/internal/shutdown"
	"github.com/mtn-man/mintmedia/internal/watch"
)

type dropCandidate struct {
	path    string
	modTime time.Time
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
	fmt.Println(console.ColorizePrefix("STARTED  mintmedia"))
	fmt.Println()

	start := time.Now()
	policy := shutdown.ResolvePolicy(shutdownGrace, shutdownForce)

	// Prevent macOS idle sleep for the lifetime of this process-drop run (best-effort).
	caffCtx, cancelCaff := context.WithCancel(context.Background())
	caff := newProcessDropCaffeinate()
	if err := caff.Start(caffCtx); err != nil {
		fmt.Fprintln(os.Stderr, console.ColorizePrefix(fmt.Sprintf("WARNING  caffeinate: %v", err)))
	}
	defer func() {
		cancelCaff()
		if err := caff.Stop(); err != nil {
			fmt.Fprintln(os.Stderr, console.ColorizePrefix(fmt.Sprintf("WARNING  caffeinate stop: %v", err)))
		}
	}()

	entries, err := os.ReadDir(dropRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, console.ColorizePrefix(fmt.Sprintf("ERROR    %v", err)))
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

		candidates = append(candidates, dropCandidate{
			path:    path,
			modTime: info.ModTime(),
		})
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
	fileCount := 0
	for _, c := range candidates {
		if ctx.Err() != nil {
			return ProcessDropOutcome{ErrorCount: errCount, Interrupted: true}
		}
		plans, planErr := proc.Plan(ctx, processor.Request{InputPath: c.path})
		if planErr != nil {
			continue // non-media or unparseable; the processing loop will handle it
		}
		fileCount += len(plans)
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

	for _, item := range candidates {
		if ctx.Err() != nil {
			if !interrupted {
				interrupted = true
			}
			break
		}

		itemCtx, cancelItem := context.WithCancel(context.Background())
		done := make(chan error, 1)
		resultEvents := make(chan processor.Result)
		itemClosed := make(chan struct{})
		var closeItemClosedOnce sync.Once
		closeItemClosed := func() {
			closeItemClosedOnce.Do(func() {
				close(itemClosed)
			})
		}
		planner := notify.NewDoneSoundPlanner(doneNotificationMode)
		playDoneCount := func(count int) {
			for i := 0; i < count; i++ {
				_ = playDoneSound(context.WithoutCancel(ctx), soundDone)
			}
		}
		recordResult := func(r processor.Result) {
			if processor.IsSuppressedResult(r) {
				return
			}
			PrintProcessDropResults([]processor.Result{r}, verbose)
			summary.Results++
			if r.Applied {
				summary.Applied++
				playDoneCount(planner.OnAppliedMain())
				return
			}
			summary.Skipped++
		}

		go func(path string) {
			err := processor.ProcessEach(itemCtx, proc, processor.Request{InputPath: path},
				func(r processor.Result) {
					select {
					case resultEvents <- r:
					case <-itemClosed:
					}
				})
			done <- err
		}(item.path)

		var (
			runErr   error
			gotFinal bool
		)

		waitForResult := func(timeout time.Duration) bool {
			if gotFinal {
				return true
			}
			timer := time.NewTimer(timeout)
			defer timer.Stop()

			for !gotFinal {
				select {
				case r := <-resultEvents:
					recordResult(r)
				case runErr = <-done:
					gotFinal = true
					return true
				case <-timer.C:
					return false
				}
			}
			return true
		}

		for !gotFinal && !timedOut {
			select {
			case r := <-resultEvents:
				recordResult(r)
			case runErr = <-done:
				gotFinal = true
			case <-ctx.Done():
				if !interrupted {
					interrupted = true
				}

				drain := shutdown.Drain(
					policy,
					true,
					waitForResult,
					cancelItem,
					shutdown.Hooks{
						OnWaitStart: func(grace time.Duration) {
							fmt.Fprint(os.Stderr, "\n"+console.ColorizePrefix(fmt.Sprintf("WARNING  shutdown requested. Waiting up to %s for in-flight item.", grace))+"\n")
						},
						OnGraceElapsed: func(force time.Duration) {
							fmt.Fprintln(os.Stderr, console.ColorizePrefix(fmt.Sprintf("WARNING  shutdown grace elapsed. Canceling in-flight item, waiting up to %s.", force)))
						},
					},
				)
				if drain.TimedOut {
					timedOut = true
					errCount++
					closeItemClosed()
					fmt.Fprintln(os.Stderr, console.ColorizePrefix("ERROR    shutdown timed out while waiting for in-flight item."))
				}
			}
		}
		cancelItem()

		if timedOut {
			break
		}

		if runErr != nil {
			PrintProcessDropItemError(item.path, runErr)
			errCount++
		}

		playCount := planner.OnJobComplete()
		playDoneCount(playCount)
		closeItemClosed()

		if interrupted {
			break
		}
	}

	if interrupted && !timedOut {
		fmt.Fprint(os.Stderr, "\n"+console.ColorizePrefix("WARNING  shutdown requested. Stopping.")+"\n")
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
