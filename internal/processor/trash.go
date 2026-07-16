package processor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mtn-man/mintmedia/internal/paths"
)

func trashHomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return home, nil
}

// cleanupSourceDirIfSafe moves inputPath to Trash when it is safe to do so:
// it must be a directory, must be inside the drop folder, and must be on the
// same device as the trash directory so os.Rename succeeds without a copy.
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

	trashDir, err := resolvePreferredTrashDir(in)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(trashDir, 0o700); err != nil {
		return fmt.Errorf("ensure trash dir: %w", err)
	}
	if sameDevice, err := paths.SameDevice(in, trashDir); err != nil {
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

