package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
		go func(path string) {
			res, err := proc.Process(itemCtx, processor.Request{InputPath: path})
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

			if timeout <= 0 {
				out := <-done
				res = out.res
				runErr = out.err
				gotFinal = true
				return true
			}

			timer := time.NewTimer(timeout)
			defer timer.Stop()

			select {
			case out := <-done:
				res = out.res
				runErr = out.err
				gotFinal = true
				return true
			case <-timer.C:
				return false
			}
		}

		select {
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
				fmt.Fprintln(os.Stderr, "process-drop: forced shutdown timeout exceeded while waiting for in-flight item")
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
		PrintProcessDropResults(res, verbose)
		appliedMainCount := 0
		for _, r := range res {
			summary.Results++
			if r.Applied {
				summary.Applied++
				appliedMainCount++
				continue
			}
			summary.Skipped++
		}
		playCount := notify.DoneSoundCount(doneNotificationMode, appliedMainCount)
		for i := 0; i < playCount; i++ {
			_ = notify.PlaySound(context.WithoutCancel(ctx), soundDone)
		}

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
