// internal/processor/category.go
package processor

import (
	"context"
	"errors"
)

// CategoryForPath learns the Category an input path would resolve to,
// without applying it, by calling proc.Plan and inspecting either the
// resulting plan or the planning error:
//   - if Plan returns at least one plan, plans[0].Category is authoritative
//     (covers both a clean success and a *PartialPlanError, where some
//     siblings in a multi-file directory hit a skippable parse error but
//     earlier ones still planned fine);
//   - otherwise, if Plan itself failed because it needed to read an
//     already-degraded destination (e.g. resolveShowFolder listing ShowsDir,
//     or checkExactDuplicate stat-ing DestMainPath), that failure is itself
//     a *DestinationUnavailableError, which already names the category.
//
// ok is false when neither source yields a category (Plan failed for an
// unrelated reason, or returned nothing usable).
func CategoryForPath(ctx context.Context, proc Processor, path string) (cat Category, ok bool) {
	plans, planErr := proc.Plan(ctx, Request{InputPath: path})
	var destErr *DestinationUnavailableError
	switch {
	case len(plans) > 0:
		return plans[0].Category, true
	case errors.As(planErr, &destErr):
		return destErr.Category, true
	default:
		return "", false
	}
}
