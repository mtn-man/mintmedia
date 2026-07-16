package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/mtn-man/mintmedia/internal/processor"
	"github.com/mtn-man/mintmedia/internal/watch"
)

// discoverDropPaths reads dropRoot, filters ignorable entries, stats each
// candidate, and returns them sorted via processor.SortCandidates. errCount
// accumulates non-fatal per-entry stat errors and non-fatal sort errors,
// both of which are already printed to stderr by this function. readErr and
// sortErr are fatal -- callers are responsible for reporting them (they
// currently do so differently, so that choice is left to the caller) and
// aborting.
func discoverDropPaths(ctx context.Context, proc processor.Processor, dropRoot string) (paths []string, errCount int, readErr error, sortErr error) {
	entries, readErr := os.ReadDir(dropRoot)
	if readErr != nil {
		return nil, 0, readErr, nil
	}

	var candidates []string
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
		candidates = append(candidates, path)
	}

	sortedPaths, sortErrs, sortErr := processor.SortCandidates(ctx, proc, candidates)
	if sortErr != nil {
		return nil, errCount, nil, sortErr
	}
	for _, se := range sortErrs {
		PrintProcessDropSortError(se.Path, se.Err)
		errCount++
	}

	return sortedPaths, errCount, nil, nil
}
