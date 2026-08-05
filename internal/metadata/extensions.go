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

// mp4FamilyExtensions are the supportedExtensions using the mov/mp4/m4v
// muxer family, which needs the -movflags use_metadata_tags AVOption for the
// title tag to actually persist; matroska (.mkv) has no such requirement.
var mp4FamilyExtensions = map[string]struct{}{
	".mp4": {},
	".m4v": {},
	".mov": {},
}

// needsMovMetadataFlag reports whether ext (including its leading dot)
// belongs to the mp4/mov muxer family.
func needsMovMetadataFlag(ext string) bool {
	_, ok := mp4FamilyExtensions[strings.ToLower(ext)]
	return ok
}
