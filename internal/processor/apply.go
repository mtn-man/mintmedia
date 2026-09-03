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
	"github.com/mtn-man/mintmedia/internal/metadata"
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
	if err := os.MkdirAll(pl.DestDir, 0o755); err != nil { //nolint:gosec // library dest dirs need group+other read for the media server
		if transfer.IsDestinationUnavailable(err) {
			return Result{Plan: pl}, &DestinationUnavailableError{Category: pl.Category, Err: err}
		}
		return Result{Plan: pl}, fmt.Errorf("create destination dir %q: %w", pl.DestDir, err)
	}

	// Best-effort: rewrite the embedded container title tag to match the
	// final sorted name before the move, so the remux runs on the file's
	// pre-move (drop folder) location rather than a possibly network-mounted
	// destination. WriteTitleToFile remuxes into a fresh temp file and never
	// touches pl.MainSourcePath -- so the retitled bytes are what move into
	// the library (mainSource below), and the untouched original is dropped
	// only after that move succeeds. If a concurrent job claims
	// pl.DestMainPath while ffmpeg is running, the Move fails with
	// ErrDestinationExists and we skip with pl.MainSourcePath still
	// byte-for-byte untouched -- the "a duplicate skip never mutates the
	// source" contract now holds across processes, not just within one.
	//
	// The stat recheck just below still short-circuits the common case (a
	// sibling already claimed the destination between Plan and here) without
	// spawning ffmpeg at all; it's an optimization now, not the safety net.
	//
	// The extension gate deliberately lives here rather than inside the
	// tagger: keeping it a plain skip (not a call) lets this block avoid
	// logging a spurious "applied" history event for formats the tagger
	// never touches, without needing a second sentinel-error return path.
	mainSource := pl.MainSourcePath
	// The embedded container "title" tag uses the resolution-free radix, so an
	// enabled append_resolution never pushes a "- 1080p" suffix into metadata.
	// MetadataTitle is empty on plans built before that field existed -- fall
	// back to DestRadix then.
	titleTag := pl.MetadataTitle
	if titleTag == "" {
		titleTag = pl.DestRadix
	}
	taggedTmp := ""
	if p.metaTagger != nil && metadata.SupportsExtension(pl.MainExt) {
		if _, statErr := os.Stat(pl.DestMainPath); statErr != nil {
			logConsoleInfo(p, logging.EventProcessorMetadataTitleWriteStarted,
				fmt.Sprintf("TAGGING  metadata title for %s (might take a moment)...", filepath.Base(pl.MainSourcePath)),
				logging.Fields{"path": pl.MainSourcePath, "title": titleTag})
			tmp, err := p.metaTagger.WriteTitleToFile(ctx, pl.MainSourcePath, titleTag)
			if err != nil {
				logConsoleWarn(p, logging.EventProcessorMetadataTitleWriteFailed,
					fmt.Sprintf("WARNING  metadata title tag not updated for %s", filepath.Base(pl.MainSourcePath)),
					err, logging.Fields{"path": pl.MainSourcePath, "title": titleTag})
				logWarnHistoryOnly(p, logging.EventProcessorMetadataTitleWriteFailed, err,
					logging.Fields{"path": pl.MainSourcePath, "title": titleTag})
			} else {
				taggedTmp = tmp
				mainSource = tmp
				logInfoHistoryOnly(p, logging.EventProcessorMetadataTitleWriteApplied, logging.Fields{
					"path": pl.MainSourcePath, "title": titleTag,
				})
			}
		}
	}
	// Clean up the retitled remux if anything returns before Move consumes
	// it (Move renames or copies+removes its source on success, so this is a
	// no-op once the move lands).
	if taggedTmp != "" {
		defer func() { _ = os.Remove(taggedTmp) }()
	}

	// Move main media first. When tagging succeeded, mainSource is taggedTmp:
	// a swallowed transfer.CleanupError (destination finalized, Move couldn't
	// unlink its own source) then warns with the .mmtag-tmp-* path, but the
	// deferred os.Remove(taggedTmp) below still clears it -- rare double-fault,
	// not a leak.
	if err := p.xfer.Move(ctx, mainSource, pl.DestMainPath); err != nil {
		if !handleCleanupError(p, err, "main", mainSource, pl.DestMainPath) {
			if transfer.IsDestinationUnavailable(err) {
				return Result{Plan: pl}, &DestinationUnavailableError{Category: pl.Category, Err: err}
			}
			if transfer.IsDestinationExists(err) {
				// Lost a race against another job/batch item that claimed
				// this destination after Plan's own duplicate check ran (see
				// pl.Duplicate above) but before this move -- treat it the
				// same as a Plan-time-detected duplicate rather than a hard
				// failure. pl.MainSourcePath is untouched either way (only
				// the discarded taggedTmp remux ever saw a write).
				return skipDuplicateResult(p, pl, duplicateSkippedByInput), nil
			}
			return Result{Plan: pl}, fmt.Errorf("move main media: %w", err)
		}
	}
	if taggedTmp != "" {
		// The retitled remux is now in the library; the drop-folder
		// original is redundant. Removing it here mirrors what Move does to
		// its own source on success -- a failure is post-finalize cleanup,
		// the same class as transfer.CleanupError, so it warns and carries
		// on rather than failing an already-applied move.
		if err := os.Remove(pl.MainSourcePath); err != nil {
			logWarn(p, logging.EventProcessorCleanupSourceFailed,
				fmt.Sprintf("main source not removed: %s -- %v", pl.MainSourcePath, err),
				err, logging.Fields{"cleanup_kind": "main", "src": pl.MainSourcePath, "dst": pl.DestMainPath})
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
		if err := os.MkdirAll(filepath.Dir(mv.Dest), 0o755); err != nil { //nolint:gosec // library dest dirs need group+other read for the media server
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
	matchPath := pl.DuplicateMatchPath
	if matchPath == "" {
		matchPath = pl.DestMainPath
	}
	reason := fmt.Sprintf("already in library: %s", matchPath)
	logInfoHistoryOnly(p, logging.EventProcessorInputSkippedDuplicate, logging.Fields{
		"input_path": pl.InputPath,
		"dest_path":  pl.DestMainPath,
		"match_path": matchPath,
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
