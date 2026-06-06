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
		pl, err := planForMain(ctx, p, req, abs, abs, showHint{}, movieParseFolderFirst)
		if err != nil {
			return nil, err
		}
		return []Plan{pl}, nil

	case st.IsDir():
		hint := showHint{}
		if name, year, ok := deriveShowHintFromFolder(p.blacklist, filepath.Base(abs)); ok {
			hint = showHint{name: name, year: year, ok: true}
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
		movieMode := movieParseFolderFirst
		if len(mainPaths) >= 2 {
			movieMode = movieParseFileOnly
		}
		plans := make([]Plan, 0, len(mainPaths))
		issues := make([]PlanIssue, 0)
		for _, main := range mainPaths {
			pl, err := planForMain(ctx, p, req, abs, main, hint, movieMode)
			if err != nil {
				if isSkippablePlanError(err) {
					issues = append(issues, PlanIssue{Path: main, Err: err})
					continue
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

func planForMain(
	ctx context.Context,
	p *processorImpl,
	req Request,
	inputPath string,
	mainPath string,
	hint showHint,
	movieMode movieParseMode,
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
		showName, showYear, season, episode, err := parseShowFromName(p.blacklist, filepath.Base(pl.InputPath), pl.MainBaseName)
		inputHadYear := err == nil && showYear != ""
		if err != nil && hint.ok && hint.name != "" {
			if s, e, ok := parseSeasonEpisode(pl.MainBaseName); ok {
				showName = hint.name
				showYear = hint.year
				season = s
				episode = e
				err = nil
			}
		}
		if err != nil {
			return Plan{}, err
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

	// 4) Associated file mapping
	assoc, err := planAssociatedMoves(ctx, p, pl)
	if err != nil {
		return Plan{}, err
	}
	pl.Associated = assoc

	return pl, nil
}
