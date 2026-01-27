

// internal/processor/regex.go
package processor

import "regexp"

// Shared regex/casing globals used across the processor package.
//
// Keeping these in a dedicated file makes ownership clear and avoids
// "ghost dependencies" between plan.go and name.go.
//
// NOTE: These are intentionally conservative; refine via tests.

var (
	// Matches SxxEyy tokens (case-insensitive), e.g. "S01E02", "s1e2", "S21E100".
	reSeasonEpisode = regexp.MustCompile(`(?i)\bS(\d{1,2})E(\d{1,3})\b`)

	// Removes bracketed tags like "[EZTVx.to]" or "[YTS]".
	reBracketedTag = regexp.MustCompile(`\[[^\]]*\]`)

	// Accept years 1900-2099.
	reYear = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)

	// Language tag suffix (case-insensitive), e.g. ".en" at end of a stem.
	reLangTag = regexp.MustCompile(`(?i)\.([a-z]{2,3})$`)

)
