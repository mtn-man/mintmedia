// internal/processor/name.go
package processor

import (
	"fmt"
	"path/filepath"
	"regexp"
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

// --- categorization ---------------------------------------------------------

func determineCategoryFromName(name string) Category {
	if hasShowSeasonSignal(name) && hasShowEpisodeSignal(name) {
		return CategoryShow
	}
	return CategoryMovie
}

func hasShowSeasonSignal(name string) bool {
	return reSeasonEpisode.MatchString(name) ||
		reSeasonEpisodeX.MatchString(name) ||
		reSeasonRange.MatchString(name) ||
		reSeasonWordRange.MatchString(name) ||
		reSeasonWord.MatchString(name)
}

func hasShowEpisodeSignal(name string) bool {
	return reSeasonEpisode.MatchString(name) ||
		reSeasonEpisodeX.MatchString(name) ||
		reEpisodeWord.MatchString(name)
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

func parseShowFromName(blacklist []*regexp.Regexp, baseName string, fileName string) (showName, showYear string, season, episode int, err error) {
	if sn, sy, s, e, ok := parseShowOnce(blacklist, baseName); ok {
		return sn, sy, s, e, nil
	}
	if sn, sy, s, e, ok := parseShowOnce(blacklist, fileName); ok {
		return sn, sy, s, e, nil
	}
	if sn, sy, s, e, ok := parseShowCrossSeasonEpisode(blacklist, baseName, fileName); ok {
		return sn, sy, s, e, nil
	}
	return "", "", 0, 0, &ParseShowError{BaseName: baseName, FileName: fileName}
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

func parseShowOnce(blacklist []*regexp.Regexp, raw string) (showName, showYear string, season, episode int, ok bool) {
	season, seasonIdx, seasonOK := parseSeasonComponent(raw)
	episode, episodeIdx, episodeOK := parseEpisodeComponent(raw)
	if !seasonOK || !episodeOK {
		return "", "", 0, 0, false
	}

	// Allow season 00 and episode 00 (both used for specials).
	if season < 0 || episode < 0 {
		return "", "", 0, 0, false
	}

	titleCut := min(seasonIdx, episodeIdx)
	if titleCut <= 0 || titleCut > len(raw) {
		return "", "", 0, 0, false
	}

	// Everything before the season/episode marker.
	titlePart := raw[:titleCut]
	titlePart = cleanReleaseName(blacklist, titlePart)
	titlePart = strings.TrimSpace(titlePart)
	if titlePart == "" {
		return "", "", 0, 0, false
	}

	// If the title contains a year token, treat it as show year and remove it from the name.
	// Example: "Fallout 2024" => ShowName "Fallout", ShowYear "2024"
	if y := findYear(titlePart); y != "" {
		showYear = y
		titlePart = removeYearToken(titlePart, y)
		titlePart = strings.TrimSpace(titlePart)
	}

	if titlePart == "" {
		return "", "", 0, 0, false
	}

	showName = titleCaseSimple(titlePart)
	return showName, showYear, season, episode, true
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

func parseSeasonEpisode(raw string) (season, episode int, ok bool) {
	season, _, seasonOK := parseSeasonComponent(raw)
	episode, _, episodeOK := parseEpisodeComponent(raw)
	if !seasonOK || !episodeOK {
		return 0, 0, false
	}

	if season < 0 || episode < 0 {
		return 0, 0, false
	}

	return season, episode, true
}

func parseShowCrossSeasonEpisode(blacklist []*regexp.Regexp, baseName string, fileName string) (showName, showYear string, season, episode int, ok bool) {
	episode, episodeIdx, episodeOK := parseEpisodeComponent(fileName)
	if !episodeOK {
		return "", "", 0, 0, false
	}

	season, _, seasonOK := parseSeasonComponent(baseName)
	if !seasonOK {
		season, _, seasonOK = parseSeasonComponent(fileName)
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
// number. idx is the start of the whole match (not the capture group),
// since callers use it to slice the show title off before the marker.
func matchComponent(raw string, patterns []componentPattern) (value int, idx int, ok bool) {
	for _, p := range patterns {
		idxs := p.re.FindStringSubmatchIndex(raw)
		gi := p.group * 2
		if idxs == nil || gi+1 >= len(idxs) || idxs[gi] < 0 {
			continue
		}
		value = atoiSafe(raw[idxs[gi]:idxs[gi+1]])
		if value >= 0 {
			return value, idxs[0], true
		}
	}
	return 0, 0, false
}

func parseSeasonComponent(raw string) (season int, idx int, ok bool) {
	return matchComponent(raw, seasonPatterns)
}

func parseEpisodeComponent(raw string) (episode int, idx int, ok bool) {
	return matchComponent(raw, episodePatterns)
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

	// Apply blacklist removals
	for _, re := range blacklist {
		s = re.ReplaceAllString(s, " ")
	}

	// Collapse whitespace
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func findYear(raw string) string {
	m := reYear.FindStringSubmatch(raw)
	if len(m) == 2 {
		return m[1]
	}
	return ""
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
