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
	// Tolerates a single space or period between the season and episode halves
	// (e.g. "S01 E01", "S5.E1"), both release-naming variants seen in the wild
	// alongside the tighter "S01E01" form. The period case can't collide with
	// reSeasonWord's "Season" match -- that requires digits immediately after
	// a literal "S", so "Season.5" never reaches this pattern at all. The
	// third, optional group recognizes a single trailing split-part letter
	// directly after the episode digits with no boundary of its own (e.g.
	// "S03E04a" -- an episode released as two physical files). A following
	// digit, or a second "E" (multi-episode packs, e.g. "S02E01E02"), is
	// still rejected: (?i) makes [a-z] match "E"/"e" too, so it also (and
	// correctly) tries to consume a second "E", which then fails its own
	// trailing \b against the digit right after -- refusing to guess at
	// multi-episode packs via this group rather than silently keeping only
	// the first episode. reSeasonEpisodeRange, tried first, already handles
	// that shape properly as a real range.
	reSeasonEpisode = regexp.MustCompile(`(?i)\bS(\d{1,2})[ .]?E(\d{1,3})([a-z]?)\b`)

	// Matches multi-episode range tokens, e.g. "S01E12-E13", "S01E12E13",
	// "s1e2-e3". The dash is optional so both the hyphenated and concatenated
	// forms match. Tried before reSeasonEpisode in the single-episode path so
	// a range is captured whole rather than silently truncated to its first
	// episode.
	reSeasonEpisodeRange = regexp.MustCompile(`(?i)\bS(\d{1,2}) ?E(\d{1,3})-?E(\d{1,3})\b`)

	// Matches two full SxxEyy tokens in a row separated by release-name
	// punctuation, e.g. "S01E02.S01E03", "S01E02 - S01E03" -- the shape a
	// double-episode file gets when both season and episode are repeated in
	// full rather than sharing a single "S01E12-E13" prefix. Go's RE2-based
	// regexp package has no backreferences, so "same season on both sides"
	// can't be expressed in the pattern itself -- both seasons are captured
	// separately and compared in code (parseEpisodeRangeComponent), the same
	// place the existing end<=start range is already rejected. The separator
	// is deliberately bounded to punctuation/whitespace only (not arbitrary
	// text), so this can't reach across real title or episode-name text to a
	// later, unrelated SxxEyy token.
	reSeasonEpisodeRepeatedRange = regexp.MustCompile(`(?i)\bS(\d{1,2})[ .]?E(\d{1,3})[\s._-]+S(\d{1,2})[ .]?E(\d{1,3})\b`)

	// Matches exactly two full SxxEyy tokens joined by "&" or the word
	// "and", e.g. "S01E01 & S01E02", "S01E01 and S01E02". Unlike the
	// dash/dot form above (an inclusive span -- "E12-E15" means episodes 12
	// through 15 even though only the endpoints are written), "&"/"and"
	// names two specific episodes, so parseEpisodeRangeComponent only
	// accepts this when they're strictly consecutive -- "S01E01 & S01E03"
	// skips episode 2 and must be refused (see detectRefusedMultiEpisode)
	// rather than misrepresented as spanning it.
	reSeasonEpisodeAmpersandPair = regexp.MustCompile(`(?i)\bS(\d{1,2})[ .]?E(\d{1,3})\s*(?:&|and)\s*S(\d{1,2})[ .]?E(\d{1,3})\b`)

	// Matches three-or-more full SxxEyy tokens chained together, joined by
	// any separator family the two-token forms above recognize (whitespace/
	// dot/dash/underscore, or "&"/"and"). A real episode list, not a range
	// -- the episode/episodeEnd model only has two endpoints, so this is
	// always refused (detectRefusedMultiEpisode) regardless of whether the
	// chain happens to be contiguous. Deliberately has no captures inside
	// the repeating group: this is a purely structural "does a chain of 3+
	// exist" check, not a value extraction, and RE2 only keeps the last
	// iteration of a repeated capture group anyway.
	reMultiEpisodeChain = regexp.MustCompile(`(?i)\bS\d{1,2}[ .]?E\d{1,3}(?:(?:[\s._-]+|\s*&\s*|\s+and\s+)S\d{1,2}[ .]?E\d{1,3}){2,}\b`)

	// Matches exactly two chained NxNN episodes of the same season, e.g.
	// "1x02x03" -- the x-form's own concatenated-token convention (there's
	// only one season digit in the whole token, shared by both episodes, so
	// unlike the SxxEyy forms above there's no separate "mismatched season"
	// case to guard against here). Uses the same custom non-digit boundary
	// as reSeasonEpisodeX rather than \b, for the same underscore-delimiter
	// reason documented there. Same rule as reSeasonEpisodeAmpersandPair:
	// parseEpisodeRangeComponent only accepts this when the two episodes
	// are strictly consecutive -- "1x02x04" skips episode 3 and must be
	// refused rather than misrepresented as a contiguous range.
	reSeasonEpisodeXPair = regexp.MustCompile(`(?i)(?:^|[^0-9x])([0-9]{1,2})x([0-9]{2,3})x([0-9]{2,3})(?:[^0-9]|$)`)

	// Matches a season followed by three-or-more chained "xNN" episode
	// segments, e.g. "1x02x03x04". Checked before reSeasonEpisodeXPair is
	// ever tried (detectRefusedMultiEpisode) -- without that ordering, a
	// 4-token chain's tail would read as a valid two-episode pair starting
	// from its second token ("02x03x04"), misreading an episode number as
	// a season number rather than being refused. Purely structural, no
	// captures needed, same reasoning as reMultiEpisodeChain.
	reMultiEpisodeXChain = regexp.MustCompile(`(?i)(?:^|[^0-9x])[0-9]{1,2}(?:x[0-9]{2,3}){3,}(?:[^0-9]|$)`)

	// Matches season range tokens, e.g. "S01-S04", "S1-S4", "S01-04".
	reSeasonRange = regexp.MustCompile(`(?i)\bS(\d{1,2})\s*-\s*S?(\d{1,2})\b`)

	// Matches worded season ranges, e.g. "Season 1-4", "Seasons.01-04".
	reSeasonWordRange = regexp.MustCompile(`(?i)\bSeasons?\s*[\s._-]*(\d{1,2})\s*-\s*(\d{1,2})\b`)

	// Matches single worded season tokens, e.g. "Season 1", "Seasons.01".
	reSeasonWord = regexp.MustCompile(`(?i)\bSeasons?\s*[\s._-]*(\d{1,2})\b`)

	// Matches worded episode tokens, e.g. "Episode 1", "Episodes.010".
	reEpisodeWord = regexp.MustCompile(`(?i)\bEpisodes?\s*[\s._-]*(\d{1,3})\b`)

	// Matches "NxNN" season/episode tokens, e.g. "1x01", "12x345". Uses a
	// custom non-digit boundary rather than \b: old-school release names
	// often use underscores as delimiters (e.g. "show_-_1x01_-_title.avi"),
	// and underscore is a \w character, so \b would silently fail to match
	// at the digit/underscore transition. The [^0-9] boundary also rejects
	// resolution-style tokens like "1920x1080".
	reSeasonEpisodeX = regexp.MustCompile(`(?i)(?:^|[^0-9x])([0-9]{1,2})x([0-9]{2,3})(?:[^0-9]|$)`)

	// Matches bare concatenated SxxEyy digits with no separator, e.g. "201"
	// (season 2, episode 01), "514" (season 5, episode 14). Deliberately
	// ambiguous with numbered movie titles/catalog numbers (e.g. "101
	// Dalmations") -- callers must only use this once a trusted season
	// number is already known from other context (see
	// parseBareSeasonEpisode) and must never wire it into classification.
	//
	// Uses a custom non-alphanumeric boundary rather than \b, for the same
	// reason as reSeasonEpisodeX: underscore is a \w character, so \b
	// silently fails to match at an underscore/digit transition (e.g.
	// "show_-_201_-_title.avi"), even though old-school release names
	// commonly use underscores as delimiters. Excluding letters as well as
	// digits (not just digits, like reSeasonEpisodeX's boundary) keeps this
	// pattern from merging into an adjacent word or release tag, e.g.
	// "Season201.avi" or "720p".
	reBareSeasonEpisode = regexp.MustCompile(`(?:^|[^0-9A-Za-z])([1-9])(\d{2})(?:[^0-9A-Za-z]|$)`)

	// Removes bracketed tags like "[EZTVx.to]" or "[YTS]".
	reBracketedTag = regexp.MustCompile(`\[[^\]]*\]`)

	// Accept years 1900-2099.
	reYear = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)

	// Language tag suffix (case-insensitive), e.g. ".en" at end of a stem.
	reLangTag = regexp.MustCompile(`(?i)\.([a-z]{2,3})$`)

	// Matches a website advertisement prefix at the start of a release name,
	// e.g. "www.UIndex.org - " or "EZTVx.to - ". Requires a dash-style separator
	// after the domain to avoid false positives on show names with dotted tokens.
	// The separator must be whitespace-flanked (real ad prefixes always space the
	// dash out) -- without that, a title like "Brooklyn.Nine-Nine." false-matches
	// "Brooklyn.Nine-" as if "Brooklyn.Nine" were a domain and "-" its separator.
	reWebsitePrefix = regexp.MustCompile(`(?i)^(?:www\.)?[a-z0-9][a-z0-9-]*\.[a-z]{2,10}(?:\.[a-z]{2,3})?\s+[-–—]+\s+`)

	// Matches a hyphen flanked by word characters on both sides (e.g. "X-Men",
	// "Spider-Man"). Used to preserve compound-word hyphens while stripping
	// separator hyphens during release-name cleaning.
	reWordHyphen = regexp.MustCompile(`\b-\b`)

	// reResolutionSep collapses every run of non-alphanumeric characters to a
	// single space. detectResolution normalizes with this first so tokens that
	// share one separator ("720p.1080p") are both visible and \b works
	// regardless of the original delimiter (dot, underscore, bracket).
	reResolutionSep = regexp.MustCompile(`[^0-9A-Za-z]+`)

	// Matches an explicit "NNNNp" resolution token (run against a
	// separator-normalized string), e.g. "1080p", "2160P". Used by
	// detectResolution -- see name.go.
	reResolution = regexp.MustCompile(`(?i)\b(480|576|720|1080|1440|2160)p\b`)

	// Matches the "4k"/"uhd" resolution aliases. Only consulted when no
	// explicit reResolution token is present, so a redundant "2160p 4K UHD"
	// still yields a single "2160p".
	reResolution4K = regexp.MustCompile(`(?i)\b(4k|uhd)\b`)

	// Matches a "WxH" pixel-dimension pair, e.g. "1920x1080", "3840x2160". The
	// 3-4 digit groups keep it clear of "NxNN" season/episode tokens.
	reResolutionDims = regexp.MustCompile(`(?i)\b(\d{3,4})x(\d{3,4})\b`)

	// Matches a trailing " - <res>" qualifier appended by the resolution_aware
	// feature (canonical buckets only), used to recover the resolution-free
	// radix during duplicate detection -- see stripTrailingResolution.
	reTrailingResolution = regexp.MustCompile(`(?i)\s+-\s+(480|576|720|1080|1440|2160)p$`)
)
