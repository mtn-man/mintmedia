package processor

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/mtn-man/mintmedia/internal/logging"
	"github.com/mtn-man/mintmedia/internal/transfer"
)

var (
	reShowFolderQualifier = regexp.MustCompile(`^(?i)(.+?)\s*\((.+?)\)\s*$`)
	reFourDigitYear       = regexp.MustCompile(`^(19\d{2}|20\d{2})$`)
)

type showFolderMatch struct {
	name string
	year string
}

// resolveShowFolder decides the destination show folder based on the parsed show name/year
// and existing folders in ShowsDir. It returns the folder name to use (relative to ShowsDir),
// the resolved year (empty when organizing into a no-year folder), or an error.
func resolveShowFolder(p *processorImpl, showsDir, showName, showYear string) (string, string, error) {
	showKey := normalizeFolderKey(showName)
	if showKey == "" {
		return "", "", fmt.Errorf("show name is empty")
	}

	entries, err := os.ReadDir(showsDir)
	if err != nil {
		if transfer.IsDestinationUnavailable(err) {
			return "", "", &DestinationUnavailableError{Category: CategoryShow, Err: err}
		}
		return "", "", fmt.Errorf("read shows dir %q: %w", showsDir, err)
	}

	var (
		noYearFolder          string
		exactYearFolder       string
		yearFolders           []showFolderMatch
		matchedYearFolder     []string
		otherQualifiedFolders []string
	)

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if !info.IsDir() {
			continue
		}

		name := entry.Name()
		if normalizeFolderKey(name) == showKey {
			if noYearFolder == "" {
				noYearFolder = name
			}
			continue
		}

		base, qualifier, ok := parseShowFolderQualifier(name)
		if !ok {
			continue
		}
		if normalizeFolderKey(base) != showKey {
			continue
		}

		if !reFourDigitYear.MatchString(qualifier) {
			otherQualifiedFolders = append(otherQualifiedFolders, name)
			continue
		}

		year := qualifier
		yearFolders = append(yearFolders, showFolderMatch{name: name, year: year})
		matchedYearFolder = append(matchedYearFolder, name)

		if showYear != "" && strings.EqualFold(year, showYear) && exactYearFolder == "" {
			exactYearFolder = name
		}
	}

	// Rule 1: Always prefer a no-year folder when it exists.
	if noYearFolder != "" {
		return noYearFolder, "", nil
	}

	// Rule 2: If input has a year, match exact year folder, fall back to a
	// qualified-folder guess, or create a new one.
	if showYear != "" {
		if exactYearFolder != "" {
			return exactYearFolder, showYear, nil
		}
		if folder, ok, err := tryQualifiedFallback(p, showsDir, showName, otherQualifiedFolders); ok {
			return folder, "", err
		}
		warnPossibleDuplicateShowFolder(p, showsDir, entries, showName, showYear)
		return fmt.Sprintf("%s (%s)", showName, showYear), showYear, nil
	}

	// Rule 3: No input year; use a single matching year folder if unambiguous.
	if len(yearFolders) == 1 {
		return yearFolders[0].name, yearFolders[0].year, nil
	}
	if len(yearFolders) > 1 {
		msg := fmt.Sprintf("WARNING  multiple show folders match %q: %s; skipping", showName, strings.Join(matchedYearFolder, ", "))
		logConsoleWarn(p, logging.EventProcessorInputSkippedParseError, msg, ErrAmbiguousShow, logging.Fields{
			"path":   showsDir,
			"reason": ErrAmbiguousShow.Error(),
		})
		return "", "", ErrAmbiguousShow
	}

	// Rule 4: No year-based match; fall back to a qualified-folder guess.
	if folder, ok, err := tryQualifiedFallback(p, showsDir, showName, otherQualifiedFolders); ok {
		return folder, "", err
	}

	// Rule 5: No matches at all; fall back to the plain show name.
	warnPossibleDuplicateShowFolder(p, showsDir, entries, showName, "")
	return showName, "", nil
}

// tryQualifiedFallback resolves against existing folders that share this
// show's base name but carry a qualifier resolveShowFolder doesn't otherwise
// recognize (e.g. "The Office (UK)"), used only when nothing else matched.
// ok is false when there was nothing to fall back to, in which case the
// caller proceeds to its own default. When ok is true, err is non-nil only
// for the ambiguous (multiple-candidate) case.
func tryQualifiedFallback(p *processorImpl, showsDir, showName string, otherQualifiedFolders []string) (folder string, ok bool, err error) {
	switch len(otherQualifiedFolders) {
	case 0:
		return "", false, nil
	case 1:
		folder = otherQualifiedFolders[0]
		logWarn(p, logging.EventProcessorShowFolderQualifiedGuess,
			fmt.Sprintf("using best-effort match for %q: existing folder %q has an unrecognized qualifier", showName, folder),
			nil, logging.Fields{"path": showsDir, "folder": folder})
		return folder, true, nil
	default:
		msg := fmt.Sprintf("WARNING  multiple show folders match %q with unrecognized qualifiers: %s; skipping", showName, strings.Join(otherQualifiedFolders, ", "))
		logConsoleWarn(p, logging.EventProcessorInputSkippedParseError, msg, ErrAmbiguousShow, logging.Fields{
			"path":   showsDir,
			"reason": ErrAmbiguousShow.Error(),
		})
		return "", true, ErrAmbiguousShow
	}
}

func parseShowFolderQualifier(folder string) (base string, qualifier string, ok bool) {
	m := reShowFolderQualifier.FindStringSubmatch(folder)
	if len(m) != 3 {
		return "", "", false
	}
	return strings.TrimSpace(m[1]), m[2], true
}

// parseShowFolderYear is like parseShowFolderQualifier but only recognizes a
// trailing 4-digit year, not an arbitrary qualifier -- used where the caller
// needs to strip a year specifically (year is tracked and re-appended
// separately) without also stripping identity-bearing qualifiers like
// country tags.
func parseShowFolderYear(folder string) (base string, year string, ok bool) {
	base, qualifier, ok := parseShowFolderQualifier(folder)
	if !ok || !reFourDigitYear.MatchString(qualifier) {
		return "", "", false
	}
	return base, qualifier, true
}

func normalizeFolderKey(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ToLower(s)
	return strings.Join(strings.Fields(s), " ")
}

// splitShowFolderTitleYear strips a trailing "(YYYY)" qualifier from a
// folder name if present, treating any other qualifier (e.g. "(UK)") as
// part of the base title rather than a year signal -- unlike
// parseShowFolderYear, which discards the base entirely for a non-year
// qualifier, this is only used for fuzzy-title comparison, where the title
// portion should still be compared regardless of qualifier type.
func splitShowFolderTitleYear(name string) (base, year string) {
	base, qualifier, ok := parseShowFolderQualifier(name)
	if !ok {
		return name, ""
	}
	if reFourDigitYear.MatchString(qualifier) {
		return base, qualifier
	}
	return base, ""
}

// findFuzzyShowFolderMatches scans entries for folders whose title
// fuzzy-matches showName. Only called from resolveShowFolder's two
// "create a brand-new folder" branches, i.e. after every exact-match rule
// has already failed to resolve a folder -- so any fuzzy hit here is, by
// construction, not an exact-base match. Candidates with yearMatchDisagree
// (both sides have an explicit, differing year) are excluded: two explicit
// years disagreeing is evidence of a reboot/different show, not ambiguity.
func findFuzzyShowFolderMatches(entries []os.DirEntry, showName, showYear string) []string {
	key := normalizeTitleKey(showName)
	if key == "" {
		return nil
	}

	var matches []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		base, folderYear := splitShowFolderTitleYear(name)
		if normalizeTitleKey(base) != key {
			continue
		}
		if classifyYearMatch(folderYear, showYear) == yearMatchDisagree {
			continue
		}
		matches = append(matches, name)
	}
	return matches
}

// warnPossibleDuplicateShowFolder logs one non-blocking WARNING (never
// zero-to-many -- a single combined line naming every match) if
// findFuzzyShowFolderMatches finds anything, mirroring
// tryQualifiedFallback's existing "best-effort guess, proceed anyway"
// warning pattern above. It never changes the folder resolveShowFolder
// returns -- Shows only ever warn on a fuzzy match, never auto-reroute,
// since a wrong guess here would misfile an episode into another show's
// folder rather than just skip a single file.
func warnPossibleDuplicateShowFolder(p *processorImpl, showsDir string, entries []os.DirEntry, showName, showYear string) {
	matches := findFuzzyShowFolderMatches(entries, showName, showYear)
	if len(matches) == 0 {
		return
	}
	logWarn(p, logging.EventProcessorShowPossibleDuplicateFolder,
		fmt.Sprintf("possible duplicate show: %q may match existing folder(s): %s", showName, strings.Join(matches, ", ")),
		nil, logging.Fields{"path": showsDir, "show": showName, "candidates": strings.Join(matches, ", ")})
}
