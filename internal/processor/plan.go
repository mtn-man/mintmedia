package processor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mtn-man/mintmedia/internal/logging"
	"github.com/mtn-man/mintmedia/internal/paths"
)

// plan is the internal implementation behind (*processorImpl).Plan().
// It is split out to keep processor.go as wiring-only.
func plan(ctx context.Context, p *processorImpl, req Request) ([]Plan, error) {
	in := strings.TrimSpace(req.InputPath)
	if in == "" {
		return nil, fmt.Errorf("input path is empty")
	}

	abs, err := filepath.Abs(in)
	if err != nil {
		return nil, fmt.Errorf("resolve input path: %w", err)
	}

	st, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat input path: %w", err)
	}

	// 1) Select main media file
	switch {
	case st.Mode().IsRegular():
		ext := strings.ToLower(filepath.Ext(abs))
		if !isExtInSet(ext, p.mainExtSet) {
			return nil, ErrNotMedia
		}
		pl, err := planForMain(ctx, p, req, abs, abs, showHint{}, movieParseFolderFirst, false, "", false)
		if err != nil {
			return nil, err
		}
		return []Plan{pl}, nil

	case st.IsDir():
		hint := showHint{}
		if name, year, season, seasonOK, ok := deriveShowHintFromFolder(p.blacklist, filepath.Base(abs)); ok {
			hint = showHint{name: name, year: year, ok: true, season: season, seasonOK: seasonOK}
		}

		mainPaths, hitMaxDepth, err := listMainMediaFromDir(ctx, p, abs)
		if err != nil {
			if errors.Is(err, ErrNoMainMediaFound) && hitMaxDepth {
				msg := fmt.Sprintf("WARNING  max depth %d reached while scanning %s; no main media found", paths.MaxDepth, abs)
				logConsoleWarn(p, logging.EventProcessorInputMaxDepthNoMedia, msg, nil, logging.Fields{
					"input_path": abs,
					"depth":      paths.MaxDepth,
				})
			}
			return nil, err
		}
		multiFile := len(mainPaths) >= 2
		movieMode := movieParseFolderFirst
		if multiFile {
			movieMode = movieParseFileOnly
		}

		// Pre-scan for show-name and show-year disagreement within the batch.
		// Name and year are reconciled independently, since a batch can
		// disagree on either one without disagreeing on the other:
		//
		//   - Name: if a folder hint exists and at least two distinct show
		//     names would result from parsing each file on its own (e.g. one
		//     file's naming convention is self-sufficient while a sibling's
		//     is only resolvable via the hint fallback), every file in the
		//     batch is forced to the hint name so they all land in the same
		//     show folder instead of splitting in two.
		//   - Year: a year carries more information than no year, so if the
		//     batch's years are all empty except one distinct value, every
		//     file is forced to that value. (If no file has a year at all,
		//     there's nothing to reconcile here -- the existing per-file
		//     fallback to the folder hint's year, below in planForMain,
		//     already covers that case uniformly.) If two files disagree on
		//     two different *actual* years, that's a genuine ambiguity --
		//     each file's own parse is left alone rather than guessing.
		//
		// A batch that already agrees with itself on both is left alone,
		// since a name/year individual files already parsed cleanly is often
		// better than one rebuilt from a release-tag-heavy folder name.
		forceHintName := false
		reconciledYear := ""
		if hint.ok && hint.name != "" && multiFile {
			names := make(map[string]struct{}, len(mainPaths))
			years := make(map[string]struct{}, len(mainPaths))
			for _, main := range mainPaths {
				sn, sy, _, _, _, err := resolveShowIdentity(p, filepath.Base(abs), main, hint, true)
				if err != nil {
					continue
				}
				names[sn] = struct{}{}
				years[sy] = struct{}{}
			}
			if len(names) > 1 {
				forceHintName = true
			}
			if len(years) > 1 {
				// years has more than one distinct key, and a map can hold
				// at most one "" key, so at least one non-empty year is
				// guaranteed to exist here.
				nonEmpty := make([]string, 0, len(years))
				for y := range years {
					if y != "" {
						nonEmpty = append(nonEmpty, y)
					}
				}
				if len(nonEmpty) == 1 {
					reconciledYear = nonEmpty[0]
				}
			}
		}

		plans := make([]Plan, 0, len(mainPaths))
		issues := make([]PlanIssue, 0)
		for _, main := range mainPaths {
			pl, err := planForMain(ctx, p, req, abs, main, hint, movieMode, forceHintName, reconciledYear, true)
			if err != nil {
				if isSkippablePlanError(err) {
					issues = append(issues, PlanIssue{Path: main, Err: err})
					continue
				}
				var destErr *DestinationUnavailableError
				if errors.As(err, &destErr) && len(plans) > 0 {
					// The destination is unavailable to every remaining
					// sibling too, so there's no point continuing to plan
					// them -- but the plans already computed for earlier
					// siblings are still good and shouldn't be thrown away;
					// the caller can apply them now instead of redoing this
					// work once the destination recovers.
					return plans, err
				}
				return nil, err
			}
			plans = append(plans, pl)
		}
		if len(plans) > 0 && len(issues) == 0 {
			plans[len(plans)-1].DeleteEmptyInputDir = true
		}
		if len(issues) > 0 {
			return plans, &PartialPlanError{Issues: issues}
		}
		return plans, nil

	default:
		return nil, ErrNotMedia
	}
}

// --- main media selection ---------------------------------------------------

func listMainMediaFromDir(ctx context.Context, p *processorImpl, dir string) ([]string, bool, error) {
	var mainPaths []string
	hitMaxDepth := false

	err := filepath.WalkDir(dir, func(path string, de os.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkErr != nil {
			if path == dir {
				return fmt.Errorf("readdir: %w", walkErr)
			}
			return nil
		}

		if de.IsDir() {
			if !paths.WithinMaxDepth(dir, path, paths.MaxDepth) {
				hitMaxDepth = true
				return filepath.SkipDir
			}
			return nil
		}

		if !paths.WithinMaxDepth(dir, filepath.Dir(path), paths.MaxDepth) {
			hitMaxDepth = true
			return nil
		}

		isRegular := de.Type().IsRegular()
		if !isRegular {
			if info, err := de.Info(); err == nil {
				isRegular = info.Mode().IsRegular()
			}
		}
		if !isRegular {
			return nil
		}

		e := strings.ToLower(filepath.Ext(path))
		if !isExtInSet(e, p.mainExtSet) {
			return nil
		}
		mainPaths = append(mainPaths, path)
		return nil
	})
	if err != nil {
		return nil, hitMaxDepth, err
	}

	if len(mainPaths) == 0 {
		return nil, hitMaxDepth, &NoMainMediaFoundError{
			Path:     dir,
			MaxDepth: paths.MaxDepth,
			DepthHit: hitMaxDepth,
		}
	}

	sort.Strings(mainPaths)
	return mainPaths, hitMaxDepth, nil
}

// --- categorization ---------------------------------------------------------

func normalizeCategory(c Category) Category {
	switch c {
	case CategoryMovie, CategoryShow:
		return c
	default:
		return ""
	}
}

type showHint struct {
	name string
	year string
	ok   bool

	// season/seasonOK are set only when the folder name pins down a single,
	// specific season (e.g. "Season 2 [dummy]"), as opposed to a season-range
	// folder (e.g. "Season 1-5") covering multiple seasons at once. Used to
	// anchor ambiguous bare-digit episode parsing -- see parseBareSeasonEpisode.
	season   int
	seasonOK bool
}

func isSkippablePlanError(err error) bool {
	if err == nil {
		return false
	}
	var pse *ParseShowError
	var pme *ParseMovieError
	if errors.As(err, &pse) || errors.As(err, &pme) {
		return true
	}
	if errors.Is(err, ErrAmbiguousShow) {
		return true
	}
	return false
}

func canonicalShowNameFromFolder(showFolder string, fallback string) string {
	showFolder = strings.TrimSpace(showFolder)
	if showFolder == "" {
		return fallback
	}

	if base, _, ok := parseShowFolderYear(showFolder); ok {
		base = strings.TrimSpace(base)
		if base != "" {
			return base
		}
	}

	return showFolder
}

// --- associated files planning ---------------------------------------------

func planAssociatedMoves(ctx context.Context, p *processorImpl, pl Plan) ([]Move, error) {
	srcDir := filepath.Dir(pl.MainSourcePath)
	mainStem := strings.TrimSuffix(pl.MainBaseName, pl.MainExt)
	mainStemLower := strings.ToLower(mainStem)

	ents, err := os.ReadDir(srcDir)
	if err != nil {
		return nil, fmt.Errorf("readdir associated: %w", err)
	}

	var moves []Move

	for _, ent := range ents {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !ent.Type().IsRegular() {
			continue
		}

		src := filepath.Join(srcDir, ent.Name())
		ext := strings.ToLower(filepath.Ext(ent.Name()))
		if ext == "" || !isExtInSet(ext, p.assocExtSet) {
			continue
		}

		stem := strings.TrimSuffix(ent.Name(), ext)
		lang := ""
		// Preserve language tag if name ends with ".en.srt" style.
		if m := reLangTag.FindStringSubmatch(stem); len(m) == 2 {
			lang = "." + strings.ToLower(m[1])
			stem = strings.TrimSuffix(stem, "."+m[1])
		}

		if strings.ToLower(stem) != mainStemLower {
			continue
		}

		dstName := pl.DestRadix + lang + ext
		dst := filepath.Join(pl.DestDir, dstName)

		moves = append(moves, Move{
			Source: src,
			Dest:   dst,
			Kind:   "associated",
		})
	}

	return moves, nil
}

// --- plan construction ------------------------------------------------------

// resolveShowIdentity parses the show name/year/season/episode for a single
// main media file, given the top-level folder it came from, its full path,
// and an optional showHint derived from that top-level folder. This is the
// one place that combines direct parsing with the hint fallback chain (plain
// SxxEyy, then the ambiguous bare-digit form), so both the batch-wide
// pre-scan in plan() and the real per-file plan in planForMain go through
// identical logic and can never disagree about what a given file parses to
// in isolation.
//
// The passed-in hint only ever reflects the top-level input folder (e.g. a
// season-range container like "Show S01-S04"), which has no specific season
// number to anchor the bare-digit fallback to. When a file sits nested under
// its own single-season subfolder (e.g. "Show S01-S04/Season 2/Show 201
// Title.avi"), that subfolder's name is also checked for a singular-season
// hint and merged in -- preferring the top-level hint's name (for batch-wide
// consistency) but filling in the season number from the immediate parent
// when the top-level hint doesn't have one of its own.
//
// dirMode must be false when the input being planned is a single file (see
// plan()'s regular-file branch, which always passes an empty hint precisely
// to signal "judge this file on its own name, ignore surrounding folders").
// Without this gate, a single file processed directly (not as part of a
// directory) would still pick up a name from its literal parent directory --
// reintroducing the folder context that single-file mode deliberately opts
// out of.
func resolveShowIdentity(p *processorImpl, folderBaseName, mainPath string, hint showHint, dirMode bool) (showName, showYear string, season, episode int, inputHadYear bool, err error) {
	mainBaseName := filepath.Base(mainPath)

	effHint := hint
	if dirMode {
		if lname, lyear, lseason, lseasonOK, lok := deriveShowHintFromFolder(p.blacklist, filepath.Base(filepath.Dir(mainPath))); lok {
			if !effHint.ok {
				effHint = showHint{name: lname, year: lyear, ok: true, season: lseason, seasonOK: lseasonOK}
			} else if !effHint.seasonOK && lseasonOK {
				effHint.season = lseason
				effHint.seasonOK = true
			}
		}
	}

	showName, showYear, season, episode, err = parseShowFromName(p.blacklist, folderBaseName, mainBaseName)
	inputHadYear = err == nil && showYear != ""
	if err != nil && effHint.ok && effHint.name != "" {
		if s, e, ok := parseSeasonEpisode(mainBaseName); ok {
			showName = effHint.name
			showYear = effHint.year
			season = s
			episode = e
			err = nil
		} else if s, e, ok := parseBareSeasonEpisode(effHint, mainBaseName); ok {
			showName = effHint.name
			showYear = effHint.year
			season = s
			episode = e
			err = nil
		}
	}
	return showName, showYear, season, episode, inputHadYear, err
}

func planForMain(
	ctx context.Context,
	p *processorImpl,
	req Request,
	inputPath string,
	mainPath string,
	hint showHint,
	movieMode movieParseMode,
	forceHintName bool,
	reconciledYear string,
	dirMode bool,
) (Plan, error) {
	pl := Plan{
		InputPath:    inputPath,
		CategoryHint: req.CategoryHint,
	}

	pl.MainSourcePath = mainPath
	pl.MainExt = strings.ToLower(filepath.Ext(mainPath))
	pl.MainBaseName = filepath.Base(mainPath)

	// 2) Determine category (Movies vs Shows)
	cat := normalizeCategory(req.CategoryHint)
	if cat == "" {
		if hint.ok {
			cat = CategoryShow
		} else {
			cat = determineCategoryFromNames(filepath.Base(pl.InputPath), pl.MainBaseName)
		}
	}
	pl.Category = cat

	// 3) Parse identity + compute destination
	switch pl.Category {
	case CategoryShow:
		showName, showYear, season, episode, inputHadYear, err := resolveShowIdentity(p, filepath.Base(pl.InputPath), pl.MainSourcePath, hint, dirMode)
		if err != nil {
			return Plan{}, err
		}
		// forceHintName is set by the caller only when a pre-scan of the
		// whole batch (see plan()) found sibling files that would otherwise
		// resolve to *different* show names -- e.g. one episode's filename
		// is self-sufficient (an unambiguous "1x01" token) while another
		// only resolves via the folder hint (an ambiguous bare "201"). In
		// that case the folder-level hint always wins, so the whole batch
		// converges on one show folder. When the batch already agrees with
		// itself, this stays false and each file keeps its own best parse
		// (which is often cleaner than a release-tag-heavy folder name).
		if forceHintName && hint.ok && hint.name != "" {
			showName = hint.name
		}
		// reconciledYear is set independently of forceHintName -- a batch
		// can disagree on the year without disagreeing on the name (e.g.
		// every file agrees on "Show" but only one file's own filename
		// happened to carry a year). Since a year is more information than
		// no year, every file adopts it. The pre-scan (see plan()) only sets
		// this when the batch's non-empty years all agree on a single value,
		// so a genuine conflict between two different actual years is left
		// alone rather than guessed at.
		if reconciledYear != "" && showYear != reconciledYear {
			showYear = reconciledYear
			inputHadYear = true
		}
		if showYear == "" && hint.ok && hint.year != "" {
			showYear = hint.year
		}
		showFolder, resolvedYear, err := resolveShowFolder(p, p.cfg.ShowsDir, showName, showYear)
		if err != nil {
			return Plan{}, err
		}

		pl.ShowName = showName
		pl.ShowYear = resolvedYear
		pl.Season = season
		pl.Episode = episode

		seasonFolder := fmt.Sprintf("Season %02d", season)
		canonicalShowName := canonicalShowNameFromFolder(showFolder, showName)
		displayShowName := canonicalShowName
		if inputHadYear && resolvedYear != "" {
			displayShowName = fmt.Sprintf("%s (%s)", canonicalShowName, resolvedYear)
		}
		pl.DestRadix = fmt.Sprintf("%s - S%02dE%s", displayShowName, season, padEpisode(episode))

		pl.DestDir = filepath.Join(p.cfg.ShowsDir, showFolder, seasonFolder)
		pl.DestMainPath = filepath.Join(pl.DestDir, pl.DestRadix+pl.MainExt)

	case CategoryMovie:
		var (
			title string
			year  string
			err   error
		)
		if movieMode == movieParseFolderFirst {
			title, year, err = parseMovieFromName(p.blacklist, filepath.Base(pl.InputPath), pl.MainBaseName)
		} else {
			title, year, err = parseMovieFromNameWithMode(
				p.blacklist,
				filepath.Base(pl.InputPath),
				pl.MainBaseName,
				movieMode,
			)
		}
		if err != nil {
			return Plan{}, err
		}
		if year != "" {
			pl.MovieTitle = fmt.Sprintf("%s (%s)", title, year)
		} else {
			pl.MovieTitle = title
		}

		pl.DestRadix = pl.MovieTitle
		pl.DestDir = filepath.Join(p.cfg.MoviesDir, pl.MovieTitle)
		pl.DestMainPath = filepath.Join(pl.DestDir, pl.DestRadix+pl.MainExt)

	default:
		return Plan{}, ErrUncategorized
	}

	// 4) Duplicate detection: does this exact destination already exist?
	if _, err := os.Stat(pl.DestMainPath); err == nil {
		pl.Duplicate = true
	} else if !os.IsNotExist(err) {
		return Plan{}, fmt.Errorf("stat destination: %w", err)
	}

	// 5) Associated file mapping
	assoc, err := planAssociatedMoves(ctx, p, pl)
	if err != nil {
		return Plan{}, err
	}
	pl.Associated = assoc

	return pl, nil
}

// CountPlans runs Plan against each path and sums the resulting plan counts
// (e.g. a season-pack directory counts as 8 files, not 1), so callers get an
// accurate human-facing file count across expansion. Paths that fail to plan
// (non-media, unparseable) are silently skipped -- the caller's own
// processing loop is responsible for surfacing that error later.
// Stops early if ctx is canceled, returning the partial count and
// interrupted=true so callers can distinguish a full count from a
// cancellation-truncated one.
func CountPlans(ctx context.Context, proc Processor, paths []string) (count int, interrupted bool) {
	for _, p := range paths {
		if ctx.Err() != nil {
			return count, true
		}
		plans, err := proc.Plan(ctx, Request{InputPath: p})
		if err != nil {
			continue
		}
		count += len(plans)
	}
	return count, false
}

// CountMainMedia counts main media files under path using the same
// extension-filtered selection as Plan (single file check, or a directory
// walk via listMainMediaFromDir), without running any naming/hint-resolution
// logic. This makes it much cheaper than Plan for a path whose contents Plan
// would otherwise have to categorize and destination-check, at the cost of
// being an estimate: a file that passes the extension filter here may still
// turn out unparseable or ambiguous once Plan actually processes it, so this
// count can be slightly higher than the real, fully-planned result.
func (p *processorImpl) CountMainMedia(ctx context.Context, path string) (int, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return 0, err
	}

	st, err := os.Stat(abs)
	if err != nil {
		return 0, err
	}

	if st.Mode().IsRegular() {
		ext := strings.ToLower(filepath.Ext(abs))
		if !isExtInSet(ext, p.mainExtSet) {
			return 0, ErrNotMedia
		}
		return 1, nil
	}

	mainPaths, _, err := listMainMediaFromDir(ctx, p, abs)
	if err != nil {
		return 0, err
	}
	return len(mainPaths), nil
}

// CountMainMedia sums the cheap, extension-only media count (see
// (*processorImpl).CountMainMedia) across paths. Use this instead of
// CountPlans for a fast upfront estimate; use CountPlans when an exact,
// fully-planned count is required.
func CountMainMedia(ctx context.Context, proc Processor, paths []string) (count int, interrupted bool) {
	for _, p := range paths {
		if ctx.Err() != nil {
			return count, true
		}
		n, err := proc.CountMainMedia(ctx, p)
		if err != nil {
			continue
		}
		count += n
	}
	return count, false
}
