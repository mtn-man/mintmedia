package main

import (
	"context"
	"errors"

	"github.com/mtn-man/mintmedia/internal/processor"
)

func planDropFolder(ctx context.Context, proc processor.Processor, dropRoot string) int {
	sortedPaths, errCount, readErr, sortErr := discoverDropPaths(ctx, proc, dropRoot)
	if readErr != nil {
		PrintFatalError(readErr)
		return 1
	}
	if sortErr != nil {
		return errCount + 1
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
		var partial *processor.PartialPlanError
		if errors.As(err, &partial) {
			if len(plans) > 0 {
				PrintPlans(plans)
			}
			PrintPlanIssues(partial.Issues)
			errCount++
			continue
		}
		if err != nil {
			PrintProcessDropItemError(p, err, 0)
			errCount++
			continue
		}
		PrintPlans(plans)
	}

	return errCount
}
