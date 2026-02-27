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
	if shutdownGrace <= 0 {
		shutdownGrace = 10 * time.Minute
	}
	if shutdownForce <= 0 {
		shutdownForce = 15 * time.Second
	}

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
			res            []processor.Result
			runErr         error
			graceTimer     *time.Timer
			graceCh        <-chan time.Time
			forceTimer     *time.Timer
			forceCh        <-chan time.Time
			forceTriggered bool
		)

	waitItem:
		for {
			select {
			case out := <-done:
				res = out.res
				runErr = out.err
				break waitItem

			case <-ctx.Done():
				if !interrupted {
					fmt.Fprintf(os.Stderr, "process-drop: shutdown requested; waiting up to %s for in-flight item\n", shutdownGrace)
					interrupted = true
				}
				if graceTimer == nil {
					graceTimer = time.NewTimer(shutdownGrace)
					graceCh = graceTimer.C
				}

			case <-graceCh:
				graceCh = nil
				if forceTriggered {
					continue
				}
				forceTriggered = true
				fmt.Fprintf(os.Stderr, "process-drop: grace elapsed; canceling in-flight item and waiting up to %s\n", shutdownForce)
				cancelItem()
				forceTimer = time.NewTimer(shutdownForce)
				forceCh = forceTimer.C

			case <-forceCh:
				timedOut = true
				errCount++
				fmt.Fprintln(os.Stderr, "process-drop: forced shutdown timeout exceeded while waiting for in-flight item")
				break waitItem
			}
		}

		if graceTimer != nil {
			graceTimer.Stop()
		}
		if forceTimer != nil {
			forceTimer.Stop()
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
