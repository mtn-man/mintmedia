package processor

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/Mtn-Man/mintmedia/internal/logging"
)

var reShowFolderYear = regexp.MustCompile(`^(?i)(.+?)\s*\((19\d{2}|20\d{2})\)\s*$`)

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
		return "", "", fmt.Errorf("read shows dir %q: %w", showsDir, err)
	}

	var (
		noYearFolder      string
		exactYearFolder   string
		yearFolders       []showFolderMatch
		matchedYearFolder []string
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

		base, year, ok := parseShowFolderYear(name)
		if !ok {
			continue
		}
		if normalizeFolderKey(base) != showKey {
			continue
		}

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

	// Rule 2: If input has a year, match exact year folder or create a new one.
	if showYear != "" {
		if exactYearFolder != "" {
			return exactYearFolder, showYear, nil
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

	// Rule 4: No matches; fall back to the plain show name.
	return showName, "", nil
}

func parseShowFolderYear(folder string) (base string, year string, ok bool) {
	m := reShowFolderYear.FindStringSubmatch(folder)
	if len(m) != 3 {
		return "", "", false
	}
	return strings.TrimSpace(m[1]), m[2], true
}

func normalizeFolderKey(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ToLower(s)
	return strings.Join(strings.Fields(s), " ")
}
