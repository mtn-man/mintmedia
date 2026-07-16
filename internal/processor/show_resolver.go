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
		if folder, err, ok := tryQualifiedFallback(p, showsDir, showName, otherQualifiedFolders); ok {
			return folder, "", err
		}
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
	if folder, err, ok := tryQualifiedFallback(p, showsDir, showName, otherQualifiedFolders); ok {
		return folder, "", err
	}

	// Rule 5: No matches at all; fall back to the plain show name.
	return showName, "", nil
}

// tryQualifiedFallback resolves against existing folders that share this
// show's base name but carry a qualifier resolveShowFolder doesn't otherwise
// recognize (e.g. "The Office (UK)"), used only when nothing else matched.
// ok is false when there was nothing to fall back to, in which case the
// caller proceeds to its own default. When ok is true, err is non-nil only
// for the ambiguous (multiple-candidate) case.
func tryQualifiedFallback(p *processorImpl, showsDir, showName string, otherQualifiedFolders []string) (folder string, err error, ok bool) {
	switch len(otherQualifiedFolders) {
	case 0:
		return "", nil, false
	case 1:
		folder = otherQualifiedFolders[0]
		logWarn(p, logging.EventProcessorShowFolderQualifiedGuess,
			fmt.Sprintf("using best-effort match for %q: existing folder %q has an unrecognized qualifier", showName, folder),
			nil, logging.Fields{"path": showsDir, "folder": folder})
		return folder, nil, true
	default:
		msg := fmt.Sprintf("WARNING  multiple show folders match %q with unrecognized qualifiers: %s; skipping", showName, strings.Join(otherQualifiedFolders, ", "))
		logConsoleWarn(p, logging.EventProcessorInputSkippedParseError, msg, ErrAmbiguousShow, logging.Fields{
			"path":   showsDir,
			"reason": ErrAmbiguousShow.Error(),
		})
		return "", ErrAmbiguousShow, true
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
// trailing 4-digit year, not an arbitrary qualifier — used where the caller
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
