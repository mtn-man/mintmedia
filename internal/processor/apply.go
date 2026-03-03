// internal/processor/apply.go
package processor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Mtn-Man/mintmedia/internal/logging"
	"github.com/Mtn-Man/mintmedia/internal/transfer"
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

	results := make([]Result, 0, len(plans))
	for _, pl := range plans {
		res, err := applyOne(ctx, p, pl, assocFailedByInput)
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

func applyOne(ctx context.Context, p *processorImpl, pl Plan, assocFailedByInput map[string]bool) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{Plan: pl}, err
	}

	if strings.TrimSpace(pl.MainSourcePath) == "" || strings.TrimSpace(pl.DestMainPath) == "" {
		return Result{Plan: pl}, fmt.Errorf("invalid plan: missing main source/dest path")
	}

	if p.xfer == nil {
		return Result{Plan: pl}, fmt.Errorf("processor misconfigured: Transferer is nil")
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(pl.DestDir, 0o755); err != nil {
		return Result{Plan: pl}, fmt.Errorf("create destination dir %q: %w", pl.DestDir, err)
	}

	// Move main media first
	if err := p.xfer.Move(ctx, pl.MainSourcePath, pl.DestMainPath); err != nil {
		if !handleCleanupError(p, err, "main", pl.MainSourcePath, pl.DestMainPath) {
			return Result{Plan: pl}, fmt.Errorf("move main media: %w", err)
		}
	}
	logInfoHistoryOnly(p, logging.EventProcessorMoveMainApplied, logging.Fields{
		"src":      pl.MainSourcePath,
		"dst":      pl.DestMainPath,
		"category": string(pl.Category),
	})

	// Move associated files best-effort
	for _, mv := range pl.Associated {
		if ctx.Err() != nil {
			return Result{Plan: pl, Applied: true}, ctx.Err()
		}
		if mv.Source == "" || mv.Dest == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(mv.Dest), 0o755); err != nil {
			return Result{Plan: pl, Applied: true}, fmt.Errorf("create associated dest dir %q: %w", filepath.Dir(mv.Dest), err)
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

	// Cleanup: move source directory to Trash if safe (only for directory inputs)
	if pl.DeleteEmptyInputDir {
		if pl.InputPath != "" && assocFailedByInput[pl.InputPath] {
			logWarn(p, logging.EventProcessorCleanupSkippedAssociatedFailed, fmt.Sprintf("CLEANUP SKIPPED: %s (reason=associated move failed)", pl.InputPath), nil, logging.Fields{
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
			logWarn(p, logging.EventProcessorCleanupSkippedFailed, fmt.Sprintf("CLEANUP SKIPPED: %s (reason=%v)", pl.InputPath, err), err, logging.Fields{
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

func cleanupSourceDirIfSafe(p *processorImpl, inputPath string) error {
	inputPath = strings.TrimSpace(inputPath)
	if inputPath == "" {
		return nil
	}

	st, err := os.Stat(inputPath)
	if err != nil {
		return nil
	}
	if !st.IsDir() {
		return nil
	}

	drop := filepath.Clean(p.cfg.DropFolder)
	in := filepath.Clean(inputPath)

	// Canonicalize paths to defend against symlink escape. If either cannot be resolved,
	// refuse trashing rather than risk moving outside the drop folder.
	dropReal, err := filepath.EvalSymlinks(drop)
	if err != nil {
		return fmt.Errorf("resolve drop folder symlinks: %w", err)
	}
	inReal, err := filepath.EvalSymlinks(in)
	if err != nil {
		return fmt.Errorf("resolve input path symlinks: %w", err)
	}
	drop = filepath.Clean(dropReal)
	in = filepath.Clean(inReal)

	if samePath(drop, in) {
		return fmt.Errorf("refusing to trash drop folder root: %s", in)
	}

	rel, err := filepath.Rel(drop, in)
	if err != nil {
		return fmt.Errorf("compute relative path: %w", err)
	}

	sep := string(os.PathSeparator)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+sep) {
		return fmt.Errorf("refusing to trash directory outside drop folder: %s", in)
	}

	trashDir, err := resolveTrashDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(trashDir, 0o700); err != nil {
		return fmt.Errorf("ensure trash dir: %w", err)
	}
	if sameDevice, err := sameDevice(in, trashDir); err != nil {
		return err
	} else if !sameDevice {
		return fmt.Errorf("cleanup skipped: drop folder and Trash are on different volumes")
	}

	return moveToTrashWithDir(in, trashDir)
}

func samePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

func moveToTrashWithDir(src, trashDir string) error {
	base := filepath.Base(src)
	if base == "" || base == "." || base == string(os.PathSeparator) {
		return fmt.Errorf("invalid trash base for %q", src)
	}

	for i := 0; i < 1000; i++ {
		name := base
		if i > 0 {
			name = fmt.Sprintf("%s %d", base, i+1)
		}
		dest := filepath.Join(trashDir, name)

		if _, err := os.Stat(dest); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat trash destination: %w", err)
		}

		renameErr := os.Rename(src, dest)
		if renameErr == nil {
			return nil
		}

		// If the destination appeared between stat and rename, try the next suffix.
		if _, err := os.Stat(dest); err == nil {
			continue
		}

		return fmt.Errorf("move to trash: %w", renameErr)
	}

	return fmt.Errorf("unable to find available trash name for %q", base)
}

func resolveTrashDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".Trash"), nil
}

func sameDevice(a, b string) (bool, error) {
	aInfo, err := os.Stat(a)
	if err != nil {
		return false, fmt.Errorf("stat source: %w", err)
	}
	bInfo, err := os.Stat(b)
	if err != nil {
		return false, fmt.Errorf("stat trash dir: %w", err)
	}
	aSys, ok := aInfo.Sys().(*syscall.Stat_t)
	if !ok || aSys == nil {
		return false, fmt.Errorf("stat source: unsupported file info")
	}
	bSys, ok := bInfo.Sys().(*syscall.Stat_t)
	if !ok || bSys == nil {
		return false, fmt.Errorf("stat trash dir: unsupported file info")
	}
	return aSys.Dev == bSys.Dev, nil
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
		fmt.Sprintf("CLEANUP WARN: %s source not removed: %s (err=%v)", kind, logSrc, logErr),
		logErr,
		logging.Fields{
			"cleanup_kind": kind,
			"src":          logSrc,
			"dst":          logDst,
		},
	)
	return true
}
