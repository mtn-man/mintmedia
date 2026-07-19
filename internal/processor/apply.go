// internal/processor/apply.go
package processor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mtn-man/mintmedia/internal/logging"
	"github.com/mtn-man/mintmedia/internal/transfer"
)

// apply executes precomputed plan(s).
// Policy (v1):
//   - Move main media first; it must succeed.
//   - Move associated files best-effort (failures do not block main success).
//   - If the original input was a directory, move it to Trash after successful main move,
//     with strong safety checks (treat leftover non-media junk as disposable).
func apply(ctx context.Context, p *processorImpl, plans []Plan) ([]Result, error) {
	return applyWithEmitter(ctx, p, plans, nil)
}

func applyWithEmitter(ctx context.Context, p *processorImpl, plans []Plan, emit func(Result)) ([]Result, error) {
	if len(plans) == 0 {
		return nil, nil
	}

	assocFailedByInput := make(map[string]bool)
	duplicateSkippedByInput := make(map[string]bool)

	results := make([]Result, 0, len(plans))
	for _, pl := range plans {
		res, err := applyOne(ctx, p, pl, assocFailedByInput, duplicateSkippedByInput)
		results = append(results, res)
		if emit != nil {
			emit(res)
		}
		if err != nil {
			return results, err
		}
	}

	return results, nil
}

func applyOne(ctx context.Context, p *processorImpl, pl Plan, assocFailedByInput, duplicateSkippedByInput map[string]bool) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{Plan: pl}, err
	}

	if strings.TrimSpace(pl.MainSourcePath) == "" || strings.TrimSpace(pl.DestMainPath) == "" {
		return Result{Plan: pl}, fmt.Errorf("invalid plan: missing main source/dest path")
	}

	if p.xfer == nil {
		return Result{Plan: pl}, fmt.Errorf("processor misconfigured: Transferer is nil")
	}

	if pl.Duplicate {
		return skipDuplicateResult(p, pl, duplicateSkippedByInput), nil
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(pl.DestDir, 0o755); err != nil {
		if transfer.IsDestinationUnavailable(err) {
			return Result{Plan: pl}, &DestinationUnavailableError{Category: pl.Category, Err: err}
		}
		return Result{Plan: pl}, fmt.Errorf("create destination dir %q: %w", pl.DestDir, err)
	}

	// Move main media first
	if err := p.xfer.Move(ctx, pl.MainSourcePath, pl.DestMainPath); err != nil {
		if !handleCleanupError(p, err, "main", pl.MainSourcePath, pl.DestMainPath) {
			if transfer.IsDestinationUnavailable(err) {
				return Result{Plan: pl}, &DestinationUnavailableError{Category: pl.Category, Err: err}
			}
			if transfer.IsDestinationExists(err) {
				// Lost a race against another job/batch item that claimed
				// this destination after Plan's own duplicate check ran (see
				// pl.Duplicate above) but before this move -- treat it the
				// same as a Plan-time-detected duplicate rather than a hard
				// failure.
				return skipDuplicateResult(p, pl, duplicateSkippedByInput), nil
			}
			return Result{Plan: pl}, fmt.Errorf("move main media: %w", err)
		}
	}
	logInfoHistoryOnly(p, logging.EventProcessorMoveMainApplied, logging.Fields{
		"src":      pl.MainSourcePath,
		"dst":      pl.DestMainPath,
		"category": string(pl.Category),
	})

	// Move associated files best-effort
	assocFailedCount := 0
	for _, mv := range pl.Associated {
		if ctx.Err() != nil {
			return Result{Plan: pl, Applied: true, Handled: true, Reason: "applied"}, ctx.Err()
		}
		if mv.Source == "" || mv.Dest == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(mv.Dest), 0o755); err != nil {
			if transfer.IsDestinationUnavailable(err) {
				return Result{Plan: pl, Applied: true, Handled: true, Reason: "applied"}, &DestinationUnavailableError{Category: pl.Category, Err: err}
			}
			return Result{Plan: pl, Applied: true, Handled: true, Reason: "applied"}, fmt.Errorf("create associated dest dir %q: %w", filepath.Dir(mv.Dest), err)
		}
		if err := p.xfer.Move(ctx, mv.Source, mv.Dest); err != nil {
			if handleCleanupError(p, err, "associated", mv.Source, mv.Dest) {
				logInfoHistoryOnly(p, logging.EventProcessorMoveAssociatedApplied, logging.Fields{
					"src":      mv.Source,
					"dst":      mv.Dest,
					"category": string(pl.Category),
				})
				continue
			}
			// A destination-unavailable error here signals the same systemic
			// problem a main-media failure would (disk full/permission
			// denied), not a one-off associated-file glitch -- it will recur
			// for every subsequent write to this category, so it escalates
			// out of the best-effort path instead of being logged and skipped.
			if transfer.IsDestinationUnavailable(err) {
				return Result{Plan: pl, Applied: true, Handled: true, Reason: "applied"}, &DestinationUnavailableError{Category: pl.Category, Err: err}
			}
			assocFailedCount++
			if pl.InputPath != "" && assocFailedByInput != nil {
				assocFailedByInput[pl.InputPath] = true
			}
			logWarnHistoryOnly(p, logging.EventProcessorMoveAssociatedFailed, err, logging.Fields{
				"src":      mv.Source,
				"dst":      mv.Dest,
				"category": string(pl.Category),
			})
			continue
		}
		logInfoHistoryOnly(p, logging.EventProcessorMoveAssociatedApplied, logging.Fields{
			"src":      mv.Source,
			"dst":      mv.Dest,
			"category": string(pl.Category),
		})
	}
	if assocFailedCount > 0 {
		logConsoleWarn(p, logging.EventProcessorMoveAssociatedFailed,
			fmt.Sprintf("WARNING  %d associated file(s) not moved for %s; see history log",
				assocFailedCount, filepath.Base(pl.MainSourcePath)),
			nil,
			logging.Fields{"input_path": pl.InputPath},
		)
	}

	// Cleanup: move source directory to Trash if safe (only for directory inputs)
	if pl.DeleteEmptyInputDir {
		if pl.InputPath != "" && assocFailedByInput[pl.InputPath] {
			logWarn(p, logging.EventProcessorCleanupSkippedAssociatedFailed, fmt.Sprintf("source folder cleanup skipped for %s (associated move failed)", pl.InputPath), nil, logging.Fields{
				"input_path": pl.InputPath,
			})
			return Result{
				Plan:    pl,
				Applied: true,
				Handled: true,
				Reason:  "applied",
			}, nil
		}
		if pl.InputPath != "" && duplicateSkippedByInput[pl.InputPath] {
			// A sibling in this batch was left in place because it was a
			// duplicate (see skipDuplicateResult) -- trashing the input
			// directory now would take that un-moved file down with it.
			logWarn(p, logging.EventProcessorCleanupSkippedDuplicate, fmt.Sprintf("source folder cleanup skipped for %s (duplicate file left in place)", pl.InputPath), nil, logging.Fields{
				"input_path": pl.InputPath,
			})
			return Result{
				Plan:    pl,
				Applied: true,
				Handled: true,
				Reason:  "applied",
			}, nil
		}
		if err := cleanupSourceDirIfSafe(p, pl.InputPath); err != nil {
			logWarn(p, logging.EventProcessorCleanupSkippedFailed, fmt.Sprintf("source folder cleanup skipped for %s: %v", pl.InputPath, err), err, logging.Fields{
				"input_path": pl.InputPath,
			})
		}
	}

	return Result{
		Plan:    pl,
		Applied: true,
		Handled: true,
		Reason:  "applied",
	}, nil
}

// skipDuplicateResult logs and builds the graceful-skip Result shared by
// both duplicate-detection paths: the Plan-time check (pl.Duplicate) and the
// Apply-time TOCTOU downgrade (transfer.IsDestinationExists). It also
// records pl.InputPath in duplicateSkippedByInput so the batch-level cleanup
// gate below won't trash a directory that still holds this un-moved file.
func skipDuplicateResult(p *processorImpl, pl Plan, duplicateSkippedByInput map[string]bool) Result {
	if pl.InputPath != "" && duplicateSkippedByInput != nil {
		duplicateSkippedByInput[pl.InputPath] = true
	}
	reason := fmt.Sprintf("already in library: %s", pl.DestMainPath)
	logInfoHistoryOnly(p, logging.EventProcessorInputSkippedDuplicate, logging.Fields{
		"input_path": pl.InputPath,
		"dest_path":  pl.DestMainPath,
	})
	return Result{Plan: pl, Applied: false, Handled: true, Reason: reason}
}

func handleCleanupError(p *processorImpl, err error, kind, src, dst string) bool {
	var ce *transfer.CleanupError
	if !errors.As(err, &ce) {
		return false
	}

	logSrc := src
	logDst := dst
	logErr := err
	if ce != nil {
		if ce.Src != "" {
			logSrc = ce.Src
		}
		if ce.Dst != "" {
			logDst = ce.Dst
		}
		if ce.Err != nil {
			logErr = ce.Err
		}
	}

	logWarn(
		p,
		logging.EventProcessorCleanupSourceFailed,
		fmt.Sprintf("%s source not removed: %s -- %v", kind, logSrc, logErr),
		logErr,
		logging.Fields{
			"cleanup_kind": kind,
			"src":          logSrc,
			"dst":          logDst,
		},
	)
	return true
}
