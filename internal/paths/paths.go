package paths

import (
	"path/filepath"
	"strings"
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
