package processor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mtn-man/mintmedia/internal/logging"
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

// warnPossibleDuplicateMovieFolder logs a non-blocking WARNING when tier2's
// asymmetric year match makes an existing library folder a possible spelling
// match for movieTitle -- ambiguous rather than a confident duplicate (see
// findFuzzyMovieMatches), so Plan proceeds normally rather than skipping.
// Mirrors warnPossibleDuplicateShowFolder's shape for the same case on the
// show side.
func warnPossibleDuplicateMovieFolder(p *processorImpl, moviesDir, movieTitle string, tier2 []movieFuzzyMatch) {
	if len(tier2) == 0 {
		return
	}
	folders := make([]string, len(tier2))
	for i, m := range tier2 {
		folders[i] = m.folder
	}
	logWarn(p, logging.EventProcessorMovieDuplicateNotice,
		fmt.Sprintf("WARNING  possible duplicate movie: %q may match existing folder(s): %s", movieTitle, strings.Join(folders, ", ")),
		nil, logging.Fields{"movies_dir": moviesDir, "incoming": movieTitle, "candidates": strings.Join(folders, ", ")})
}

// resScan is the single-ReadDir view of a target folder used by
// resolution_aware duplicate detection -- shared by movies (scanned via
// scanMovieFolderForResolution, where the whole folder is one identity) and
// shows (scanned via scanShowFolderForResolution, where a season folder holds
// many episodes and matching entries are filtered by identity first). At most
// one field per existing file is populated; when several files fall in the
// same class the first by name wins (os.ReadDir returns entries sorted), so
// the result is deterministic.
type resScan struct {
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

// scanMovieFolderForResolution reads dir once and classifies the main-media
// files it holds relative to pl (pl.DestRadix, pl.MetadataTitle). mainExtSet
// is the configured main-media extension set (p.mainExtSet) -- membership in
// it, not equality with pl.MainExt, is what qualifies a file as a candidate,
// so a same movie in a different container (e.g. an existing ".mkv" against
// an incoming ".mp4") is still recognized as the same identity; container
// format was never part of a movie's identity. A non-media sidecar (a
// "-thumb.jpg", a ".srt") is still excluded, since it isn't in the set
// either way. A missing dir is not an error -- resScan{dirExists:false} is
// returned. A movie folder holds only one movie, so unlike
// scanShowFolderForResolution every same-extension file in dir is a
// candidate -- there's no separate identity pre-filter to apply.
func scanMovieFolderForResolution(dir string, pl *Plan, mainExtSet map[string]struct{}) (resScan, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return resScan{}, nil
		}
		if transfer.IsDestinationUnavailable(err) {
			return resScan{}, &DestinationUnavailableError{Category: pl.Category, Err: err}
		}
		return resScan{}, fmt.Errorf("readdir destination: %w", err)
	}

	sc := resScan{dirExists: true}
	for _, ent := range ents {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		ext := filepath.Ext(name)
		if !isExtInSet(ext, mainExtSet) {
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

// decideResolutionDuplicate applies the shared resolution_aware decision
// table (movies and shows alike) to a folder scan. incomingTagged is
// (pl.Resolution != ""). warn is a fully formatted, ready-to-log message, or
// "" when there is nothing to warn about. matchPath is the existing-library
// file the verdict points at, or "" for DuplicateNone / DuplicateSortAlong.
func decideResolutionDuplicate(sc resScan, incomingTagged bool) (v DuplicateKind, matchPath, warn string) {
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
