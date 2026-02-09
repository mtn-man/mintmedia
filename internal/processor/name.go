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

// Note: This file relies on shared, package-level helpers declared elsewhere in the
// processor package (e.g., reSeasonEpisode, reBracketedTag, and reYear).
// If you later remove those declarations from other files, move them here.

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
// It only returns ok=true when a season range marker is present.
func deriveShowHintFromFolder(blacklist []*regexp.Regexp, folderName string) (showName, showYear string, ok bool) {
	raw := strings.TrimSpace(folderName)
	if raw == "" {
		return "", "", false
	}

	if !reSeasonRange.MatchString(raw) && !reSeasonWordRange.MatchString(raw) {
		return "", "", false
	}

	// Remove season range markers before cleaning.
	raw = reSeasonWordRange.ReplaceAllString(raw, " ")
	raw = reSeasonRange.ReplaceAllString(raw, " ")

	raw = cleanReleaseName(blacklist, raw)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}

	// Extract and remove year token if present.
	if y := findYear(raw); y != "" {
		showYear = y
		raw = removeYearToken(raw, y)
		raw = strings.TrimSpace(raw)
	}

	if raw == "" {
		return "", "", false
	}

	showName = titleCaseSimple(raw)
	return showName, showYear, true
}

func parseShowOnce(blacklist []*regexp.Regexp, raw string) (showName, showYear string, season, episode int, ok bool) {
	season, seasonIdx, seasonOK := parseSeasonComponent(raw)
	episode, episodeIdx, episodeOK := parseEpisodeComponent(raw)
	if !seasonOK || !episodeOK {
		return "", "", 0, 0, false
	}

	// Allow episode 00 (specials), but keep season > 0.
	if season <= 0 || episode < 0 {
		return "", "", 0, 0, false
	}

	titleCut := seasonIdx
	if episodeIdx < titleCut {
		titleCut = episodeIdx
	}
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

func parseSeasonEpisode(raw string) (season, episode int, ok bool) {
	season, _, seasonOK := parseSeasonComponent(raw)
	episode, _, episodeOK := parseEpisodeComponent(raw)
	if !seasonOK || !episodeOK {
		return 0, 0, false
	}

	if season <= 0 || episode < 0 {
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

func parseSeasonComponent(raw string) (season int, idx int, ok bool) {
	if idxs := reSeasonEpisode.FindStringSubmatchIndex(raw); len(idxs) == 6 {
		season = atoiSafe(raw[idxs[2]:idxs[3]])
		if season > 0 {
			return season, idxs[0], true
		}
	}

	if idxs := reSeasonWord.FindStringSubmatchIndex(raw); len(idxs) == 4 {
		season = atoiSafe(raw[idxs[2]:idxs[3]])
		if season > 0 {
			return season, idxs[0], true
		}
	}

	if idxs := reSeasonRange.FindStringSubmatchIndex(raw); len(idxs) == 6 {
		season = atoiSafe(raw[idxs[2]:idxs[3]])
		if season > 0 {
			return season, idxs[0], true
		}
	}

	if idxs := reSeasonWordRange.FindStringSubmatchIndex(raw); len(idxs) == 6 {
		season = atoiSafe(raw[idxs[2]:idxs[3]])
		if season > 0 {
			return season, idxs[0], true
		}
	}

	return 0, 0, false
}

func parseEpisodeComponent(raw string) (episode int, idx int, ok bool) {
	if idxs := reSeasonEpisode.FindStringSubmatchIndex(raw); len(idxs) == 6 {
		episode = atoiSafe(raw[idxs[4]:idxs[5]])
		if episode >= 0 {
			return episode, idxs[0], true
		}
	}

	if idxs := reEpisodeWord.FindStringSubmatchIndex(raw); len(idxs) == 4 {
		episode = atoiSafe(raw[idxs[2]:idxs[3]])
		if episode >= 0 {
			return episode, idxs[0], true
		}
	}

	return 0, 0, false
}

func parseMovieFromName(blacklist []*regexp.Regexp, baseName string, fileName string) (title string, year string, err error) {
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
	if year != "" {
		low := strings.ToLower(raw)
		yidx := strings.Index(low, year)
		if yidx > 0 {
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

	// Replace separators with spaces
	s = strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(s)

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
	// Conservative title casing with acronym preservation.
	// We keep tokens that are already ALLCAPS (2-4 letters) as-is, and also preserve a small allowlist.
	parts := strings.Fields(strings.TrimSpace(s))
	if len(parts) == 0 {
		return ""
	}

	caser := cases.Title(language.English)

	// Common acronyms worth preserving even if input arrives in lowercase.
	// "US" is handled separately so we can avoid forcing uppercase in the middle
	// of regular titles like "All of Us Strangers".
	acronyms := map[string]struct{}{
		"UK":  {},
		"UAE": {},
		"EU":  {},
		"USA": {},
	}

	for i := range parts {
		tok := parts[i]
		up := strings.ToUpper(tok)

		// Preserve roman numerals (e.g. II, IV, VIII, X)
		if len(up) >= 2 && reRomanNumeral.MatchString(up) {
			parts[i] = up
			continue
		}

		// Preserve "US" only if explicitly uppercase in source, or if it appears
		// as a trailing suffix token (e.g. "Hells Kitchen us" => "Hells Kitchen US").
		if up == "US" {
			if tok == up || i == len(parts)-1 {
				parts[i] = up
				continue
			}
		}

		// Preserve allowlisted acronyms regardless of case.
		if _, ok := acronyms[up]; ok {
			parts[i] = up
			continue
		}

		// Preserve tokens that are already all-uppercase short acronyms (e.g. "US", "UFC").
		if len(tok) >= 2 && len(tok) <= 4 && tok == up && isAllLetters(tok) {
			parts[i] = tok
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

func isAllLetters(s string) bool {
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			continue
		}
		return false
	}
	return true
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
	return strings.TrimRightFunc(s, func(r rune) bool {
		// keep letters/digits; trim everything else
		isAlphaNum := (r >= '0' && r <= '9') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= 'a' && r <= 'z')
		return !isAlphaNum
	})
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
		trimmed := strings.TrimFunc(p, func(r rune) bool {
			isAlphaNum := (r >= '0' && r <= '9') ||
				(r >= 'A' && r <= 'Z') ||
				(r >= 'a' && r <= 'z')
			return !isAlphaNum
		})
		if trimmed == year {
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, " ")
}
