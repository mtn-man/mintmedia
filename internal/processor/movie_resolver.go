package processor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
		// Not splitShowFolderTitleYear, deliberately: a non-year qualifier stays
		// part of the base, so "Blade Runner (Final Cut)" does not match an
		// incoming "Blade Runner". Stripping it would leave both sides year-less,
		// classify as yearMatchAgree, and skip a film the library does not have.
		// Shows can strip because a fuzzy hit there only warns.
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

// movieResScan is the single-ReadDir view of a target movie folder used by
// resolution_aware duplicate detection. At most one field per existing file is
// populated; when several files fall in the same class the first by name wins
// (os.ReadDir returns entries sorted), so the result is deterministic.
type movieResScan struct {
	dirExists bool

	// exactMatchPath: an existing file whose full stem (including any
	// " - <res>" qualifier) case-insensitively equals pl.DestRadix -- the same
	// movie at the same resolution, or (for an untagged incoming file) the same
	// untagged name.
	exactMatchPath string

	// untaggedSiblingPath: an existing file named exactly pl.MetadataTitle with
	// no " - <res>" qualifier -- a copy sorted before resolution_aware was
	// enabled, or hand-named.
	untaggedSiblingPath string

	// variantPath: an existing file that is the same movie at a *different*
	// resolution -- its stem with the trailing " - <res>" stripped equals
	// pl.MetadataTitle, but the stem itself carried a qualifier.
	variantPath string
}

// scanMovieFolderForResolution reads dir once and classifies the
// same-extension files it holds relative to pl (pl.DestRadix, pl.MetadataTitle,
// pl.MainExt). A missing dir is not an error -- movieResScan{dirExists:false}
// is returned. Error handling mirrors checkDuplicateWithResolution.
func scanMovieFolderForResolution(dir string, pl *Plan) (movieResScan, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return movieResScan{}, nil
		}
		if transfer.IsDestinationUnavailable(err) {
			return movieResScan{}, &DestinationUnavailableError{Category: pl.Category, Err: err}
		}
		return movieResScan{}, fmt.Errorf("readdir destination: %w", err)
	}

	sc := movieResScan{dirExists: true}
	for _, ent := range ents {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		ext := filepath.Ext(name)
		if !strings.EqualFold(ext, pl.MainExt) {
			continue
		}
		rawStem := strings.TrimSuffix(name, ext)
		stripped := stripTrailingResolution(rawStem)
		full := filepath.Join(dir, name)

		switch {
		case strings.EqualFold(rawStem, pl.DestRadix):
			if sc.exactMatchPath == "" {
				sc.exactMatchPath = full
			}
		case stripped == rawStem && strings.EqualFold(rawStem, pl.MetadataTitle):
			if sc.untaggedSiblingPath == "" {
				sc.untaggedSiblingPath = full
			}
		case stripped != rawStem && strings.EqualFold(stripped, pl.MetadataTitle):
			if sc.variantPath == "" {
				sc.variantPath = full
			}
		}
	}
	return sc, nil
}

// decideMovieResolutionDuplicate applies the resolution_aware movie decision
// table to a folder scan. incomingTagged is (pl.Resolution != ""). warn is a
// fully formatted, ready-to-log message, or "" when there is nothing to warn
// about. matchPath is the existing-library file the verdict points at, or ""
// for DuplicateNone / DuplicateSortAlong.
func decideMovieResolutionDuplicate(sc movieResScan, incomingTagged bool) (v DuplicateKind, matchPath, warn string) {
	switch {
	case sc.exactMatchPath != "":
		return DuplicateExact, sc.exactMatchPath, ""
	case incomingTagged && sc.untaggedSiblingPath != "":
		return DuplicateSortAlong, "", fmt.Sprintf(
			"possible duplicate: untagged copy %q already in this folder -- sorting the tagged release in alongside it",
			filepath.Base(sc.untaggedSiblingPath))
	case incomingTagged && sc.variantPath != "":
		return DuplicateSortAlong, "", ""
	case !incomingTagged && sc.variantPath != "":
		return DuplicateReviewHold, sc.variantPath, fmt.Sprintf(
			"untagged release: folder already holds a resolution-tagged copy (%s) -- left for human review",
			filepath.Base(sc.variantPath))
	default:
		return DuplicateNone, "", ""
	}
}
