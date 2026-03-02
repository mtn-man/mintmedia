package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/Mtn-Man/mintmedia/internal/notify"
	"github.com/Mtn-Man/mintmedia/internal/processor"
	"github.com/Mtn-Man/mintmedia/internal/shutdown"
	"github.com/Mtn-Man/mintmedia/internal/watch"
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

type processDropCaffeinateController interface {
	Start(context.Context) error
	Stop() error
}

var playDoneSound = notify.PlaySound
var newProcessDropCaffeinate = func() processDropCaffeinateController {
	return notify.NewCaffeinate()
}

func processDropFolder(
	ctx context.Context,
	proc processor.Processor,
	dropRoot string,
	soundDone string,
	doneNotificationMode string,
	verbose bool,
	shutdownGrace time.Duration,
	shutdownForce time.Duration,
) ProcessDropOutcome {
	start := time.Now()
	policy := shutdown.ResolvePolicy(shutdownGrace, shutdownForce)

	// Prevent macOS idle sleep for the lifetime of this process-drop run (best-effort).
	caffCtx, cancelCaff := context.WithCancel(context.Background())
	caff := newProcessDropCaffeinate()
	if err := caff.Start(caffCtx); err != nil {
		fmt.Fprintf(os.Stderr, "caffeinate warning: %v\n", err)
	}
	defer func() {
		cancelCaff()
		if err := caff.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "caffeinate stop warning: %v\n", err)
		}
	}()

	entries, err := os.ReadDir(dropRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
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

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.Before(candidates[j].modTime)
	})

	if len(candidates) == 0 && errCount == 0 {
		PrintProcessDropNoFiles()
		return ProcessDropOutcome{}
	}
	if len(candidates) == 0 {
		return ProcessDropOutcome{ErrorCount: errCount}
	}

	PrintProcessDropCandidates(len(candidates), verbose)

	summary := ProcessDropSummary{
		Candidates: len(candidates),
	}

	interrupted := false
	timedOut := false

	for _, item := range candidates {
		if ctx.Err() != nil {
			if !interrupted {
				fmt.Fprintf(os.Stderr, "process-drop: shutdown requested; stopping before next item\n")
				interrupted = true
			}
			break
		}

		itemCtx, cancelItem := context.WithCancel(context.Background())
		type processResult struct {
			res []processor.Result
			err error
		}
		done := make(chan processResult, 1)
		resultEvents := make(chan processor.Result)
		itemClosed := make(chan struct{})
		var closeItemClosedOnce sync.Once
		closeItemClosed := func() {
			closeItemClosedOnce.Do(func() {
				close(itemClosed)
			})
		}
		streamed := false
		planner := notify.NewDoneSoundPlanner(doneNotificationMode)
		playDoneCount := func(count int) {
			for i := 0; i < count; i++ {
				_ = playDoneSound(context.WithoutCancel(ctx), soundDone)
			}
		}
		recordResult := func(r processor.Result) {
			PrintProcessDropResults([]processor.Result{r}, verbose)
			summary.Results++
			if r.Applied {
				summary.Applied++
				playDoneCount(planner.OnAppliedMain())
				return
			}
			summary.Skipped++
		}
		handleStreamResult := func(r processor.Result) {
			streamed = true
			recordResult(r)
		}

		go func(path string) {
			req := processor.Request{
				InputPath: path,
				OnResult: func(r processor.Result) {
					select {
					case resultEvents <- r:
					case <-itemClosed:
					}
				},
			}
			res, err := proc.Process(itemCtx, req)
			done <- processResult{res: res, err: err}
		}(item.path)

		var (
			res      []processor.Result
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
					handleStreamResult(r)
				case out := <-done:
					res = out.res
					runErr = out.err
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
				handleStreamResult(r)
			case out := <-done:
				res = out.res
				runErr = out.err
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
							fmt.Fprintf(os.Stderr, "process-drop: shutdown requested; waiting up to %s for in-flight item\n", grace)
						},
						OnGraceElapsed: func(force time.Duration) {
							fmt.Fprintf(os.Stderr, "process-drop: grace elapsed; canceling in-flight item and waiting up to %s\n", force)
						},
					},
				)
				if drain.TimedOut {
					timedOut = true
					errCount++
					closeItemClosed()
					fmt.Fprintln(os.Stderr, "process-drop: forced shutdown timeout exceeded while waiting for in-flight item")
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

		if !streamed {
			for _, r := range res {
				recordResult(r)
			}
		}

		playCount := planner.OnJobComplete()
		playDoneCount(playCount)
		closeItemClosed()

		if interrupted {
			fmt.Fprintf(os.Stderr, "process-drop: shutdown requested; stopping after current item\n")
			break
		}
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
