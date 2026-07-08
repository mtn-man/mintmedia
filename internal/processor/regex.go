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

	// Matches season range tokens, e.g. "S01-S04", "S1-S4", "S01-04".
	reSeasonRange = regexp.MustCompile(`(?i)\bS(\d{1,2})\s*-\s*S?(\d{1,2})\b`)

	// Matches worded season ranges, e.g. "Season 1-4", "Seasons.01-04".
	reSeasonWordRange = regexp.MustCompile(`(?i)\bSeasons?\s*[\s._-]*(\d{1,2})\s*-\s*(\d{1,2})\b`)

	// Matches single worded season tokens, e.g. "Season 1", "Seasons.01".
	reSeasonWord = regexp.MustCompile(`(?i)\bSeasons?\s*[\s._-]*(\d{1,2})\b`)

	// Matches worded episode tokens, e.g. "Episode 1", "Episodes.010".
	reEpisodeWord = regexp.MustCompile(`(?i)\bEpisodes?\s*[\s._-]*(\d{1,3})\b`)

	// Removes bracketed tags like "[EZTVx.to]" or "[YTS]".
	reBracketedTag = regexp.MustCompile(`\[[^\]]*\]`)

	// Accept years 1900-2099.
	reYear = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)

	// Language tag suffix (case-insensitive), e.g. ".en" at end of a stem.
	reLangTag = regexp.MustCompile(`(?i)\.([a-z]{2,3})$`)

	// Matches a website advertisement prefix at the start of a release name,
	// e.g. "www.UIndex.org - " or "EZTVx.to - ". Requires a dash-style separator
	// after the domain to avoid false positives on show names with dotted tokens.
	reWebsitePrefix = regexp.MustCompile(`(?i)^(?:www\.)?[a-z0-9][a-z0-9-]*\.[a-z]{2,10}(?:\.[a-z]{2,3})?\s*[-–—]+\s*`)

	// Matches a hyphen flanked by word characters on both sides (e.g. "X-Men",
	// "Spider-Man"). Used to preserve compound-word hyphens while stripping
	// separator hyphens during release-name cleaning.
	reWordHyphen = regexp.MustCompile(`\b-\b`)
)
