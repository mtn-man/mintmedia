// internal/metadata/extensions.go
package metadata

import "strings"

// supportedExtensions lists container formats known to support a clean
// title-tag rewrite via ffmpeg stream copy (remux, no re-encode). This is an
// implementation detail of how ffmpeg remuxing works per-container, not a
// user-configurable preference.
var supportedExtensions = map[string]struct{}{
	".mp4": {},
	".m4v": {},
	".mkv": {},
	".mov": {},
}

// SupportsExtension reports whether ext (including its leading dot) is a
// container format WriteTitle knows how to remux. An unsupported extension
// (e.g. ".avi", ".ts") is an expected, normal case, not a failure -- callers
// should skip silently rather than attempt the rewrite.
func SupportsExtension(ext string) bool {
	_, ok := supportedExtensions[strings.ToLower(ext)]
	return ok
}
