package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// MaxDepth is the maximum directory depth relative to a scan root.
// Depth 0 is the root directory itself; depth 1 is a direct child.
const MaxDepth = 6

// RelComponents splits path into its path components relative to root.
// ok is false if path is outside root or inputs are invalid. A path equal
// to root itself yields a nil, zero-length parts slice.
func RelComponents(root, path string) (parts []string, ok bool) {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if root == "" || path == "" {
		return nil, false
	}

	rel, err := filepath.Rel(root, path)
	if err != nil {
		return nil, false
	}
	rel = filepath.Clean(rel)
	if rel == "." {
		return nil, true
	}

	sep := string(filepath.Separator)
	if rel == ".." || strings.HasPrefix(rel, ".."+sep) {
		return nil, false
	}

	return strings.Split(rel, sep), true
}

// DirDepthFromRoot returns the directory depth for dir relative to root.
// ok is false if dir is outside root or inputs are invalid.
func DirDepthFromRoot(root, dir string) (depth int, ok bool) {
	parts, ok := RelComponents(root, dir)
	if !ok {
		return 0, false
	}
	return len(parts), true
}

// WithinMaxDepth reports whether dir is within maxDepth of root.
func WithinMaxDepth(root, dir string, maxDepth int) bool {
	depth, ok := DirDepthFromRoot(root, dir)
	return ok && depth <= maxDepth
}

// SameDevice reports whether src and dst reside on the same filesystem
// device, so callers can decide whether os.Rename can succeed in place or a
// cross-device copy is required. src is Lstat'd rather than Stat'd because
// os.Rename itself never follows a symlink at the source -- it moves the
// directory entry, wherever that entry's own inode lives, not whatever it
// points to. dst is Stat'd since callers care about the real device
// underlying the destination directory, symlinks and all.
func SameDevice(src, dst string) (bool, error) {
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return false, fmt.Errorf("lstat %q: %w", src, err)
	}
	dstInfo, err := os.Stat(dst)
	if err != nil {
		return false, fmt.Errorf("stat %q: %w", dst, err)
	}

	srcStat, ok := srcInfo.Sys().(*syscall.Stat_t)
	if !ok || srcStat == nil {
		return false, fmt.Errorf("stat %q: missing syscall.Stat_t", src)
	}
	dstStat, ok := dstInfo.Sys().(*syscall.Stat_t)
	if !ok || dstStat == nil {
		return false, fmt.Errorf("stat %q: missing syscall.Stat_t", dst)
	}

	return srcStat.Dev == dstStat.Dev, nil
}
