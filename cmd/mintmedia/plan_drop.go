package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/mtn-man/mintmedia/internal/processor"
	"github.com/mtn-man/mintmedia/internal/watch"
)

func planDropFolder(ctx context.Context, proc processor.Processor, dropRoot string) int {
	entries, err := os.ReadDir(dropRoot)
	if err != nil {
		PrintFatalError(err)
		return 1
	}

	var paths []string
	errCount := 0

	for _, ent := range entries {
		name := ent.Name()
		if watch.IsIgnorableDropEntry(name) {
			continue
		}
		info, err := ent.Info()
		if err != nil {
			PrintProcessDropStatError(filepath.Join(dropRoot, name), err)
			errCount++
			continue
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			continue
		}
		paths = append(paths, filepath.Join(dropRoot, name))
	}

	sortedPaths, sortErrs, sortErr := processor.SortCandidates(ctx, proc, paths)
	if sortErr != nil {
		return errCount + 1
	}
	for _, se := range sortErrs {
		PrintProcessDropSortError(se.Path, se.Err)
		errCount++
	}

	if len(sortedPaths) == 0 && errCount == 0 {
		PrintProcessDropNoFiles()
		return 0
	}

	for _, p := range sortedPaths {
		if ctx.Err() != nil {
			return errCount + 1
		}
		plans, err := proc.Plan(ctx, processor.Request{InputPath: p})
		if err != nil {
			PrintProcessDropItemError(p, err)
			errCount++
			continue
		}
		PrintPlans(plans)
	}

	return errCount
}
