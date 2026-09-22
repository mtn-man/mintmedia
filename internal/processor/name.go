// internal/processor/name.go
package processor

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var reRomanNumeral = regexp.MustCompile(`^(?i)(M{0,4})(CM|CD|D?C{0,3})(XC|XL|L?X{0,3})(IX|IV|V?I{0,3})$`)

// Common acronyms worth preserving even if input arrives in lowercase.
// "US" is handled separately (see titleCaseSimple) so we can avoid forcing
// uppercase in the middle of regular titles like "All of Us Strangers".
var titleCaseAcronyms = map[string]struct{}{
	"AI":   {},
	"CIA":  {},
	"DEA":  {},
	"EU":   {},
	"FBI":  {},
	"NASA": {},
	"NYC":  {},
	"UAE":  {},
	"UFC":  {},
	"UK":   {},
	"USA":  {},
	"WWE":  {},
}

var lowerTitleWords = map[string]struct{}{
	"a":    {},
	"an":   {},
	"and":  {},
	"as":   {},
	"at":   {},
	"by":   {},
	"for":  {},
	"from": {},
	"in":   {},
	"into": {},
	"of":   {},
	"on":   {},
	"or":   {},
	"the":  {},
	"to":   {},
	"vs":   {},
	"with": {},
}

// monthNameToNumber maps full and 3-letter month names (lowercase) to their
// calendar number, for parseAirDate's month-name date shape.
var monthNameToNumber = map[string]int{
	"jan": 1, "january": 1,
	"feb": 2, "february": 2,
	"mar": 3, "march": 3,
	"apr": 4, "april": 4,
	"may": 5,
	"jun": 6, "june": 6,
	"jul": 7, "july": 7,
	"aug": 8, "august": 8,
	"sep": 9, "september": 9,
	"oct": 10, "october": 10,
	"nov": 11, "november": 11,
	"dec": 12, "december": 12,
}

// --- categorization ---------------------------------------------------------

func determineCategoryFromName(name string) Category {
	if hasShowSeasonSignal(name) && hasShowEpisodeSignal(name) {
		return CategoryShow
	}
	if hasDatedEpisodeSignal(name) {
		return CategoryShow
	}
	return CategoryMovie
}

func hasShowSeasonSignal(name string) bool {
	return reSeasonEpisode.MatchString(name) ||
		reSeasonEpisodeRange.MatchString(name) ||
		reSeasonEpisodeX.MatchString(name) ||
		reSeasonRange.MatchString(name) ||
		reSeasonWordRange.MatchString(name) ||
		reSeasonWord.MatchString(name)
}

func hasShowEpisodeSignal(name string) bool {
	return reSeasonEpisode.MatchString(name) ||
		reSeasonEpisodeRange.MatchString(name) ||
		reSeasonEpisodeX.MatchString(name) ||
		reEpisodeWord.MatchString(name)
}

// hasDatedEpisodeSignal reports whether name carries a date-based episode
// identifier (see parseAirDate) -- the daily/talk-show naming convention,
// e.g. "The Daily Show.July.30.2021...". Counted in determineCategoryFromName
// as satisfying both the season and episode signal requirements at once,
// since a dated episode has no separate season/episode tokens to check.
func hasDatedEpisodeSignal(name string) bool {
	_, _, ok := parseAirDate(name)
	return ok
}

// airDatePattern pairs a date regex with a function that pulls year/month/day
// (as strings) out of its submatches, so parseAirDate can try several
// accepted input shapes uniformly.
type airDatePattern struct {
	re      *regexp.Regexp
	extract func(m []string) (year, month, day string, ok bool)
}

var airDatePatterns = []airDatePattern{
	{reDateISO, func(m []string) (string, string, string, bool) { return m[1], m[2], m[3], true }},
	{reDateEuropean, func(m []string) (string, string, string, bool) { return m[3], m[2], m[1], true }},
	{reDateMonthName, func(m []string) (string, string, string, bool) {
		month, ok := monthNameToNumber[strings.ToLower(m[1])]
		if !ok {
			return "", "", "", false
		}
		return m[3], fmt.Sprintf("%d", month), m[2], true
	}},
}

// parseAirDate locates a date-based episode identifier in raw and returns it
// normalized to canonical "YYYY-MM-DD", regardless of which accepted input
// shape (ISO, European/scene, or month-name) matched -- see reDateISO/
// reDateEuropean/reDateMonthName. idx is the match start (mirroring
// parseSeasonComponent/parseEpisodeComponent's contract), used by
// parseDatedShowOnce to slice the show title off before it. ok is false when
// no accepted shape matches, or the matched digits don't validate to a real
// day (1-31) / month (1-12) -- mostly redundant with the regexes' own
// enumerated ranges, kept as the same belt-and-suspenders check
// parseEpisodeRangeComponent already applies on top of its own captures.
func parseAirDate(raw string) (dateStr string, idx int, ok bool) {
	for _, p := range airDatePatterns {
		idxs := p.re.FindStringSubmatchIndex(raw)
		if idxs == nil {
			continue
		}
		m := make([]string, len(idxs)/2)
		for i := range m {
			if idxs[2*i] >= 0 {
				m[i] = raw[idxs[2*i]:idxs[2*i+1]]
			}
		}
		year, month, day, extractOK := p.extract(m)
		if !extractOK {
			continue
		}
		y, mo, d := atoiSafe(year), atoiSafe(month), atoiSafe(day)
		if y == 0 || mo < 1 || mo > 12 || d < 1 || d > 31 {
			continue
		}
		return fmt.Sprintf("%04d-%02d-%02d", y, mo, d), idxs[0], true
	}
	return "", 0, false
}

func determineCategoryFromNames(inputName, mainName string) Category {
	if determineCategoryFromName(inputName) == CategoryShow {
		return CategoryShow
	}
	if determineCategoryFromName(mainName) == CategoryShow {
		return CategoryShow
	}
	if hasShowSeasonSignal(inputName) && hasShowEpisodeSignal(mainName) {
		return CategoryShow
	}
	if hasShowSeasonSignal(mainName) && hasShowEpisodeSignal(inputName) {
		return CategoryShow
	}
	return CategoryMovie
}

func parseShowFromName(blacklist []*regexp.Regexp, baseName string, fileName string) (showName, showYear string, season, episode, episodeEnd int, episodePart string, err error) {
	// Checked once, up front, against both candidate strings -- covers all
	// three strategies below (including parseShowCrossSeasonEpisode, which
	// derives season/episode independently and would otherwise silently
	// reconstruct a refused shape parseShowOnce just declined to guess at).
	if detectRefusedMultiEpisode(baseName) || detectRefusedMultiEpisode(fileName) {
		return "", "", 0, 0, 0, "", &ParseShowError{BaseName: baseName, FileName: fileName}
	}
	if sn, sy, s, e, ee, ep, ok := parseShowOnce(blacklist, baseName); ok {
		return sn, sy, s, e, ee, ep, nil
	}
	if sn, sy, s, e, ee, ep, ok := parseShowOnce(blacklist, fileName); ok {
		return sn, sy, s, e, ee, ep, nil
	}
	if sn, sy, s, e, ok := parseShowCrossSeasonEpisode(blacklist, baseName, fileName); ok {
		return sn, sy, s, e, 0, "", nil
	}
	return "", "", 0, 0, 0, "", &ParseShowError{BaseName: baseName, FileName: fileName}
}

// deriveShowHintFromFolder attempts to extract a show name/year from a season-pack style folder.
// It returns ok=true when the folder name itself carries a season marker -- either a season range
// (e.g. "Season 1-5") or a single season (e.g. "Season 2"). For the single-season case, seasonOK is
// also set with the specific season number, letting callers force Show category and anchor
// bare-digit episode parsing (see parseBareSeasonEpisode) to a season already trusted from the
// folder, rather than re-deriving it from an ambiguous filename token.
func deriveShowHintFromFolder(blacklist []*regexp.Regexp, folderName string) (showName, showYear string, season int, seasonOK bool, ok bool) {
	raw := strings.TrimSpace(folderName)
	if raw == "" {
		return "", "", 0, false, false
	}

	isRange := reSeasonRange.MatchString(raw) || reSeasonWordRange.MatchString(raw)
	if !isRange {
		if m := reSeasonWord.FindStringSubmatch(raw); m != nil {
			season = atoiSafe(m[1])
			seasonOK = true
		} else {
			return "", "", 0, false, false
		}
	}

	// Remove season markers before cleaning.
	raw = reSeasonWordRange.ReplaceAllString(raw, " ")
	raw = reSeasonRange.ReplaceAllString(raw, " ")
	raw = reSeasonWord.ReplaceAllString(raw, " ")

	raw = cleanReleaseName(blacklist, raw)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", 0, false, false
	}

	// Extract and remove year token if present.
	if y := findYear(raw); y != "" {
		showYear = y
		raw = removeYearToken(raw, y)
		raw = strings.TrimSpace(raw)
	}

	if raw == "" {
		return "", "", 0, false, false
	}

	showName = titleCaseSimple(raw)
	return showName, showYear, season, seasonOK, true
}

// parseShowOnce is only ever called from parseShowFromName, which already
// checks detectRefusedMultiEpisode against both candidate strings before
// trying any strategy -- see that function's doc.
func parseShowOnce(blacklist []*regexp.Regexp, raw string) (showName, showYear string, season, episode, episodeEnd int, episodePart string, ok bool) {
	var seasonIdx, episodeIdx int
	var seasonOK, episodeOK bool
	if s, start, end, idx, _, rangeOK := parseEpisodeRangeComponent(raw); rangeOK {
		season, episode, episodeEnd = s, start, end
		seasonIdx, episodeIdx = idx, idx
		seasonOK, episodeOK = true, true
	} else {
		season, seasonIdx, _, seasonOK = parseSeasonComponent(raw)
		episode, episodeIdx, _, episodeOK = parseEpisodeComponent(raw)
		if episodeOK {
			episodePart = episodePartLetter(raw)
		}
	}
	if !seasonOK || !episodeOK {
		return "", "", 0, 0, 0, "", false
	}

	// Allow season 00 and episode 00 (both used for specials).
	if season < 0 || episode < 0 {
		return "", "", 0, 0, 0, "", false
	}

	titleCut := min(seasonIdx, episodeIdx)
	if titleCut <= 0 || titleCut > len(raw) {
		return "", "", 0, 0, 0, "", false
	}

	// Everything before the season/episode marker.
	titlePart := raw[:titleCut]
	titlePart = cleanReleaseName(blacklist, titlePart)
	titlePart = strings.TrimSpace(titlePart)
	if titlePart == "" {
		return "", "", 0, 0, 0, "", false
	}

	// If the title contains a year token, treat it as show year and remove it from the name.
	// Example: "Fallout 2024" => ShowName "Fallout", ShowYear "2024"
	if y := findYear(titlePart); y != "" {
		showYear = y
		titlePart = removeYearToken(titlePart, y)
		titlePart = strings.TrimSpace(titlePart)
	}

	if titlePart == "" {
		return "", "", 0, 0, 0, "", false
	}

	showName = titleCaseSimple(titlePart)
	return showName, showYear, season, episode, episodeEnd, episodePart, true
}

// isLeadingBareToken reports whether fileName[:pos] contains nothing but release-noise --
// bracketed tags and separator punctuation -- meaning a match starting at pos is effectively
// the first real content in the name: the shape of a movie catalog-number prefix (e.g.
// "[GRP] 101.Dalmations.1961...", or plain "101.Dalmations.1961..."). Genuine leading title
// text (e.g. "Alias 101 Dalmations 1961.mkv") is deliberately not treated as noise -- that's a
// narrower, accepted gap rather than risking false rejections of real embedded-year show titles.
func isLeadingBareToken(fileName string, pos int) bool {
	prefix := reBracketedTag.ReplaceAllString(fileName[:pos], "")
	return strings.Trim(prefix, " ._-") == ""
}

// parseBareSeasonEpisode interprets a bare concatenated SxxEyy token (e.g. "201" as season 2
// episode 01) in fileName. This form is inherently ambiguous with numbered movie titles/catalog
// numbers (e.g. "101 Dalmations"), so it is only ever accepted when hint carries a season number
// already trusted from the folder (see deriveShowHintFromFolder), and the extracted season must
// match it exactly -- this function never guesses a season on its own. As a secondary guard, a
// leading bare-digit token (allowing for release-noise like bracket tags before it) that is
// followed *later in the filename* by a year (the "101.Dalmations.1961..." shape) is rejected,
// since that shape reads as a movie catalog/sequence prefix rather than an embedded episode code.
// The year search is scoped to the text after the match specifically -- a year appearing before
// or overlapping the match (e.g. an unrelated upload-date tag like "[2020] 201 Title.avi") must
// not count, or it would falsely reject a legitimate, hint-confirmed episode.
//
// Known accepted gap: this still can't distinguish a movie's "number, title, year" shape from a
// show whose own episode title happens to mention a year in freeform text (e.g. "201 The Year
// 1969 Special.avi") -- both have a year somewhere after the leading digits. A precise fix would
// need to check that the year appears before any release-tag-like text (resolution, codec, etc.)
// rather than merely "somewhere after," which isn't implemented yet since a real-world example of
// the false-negative case hasn't been observed. TODO: tighten this if it turns out to matter.
//
// A filename can contain more than one bare 3-digit run (e.g. a stray number
// elsewhere in the title). Every run is checked, not just the first: if
// exactly one matches the trusted season, it's accepted; if more than one
// does, that's a genuine ambiguity and this refuses to guess, same as if none
// had matched.
func parseBareSeasonEpisode(hint showHint, fileName string) (season, episode int, ok bool) {
	if !hint.seasonOK {
		return 0, 0, false
	}

	matches := reBareSeasonEpisode.FindAllStringSubmatchIndex(fileName, -1)
	if matches == nil {
		return 0, 0, false
	}

	found := false
	for _, idxs := range matches {
		// Use the digit capture group's own bounds (idxs[2], idxs[5]), not the
		// full match bounds (idxs[0], idxs[1]) -- unlike \b, this regex's
		// custom boundary consumes a real character, so the full match can
		// extend one character beyond the digits themselves. Anchoring to the
		// digits directly keeps "leading" and "after the match" precise
		// regardless of what boundary character (if any) was consumed.
		if isLeadingBareToken(fileName, idxs[2]) && findYear(fileName[idxs[5]:]) != "" {
			continue
		}

		s := atoiSafe(fileName[idxs[2]:idxs[3]])
		if s != hint.season {
			continue
		}

		if found {
			// A second candidate also matches the trusted season -- ambiguous
			// which one is the real episode code, so refuse to guess.
			return 0, 0, false
		}
		season = s
		episode = atoiSafe(fileName[idxs[4]:idxs[5]])
		found = true
	}

	return season, episode, found
}

// tokenEnd is the end of whichever episode-token match was used -- the end-
// side counterpart to the title-cut logic elsewhere (which uses the token's
// *start*), letting a caller read whatever text trails the token, e.g. an
// already-clean embedded episode title. Named distinctly from episodeEnd
// (the range's end *episode number*) to avoid confusing the two.
func parseSeasonEpisode(raw string) (season, episode, episodeEnd int, episodePart string, tokenEnd int, ok bool) {
	if detectRefusedMultiEpisode(raw) {
		return 0, 0, 0, "", 0, false
	}

	if s, start, end, _, matchEnd, rangeOK := parseEpisodeRangeComponent(raw); rangeOK {
		return s, start, end, "", matchEnd, true
	}

	season, _, seasonEnd, seasonOK := parseSeasonComponent(raw)
	episode, _, episodeCompEnd, episodeOK := parseEpisodeComponent(raw)
	if !seasonOK || !episodeOK {
		return 0, 0, 0, "", 0, false
	}

	if season < 0 || episode < 0 {
		return 0, 0, 0, "", 0, false
	}

	return season, episode, 0, episodePartLetter(raw), max(seasonEnd, episodeCompEnd), true
}

// extractShowEpisodeTitle recognizes an already-clean trailing episode title
// on a show filename, e.g. "Bad Optics" from
// "Lanterns - S01E06 - Bad Optics.mkv", and returns it verbatim -- never
// re-cased, unlike showName. stem is the main file's basename with its
// extension already trimmed off.
//
// It re-parses stem independently of however season/episode/episodeEnd/
// episodePart were actually resolved (a folder hint, the cross-season
// fallback, or a bare-digit token all bypass the file's own name entirely):
// if stem's own parse doesn't produce an exact match against the values
// already resolved for pl, there is no title tail to read from this file's
// name, and ok is false.
//
// Deliberately strict rather than attempting general release-tag salvage:
// the tail must open with the " - " separator (the Plex/Jellyfin-recommended
// naming convention, which mintmedia's own output already follows -- a
// dot/underscore-style scene name like "S01E06.Bad.Optics.mkv" does not
// qualify), and once any resolution qualifier is set aside, it must contain
// no recognized release-tag junk at all -- any junk anywhere in it means ok
// is false rather than an attempted partial salvage.
func extractShowEpisodeTitle(
	blacklist []*regexp.Regexp,
	resolutionAware bool,
	resolution string,
	stem string,
	season, episode, episodeEnd int,
	episodePart string,
) (title string, ok bool) {
	s, e, ee, ep, tokenEnd, parseOK := parseSeasonEpisode(stem)
	if !parseOK || s != season || e != episode || ee != episodeEnd || ep != episodePart {
		return "", false
	}

	tail := stem[tokenEnd:]
	if !strings.HasPrefix(tail, resolutionSuffixSep) {
		return "", false
	}
	candidate := strings.TrimPrefix(tail, resolutionSuffixSep)

	if resolutionAware && resolution != "" {
		candidate = stripTrailingResolution(candidate)
	}

	if lastBlacklistMatchEnd(blacklist, candidate) >= 0 {
		return "", false
	}

	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return "", false
	}

	return candidate, true
}

// parseShowCrossSeasonEpisode is the fallback used when the season and
// episode are split across the folder and file names (e.g. folder "Season
// 01", file "12 - Title.mkv"). It deliberately does not attempt multi-episode
// range parsing: the file-side token here typically has no "S##" prefix at
// all (season comes from the folder), so reSeasonEpisodeRange wouldn't match
// it anyway. Accepted gap, same as parseBareSeasonEpisode's documented gaps.
func parseShowCrossSeasonEpisode(blacklist []*regexp.Regexp, baseName string, fileName string) (showName, showYear string, season, episode int, ok bool) {
	episode, episodeIdx, _, episodeOK := parseEpisodeComponent(fileName)
	if !episodeOK {
		return "", "", 0, 0, false
	}

	season, _, _, seasonOK := parseSeasonComponent(baseName)
	if !seasonOK {
		season, _, _, seasonOK = parseSeasonComponent(fileName)
	}
	if !seasonOK {
		return "", "", 0, 0, false
	}

	if episodeIdx <= 0 || episodeIdx > len(fileName) {
		return "", "", 0, 0, false
	}

	titlePart := fileName[:episodeIdx]
	titlePart = cleanReleaseName(blacklist, titlePart)
	titlePart = strings.TrimSpace(titlePart)
	if titlePart == "" {
		return "", "", 0, 0, false
	}

	if y := findYear(titlePart); y != "" {
		showYear = y
		titlePart = removeYearToken(titlePart, y)
		titlePart = strings.TrimSpace(titlePart)
	}
	if titlePart == "" {
		return "", "", 0, 0, false
	}

	showName = titleCaseSimple(titlePart)
	if showName == "" {
		return "", "", 0, 0, false
	}

	return showName, showYear, season, episode, true
}

// parseDatedShowOnce mirrors parseShowOnce's slice/clean/year-extract/
// title-case pipeline for a single candidate string, but keys the split on a
// date-based episode identifier (parseAirDate) instead of season/episode
// markers -- there is no season/episode to extract here, only a show name
// and a canonical air date.
func parseDatedShowOnce(blacklist []*regexp.Regexp, raw string) (showName, showYear, airDate string, ok bool) {
	dateStr, idx, dateOK := parseAirDate(raw)
	if !dateOK || idx <= 0 || idx > len(raw) {
		return "", "", "", false
	}

	titlePart := raw[:idx]
	titlePart = cleanReleaseName(blacklist, titlePart)
	titlePart = strings.TrimSpace(titlePart)
	if titlePart == "" {
		return "", "", "", false
	}

	// The date token (and its own year) is already sliced out of titlePart
	// above, so any year still found here is a genuine second marker (e.g.
	// an unrelated upload-year tag) -- not the air date's year. findYear
	// returns the *last* 19xx/20xx match in its input, so this ordering
	// matters: running it on the raw string before slicing out the date
	// would instead misread the date's own year as the show year.
	if y := findYear(titlePart); y != "" {
		showYear = y
		titlePart = removeYearToken(titlePart, y)
		titlePart = strings.TrimSpace(titlePart)
	}
	if titlePart == "" {
		return "", "", "", false
	}

	showName = titleCaseSimple(titlePart)
	if showName == "" {
		return "", "", "", false
	}
	return showName, showYear, dateStr, true
}

// parseDatedShowFromName is the date-identified counterpart to
// parseShowFromName: baseName then fileName, tried in the same order as
// parseShowFromName's own direct strategies. There is no cross-file fallback
// analogous to parseShowCrossSeasonEpisode -- a date token is self-sufficient
// within one filename and never needs anchoring from a sibling folder/file
// the way an ambiguous bare season/episode does.
func parseDatedShowFromName(blacklist []*regexp.Regexp, baseName string, fileName string) (showName, showYear, airDate string, ok bool) {
	if detectRefusedMultiEpisode(baseName) || detectRefusedMultiEpisode(fileName) {
		return "", "", "", false
	}
	if sn, sy, dt, dok := parseDatedShowOnce(blacklist, baseName); dok {
		return sn, sy, dt, true
	}
	if sn, sy, dt, dok := parseDatedShowOnce(blacklist, fileName); dok {
		return sn, sy, dt, true
	}
	return "", "", "", false
}

// componentPattern pairs a regex with the 1-based capture group that holds
// the digits to extract for that pattern.
type componentPattern struct {
	re    *regexp.Regexp
	group int
}

// Order matters: earlier patterns take priority when a raw string matches
// more than one (e.g. "Season.1-4.S01-S04" matches reSeasonWord and both
// range patterns -- reSeasonWord must win).
var (
	seasonPatterns = []componentPattern{
		{reSeasonEpisode, 1},
		{reSeasonEpisodeX, 1},
		{reSeasonWord, 1},
		{reSeasonRange, 1},
		{reSeasonWordRange, 1},
	}
	episodePatterns = []componentPattern{
		{reSeasonEpisode, 2},
		{reSeasonEpisodeX, 2},
		{reEpisodeWord, 1},
	}
)

// matchComponent tries each pattern in order, returning the digit value of
// the first pattern whose capture group parses to a valid (non-negative)
// number. idx is the start of the whole match (not the capture group), since
// callers use it to slice the show title off before the marker; end is the
// match's end, used to find where a trailing episode title would begin.
func matchComponent(raw string, patterns []componentPattern) (value int, idx int, end int, ok bool) {
	for _, p := range patterns {
		idxs := p.re.FindStringSubmatchIndex(raw)
		gi := p.group * 2
		if idxs == nil || gi+1 >= len(idxs) || idxs[gi] < 0 {
			continue
		}
		value = atoiSafe(raw[idxs[gi]:idxs[gi+1]])
		if value >= 0 {
			return value, idxs[0], idxs[1], true
		}
	}
	return 0, 0, 0, false
}

func parseSeasonComponent(raw string) (season int, idx int, end int, ok bool) {
	return matchComponent(raw, seasonPatterns)
}

func parseEpisodeComponent(raw string) (episode int, idx int, end int, ok bool) {
	return matchComponent(raw, episodePatterns)
}

// detectRefusedMultiEpisode reports whether raw carries a multi-episode
// token shape mintmedia deliberately declines to guess at, rather than
// silently keeping only the first episode of a many-episode file (which the
// single-component fallback in parseShowOnce/parseSeasonEpisode would
// otherwise do). Both callers check this before attempting any other
// parsing. Each clause here is a distinct known-bad shape; more may be added
// over time as they're identified.
//
// reMultiEpisodeChain, reMultiEpisodeXChain, and reMultiEpisodeRangeChain are
// checked first, and must be: a 3+ chain's first two tokens (e.g.
// "S01E00 & S01E01" out of "S01E00 & S01E01 & S01E02", "02x03" out of
// "1x02x03x04", or "S01E12-E13" out of "S01E12-E13-E14") would otherwise read
// as a perfectly valid two-token pair to the clauses below -- for the x-form
// chain this is worse than just dropping an episode, since the second token
// gets misread as the season number entirely (see reMultiEpisodeXChain's
// doc).
func detectRefusedMultiEpisode(raw string) bool {
	if reMultiEpisodeChain.MatchString(raw) || reMultiEpisodeXChain.MatchString(raw) || reMultiEpisodeRangeChain.MatchString(raw) {
		return true
	}

	if idxs := reSeasonEpisodeRepeatedRange.FindStringSubmatchIndex(raw); idxs != nil {
		season1 := atoiSafe(raw[idxs[2]:idxs[3]])
		season2 := atoiSafe(raw[idxs[6]:idxs[7]])
		if season1 != season2 {
			// e.g. "S01E24.S02E01" -- a season finale immediately followed
			// by the next season's premiere, not a range. The pattern can't
			// express "same season" itself (no backreferences in RE2), so
			// this is validated here, same as parseEpisodeRangeComponent's
			// own end<=start check below.
			return true
		}
	}

	if idxs := reSeasonEpisodeAmpersandPair.FindStringSubmatchIndex(raw); idxs != nil {
		season1 := atoiSafe(raw[idxs[2]:idxs[3]])
		start := atoiSafe(raw[idxs[4]:idxs[5]])
		season2 := atoiSafe(raw[idxs[6]:idxs[7]])
		end := atoiSafe(raw[idxs[8]:idxs[9]])
		if season1 != season2 || end != start+1 {
			// "&"/"and" names two specific episodes rather than denoting an
			// inclusive span, so unlike the dash/dot form above, anything
			// but strict adjacency (e.g. "S01E01 & S01E03", which skips
			// episode 2) is refused rather than accepted as a range.
			return true
		}
	}

	if idxs := reSeasonEpisodeXPair.FindStringSubmatchIndex(raw); idxs != nil {
		start := atoiSafe(raw[idxs[4]:idxs[5]])
		end := atoiSafe(raw[idxs[6]:idxs[7]])
		if end != start+1 {
			// Same strict-adjacency rule as the "&"/"and" pair above --
			// "1x02x04" skips episode 3.
			return true
		}
	}

	return false
}

// parseEpisodeRangeComponent matches a multi-episode token (e.g. "S01E12-E13",
// "S01E12E13", "S01E12.S01E13", "S01E12 & S01E13", "1x12x13") in raw,
// returning the season and both episode numbers. Refuses to guess (ok=false)
// when the second number doesn't exceed the first -- e.g. a malformed
// "S03E13-E12" -- rather than reporting a nonsensical range. Callers check
// detectRefusedMultiEpisode before reaching here, so none of the shapes it
// refuses (mismatched-season pairs, non-consecutive "&"/"and"/x-form pairs,
// 3+ chains) ever fall through to the single-token components.
// matchEnd is the end of the whole match (distinct from end, the range's end
// *episode number* -- naming these the same would be a real trap for a
// caller trying to slice a trailing episode title off after the token).
func parseEpisodeRangeComponent(raw string) (season, start, end, idx, matchEnd int, ok bool) {
	if idxs := reSeasonEpisodeRange.FindStringSubmatchIndex(raw); idxs != nil {
		season = atoiSafe(raw[idxs[2]:idxs[3]])
		start = atoiSafe(raw[idxs[4]:idxs[5]])
		end = atoiSafe(raw[idxs[6]:idxs[7]])
		if end > start {
			return season, start, end, idxs[0], idxs[1], true
		}
	}

	if idxs := reSeasonEpisodeRepeatedRange.FindStringSubmatchIndex(raw); idxs != nil {
		season1 := atoiSafe(raw[idxs[2]:idxs[3]])
		start = atoiSafe(raw[idxs[4]:idxs[5]])
		season2 := atoiSafe(raw[idxs[6]:idxs[7]])
		end = atoiSafe(raw[idxs[8]:idxs[9]])
		if season1 == season2 && end > start {
			return season1, start, end, idxs[0], idxs[1], true
		}
	}

	if idxs := reSeasonEpisodeAmpersandPair.FindStringSubmatchIndex(raw); idxs != nil {
		season1 := atoiSafe(raw[idxs[2]:idxs[3]])
		start = atoiSafe(raw[idxs[4]:idxs[5]])
		season2 := atoiSafe(raw[idxs[6]:idxs[7]])
		end = atoiSafe(raw[idxs[8]:idxs[9]])
		if season1 == season2 && end == start+1 {
			return season1, start, end, idxs[0], idxs[1], true
		}
	}

	if idxs := reSeasonEpisodeXPair.FindStringSubmatchIndex(raw); idxs != nil {
		season = atoiSafe(raw[idxs[2]:idxs[3]])
		start = atoiSafe(raw[idxs[4]:idxs[5]])
		end = atoiSafe(raw[idxs[6]:idxs[7]])
		if end == start+1 {
			return season, start, end, idxs[0], idxs[1], true
		}
	}

	return 0, 0, 0, 0, 0, false
}

func parseMovieFromName(blacklist []*regexp.Regexp, baseName string, fileName string) (title string, year string, err error) {
	return parseMovieFromNameWithMode(blacklist, baseName, fileName, movieParseFolderFirst)
}

type movieParseMode int

const (
	// folder-first parsing preserves existing behavior for single-movie inputs.
	movieParseFolderFirst movieParseMode = iota
	// file-only parsing is used for multi-movie packs to avoid folder-name bleed.
	movieParseFileOnly
)

func parseMovieFromNameWithMode(
	blacklist []*regexp.Regexp,
	baseName string,
	fileName string,
	mode movieParseMode,
) (title string, year string, err error) {
	if mode == movieParseFileOnly {
		if t, y, ok := parseMovieOnce(blacklist, fileName); ok {
			return t, y, nil
		}
		return "", "", &ParseMovieError{BaseName: baseName, FileName: fileName}
	}

	if t, y, ok := parseMovieOnce(blacklist, baseName); ok {
		return t, y, nil
	}
	if t, y, ok := parseMovieOnce(blacklist, fileName); ok {
		return t, y, nil
	}
	return "", "", &ParseMovieError{BaseName: baseName, FileName: fileName}
}

func parseMovieOnce(blacklist []*regexp.Regexp, raw string) (title string, year string, ok bool) {
	year = findYear(raw)

	// Remove extension if present.
	raw = strings.TrimSuffix(raw, filepath.Ext(raw))

	// If year exists, keep only portion before the year occurrence for the title.
	// Year is always ASCII digits -- no case fold needed.
	if year != "" {
		if yidx := strings.Index(raw, year); yidx > 0 {
			raw = raw[:yidx]
		}
		raw = trimRightJunk(raw) // remove dangling "(" etc.
	}

	raw = cleanReleaseName(blacklist, raw)
	raw = trimRightJunk(raw) // remove trailing junk after cleaning
	if raw == "" {
		return "", "", false
	}

	title = titleCaseSimple(raw)
	return title, year, true
}

func cleanReleaseName(blacklist []*regexp.Regexp, raw string) string {
	s := raw

	// Remove bracketed tags like [EZTVx.to]
	s = reBracketedTag.ReplaceAllString(s, " ")

	// Strip website prefix like "www.UIndex.org - " before dots are replaced.
	s = reWebsitePrefix.ReplaceAllLiteralString(s, "")

	// Replace separators with spaces, but preserve hyphens between word characters
	// (e.g. "X-Men", "Spider-Man"). In torrent release names, hyphens within the
	// title portion are compound-word punctuation; dots/underscores are the actual
	// word separators. Placeholder avoids the need for lookaheads (RE2 limitation).
	s = strings.NewReplacer(".", " ", "_", " ").Replace(s)
	s = reWordHyphen.ReplaceAllLiteralString(s, "\x00")
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "\x00", "-")

	// Once title text gives way to a genuine trailing run of quality/source/
	// codec metadata, whatever follows is never real title content, whether
	// or not it's itself a recognized tag -- so truncate at the start of that
	// run, the same truncation the year already gets. Normally masked because
	// a year truncates the title before this ever runs; without a year (the
	// only case this matters for), a bare non-bracketed release-group tag
	// (e.g. "YIFY") would otherwise survive into the title untouched.
	if cut := trailingReleaseTagStart(blacklist, s); cut >= 0 {
		s = s[:cut]
	}

	// Apply blacklist removals
	for _, re := range blacklist {
		s = re.ReplaceAllString(s, " ")
	}

	// Collapse whitespace
	s = strings.Join(strings.Fields(s), " ")
	return s
}

// lastBlacklistMatchEnd returns the end index (in s) of whichever blacklist
// pattern's match ends furthest to the right, or -1 if none match. Used only
// as a boolean "does any junk appear anywhere in s" check by
// extractShowEpisodeTitle -- for truncating a title at a trailing run of
// release metadata, see trailingReleaseTagStart instead.
func lastBlacklistMatchEnd(blacklist []*regexp.Regexp, s string) int {
	end := -1
	for _, re := range blacklist {
		locs := re.FindAllStringIndex(s, -1)
		if len(locs) == 0 {
			continue
		}
		if last := locs[len(locs)-1][1]; last > end {
			end = last
		}
	}
	return end
}

// reTrailingToken splits a string into whitespace-delimited tokens while
// keeping each token's byte offset, so trailingReleaseTagStart can walk them
// backward and still report a cut position in the original string.
var reTrailingToken = regexp.MustCompile(`\S+`)

// matchesAnyBlacklist reports whether any blacklist pattern matches
// somewhere within tok (not necessarily the whole token -- a compound token
// like "x264-GROUP" still counts as a match on "x264").
func matchesAnyBlacklist(blacklist []*regexp.Regexp, tok string) bool {
	for _, re := range blacklist {
		if re.MatchString(tok) {
			return true
		}
	}
	return false
}

// trailingReleaseTagStart returns the start index of a trailing run of
// release-tag metadata at the end of s (e.g. "1080p BrRip x264 YIFY"), or -1
// if s doesn't end in one. Unlike lastBlacklistMatchEnd -- used elsewhere only
// as a boolean "any junk anywhere" check -- this walks s backward token by
// token so an isolated blacklist word mid-title ("Remastered" in "The Great
// Escape Remastered Anniversary Cut") doesn't truncate real trailing title
// text that follows it. A single unrecognized trailing token is tolerated
// (the bare release-group-tag case, e.g. "YIFY" after "1080p.BrRip.x264") as
// long as a real blacklist match follows it further back; two unrecognized
// tokens in a row with no blacklist match yet means there's no metadata block
// here at all, just ordinary title text -- abort with no truncation.
func trailingReleaseTagStart(blacklist []*regexp.Regexp, s string) int {
	tokens := reTrailingToken.FindAllStringIndex(s, -1)
	sawBareToken, sawBlacklist, cut := false, false, -1
	for _, loc := range slices.Backward(tokens) {
		tok := s[loc[0]:loc[1]]
		if matchesAnyBlacklist(blacklist, tok) {
			sawBlacklist = true
			cut = loc[0]
			continue
		}
		if !sawBlacklist && !sawBareToken {
			sawBareToken = true
			continue
		}
		break
	}
	if !sawBlacklist {
		return -1
	}
	return cut
}

// findYear returns the release year embedded in raw, preferring the *last*
// 19xx/20xx-shaped match rather than the first. A title that itself embeds a
// year-looking number (e.g. "Blade.Runner.2049.2017.1080p...") matches both
// "2049" (part of the title) and "2017" (the real release year) -- since
// callers truncate the title at the matched year's index, picking the first
// match would both mis-assign the year and cut the real year (and everything
// meant to be discarded after it) off the title. Release-naming convention
// always places the true year immediately before the quality/source/codec
// block, and that block never produces an isolated 19xx/20xx-shaped run (a
// "2160p" token doesn't match -- the trailing "p" breaks \b), so the later
// match is always the more trustworthy one.
//
// Known limitation, accepted rather than fixed: this heuristic can misfire on
// the mirror-image shape, where the *first* year is the real one and a later,
// unrelated year appears further in (e.g. "Blade Runner 1982 Final Cut 2007
// BluRay" resolves year=2007, not the correct 1982). Filename-only parsing
// has no way to distinguish "a number that looks like a year but is really
// part of the title" from "a number that looks like a year and really is one"
// without a title database -- optimizing for one shape necessarily trades off
// against the other, and the "title embeds a year" shape this was written for
// is the more common one in practice.
func findYear(raw string) string {
	ms := reYear.FindAllStringSubmatch(raw, -1)
	if len(ms) == 0 {
		return ""
	}
	return ms[len(ms)-1][1]
}

// canonicalResolutions lists the resolution buckets detectResolution emits, in
// ascending order. Used to rank multiple "NNNNp" tokens and to bucket a "WxH"
// dimension pair by its height.
var canonicalResolutions = []int{480, 576, 720, 1080, 1440, 2160}

// detectResolution extracts a release resolution from raw and returns it in
// canonical "<height>p" form (e.g. "1080p"), or "" when none is found.
//
// raw must be a *pre-cleanup* name: cleanReleaseName and the media-tag
// blacklist both delete these tokens on the way to a clean title, so callers
// pass the untouched basename.
//
// An explicit "NNNNp" token is authoritative. "4k"/"uhd" and a "WxH" pair are
// lossy aliases, consulted only when no "NNNNp" token is present -- so a
// redundant "2160p 4K UHD" collapses to a single "2160p" and a contradictory
// "1080p 4K" keeps the explicit "1080p". When more than one distinct "NNNNp"
// token appears (a malformed name), the highest wins.
func detectResolution(raw string) string {
	norm := reResolutionSep.ReplaceAllString(raw, " ")

	if ms := reResolution.FindAllStringSubmatch(norm, -1); len(ms) > 0 {
		best := 0
		for _, m := range ms {
			if n := atoiSafe(m[1]); n > best {
				best = n
			}
		}
		if best > 0 {
			return fmt.Sprintf("%dp", best)
		}
	}

	if reResolution4K.MatchString(norm) {
		return "2160p"
	}

	if m := reResolutionDims.FindStringSubmatch(norm); m != nil {
		if b := heightToBucket(atoiSafe(m[2])); b > 0 {
			return fmt.Sprintf("%dp", b)
		}
	}

	return ""
}

// heightToBucket maps a pixel height to the largest canonical resolution
// bucket that does not exceed it (e.g. 2160 -> 2160, 1200 -> 1080), or 0 when
// the height is below the smallest bucket.
func heightToBucket(h int) int {
	bucket := 0
	for _, c := range canonicalResolutions {
		if h >= c {
			bucket = c
		}
	}
	return bucket
}

// pathStem returns path's filename without its extension -- the "sorted-name
// form" library-entry comparisons in this package share (see
// stripTrailingResolution for the resolution-suffix-aware variant).
func pathStem(path string) string {
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

// stripTrailingResolution removes a trailing " - <res>" qualifier (as appended
// by the resolution_aware feature) from a filename stem, leaving the
// resolution-free radix. A stem without such a suffix is returned unchanged.
func stripTrailingResolution(stem string) string {
	return reTrailingResolution.ReplaceAllString(stem, "")
}

func titleCaseSimple(s string) string {
	// Title casing with explicit acronym preservation.
	// Only roman numerals, "US" (context-sensitive), and the acronyms allowlist are kept uppercase.
	parts := strings.Fields(strings.TrimSpace(s))
	if len(parts) == 0 {
		return ""
	}

	caser := cases.Title(language.English)

	for i := range parts {
		tok := parts[i]
		prefix, core, suffix := tokenParts(tok)
		coreUp := strings.ToUpper(core)

		// Preserve roman numerals (e.g. II, IV, VIII, X).
		// tokenParts strips surrounding punctuation so "(IV)" is recognized as "IV".
		if len(coreUp) >= 2 && reRomanNumeral.MatchString(coreUp) {
			parts[i] = prefix + coreUp + suffix
			continue
		}

		// Preserve "US" only if explicitly uppercase in source, or if it appears
		// as a trailing suffix token (e.g. "Hells Kitchen us" => "Hells Kitchen US").
		// Also handles parenthesized form: "(US)" => "(US)".
		if coreUp == "US" {
			if core == coreUp || i == len(parts)-1 {
				parts[i] = prefix + "US" + suffix
				continue
			}
		}

		// Preserve allowlisted acronyms regardless of case.
		if _, ok := titleCaseAcronyms[coreUp]; ok {
			parts[i] = prefix + coreUp + suffix
			continue
		}

		parts[i] = caser.String(strings.ToLower(tok))
	}

	// Lowercase common title “small words” when not at the beginning or end.
	if len(parts) >= 3 {
		for i := 1; i < len(parts)-1; i++ {
			low := strings.ToLower(parts[i])
			if _, ok := lowerTitleWords[low]; !ok {
				continue
			}
			// Don't lowercase if this token is an intentional acronym/roman numeral we preserved.
			up := strings.ToUpper(parts[i])
			if parts[i] == up && isAllLetters(parts[i]) {
				continue
			}
			parts[i] = low
		}
	}

	return strings.Join(parts, " ")
}

// isAlphaNum reports whether r is an ASCII letter or digit.
func isAlphaNum(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
}

func isAllLetters(s string) bool {
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			continue
		}
		return false
	}
	return true
}

// tokenParts splits tok into its surrounding non-alphanumeric punctuation and
// its inner alphanumeric core. E.g. "(US)" → ("(", "US", ")").
// When tok has no surrounding punctuation, prefix and suffix are empty.
func tokenParts(tok string) (prefix, core, suffix string) {
	start := 0
	for start < len(tok) {
		if isAlphaNum(rune(tok[start])) {
			break
		}
		start++
	}
	end := len(tok)
	for end > start {
		if isAlphaNum(rune(tok[end-1])) {
			break
		}
		end--
	}
	return tok[:start], tok[start:end], tok[end:]
}

func padEpisode(ep int) string {
	if ep >= 100 {
		return fmt.Sprintf("%d", ep)
	}
	return fmt.Sprintf("%02d", ep)
}

// formatEpisodeTag renders the "E<nn>" portion of a show's destination
// radix -- "E<nn>-E<mm>" for a multi-episode range, or "E<nn><part>" for a
// split-part episode (e.g. "E04a"). episodePart is always "" whenever
// episodeEnd > 0 -- a range and a split part are mutually exclusive by
// construction -- so the append order below is safe without a branch.
func formatEpisodeTag(episode, episodeEnd int, episodePart string) string {
	tag := "E" + padEpisode(episode) + episodePart
	if episodeEnd > 0 {
		tag += "-E" + padEpisode(episodeEnd)
	}
	return tag
}

// episodePartLetter returns the lowercase split-part letter captured by
// reSeasonEpisode's third group (e.g. "a" in "S03E04a"), or "" when raw
// carries no SxxExx token or no such suffix. Safe to call independently:
// reSeasonEpisode is the highest-priority pattern in both seasonPatterns
// and episodePatterns, so whenever it matches raw at all, it is also the
// pattern that produced season and episode -- no risk of attributing a
// part letter from an unrelated, lower-priority match.
func episodePartLetter(raw string) string {
	m := reSeasonEpisode.FindStringSubmatch(raw)
	if m == nil || m[3] == "" {
		return ""
	}
	return strings.ToLower(m[3])
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func trimRightJunk(s string) string {
	s = strings.TrimSpace(s)
	// keep letters/digits; trim everything else
	return strings.TrimRightFunc(s, func(r rune) bool { return !isAlphaNum(r) })
}

func removeYearToken(s string, year string) string {
	// Remove the year as a standalone token.
	// We keep it simple: split into fields, drop exact matches, rejoin.
	parts := strings.Fields(s)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == year {
			continue
		}
		trimmed := strings.TrimFunc(p, func(r rune) bool { return !isAlphaNum(r) })
		if trimmed == year {
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, " ")
}
