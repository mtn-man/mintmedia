package paths

import (
	"path/filepath"
	"strings"
)

// MaxDepth is the maximum directory depth relative to a scan root.
// Depth 0 is the root directory itself; depth 1 is a direct child.
const MaxDepth = 6

// DirDepthFromRoot returns the directory depth for dir relative to root.
// ok is false if dir is outside root or inputs are invalid.
func DirDepthFromRoot(root, dir string) (depth int, ok bool) {
	root = filepath.Clean(root)
	dir = filepath.Clean(dir)
	if root == "" || dir == "" {
		return 0, false
	}

	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return 0, false
	}
	rel = filepath.Clean(rel)
	if rel == "." {
		return 0, true
	}

	sep := string(filepath.Separator)
	if rel == ".." || strings.HasPrefix(rel, ".."+sep) {
		return 0, false
	}

	depth = len(strings.Split(rel, sep))
	return depth, true
}

// WithinMaxDepth reports whether dir is within maxDepth of root.
func WithinMaxDepth(root, dir string, maxDepth int) bool {
	depth, ok := DirDepthFromRoot(root, dir)
	return ok && depth <= maxDepth
}
