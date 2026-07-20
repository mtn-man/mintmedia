package processor

import (
	"fmt"
	"os"

	"github.com/mtn-man/mintmedia/internal/transfer"
)

// movieFuzzyMatch is an existing library folder whose normalized title
// matches an incoming movie's normalized title.
type movieFuzzyMatch struct {
	folder string // library folder name, e.g. "Survivor (2000)"
	year   string // "" if the folder carries no year
}

// findFuzzyMovieMatches scans moviesDir once and returns every existing
// library folder whose normalized title matches incomingTitle, split by
// whether the year is ambiguous enough to warrant a report:
//
//   - tier1 (confident duplicate): both sides have no year, or both have
//     the identical year.
//   - tier2 (possible duplicate): exactly one side has a year.
//   - neither: both sides have a year and they differ -- treated as strong
//     evidence of two different movies, not reported at all.
func findFuzzyMovieMatches(moviesDir, incomingTitle, incomingYear string) (tier1, tier2 []movieFuzzyMatch, err error) {
	key := normalizeTitleKey(incomingTitle)
	if key == "" {
		return nil, nil, nil
	}

	entries, err := os.ReadDir(moviesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		if transfer.IsDestinationUnavailable(err) {
			return nil, nil, &DestinationUnavailableError{Category: CategoryMovie, Err: err}
		}
		return nil, nil, fmt.Errorf("read movies dir %q: %w", moviesDir, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		base, folderYear, ok := parseShowFolderYear(name)
		if !ok {
			base, folderYear = name, ""
		}
		if normalizeTitleKey(base) != key {
			continue
		}

		match := movieFuzzyMatch{folder: name, year: folderYear}
		switch classifyYearMatch(folderYear, incomingYear) {
		case yearMatchAgree:
			tier1 = append(tier1, match)
		case yearMatchAsymmetric:
			tier2 = append(tier2, match)
		}
	}

	return tier1, tier2, nil
}
