package processor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlan_TableDriven(t *testing.T) {

	type tc struct {
		name      string
		setup     func(t *testing.T, p *processorImpl) (inputPath string)
		check     func(t *testing.T, p *processorImpl, inputPath string, pl Plan, err error)
		checkMany func(t *testing.T, p *processorImpl, inputPath string, plans []Plan, err error)
	}

	cases := []tc{
		{
			name: "ShowFile_Sherlock_S04E00_AllowsEpisodeZero",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "Sherlock.S04E00.The.Abominable.Bride.2016.1080p.x265.mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, _ *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.Category != CategoryShow {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
				}
				if pl.ShowName != "Sherlock" {
					t.Fatalf("ShowName = %q, want %q", pl.ShowName, "Sherlock")
				}
				if pl.Season != 4 || pl.Episode != 0 {
					t.Fatalf("Season/Episode = %d/%d, want 4/0", pl.Season, pl.Episode)
				}
				if pl.DestRadix != "Sherlock - S04E00" {
					t.Fatalf("DestRadix = %q, want %q", pl.DestRadix, "Sherlock - S04E00")
				}
			},
		},
		{
			name: "ShowFile_StrangerThings_S05E08_WithSubtitle",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "Stranger.Things.S05E08.1080p.HEVC.x265-MeGusta[EZTVx.to].mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")

				// Associated subtitle with language tag
				sub := filepath.Join(p.cfg.DropFolder, "Stranger.Things.S05E08.1080p.HEVC.x265-MeGusta[EZTVx.to].en.srt")
				writeFile(t, sub, "sub")

				return src
			},
			check: func(t *testing.T, p *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}

				if pl.Category != CategoryShow {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
				}
				if pl.ShowName != "Stranger Things" {
					t.Fatalf("ShowName = %q, want %q", pl.ShowName, "Stranger Things")
				}
				if pl.Season != 5 || pl.Episode != 8 {
					t.Fatalf("Season/Episode = %d/%d, want 5/8", pl.Season, pl.Episode)
				}

				wantRadix := "Stranger Things - S05E08"
				if pl.DestRadix != wantRadix {
					t.Fatalf("DestRadix = %q, want %q", pl.DestRadix, wantRadix)
				}

				if !strings.Contains(pl.DestDir, filepath.Join(p.cfg.ShowsDir, "Stranger Things")) {
					t.Fatalf("DestDir = %q, expected under shows dir %q", pl.DestDir, p.cfg.ShowsDir)
				}
				if !strings.HasSuffix(pl.DestDir, "Season 05") {
					t.Fatalf("DestDir = %q, want suffix %q", pl.DestDir, "Season 05")
				}
				if !strings.HasSuffix(pl.DestMainPath, wantRadix+".mkv") {
					t.Fatalf("DestMainPath = %q, want suffix %q", pl.DestMainPath, wantRadix+".mkv")
				}

				// Associated mapping should include the subtitle, renamed to radix.en.srt
				sub := filepath.Join(p.cfg.DropFolder, "Stranger.Things.S05E08.1080p.HEVC.x265-MeGusta[EZTVx.to].en.srt")
				found := false
				for _, mv := range pl.Associated {
					if mv.Kind != "associated" {
						continue
					}
					if mv.Source == sub {
						found = true
						if !strings.HasSuffix(mv.Dest, wantRadix+".en.srt") {
							t.Fatalf("Associated dest = %q, want suffix %q", mv.Dest, wantRadix+".en.srt")
						}
					}
				}
				if !found {
					t.Fatalf("expected associated move for %q", sub)
				}
			},
		},
		{
			name: "ShowFolder_SeasonPack_ProcessesSubfolders",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				root := filepath.Join(p.cfg.DropFolder, "Sherlock.Season.1-4.S01-S04.1080p.10bit.BluRay.5.1.x265.HEVC-MZABI")
				season01 := filepath.Join(root, "Season 01")
				season04 := filepath.Join(root, "Season 04")
				writeFile(t, filepath.Join(season01, "Sherlock.S01E01.1080p.x265.mkv"), "dummy")
				writeFile(t, filepath.Join(season04, "Sherlock.S04E00.1080p.x265.mkv"), "dummy")
				return root
			},
			checkMany: func(t *testing.T, _ *processorImpl, inputPath string, plans []Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if len(plans) != 2 {
					t.Fatalf("expected 2 plans, got %d", len(plans))
				}

				want := map[string]struct{}{
					filepath.Join(inputPath, "Season 01", "Sherlock.S01E01.1080p.x265.mkv"): {},
					filepath.Join(inputPath, "Season 04", "Sherlock.S04E00.1080p.x265.mkv"): {},
				}

				for _, pl := range plans {
					if pl.Category != CategoryShow {
						t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
					}
					if pl.ShowName != "Sherlock" {
						t.Fatalf("ShowName = %q, want %q", pl.ShowName, "Sherlock")
					}
					if _, ok := want[pl.MainSourcePath]; !ok {
						t.Fatalf("unexpected plan for %q", pl.MainSourcePath)
					}
				}
			},
		},
		{
			name: "ShowFolder_SeasonPack_HintFallback_NoShowInFilename",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				root := filepath.Join(p.cfg.DropFolder, "Sherlock.Season.1-4.S01-S04")
				writeFile(t, filepath.Join(root, "S01E01.mkv"), "dummy")
				writeFile(t, filepath.Join(root, "S01E02.mkv"), "dummy")
				return root
			},
			checkMany: func(t *testing.T, p *processorImpl, _ string, plans []Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if len(plans) != 2 {
					t.Fatalf("expected 2 plans, got %d", len(plans))
				}
				for _, pl := range plans {
					if pl.Category != CategoryShow {
						t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
					}
					if pl.ShowName != "Sherlock" {
						t.Fatalf("ShowName = %q, want %q", pl.ShowName, "Sherlock")
					}
					if !strings.Contains(pl.DestDir, filepath.Join(p.cfg.ShowsDir, "Sherlock")) {
						t.Fatalf("DestDir = %q, expected under shows dir %q", pl.DestDir, p.cfg.ShowsDir)
					}
				}
			},
		},
		{
			name: "ShowFolder_SeasonWordWithEpisodeWord_CategorizesAndParsesAsShow",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				root := filepath.Join(
					p.cfg.DropFolder,
					"Planet Earth II (2016) - Season 1 - (1080p DS4K BluRay x265 10-bit HDR AAC 5.1) [WeSLeY]",
				)
				mainName := "Planet Earth II (2016) - Episode 1 - Islands (1080p DS4K BluRay x265 10-bit HDR AAC 5.1) [WeSLeY].mkv"
				writeFile(t, filepath.Join(root, mainName), "dummy")
				return root
			},
			checkMany: func(t *testing.T, _ *processorImpl, _ string, plans []Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if len(plans) != 1 {
					t.Fatalf("expected 1 plan, got %d", len(plans))
				}

				pl := plans[0]
				if pl.Category != CategoryShow {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
				}
				if pl.ShowName != "Planet Earth II" {
					t.Fatalf("ShowName = %q, want %q", pl.ShowName, "Planet Earth II")
				}
				if pl.ShowYear != "2016" {
					t.Fatalf("ShowYear = %q, want %q", pl.ShowYear, "2016")
				}
				if pl.Season != 1 || pl.Episode != 1 {
					t.Fatalf("Season/Episode = %d/%d, want 1/1", pl.Season, pl.Episode)
				}
				if pl.DestRadix != "Planet Earth II (2016) - S01E01" {
					t.Fatalf("DestRadix = %q, want %q", pl.DestRadix, "Planet Earth II (2016) - S01E01")
				}
				if !strings.HasSuffix(pl.DestDir, "Season 01") {
					t.Fatalf("DestDir = %q, want suffix %q", pl.DestDir, "Season 01")
				}
			},
		},
		{
			name: "ShowFolder_SingleSeason_NxNN_UnderscoreDelimited",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				root := filepath.Join(p.cfg.DropFolder, "X-Men TAS [HQ] Season 1 [vpc]")
				mainName := "x-men_-_1x01_-_night_of_the_sentinels_-_part_1_[vpc].avi"
				writeFile(t, filepath.Join(root, mainName), "dummy")
				return root
			},
			checkMany: func(t *testing.T, _ *processorImpl, _ string, plans []Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if len(plans) != 1 {
					t.Fatalf("expected 1 plan, got %d", len(plans))
				}

				pl := plans[0]
				if pl.Category != CategoryShow {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
				}
				// The NxNN token alone carries both season and episode, so
				// parseShowFromName succeeds directly on the filename (which
				// only says "x-men", not "TAS") without ever needing the
				// folder hint -- this matches how these files are actually
				// named in the wild.
				if pl.ShowName != "X-Men" {
					t.Fatalf("ShowName = %q, want %q", pl.ShowName, "X-Men")
				}
				if pl.Season != 1 || pl.Episode != 1 {
					t.Fatalf("Season/Episode = %d/%d, want 1/1", pl.Season, pl.Episode)
				}
			},
		},
		{
			name: "ShowFolder_SingleSeason_BareDigitSEE",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				root := filepath.Join(p.cfg.DropFolder, "X-Men TAS [HQ] Season 2 [dummy]")
				mainName := "X-Men TAS 201 'Til Death Do Us Part 1of2 [dummy].avi"
				writeFile(t, filepath.Join(root, mainName), "dummy")
				return root
			},
			checkMany: func(t *testing.T, _ *processorImpl, _ string, plans []Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if len(plans) != 1 {
					t.Fatalf("expected 1 plan, got %d", len(plans))
				}

				pl := plans[0]
				if pl.Category != CategoryShow {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
				}
				if pl.ShowName != "X-Men Tas" {
					t.Fatalf("ShowName = %q, want %q", pl.ShowName, "X-Men Tas")
				}
				if pl.Season != 2 || pl.Episode != 1 {
					t.Fatalf("Season/Episode = %d/%d, want 2/1", pl.Season, pl.Episode)
				}
			},
		},
		{
			name: "ShowFolder_SingleSeason_BareDigitSEE_UnderscoreDelimited",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				// Same old-school underscore-delimited naming convention as
				// the NxNN case above, but using bare concatenated digits
				// instead. reBareSeasonEpisode originally used \b, which
				// (like reSeasonEpisodeX before it was fixed) silently fails
				// to match across underscore delimiters, since underscore is
				// a \w character.
				root := filepath.Join(p.cfg.DropFolder, "Show Season 2 [dummy]")
				mainName := "show_-_201_-_title.avi"
				writeFile(t, filepath.Join(root, mainName), "dummy")
				return root
			},
			checkMany: func(t *testing.T, _ *processorImpl, _ string, plans []Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if len(plans) != 1 {
					t.Fatalf("expected 1 plan, got %d", len(plans))
				}

				pl := plans[0]
				if pl.Category != CategoryShow {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
				}
				if pl.Season != 2 || pl.Episode != 1 {
					t.Fatalf("Season/Episode = %d/%d, want 2/1", pl.Season, pl.Episode)
				}
			},
		},
		{
			name: "ShowFolder_SingleSeason_BareDigitSEE_TwoDigitEpisode",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				root := filepath.Join(p.cfg.DropFolder, "X-Men TAS [HQ] Season 5 [dummy]")
				mainName := "X-Men TAS 514 Graduation Day [dummy].avi"
				writeFile(t, filepath.Join(root, mainName), "dummy")
				return root
			},
			checkMany: func(t *testing.T, _ *processorImpl, _ string, plans []Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if len(plans) != 1 {
					t.Fatalf("expected 1 plan, got %d", len(plans))
				}

				pl := plans[0]
				if pl.Category != CategoryShow {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
				}
				if pl.Season != 5 || pl.Episode != 14 {
					t.Fatalf("Season/Episode = %d/%d, want 5/14", pl.Season, pl.Episode)
				}
			},
		},
		{
			name: "ShowFolder_MixedNumberingAcrossSeasons_ReconcilesToOneShowName",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				root := filepath.Join(p.cfg.DropFolder, "X-Men TAS [HQ] Season 1-5")
				writeFile(t, filepath.Join(root, "X-Men TAS [HQ] Season 1 [vpc]", "x-men_-_1x01_-_night_of_the_sentinels_-_part_1_[vpc].avi"), "dummy")
				writeFile(t, filepath.Join(root, "X-Men TAS [HQ] Season 2 [dummy]", "X-Men TAS 201 'Til Death Do Us Part 1of2 [dummy].avi"), "dummy")
				return root
			},
			checkMany: func(t *testing.T, _ *processorImpl, _ string, plans []Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if len(plans) != 2 {
					t.Fatalf("expected 2 plans, got %d", len(plans))
				}

				// Season 1's filename is self-sufficient (the "1x01" token
				// carries both season and episode on its own), while Season
				// 2's bare "201" can only resolve via the folder hint. Left
				// unreconciled, these would parse to two different show
				// names ("X-Men" vs "X-Men Tas") and split one show across
				// two folders. Both must land under the same show name.
				names := make(map[string]int)
				for _, pl := range plans {
					if pl.Category != CategoryShow {
						t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
					}
					names[pl.ShowName]++
				}
				if len(names) != 1 {
					t.Fatalf("expected all plans to share one ShowName, got %v", names)
				}
			},
		},
		{
			name: "ShowFolder_YearOnOneSiblingOnly_ReconciledAcrossBatch",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				root := filepath.Join(p.cfg.DropFolder, "Firefly Season 1-2")
				writeFile(t, filepath.Join(root, "Firefly Season 1", "Firefly.2002.1x01.Title.mkv"), "dummy")
				writeFile(t, filepath.Join(root, "Firefly Season 2", "Firefly 201 Other Title.avi"), "dummy")
				return root
			},
			checkMany: func(t *testing.T, _ *processorImpl, _ string, plans []Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if len(plans) != 2 {
					t.Fatalf("expected 2 plans, got %d", len(plans))
				}

				// Both files already agree on ShowName ("Firefly"), but only
				// the first's filename carries a year. A year is more
				// information than no year, so both must resolve to the same
				// year and land in the same destination folder -- otherwise
				// "Firefly (2002)/" and "Firefly/" would silently split one
				// show across two folders despite sharing a ShowName.
				dests := make(map[string]int)
				for _, pl := range plans {
					if pl.ShowYear != "2002" {
						t.Fatalf("ShowYear = %q, want %q", pl.ShowYear, "2002")
					}
					dests[filepath.Dir(pl.DestDir)]++
				}
				if len(dests) != 1 {
					t.Fatalf("expected both plans under one show folder, got %v", dests)
				}
			},
		},
		{
			name: "ShowFolder_GenuinelyConflictingYears_LeftUnreconciled",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				root := filepath.Join(p.cfg.DropFolder, "Show Season 1-2")
				writeFile(t, filepath.Join(root, "Show Season 1", "Show.1999.1x01.Title.mkv"), "dummy")
				writeFile(t, filepath.Join(root, "Show Season 2", "Show.2005.2x01.Title.mkv"), "dummy")
				return root
			},
			checkMany: func(t *testing.T, _ *processorImpl, _ string, plans []Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if len(plans) != 2 {
					t.Fatalf("expected 2 plans, got %d", len(plans))
				}

				// Two siblings each carry their own distinct, real year --
				// a genuine ambiguity with no objectively "right" answer, so
				// each file's own parse must be left alone rather than
				// forcibly guessed at.
				years := make(map[string]int)
				for _, pl := range plans {
					years[pl.ShowYear]++
				}
				if len(years) != 2 || years["1999"] != 1 || years["2005"] != 1 {
					t.Fatalf("expected years 1999 and 2005 preserved independently, got %v", years)
				}
			},
		},
		{
			name: "ShowFolder_BareDigitSEE_StrayEarlierNumberNotBlocking",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				root := filepath.Join(p.cfg.DropFolder, "Show Season 2 [dummy]")
				mainName := "Show - Anniversary 300 Cut - 201 Title [dummy].avi"
				writeFile(t, filepath.Join(root, mainName), "dummy")
				return root
			},
			checkMany: func(t *testing.T, _ *processorImpl, _ string, plans []Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if len(plans) != 1 {
					t.Fatalf("expected 1 plan, got %d", len(plans))
				}

				// The stray "300" earlier in the filename doesn't match the
				// folder's trusted season (2), so it must not block the
				// real "201" token later in the name from being found.
				pl := plans[0]
				if pl.Category != CategoryShow {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
				}
				if pl.Season != 2 || pl.Episode != 1 {
					t.Fatalf("Season/Episode = %d/%d, want 2/1", pl.Season, pl.Episode)
				}
			},
		},
		{
			name: "ShowFolder_BareDigitSEE_AmbiguousCandidates_Skipped",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				root := filepath.Join(p.cfg.DropFolder, "Show Season 2 [dummy]")
				mainName := "Show 201 vs 245 Title [dummy].avi"
				writeFile(t, filepath.Join(root, mainName), "dummy")
				return root
			},
			checkMany: func(t *testing.T, _ *processorImpl, _ string, plans []Plan, err error) {
				t.Helper()

				// Two candidate tokens ("201" and "245") both match the
				// folder's trusted season (2) -- which one is the real
				// episode code is genuinely ambiguous, so the file must be
				// skipped and reported rather than guessed at.
				var ppe *PartialPlanError
				if !errors.As(err, &ppe) {
					t.Fatalf("expected PartialPlanError, got %v (plans=%v)", err, plans)
				}
				if len(ppe.Issues) != 1 {
					t.Fatalf("expected 1 skipped issue, got %d", len(ppe.Issues))
				}
			},
		},
		{
			name: "ShowFolder_MovieInsideSeasonFolder_SkippedNotMisfiled",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				// A movie file happens to sit inside a folder literally
				// named "Season N" (e.g. a bonus-feature folder). The
				// folder's singular-season hint forces this file to be
				// planned as a show, but it doesn't parse as an episode by
				// any method -- it must be skipped and reported for human
				// review, not silently misfiled as a garbage "movie" or
				// "show".
				root := filepath.Join(p.cfg.DropFolder, "Bonus Season 2 Movie")
				mainName := "101 Dalmations 1961 720p BluRay.mkv"
				writeFile(t, filepath.Join(root, mainName), "dummy")
				return root
			},
			checkMany: func(t *testing.T, _ *processorImpl, _ string, plans []Plan, err error) {
				t.Helper()

				var ppe *PartialPlanError
				if !errors.As(err, &ppe) {
					t.Fatalf("expected PartialPlanError, got %v (plans=%v)", err, plans)
				}
				if len(ppe.Issues) != 1 {
					t.Fatalf("expected 1 skipped issue, got %d", len(ppe.Issues))
				}
				var pse *ParseShowError
				if !errors.As(ppe.Issues[0].Err, &pse) {
					t.Fatalf("expected *ParseShowError, got %v", ppe.Issues[0].Err)
				}
			},
		},
		{
			name: "ShowFolder_MovieWithBracketTagPrefix_StillSkippedNotMisfiled",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				// Same as ShowFolder_MovieInsideSeasonFolder_SkippedNotMisfiled,
				// but with a release-group bracket tag before the catalog
				// number. The movie-shape guard must see past this noise --
				// it previously only recognized the catalog number as
				// "leading" when it sat at literal byte offset 0.
				root := filepath.Join(p.cfg.DropFolder, "Bonus Season 1 Movie")
				mainName := "[GRP] 101 Dalmations 1961.mkv"
				writeFile(t, filepath.Join(root, mainName), "dummy")
				return root
			},
			checkMany: func(t *testing.T, _ *processorImpl, _ string, plans []Plan, err error) {
				t.Helper()

				var ppe *PartialPlanError
				if !errors.As(err, &ppe) {
					t.Fatalf("expected PartialPlanError, got %v (plans=%v)", err, plans)
				}
				if len(ppe.Issues) != 1 {
					t.Fatalf("expected 1 skipped issue, got %d", len(ppe.Issues))
				}
			},
		},
		{
			name: "SingleFile_ParentFolderSeasonHint_NotLeaked",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				// Processing this file directly (not its containing
				// directory) -- single-file mode passes an empty showHint
				// specifically to judge the file on its own name. It must
				// not pick up "MyShow" from its immediate parent directory
				// just because that directory happens to look like a season
				// folder; with no title of its own, this file genuinely
				// doesn't have enough info and should fail to parse rather
				// than guess from surrounding folder context it was never
				// asked to consider.
				src := filepath.Join(p.cfg.DropFolder, "MyShow Season 5", "S02E01.mkv")
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, _ *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				var pse *ParseShowError
				if !errors.As(err, &pse) {
					t.Fatalf("expected *ParseShowError, got %v (plan=%+v)", err, pl)
				}
			},
		},
		{
			name: "ShowFolder_BareDigitSEE_UnrelatedYearBeforeMatch_NotMisparsedAsMovie",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				// The bracketed "[2020]" tag is an unrelated upload/release
				// date, not a movie release year -- it sits BEFORE the bare
				// episode code, not after it. The movie-catalog-number guard
				// must only reject a year that appears after the matched
				// digits (the "101.Dalmations.1961..." shape); a coincidental
				// year earlier in the filename must not count.
				root := filepath.Join(p.cfg.DropFolder, "Show Season 2 [dummy]")
				mainName := "[2020] 201 Title.avi"
				writeFile(t, filepath.Join(root, mainName), "dummy")
				return root
			},
			checkMany: func(t *testing.T, _ *processorImpl, _ string, plans []Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if len(plans) != 1 {
					t.Fatalf("expected 1 plan, got %d", len(plans))
				}

				pl := plans[0]
				if pl.Category != CategoryShow {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
				}
				if pl.Season != 2 || pl.Episode != 1 {
					t.Fatalf("Season/Episode = %d/%d, want 2/1", pl.Season, pl.Episode)
				}
			},
		},
		{
			name: "MovieFile_BareDigitPrefixBeforeYear_NotMisparsedAsEpisode",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "101.Dalmations.1961.720p.BluRay.x264-x0r.mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, _ *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.Category != CategoryMovie {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryMovie)
				}
			},
		},
		{
			name: "ShowFolder_SeasonPack_PartialSkip_UnparseableFile",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				root := filepath.Join(p.cfg.DropFolder, "Sherlock.Season.1-4.S01-S04")
				writeFile(t, filepath.Join(root, "S01E01.mkv"), "dummy")
				writeFile(t, filepath.Join(root, "Episode01.mkv"), "dummy")
				return root
			},
			checkMany: func(t *testing.T, _ *processorImpl, inputPath string, plans []Plan, err error) {
				t.Helper()

				var partial *PartialPlanError
				if !errors.As(err, &partial) {
					t.Fatalf("expected PartialPlanError, got %v", err)
				}
				if len(plans) != 1 {
					t.Fatalf("expected 1 plan, got %d", len(plans))
				}
				if plans[0].DeleteEmptyInputDir {
					t.Fatalf("DeleteEmptyInputDir = true, want false")
				}
				if partial == nil || len(partial.Issues) != 1 {
					t.Fatalf("expected 1 issue, got %v", partial)
				}
				wantPath := filepath.Join(inputPath, "Episode01.mkv")
				if partial.Issues[0].Path != wantPath {
					t.Fatalf("issue path = %q, want %q", partial.Issues[0].Path, wantPath)
				}
			},
		},
		{
			name: "ShowFolder_SeasonPack_HintYear_UsedWhenFilenameHasNoYear",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				root := filepath.Join(p.cfg.DropFolder, "Sherlock.2010.Season.1-4.S01-S04")
				writeFile(t, filepath.Join(root, "S01E01.mkv"), "dummy")
				return root
			},
			checkMany: func(t *testing.T, _ *processorImpl, _ string, plans []Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if len(plans) != 1 {
					t.Fatalf("expected 1 plan, got %d", len(plans))
				}
				if plans[0].ShowYear != "2010" {
					t.Fatalf("ShowYear = %q, want %q", plans[0].ShowYear, "2010")
				}
			},
		},
		{
			name: "ShowFolder_SeasonPack_FilenameYear_WinsOverHintYear",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				root := filepath.Join(p.cfg.DropFolder, "Sherlock.2010.Season.1-4.S01-S04")
				writeFile(t, filepath.Join(root, "Sherlock.2017.S01E01.mkv"), "dummy")
				return root
			},
			checkMany: func(t *testing.T, _ *processorImpl, _ string, plans []Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if len(plans) != 1 {
					t.Fatalf("expected 1 plan, got %d", len(plans))
				}
				if plans[0].ShowYear != "2017" {
					t.Fatalf("ShowYear = %q, want %q", plans[0].ShowYear, "2017")
				}
			},
		},
		{
			name: "ShowFolder_SeasonPack_HintYear_NoLeakIntoFilename",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				root := filepath.Join(p.cfg.DropFolder, "Sherlock.2010.Season.1-4.S01-S04")
				writeFile(t, filepath.Join(root, "S01E01.mkv"), "dummy")
				return root
			},
			checkMany: func(t *testing.T, _ *processorImpl, _ string, plans []Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if len(plans) != 1 {
					t.Fatalf("expected 1 plan, got %d", len(plans))
				}
				if plans[0].ShowYear != "2010" {
					t.Fatalf("ShowYear = %q, want %q", plans[0].ShowYear, "2010")
				}
				if plans[0].DestRadix != "Sherlock - S01E01" {
					t.Fatalf("DestRadix = %q, want %q", plans[0].DestRadix, "Sherlock - S01E01")
				}
			},
		},
		{
			name: "ShowFolder_SeasonPack_FilenameYear_KeptInFilename",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				root := filepath.Join(p.cfg.DropFolder, "Sherlock.2010.Season.1-4.S01-S04")
				writeFile(t, filepath.Join(root, "Sherlock.2017.S01E01.mkv"), "dummy")
				return root
			},
			checkMany: func(t *testing.T, _ *processorImpl, _ string, plans []Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if len(plans) != 1 {
					t.Fatalf("expected 1 plan, got %d", len(plans))
				}
				if plans[0].ShowYear != "2017" {
					t.Fatalf("ShowYear = %q, want %q", plans[0].ShowYear, "2017")
				}
				if plans[0].DestRadix != "Sherlock (2017) - S01E01" {
					t.Fatalf("DestRadix = %q, want %q", plans[0].DestRadix, "Sherlock (2017) - S01E01")
				}
			},
		},
		{
			name: "ShowFile_YearWithNoYearFolder_DropsYear",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				mkdirAll(t, filepath.Join(p.cfg.ShowsDir, "Sherlock"))

				name := "Sherlock 2010 S01E01 1080p x265-ELiTE[EZTVx.to].mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, p *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.ShowYear != "" {
					t.Fatalf("ShowYear = %q, want empty", pl.ShowYear)
				}
				if pl.DestRadix != "Sherlock - S01E01" {
					t.Fatalf("DestRadix = %q, want %q", pl.DestRadix, "Sherlock - S01E01")
				}
				if !strings.Contains(pl.DestDir, filepath.Join(p.cfg.ShowsDir, "Sherlock")) {
					t.Fatalf("DestDir = %q, expected under shows dir %q", pl.DestDir, p.cfg.ShowsDir)
				}
			},
		},
		{
			name: "ShowFolder_SeasonPack_ConflictingYear_FilenameWins",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				root := filepath.Join(p.cfg.DropFolder, "Sherlock.2010.Season.1-4.S01-S04")
				writeFile(t, filepath.Join(root, "Sherlock.2014.S01E02.mkv"), "dummy")
				return root
			},
			checkMany: func(t *testing.T, _ *processorImpl, _ string, plans []Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if len(plans) != 1 {
					t.Fatalf("expected 1 plan, got %d", len(plans))
				}
				if plans[0].ShowYear != "2014" {
					t.Fatalf("ShowYear = %q, want %q", plans[0].ShowYear, "2014")
				}
			},
		},
		{
			name: "ShowFile_StrangerThings_S05E07_WithEpisodeTitleNoise",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "Stranger.Things.S05E07.Chapter.Seven.The.Bridge.1080p.HEVC.x265-MeGusta[EZTVx.to].mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, _ *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}

				if pl.Category != CategoryShow {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
				}
				if pl.ShowName != "Stranger Things" {
					t.Fatalf("ShowName = %q, want %q", pl.ShowName, "Stranger Things")
				}
				if pl.Season != 5 || pl.Episode != 7 {
					t.Fatalf("Season/Episode = %d/%d, want 5/7", pl.Season, pl.Episode)
				}
				if pl.DestRadix != "Stranger Things - S05E07" {
					t.Fatalf("DestRadix = %q, want %q", pl.DestRadix, "Stranger Things - S05E07")
				}
			},
		},
		{
			name: "ShowFile_CopenhagenTest_S01E01",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "The.Copenhagen.Test.S01E01.1080p.HEVC.x265-MeGusta[EZTVx.to].mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, _ *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}

				if pl.Category != CategoryShow {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
				}
				if pl.ShowName != "The Copenhagen Test" {
					t.Fatalf("ShowName = %q, want %q", pl.ShowName, "The Copenhagen Test")
				}
				if pl.Season != 1 || pl.Episode != 1 {
					t.Fatalf("Season/Episode = %d/%d, want 1/1", pl.Season, pl.Episode)
				}
				if pl.DestRadix != "The Copenhagen Test - S01E01" {
					t.Fatalf("DestRadix = %q, want %q", pl.DestRadix, "The Copenhagen Test - S01E01")
				}
			},
		},
		{
			name: "ShowFile_Invincible_S00E01_Special",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "invincible.2021.s00e01.presenting.atom.eve.special.episode.1080p.web.h264-nhtfs.mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, _ *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.Category != CategoryShow {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
				}
				if pl.ShowName != "Invincible" {
					t.Fatalf("ShowName = %q, want %q", pl.ShowName, "Invincible")
				}
				if pl.Season != 0 || pl.Episode != 1 {
					t.Fatalf("Season/Episode = %d/%d, want 0/1", pl.Season, pl.Episode)
				}
				if pl.DestRadix != "Invincible (2021) - S00E01" {
					t.Fatalf("DestRadix = %q, want %q", pl.DestRadix, "Invincible (2021) - S00E01")
				}
			},
		},
		{
			name: "ShowFile_Fallout_2024_S02E04",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "Fallout 2024 S02E04 1080p x265-ELiTE[EZTVx.to].mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, _ *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}

				if pl.Category != CategoryShow {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
				}
				if pl.ShowName != "Fallout" {
					t.Fatalf("ShowName = %q, want %q", pl.ShowName, "Fallout")
				}
				if pl.ShowYear != "2024" {
					t.Fatalf("ShowYear = %q, want %q", pl.ShowYear, "2024")
				}
				if pl.Season != 2 || pl.Episode != 4 {
					t.Fatalf("Season/Episode = %d/%d, want 2/4", pl.Season, pl.Episode)
				}
				if pl.DestRadix != "Fallout (2024) - S02E04" {
					t.Fatalf("DestRadix = %q, want %q", pl.DestRadix, "Fallout (2024) - S02E04")
				}
			},
		},
		{
			name: "ShowFile_Fallout_YearPrefersNoYearFolder",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				mkdirAll(t, filepath.Join(p.cfg.ShowsDir, "Fallout"))

				name := "Fallout 2024 S02E07 1080p x265-ELiTE[EZTVx.to].mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, p *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.ShowName != "Fallout" {
					t.Fatalf("ShowName = %q, want %q", pl.ShowName, "Fallout")
				}
				if pl.ShowYear != "" {
					t.Fatalf("ShowYear = %q, want empty", pl.ShowYear)
				}
				if !strings.Contains(pl.DestDir, filepath.Join(p.cfg.ShowsDir, "Fallout")) {
					t.Fatalf("DestDir = %q, expected under shows dir %q", pl.DestDir, p.cfg.ShowsDir)
				}
			},
		},
		{
			name: "ShowFile_TheBear_YearInParens_PrefersNoYearFolder",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				mkdirAll(t, filepath.Join(p.cfg.ShowsDir, "The Bear"))

				name := "The Bear (2022) - S02E01 - Beef (1080p HULU WEB-DL x265 Silence).mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, p *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.ShowName != "The Bear" {
					t.Fatalf("ShowName = %q, want %q", pl.ShowName, "The Bear")
				}
				if pl.ShowYear != "" {
					t.Fatalf("ShowYear = %q, want empty", pl.ShowYear)
				}
				if pl.DestRadix != "The Bear - S02E01" {
					t.Fatalf("DestRadix = %q, want %q", pl.DestRadix, "The Bear - S02E01")
				}
				if !strings.Contains(pl.DestDir, filepath.Join(p.cfg.ShowsDir, "The Bear")) {
					t.Fatalf("DestDir = %q, expected under shows dir %q", pl.DestDir, p.cfg.ShowsDir)
				}
			},
		},
		{
			name: "ShowFile_TheBear_YearInParens_CreatesYearFolder",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "The Bear (2022) - S02E01 - Beef (1080p HULU WEB-DL x265 Silence).mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, p *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.ShowName != "The Bear" {
					t.Fatalf("ShowName = %q, want %q", pl.ShowName, "The Bear")
				}
				if pl.ShowYear != "2022" {
					t.Fatalf("ShowYear = %q, want %q", pl.ShowYear, "2022")
				}
				if pl.DestRadix != "The Bear (2022) - S02E01" {
					t.Fatalf("DestRadix = %q, want %q", pl.DestRadix, "The Bear (2022) - S02E01")
				}
				if !strings.Contains(pl.DestDir, filepath.Join(p.cfg.ShowsDir, "The Bear (2022)")) {
					t.Fatalf("DestDir = %q, expected under shows dir %q", pl.DestDir, p.cfg.ShowsDir)
				}
			},
		},
		{
			name: "ShowFile_Fallout_YearMatchesExactFolder",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				mkdirAll(t, filepath.Join(p.cfg.ShowsDir, "Fallout (2024)"))

				name := "Fallout 2024 S02E07 1080p x265-ELiTE[EZTVx.to].mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, p *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.ShowYear != "2024" {
					t.Fatalf("ShowYear = %q, want %q", pl.ShowYear, "2024")
				}
				if !strings.Contains(pl.DestDir, filepath.Join(p.cfg.ShowsDir, "Fallout (2024)")) {
					t.Fatalf("DestDir = %q, expected under shows dir %q", pl.DestDir, p.cfg.ShowsDir)
				}
			},
		},
		{
			name: "ShowFile_Fallout_YearCreatesNewWhenOnlyDifferentYearExists",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				mkdirAll(t, filepath.Join(p.cfg.ShowsDir, "Fallout (1997)"))

				name := "Fallout 2024 S02E07 1080p x265-ELiTE[EZTVx.to].mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, p *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.ShowYear != "2024" {
					t.Fatalf("ShowYear = %q, want %q", pl.ShowYear, "2024")
				}
				if !strings.Contains(pl.DestDir, filepath.Join(p.cfg.ShowsDir, "Fallout (2024)")) {
					t.Fatalf("DestDir = %q, expected under shows dir %q", pl.DestDir, p.cfg.ShowsDir)
				}
			},
		},
		{
			name: "ShowFile_Fallout_NoYearPrefersNoYearFolder",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				mkdirAll(t, filepath.Join(p.cfg.ShowsDir, "Fallout"))

				name := "Fallout S02E07 1080p x265-ELiTE[EZTVx.to].mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, p *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.ShowYear != "" {
					t.Fatalf("ShowYear = %q, want empty", pl.ShowYear)
				}
				if !strings.Contains(pl.DestDir, filepath.Join(p.cfg.ShowsDir, "Fallout")) {
					t.Fatalf("DestDir = %q, expected under shows dir %q", pl.DestDir, p.cfg.ShowsDir)
				}
			},
		},
		{
			name: "ShowFile_Fallout_NoYearSingleYearFolder",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				mkdirAll(t, filepath.Join(p.cfg.ShowsDir, "Fallout (2024)"))

				name := "Fallout S02E07 1080p x265-ELiTE[EZTVx.to].mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, p *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.ShowYear != "2024" {
					t.Fatalf("ShowYear = %q, want %q", pl.ShowYear, "2024")
				}
				if !strings.Contains(pl.DestDir, filepath.Join(p.cfg.ShowsDir, "Fallout (2024)")) {
					t.Fatalf("DestDir = %q, expected under shows dir %q", pl.DestDir, p.cfg.ShowsDir)
				}
			},
		},
		{
			name: "ShowFile_Fallout_NoYearAmbiguousYearFolders",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				mkdirAll(t, filepath.Join(p.cfg.ShowsDir, "Fallout (1997)"))
				mkdirAll(t, filepath.Join(p.cfg.ShowsDir, "Fallout (2024)"))

				name := "Fallout S02E07 1080p x265-ELiTE[EZTVx.to].mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, _ *processorImpl, _ string, _ Plan, err error) {
				t.Helper()

				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !errors.Is(err, ErrAmbiguousShow) {
					t.Fatalf("error = %v, want ErrAmbiguousShow", err)
				}
			},
		},
		{
			name: "ShowFile_LowercaseSeasonEpisodeToken",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "the.copenhagen.test.s01e02.1080p.x265.mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, _ *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.Category != CategoryShow {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
				}
				if pl.ShowName != "The Copenhagen Test" {
					t.Fatalf("ShowName = %q, want %q", pl.ShowName, "The Copenhagen Test")
				}
				if pl.Season != 1 || pl.Episode != 2 {
					t.Fatalf("Season/Episode = %d/%d, want 1/2", pl.Season, pl.Episode)
				}
				if pl.DestRadix != "The Copenhagen Test - S01E02" {
					t.Fatalf("DestRadix = %q, want %q", pl.DestRadix, "The Copenhagen Test - S01E02")
				}
			},
		},
		{
			name: "ShowFile_ThreeDigitEpisodeNumber",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "One.Piece.S21E100.1080p.WEBRip.x265.mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, _ *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.Category != CategoryShow {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
				}
				if pl.ShowName != "One Piece" {
					t.Fatalf("ShowName = %q, want %q", pl.ShowName, "One Piece")
				}
				if pl.Season != 21 || pl.Episode != 100 {
					t.Fatalf("Season/Episode = %d/%d, want 21/100", pl.Season, pl.Episode)
				}
				if pl.DestRadix != "One Piece - S21E100" {
					t.Fatalf("DestRadix = %q, want %q", pl.DestRadix, "One Piece - S21E100")
				}
				if !strings.HasSuffix(pl.DestDir, "Season 21") {
					t.Fatalf("DestDir = %q, want suffix %q", pl.DestDir, "Season 21")
				}
			},
		},
		{
			name: "ShowFile_WebsitePrefixStripped",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "www.UIndex.org - Invincible 2021 S02E01 A LESSON FOR YOUR NEXT LIFE 1080p AMZN WEB-DL DDP5 1 H 264-FLUX.mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, _ *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.Category != CategoryShow {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
				}
				if pl.ShowName != "Invincible" {
					t.Fatalf("ShowName = %q, want %q", pl.ShowName, "Invincible")
				}
				if pl.ShowYear != "2021" {
					t.Fatalf("ShowYear = %q, want %q", pl.ShowYear, "2021")
				}
				if pl.Season != 2 || pl.Episode != 1 {
					t.Fatalf("Season/Episode = %d/%d, want 2/1", pl.Season, pl.Episode)
				}
				if pl.DestRadix != "Invincible (2021) - S02E01" {
					t.Fatalf("DestRadix = %q, want %q", pl.DestRadix, "Invincible (2021) - S02E01")
				}
			},
		},
		{
			name: "MovieFile_MultipleBracketTags",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "Movie.Title.2024.[WEBRip].[x265].[YTS].mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, _ *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.Category != CategoryMovie {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryMovie)
				}
				if pl.MovieTitle != "Movie Title (2024)" {
					t.Fatalf("MovieTitle = %q, want %q", pl.MovieTitle, "Movie Title (2024)")
				}
				if !strings.HasSuffix(pl.DestMainPath, "Movie Title (2024).mkv") {
					t.Fatalf("DestMainPath = %q, want suffix %q", pl.DestMainPath, "Movie Title (2024).mkv")
				}
			},
		},
		{
			name: "MovieFile_EpisodeWordOnly_RemainsMovie",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "Star.Wars.Episode.IV.1977.1080p.BluRay.x265.mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, _ *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.Category != CategoryMovie {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryMovie)
				}
				if pl.MovieTitle != "Star Wars Episode IV (1977)" {
					t.Fatalf("MovieTitle = %q, want %q", pl.MovieTitle, "Star Wars Episode IV (1977)")
				}
			},
		},
		{
			name: "MovieFile_LowercaseSmallTitleWords",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "War.of.the.Worlds.2005.1080p.BluRay.x265.mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, _ *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.Category != CategoryMovie {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryMovie)
				}
				if pl.MovieTitle != "War of the Worlds (2005)" {
					t.Fatalf("MovieTitle = %q, want %q", pl.MovieTitle, "War of the Worlds (2005)")
				}
				if !strings.HasSuffix(pl.DestMainPath, "War of the Worlds (2005).mkv") {
					t.Fatalf("DestMainPath = %q, want suffix %q", pl.DestMainPath, "War of the Worlds (2005).mkv")
				}
			},
		},
		{
			name: "MovieFile_AllOfUsStrangers_UsNotForcedUppercase",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "All.Of.Us.Strangers.2023.1080p.WebRip.X264.Will1869.mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, _ *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.Category != CategoryMovie {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryMovie)
				}
				if pl.MovieTitle != "All of Us Strangers (2023)" {
					t.Fatalf("MovieTitle = %q, want %q", pl.MovieTitle, "All of Us Strangers (2023)")
				}
				if !strings.HasSuffix(pl.DestMainPath, "All of Us Strangers (2023).mkv") {
					t.Fatalf("DestMainPath = %q, want suffix %q", pl.DestMainPath, "All of Us Strangers (2023).mkv")
				}
			},
		},
		{
			name: "ShowFile_AssociatedNFO_NoLangTag",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "Robin.Hood.2025.S01E01.1080p.x265-ELiTE.mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")

				nfo := filepath.Join(p.cfg.DropFolder, "Robin.Hood.2025.S01E01.1080p.x265-ELiTE.nfo")
				writeFile(t, nfo, "nfo")

				return src
			},
			check: func(t *testing.T, p *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.Category != CategoryShow {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
				}
				if pl.ShowName != "Robin Hood" {
					t.Fatalf("ShowName = %q, want %q", pl.ShowName, "Robin Hood")
				}
				if pl.Season != 1 || pl.Episode != 1 {
					t.Fatalf("Season/Episode = %d/%d, want 1/1", pl.Season, pl.Episode)
				}
				wantRadix := "Robin Hood (2025) - S01E01"
				if pl.DestRadix != wantRadix {
					t.Fatalf("DestRadix = %q, want %q", pl.DestRadix, wantRadix)
				}

				nfo := filepath.Join(p.cfg.DropFolder, "Robin.Hood.2025.S01E01.1080p.x265-ELiTE.nfo")
				found := false
				for _, mv := range pl.Associated {
					if mv.Kind != "associated" {
						continue
					}
					if mv.Source == nfo {
						found = true
						if !strings.HasSuffix(mv.Dest, wantRadix+".nfo") {
							t.Fatalf("Associated dest = %q, want suffix %q", mv.Dest, wantRadix+".nfo")
						}
					}
				}
				if !found {
					t.Fatalf("expected associated move for %q", nfo)
				}
			},
		},
		{
			name: "ShowFile_AcronymUS_PreservedUppercase",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "Hells.Kitchen.US.S24E14.1080p.HEVC.x265.mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, _ *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.Category != CategoryShow {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
				}
				if pl.ShowName != "Hells Kitchen US" {
					t.Fatalf("ShowName = %q, want %q", pl.ShowName, "Hells Kitchen US")
				}
			},
		},
		{
			name: "ShowFile_AcronymUS_PreservedUppercase_WhenSuffixLowercase",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "Hells.Kitchen.us.S24E14.1080p.HEVC.x265.mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, _ *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.Category != CategoryShow {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
				}
				if pl.ShowName != "Hells Kitchen US" {
					t.Fatalf("ShowName = %q, want %q", pl.ShowName, "Hells Kitchen US")
				}
			},
		},
		{
			name: "ShowFile_Hyphen_Preserve_XMen97",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "X-Men.97.S01E01.720p.DSNP.WEBRip.x264-GalaxyTV.mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, _ *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.Category != CategoryShow {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
				}
				if pl.ShowName != "X-Men 97" {
					t.Fatalf("ShowName = %q, want %q", pl.ShowName, "X-Men 97")
				}
				if pl.Season != 1 || pl.Episode != 1 {
					t.Fatalf("Season/Episode = %d/%d, want 1/1", pl.Season, pl.Episode)
				}
			},
		},
		{
			name: "ShowFile_AcronymUS_Preserved_InParens",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "Ghosts (US) (2021) - S04E01 - Patience.mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, _ *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.Category != CategoryShow {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
				}
				if pl.ShowName != "Ghosts (US)" {
					t.Fatalf("ShowName = %q, want %q", pl.ShowName, "Ghosts (US)")
				}
				if pl.ShowYear != "2021" {
					t.Fatalf("ShowYear = %q, want %q", pl.ShowYear, "2021")
				}
				if pl.Season != 4 || pl.Episode != 1 {
					t.Fatalf("Season/Episode = %d/%d, want 4/1", pl.Season, pl.Episode)
				}
			},
		},
		{
			name: "ShowFile_TheOffice_NoYear_SingleQualifiedFolder_UsesBestEffortMatch",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				mkdirAll(t, filepath.Join(p.cfg.ShowsDir, "The Office (UK)"))

				name := "The.Office.S03E01.mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, p *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if !strings.Contains(pl.DestDir, filepath.Join(p.cfg.ShowsDir, "The Office (UK)")) {
					t.Fatalf("DestDir = %q, expected under shows dir %q", pl.DestDir, filepath.Join(p.cfg.ShowsDir, "The Office (UK)"))
				}
			},
		},
		{
			name: "ShowFile_TheOffice_NoYear_MultipleQualifiedFolders_Ambiguous",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				mkdirAll(t, filepath.Join(p.cfg.ShowsDir, "The Office (UK)"))
				mkdirAll(t, filepath.Join(p.cfg.ShowsDir, "The Office (US)"))

				name := "The.Office.S03E01.mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, _ *processorImpl, _ string, _ Plan, err error) {
				t.Helper()

				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !errors.Is(err, ErrAmbiguousShow) {
					t.Fatalf("error = %v, want ErrAmbiguousShow", err)
				}
			},
		},
		{
			name: "ShowFile_TheOffice_WithYear_QualifiedFolderPreferredOverNewYearFolder",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				mkdirAll(t, filepath.Join(p.cfg.ShowsDir, "The Office (UK)"))

				name := "The.Office.2019.S03E01.mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, p *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if !strings.Contains(pl.DestDir, filepath.Join(p.cfg.ShowsDir, "The Office (UK)")) {
					t.Fatalf("DestDir = %q, expected under shows dir %q", pl.DestDir, filepath.Join(p.cfg.ShowsDir, "The Office (UK)"))
				}
				if strings.Contains(pl.DestDir, "2019") {
					t.Fatalf("DestDir = %q, should not have created a new (2019) folder", pl.DestDir)
				}
			},
		},
		{
			name: "ShowFolder_YearPack_WithExistingNoYearFolder_Rule1_DropsYear",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				// Simulate a prior season (no year in filename) having created Ghosts/ already.
				mkdirAll(t, filepath.Join(p.cfg.ShowsDir, "Ghosts"))

				// Season pack folder with year embedded in folder name (common eztv pattern).
				root := filepath.Join(p.cfg.DropFolder, "Ghosts.2021.S02.1080p.WEBRip.x265")
				writeFile(t, filepath.Join(root, "Ghosts.2021.S02E01.1080p.WEBRip.x265.mp4"), "dummy")
				return root
			},
			checkMany: func(t *testing.T, p *processorImpl, _ string, plans []Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if len(plans) != 1 {
					t.Fatalf("expected 1 plan, got %d", len(plans))
				}
				pl := plans[0]
				if pl.Category != CategoryShow {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
				}
				if pl.ShowName != "Ghosts" {
					t.Fatalf("ShowName = %q, want %q", pl.ShowName, "Ghosts")
				}
				// Rule 1 must fire: existing no-year folder takes precedence.
				if pl.ShowYear != "" {
					t.Fatalf("ShowYear = %q, want empty (Rule 1: no-year folder exists)", pl.ShowYear)
				}
				if pl.DestRadix != "Ghosts - S02E01" {
					t.Fatalf("DestRadix = %q, want %q", pl.DestRadix, "Ghosts - S02E01")
				}
				wantDir := filepath.Join(p.cfg.ShowsDir, "Ghosts", "Season 02")
				if !strings.Contains(pl.DestDir, wantDir) {
					t.Fatalf("DestDir = %q, expected to contain %q", pl.DestDir, wantDir)
				}
			},
		},
		{
			name: "ShowFile_ExistingFolder_TheLastOfUs_UsesFolderCasingInDestRadix",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				mkdirAll(t, filepath.Join(p.cfg.ShowsDir, "The Last of Us"))

				name := "The.Last.of.US.S02E01.1080p.HEVC.x265.mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, p *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.Category != CategoryShow {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
				}
				if !strings.Contains(pl.DestDir, filepath.Join(p.cfg.ShowsDir, "The Last of Us")) {
					t.Fatalf("DestDir = %q, expected under shows dir %q", pl.DestDir, filepath.Join(p.cfg.ShowsDir, "The Last of Us"))
				}
				if pl.DestRadix != "The Last of Us - S02E01" {
					t.Fatalf("DestRadix = %q, want %q", pl.DestRadix, "The Last of Us - S02E01")
				}
				if !strings.HasSuffix(pl.DestMainPath, "The Last of Us - S02E01.mkv") {
					t.Fatalf("DestMainPath = %q, want suffix %q", pl.DestMainPath, "The Last of Us - S02E01.mkv")
				}
			},
		},
		{
			name: "ShowFile_ExistingYearFolder_TheLastOfUs_UsesFolderCasingInDestRadix",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				mkdirAll(t, filepath.Join(p.cfg.ShowsDir, "The Last of Us (2023)"))

				name := "The.Last.of.US.2023.S02E01.1080p.HEVC.x265.mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, p *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.Category != CategoryShow {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryShow)
				}
				if pl.ShowYear != "2023" {
					t.Fatalf("ShowYear = %q, want %q", pl.ShowYear, "2023")
				}
				if !strings.Contains(pl.DestDir, filepath.Join(p.cfg.ShowsDir, "The Last of Us (2023)")) {
					t.Fatalf("DestDir = %q, expected under shows dir %q", pl.DestDir, filepath.Join(p.cfg.ShowsDir, "The Last of Us (2023)"))
				}
				if pl.DestRadix != "The Last of Us (2023) - S02E01" {
					t.Fatalf("DestRadix = %q, want %q", pl.DestRadix, "The Last of Us (2023) - S02E01")
				}
				if !strings.HasSuffix(pl.DestMainPath, "The Last of Us (2023) - S02E01.mkv") {
					t.Fatalf("DestMainPath = %q, want suffix %q", pl.DestMainPath, "The Last of Us (2023) - S02E01.mkv")
				}
			},
		},
		{
			name: "MovieFile_RomanNumeral_PreservedUppercase",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "Rocky.IV.1985.1080p.BluRay.x265.mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, _ *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.Category != CategoryMovie {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryMovie)
				}
				if pl.MovieTitle != "Rocky IV (1985)" {
					t.Fatalf("MovieTitle = %q, want %q", pl.MovieTitle, "Rocky IV (1985)")
				}
				if !strings.HasSuffix(pl.DestMainPath, "Rocky IV (1985).mkv") {
					t.Fatalf("DestMainPath = %q, want suffix %q", pl.DestMainPath, "Rocky IV (1985).mkv")
				}
			},
		},
		{
			name: "MovieFile_Hyphen_Preserve_SpiderMan",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "Spider-Man.No.Way.Home.2021.1080p.x265.mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, _ *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if pl.Category != CategoryMovie {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryMovie)
				}
				if pl.MovieTitle != "Spider-Man No Way Home (2021)" {
					t.Fatalf("MovieTitle = %q, want %q", pl.MovieTitle, "Spider-Man No Way Home (2021)")
				}
			},
		},
		{
			name: "MovieFolder_GetSmart_2008",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				folderName := "Get Smart (2008) [1080p]"
				folder := filepath.Join(p.cfg.DropFolder, folderName)
				mkdirAll(t, folder)

				fileName := "Get.Smart.2008.1080p.BRrip.x264.YIFY.mp4"
				srcFile := filepath.Join(folder, fileName)
				writeFile(t, srcFile, "dummy")

				return folder
			},
			check: func(t *testing.T, p *processorImpl, _ string, pl Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}

				folderName := "Get Smart (2008) [1080p]"
				folder := filepath.Join(p.cfg.DropFolder, folderName)
				fileName := "Get.Smart.2008.1080p.BRrip.x264.YIFY.mp4"
				srcFile := filepath.Join(folder, fileName)

				if pl.Category != CategoryMovie {
					t.Fatalf("Category = %q, want %q", pl.Category, CategoryMovie)
				}
				if pl.MovieTitle != "Get Smart (2008)" {
					t.Fatalf("MovieTitle = %q, want %q", pl.MovieTitle, "Get Smart (2008)")
				}
				if pl.MainSourcePath != srcFile {
					t.Fatalf("MainSourcePath = %q, want %q", pl.MainSourcePath, srcFile)
				}
				if !strings.HasSuffix(pl.DestMainPath, "Get Smart (2008).mp4") {
					t.Fatalf("DestMainPath = %q, want suffix %q", pl.DestMainPath, "Get Smart (2008).mp4")
				}
			},
		},
		{
			name: "MovieFolder_BourneCollection_MultiMoviePack_PrefersFilenameParsing",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				root := filepath.Join(p.cfg.DropFolder, "The Jason Bourne Collection 2004-2016 1080p BluRay HEVC x265 5.1 BONE")
				writeFile(t, filepath.Join(root, "Jason Bourne 2016 1080p BluRay HEVC x265 5.1 BONE.mkv"), "dummy")
				writeFile(t, filepath.Join(root, "The Bourne Identity 2002 1080p BluRay HEVC x265 5.1 BONE.mkv"), "dummy")
				writeFile(t, filepath.Join(root, "The Bourne Supremacy 2004 1080p BluRay HEVC x265 5.1 BONE.mkv"), "dummy")
				writeFile(t, filepath.Join(root, "The Bourne Ultimatum 2007 1080p BluRay HEVC x265 5.1 BONE.mkv"), "dummy")
				return root
			},
			checkMany: func(t *testing.T, _ *processorImpl, _ string, plans []Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if len(plans) != 4 {
					t.Fatalf("expected 4 plans, got %d", len(plans))
				}

				wantTitles := map[string]struct{}{
					"Jason Bourne (2016)":         {},
					"The Bourne Identity (2002)":  {},
					"The Bourne Supremacy (2004)": {},
					"The Bourne Ultimatum (2007)": {},
				}

				for _, pl := range plans {
					if pl.Category != CategoryMovie {
						t.Fatalf("Category = %q, want %q", pl.Category, CategoryMovie)
					}
					if pl.MovieTitle == "The Jason Bourne Collection (2004)" {
						t.Fatalf("MovieTitle used folder parse fallback: %q", pl.MovieTitle)
					}
					if _, ok := wantTitles[pl.MovieTitle]; !ok {
						t.Fatalf("unexpected MovieTitle = %q", pl.MovieTitle)
					}
					delete(wantTitles, pl.MovieTitle)
					if !strings.HasSuffix(pl.DestMainPath, pl.MovieTitle+pl.MainExt) {
						t.Fatalf("DestMainPath = %q, want suffix %q", pl.DestMainPath, pl.MovieTitle+pl.MainExt)
					}
				}
				if len(wantTitles) != 0 {
					t.Fatalf("missing expected movie titles: %v", wantTitles)
				}
			},
		},
		{
			name: "MovieFolder_MultiMoviePack_UnparseableFilename_SkippedNoFolderFallback",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				root := filepath.Join(p.cfg.DropFolder, "The Jason Bourne Collection 2004-2016 1080p BluRay HEVC x265 5.1 BONE")
				writeFile(t, filepath.Join(root, "The Bourne Identity 2002 1080p BluRay HEVC x265 5.1 BONE.mkv"), "dummy")
				writeFile(t, filepath.Join(root, "1080p.x265.hevc.bluray.mkv"), "dummy")
				return root
			},
			checkMany: func(t *testing.T, _ *processorImpl, inputPath string, plans []Plan, err error) {
				t.Helper()

				var partial *PartialPlanError
				if !errors.As(err, &partial) {
					t.Fatalf("expected PartialPlanError, got %v", err)
				}
				if len(plans) != 1 {
					t.Fatalf("expected 1 plan, got %d", len(plans))
				}
				if plans[0].Category != CategoryMovie {
					t.Fatalf("Category = %q, want %q", plans[0].Category, CategoryMovie)
				}
				if plans[0].MovieTitle != "The Bourne Identity (2002)" {
					t.Fatalf("MovieTitle = %q, want %q", plans[0].MovieTitle, "The Bourne Identity (2002)")
				}
				if partial == nil || len(partial.Issues) != 1 {
					t.Fatalf("expected 1 issue, got %v", partial)
				}

				wantPath := filepath.Join(inputPath, "1080p.x265.hevc.bluray.mkv")
				if partial.Issues[0].Path != wantPath {
					t.Fatalf("issue path = %q, want %q", partial.Issues[0].Path, wantPath)
				}
				var pme *ParseMovieError
				if !errors.As(partial.Issues[0].Err, &pme) {
					t.Fatalf("issue err type = %T, want *ParseMovieError", partial.Issues[0].Err)
				}
			},
		},
		{
			name: "DirSelectsAllMediaWithinDepth2",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				// Folder contains a small mkv at depth 1 and a larger mkv at depth 2.
				rootFolder := filepath.Join(p.cfg.DropFolder, "Some.Show.S01E01.1080p.WEB-DL.x265-Group")
				mkdirAll(t, rootFolder)

				small := filepath.Join(rootFolder, "small.mkv")
				writeFile(t, small, strings.Repeat("a", 10))

				subdir := filepath.Join(rootFolder, "sub")
				mkdirAll(t, subdir)

				large := filepath.Join(subdir, "large.mkv")
				writeFile(t, large, strings.Repeat("b", 200))

				return rootFolder
			},
			checkMany: func(t *testing.T, p *processorImpl, _ string, plans []Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}
				if len(plans) != 2 {
					t.Fatalf("expected 2 plans, got %d", len(plans))
				}

				rootFolder := filepath.Join(p.cfg.DropFolder, "Some.Show.S01E01.1080p.WEB-DL.x265-Group")
				subdir := filepath.Join(rootFolder, "sub")
				small := filepath.Join(rootFolder, "small.mkv")
				large := filepath.Join(subdir, "large.mkv")

				var gotSmall bool
				var gotLarge bool
				for _, pl := range plans {
					if pl.MainSourcePath == small {
						gotSmall = true
					}
					if pl.MainSourcePath == large {
						gotLarge = true
					}
				}
				if !gotSmall || !gotLarge {
					t.Fatalf("expected plans for %q and %q", small, large)
				}
			},
		},
		{
			name: "DirSelectsMediaWithinMaxDepth",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				rootFolder := filepath.Join(p.cfg.DropFolder, "DepthTest")
				mkdirAll(t, rootFolder)

				inDepthDir := filepath.Join(rootFolder, "d1", "d2", "d3", "d4", "d5", "d6")
				mkdirAll(t, inDepthDir)
				inDepth := filepath.Join(inDepthDir, "in_depth.mkv")
				writeFile(t, inDepth, strings.Repeat("a", 10))

				tooDeepDir := filepath.Join(inDepthDir, "d7")
				mkdirAll(t, tooDeepDir)
				tooDeep := filepath.Join(tooDeepDir, "too_deep.mkv")
				writeFile(t, tooDeep, strings.Repeat("b", 10))

				return rootFolder
			},
			checkMany: func(t *testing.T, p *processorImpl, _ string, plans []Plan, err error) {
				t.Helper()

				if err != nil {
					t.Fatalf("Plan() error: %v", err)
				}

				rootFolder := filepath.Join(p.cfg.DropFolder, "DepthTest")
				inDepth := filepath.Join(rootFolder, "d1", "d2", "d3", "d4", "d5", "d6", "in_depth.mkv")
				tooDeep := filepath.Join(rootFolder, "d1", "d2", "d3", "d4", "d5", "d6", "d7", "too_deep.mkv")

				var gotInDepth bool
				var gotTooDeep bool
				for _, pl := range plans {
					if pl.MainSourcePath == inDepth {
						gotInDepth = true
					}
					if pl.MainSourcePath == tooDeep {
						gotTooDeep = true
					}
				}
				if !gotInDepth {
					t.Fatalf("expected plan for %q", inDepth)
				}
				if gotTooDeep {
					t.Fatalf("did not expect plan for %q", tooDeep)
				}
			},
		},
		{
			name: "NotMedia_ReturnsErrNotMedia",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				src := filepath.Join(p.cfg.DropFolder, "_smoke_test.txt")
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, _ *processorImpl, _ string, _ Plan, err error) {
				t.Helper()

				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !errors.Is(err, ErrNotMedia) {
					t.Fatalf("error = %v, want ErrNotMedia", err)
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			p := newTestProcessor(t)
			input := c.setup(t, p)

			plans, err := p.Plan(context.Background(), Request{InputPath: input})
			if c.checkMany != nil {
				c.checkMany(t, p, input, plans, err)
				return
			}
			var pl Plan
			if err == nil {
				if len(plans) != 1 {
					t.Fatalf("expected 1 plan, got %d", len(plans))
				}
				pl = plans[0]
			}
			c.check(t, p, input, pl, err)
		})
	}
}

// TestPlan_DirectoryBatch_PreservesEarlierPlansOnDestinationUnavailable covers
// a mixed-category directory (a movie file plans first, alphabetically,
// before a show file) where the show file's resolveShowFolder call hits a
// ShowsDir that's unavailable. plan() must return the already-computed movie
// plan alongside the error instead of discarding it.
func TestPlan_DirectoryBatch_PreservesEarlierPlansOnDestinationUnavailable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permission bits")
	}
	p := newTestProcessor(t)

	dir := filepath.Join(p.cfg.DropFolder, "MixedBatch")
	mkdirAll(t, dir)
	// "A" sorts before "Z" so the movie plans successfully before the show
	// file's resolveShowFolder call ever runs.
	writeFile(t, filepath.Join(dir, "AMovie.2020.1080p.BluRay.x264-GROUP.mkv"), "dummy")
	writeFile(t, filepath.Join(dir, "Zdeadwood.S01E01.1080p.HEVC.x265-MeGusta.mkv"), "dummy")

	if err := os.Chmod(p.cfg.ShowsDir, 0o000); err != nil {
		t.Fatalf("chmod ShowsDir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(p.cfg.ShowsDir, 0o755) })

	plans, err := p.Plan(context.Background(), Request{InputPath: dir})

	var destErr *DestinationUnavailableError
	if !errors.As(err, &destErr) {
		t.Fatalf("expected *DestinationUnavailableError, got: %v", err)
	}
	if destErr.Category != CategoryShow {
		t.Fatalf("Category = %v, want %v", destErr.Category, CategoryShow)
	}

	if len(plans) != 1 {
		t.Fatalf("expected the already-planned movie to be preserved, got %d plans", len(plans))
	}
	if plans[0].Category != CategoryMovie {
		t.Fatalf("preserved plan Category = %v, want %v", plans[0].Category, CategoryMovie)
	}
}

// TestPlan_Duplicate_MovieAlreadyInLibrary covers case 1 of duplicate
// detection: a file already sits at the computed DestMainPath, so Plan must
// mark pl.Duplicate rather than leaving Apply to discover the collision as a
// raw transfer error.
func TestPlan_Duplicate_MovieAlreadyInLibrary(t *testing.T) {
	p := newTestProcessor(t)

	src := filepath.Join(p.cfg.DropFolder, "Get.Smart.2008.1080p.BluRay.x264-GROUP.mkv")
	writeFile(t, src, "dummy")

	// Pre-seed the exact destination the plan will compute.
	writeFile(t, filepath.Join(p.cfg.MoviesDir, "Get Smart (2008)", "Get Smart (2008).mkv"), "already here")

	pl, err := planOne(t, p, src)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if !pl.Duplicate {
		t.Fatalf("Duplicate = false, want true")
	}
}

// TestPlan_Duplicate_ShowAlreadyInLibrary mirrors
// TestPlan_Duplicate_MovieAlreadyInLibrary for the show category.
func TestPlan_Duplicate_ShowAlreadyInLibrary(t *testing.T) {
	p := newTestProcessor(t)

	src := filepath.Join(p.cfg.DropFolder, "Deadwood.S01E01.1080p.HEVC.x265-MeGusta.mkv")
	writeFile(t, src, "dummy")

	writeFile(t, filepath.Join(p.cfg.ShowsDir, "Deadwood", "Season 01", "Deadwood - S01E01.mkv"), "already here")

	pl, err := planOne(t, p, src)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if !pl.Duplicate {
		t.Fatalf("Duplicate = false, want true")
	}
}

// TestPlan_Duplicate_FalseWhenDestDirMissing covers the common case: a
// brand-new title has no destination folder yet, so no stat collision is
// possible and Plan must not report a duplicate.
func TestPlan_Duplicate_FalseWhenDestDirMissing(t *testing.T) {
	p := newTestProcessor(t)

	src := filepath.Join(p.cfg.DropFolder, "Fringe.2008.1080p.BluRay.x264-GROUP.mkv")
	writeFile(t, src, "dummy")

	pl, err := planOne(t, p, src)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if pl.Duplicate {
		t.Fatalf("Duplicate = true, want false")
	}
}

// TestPlan_Duplicate_StatErrorSurfaces covers a stat failure on
// DestMainPath that isn't "not exist" (e.g. permission denied on the
// destination folder) -- it must surface as a plan error rather than being
// silently treated as "no duplicate".
func TestPlan_Duplicate_StatErrorSurfaces(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permission bits")
	}
	p := newTestProcessor(t)

	src := filepath.Join(p.cfg.DropFolder, "Get.Smart.2008.1080p.BluRay.x264-GROUP.mkv")
	writeFile(t, src, "dummy")

	destDir := filepath.Join(p.cfg.MoviesDir, "Get Smart (2008)")
	mkdirAll(t, destDir)
	if err := os.Chmod(destDir, 0o000); err != nil {
		t.Fatalf("chmod destDir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(destDir, 0o755) })

	_, err := planOne(t, p, src)
	if err == nil {
		t.Fatalf("expected a plan error, got nil")
	}
}

// TestPlan_Duplicate_DifferentTitleNotFlagged ensures a sibling file in the
// same destination folder with a different title doesn't false-positive the
// duplicate check -- only an exact DestMainPath match should.
func TestPlan_Duplicate_DifferentTitleNotFlagged(t *testing.T) {
	p := newTestProcessor(t)

	src := filepath.Join(p.cfg.DropFolder, "Deadwood.S01E02.1080p.HEVC.x265-MeGusta.mkv")
	writeFile(t, src, "dummy")

	// A different episode already lives in the same season folder.
	writeFile(t, filepath.Join(p.cfg.ShowsDir, "Deadwood", "Season 01", "Deadwood - S01E01.mkv"), "already here")

	pl, err := planOne(t, p, src)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if pl.Duplicate {
		t.Fatalf("Duplicate = true, want false")
	}
}

// TestPlan_Duplicate_MovieFuzzyDiacritic covers tier 1 of fuzzy movie
// duplicate detection: a diacritic-only spelling difference against an
// existing folder, same year, must be treated as a confident duplicate.
func TestPlan_Duplicate_MovieFuzzyDiacritic(t *testing.T) {
	p := newTestProcessor(t)

	src := filepath.Join(p.cfg.DropFolder, "Amelie.2001.1080p.BluRay.x264-GROUP.mkv")
	writeFile(t, src, "dummy")

	existing := filepath.Join(p.cfg.MoviesDir, "Amélie (2001)", "Amélie (2001).mkv")
	writeFile(t, existing, "already here")

	pl, err := planOne(t, p, src)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if !pl.Duplicate {
		t.Fatalf("Duplicate = false, want true")
	}
	wantMatch := filepath.Join(p.cfg.MoviesDir, "Amélie (2001)")
	if pl.DuplicateMatchPath != wantMatch {
		t.Fatalf("DuplicateMatchPath = %q, want %q", pl.DuplicateMatchPath, wantMatch)
	}
}

// TestPlan_Duplicate_MovieFuzzyPunctuation covers tier 1 via
// punctuation/whitespace-only differences (e.g. a hyphen vs. a space).
func TestPlan_Duplicate_MovieFuzzyPunctuation(t *testing.T) {
	p := newTestProcessor(t)

	src := filepath.Join(p.cfg.DropFolder, "Spider-Man.2002.1080p.BluRay.x264-GROUP.mkv")
	writeFile(t, src, "dummy")

	writeFile(t, filepath.Join(p.cfg.MoviesDir, "Spider Man (2002)", "Spider Man (2002).mkv"), "already here")

	pl, err := planOne(t, p, src)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if !pl.Duplicate {
		t.Fatalf("Duplicate = false, want true")
	}
}

// TestPlan_Duplicate_MovieFuzzyBothNoYear covers tier 1 when neither side
// carries a year at all -- still a confident match if the normalized
// titles agree.
func TestPlan_Duplicate_MovieFuzzyBothNoYear(t *testing.T) {
	p := newTestProcessor(t)

	src := filepath.Join(p.cfg.DropFolder, "Amelie.mkv")
	writeFile(t, src, "dummy")

	writeFile(t, filepath.Join(p.cfg.MoviesDir, "Amélie", "Amélie.mkv"), "already here")

	pl, err := planOne(t, p, src)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if !pl.Duplicate {
		t.Fatalf("Duplicate = false, want true")
	}
}

// TestPlan_Duplicate_MovieFuzzyPossible_ExistingHasYear covers tier 2: the
// existing library folder carries a year the incoming file doesn't. This
// must not skip -- the new folder is still created -- since we can't tell
// from the title alone whether it's the same movie or a different one.
func TestPlan_Duplicate_MovieFuzzyPossible_ExistingHasYear(t *testing.T) {
	p := newTestProcessor(t)

	src := filepath.Join(p.cfg.DropFolder, "Survivor.mkv")
	writeFile(t, src, "dummy")

	writeFile(t, filepath.Join(p.cfg.MoviesDir, "Survivor (2000)", "Survivor (2000).mkv"), "already here")

	pl, err := planOne(t, p, src)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if pl.Duplicate {
		t.Fatalf("Duplicate = true, want false (tier 2 should warn, not skip)")
	}
}

// TestPlan_Duplicate_MovieFuzzyPossible_IncomingHasYear mirrors
// TestPlan_Duplicate_MovieFuzzyPossible_ExistingHasYear with the year
// asymmetry reversed: the incoming file has a year, the existing folder
// doesn't.
func TestPlan_Duplicate_MovieFuzzyPossible_IncomingHasYear(t *testing.T) {
	p := newTestProcessor(t)

	src := filepath.Join(p.cfg.DropFolder, "Survivor.2000.1080p.BluRay.x264-GROUP.mkv")
	writeFile(t, src, "dummy")

	writeFile(t, filepath.Join(p.cfg.MoviesDir, "Survivor", "Survivor.mkv"), "already here")

	pl, err := planOne(t, p, src)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if pl.Duplicate {
		t.Fatalf("Duplicate = true, want false (tier 2 should warn, not skip)")
	}
}

// TestPlan_Duplicate_MovieFuzzyPossible_MultipleCandidates covers the case
// where more than one existing folder fuzzy-matches the incoming title
// under tier 2 -- Plan must still proceed (create normally) rather than
// erroring or picking one arbitrarily.
func TestPlan_Duplicate_MovieFuzzyPossible_MultipleCandidates(t *testing.T) {
	p := newTestProcessor(t)

	src := filepath.Join(p.cfg.DropFolder, "Survivor.mkv")
	writeFile(t, src, "dummy")

	writeFile(t, filepath.Join(p.cfg.MoviesDir, "Survivor (2000)", "Survivor (2000).mkv"), "already here")
	writeFile(t, filepath.Join(p.cfg.MoviesDir, "Survivor (2015)", "Survivor (2015).mkv"), "already here too")

	pl, err := planOne(t, p, src)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if pl.Duplicate {
		t.Fatalf("Duplicate = true, want false (tier 2 should warn, not skip)")
	}
}

// TestPlan_Duplicate_MovieFuzzy_DifferentYearsNoAction covers the
// no-ambiguity case: both sides carry a year and they disagree, which is
// treated as evidence of two different movies, not a possible duplicate.
func TestPlan_Duplicate_MovieFuzzy_DifferentYearsNoAction(t *testing.T) {
	p := newTestProcessor(t)

	src := filepath.Join(p.cfg.DropFolder, "Survivor.2006.1080p.BluRay.x264-GROUP.mkv")
	writeFile(t, src, "dummy")

	writeFile(t, filepath.Join(p.cfg.MoviesDir, "Survivor (2000)", "Survivor (2000).mkv"), "already here")

	pl, err := planOne(t, p, src)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if pl.Duplicate {
		t.Fatalf("Duplicate = true, want false (different years means different movies)")
	}
}

// TestPlan_Duplicate_MovieFuzzy_ArticleNotDropped proves the normalizer
// never drops words: a leading article difference must not fuzzy-match,
// even though every other character is identical.
func TestPlan_Duplicate_MovieFuzzy_ArticleNotDropped(t *testing.T) {
	p := newTestProcessor(t)

	src := filepath.Join(p.cfg.DropFolder, "The.Amazing.Spiderman.2012.1080p.BluRay.x264-GROUP.mkv")
	writeFile(t, src, "dummy")

	writeFile(t, filepath.Join(p.cfg.MoviesDir, "Amazing Spiderman (2012)", "Amazing Spiderman (2012).mkv"), "already here")

	pl, err := planOne(t, p, src)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if pl.Duplicate {
		t.Fatalf("Duplicate = true, want false (leading article must not be dropped by normalization)")
	}
}
