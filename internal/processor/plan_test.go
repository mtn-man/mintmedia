package processor

import (
	"context"
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
			check: func(t *testing.T, p *processorImpl, inputPath string, pl Plan, err error) {
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
			name: "ShowFile_StrangerThings_S05E07_WithEpisodeTitleNoise",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "Stranger.Things.S05E07.Chapter.Seven.The.Bridge.1080p.HEVC.x265-MeGusta[EZTVx.to].mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, p *processorImpl, inputPath string, pl Plan, err error) {
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
			check: func(t *testing.T, p *processorImpl, inputPath string, pl Plan, err error) {
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
			name: "ShowFile_Fallout_2024_S02E04",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "Fallout 2024 S02E04 1080p x265-ELiTE[EZTVx.to].mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, p *processorImpl, inputPath string, pl Plan, err error) {
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
				if pl.DestRadix != "Fallout - S02E04" {
					t.Fatalf("DestRadix = %q, want %q", pl.DestRadix, "Fallout - S02E04")
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
			check: func(t *testing.T, p *processorImpl, inputPath string, pl Plan, err error) {
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
			check: func(t *testing.T, p *processorImpl, inputPath string, pl Plan, err error) {
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
			check: func(t *testing.T, p *processorImpl, inputPath string, pl Plan, err error) {
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
			name: "MovieFile_LowercaseSmallTitleWords",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "War.of.the.Worlds.2005.1080p.BluRay.x265.mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, p *processorImpl, inputPath string, pl Plan, err error) {
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
			check: func(t *testing.T, p *processorImpl, inputPath string, pl Plan, err error) {
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
				wantRadix := "Robin Hood - S01E01"
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
			check: func(t *testing.T, p *processorImpl, inputPath string, pl Plan, err error) {
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
			name: "MovieFile_RomanNumeral_PreservedUppercase",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "Rocky.IV.1985.1080p.BluRay.x265.mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, p *processorImpl, inputPath string, pl Plan, err error) {
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
			name: "TODO_MovieFile_Hyphen_Preserve",
			setup: func(t *testing.T, p *processorImpl) string {
				t.Helper()

				name := "Spider-Man.No.Way.Home.2021.1080p.x265.mkv"
				src := filepath.Join(p.cfg.DropFolder, name)
				writeFile(t, src, "dummy")
				return src
			},
			check: func(t *testing.T, p *processorImpl, inputPath string, pl Plan, err error) {
				t.Helper()

				t.Skip("TODO: decide whether to preserve hyphens; current sanitizer replaces '-' with space")

				_ = p
				_ = inputPath
				_ = pl
				_ = err
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
			check: func(t *testing.T, p *processorImpl, inputPath string, pl Plan, err error) {
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
			checkMany: func(t *testing.T, p *processorImpl, inputPath string, plans []Plan, err error) {
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
			checkMany: func(t *testing.T, p *processorImpl, inputPath string, plans []Plan, err error) {
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
			check: func(t *testing.T, p *processorImpl, inputPath string, pl Plan, err error) {
				t.Helper()

				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if err != ErrNotMedia {
					t.Fatalf("error = %v, want ErrNotMedia", err)
				}
			},
		},
	}

	for _, c := range cases {
		c := c
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

// --- test helpers -----------------------------------------------------------

func newTestProcessor(t *testing.T) *processorImpl {
	t.Helper()

	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")
	errDir := filepath.Join(root, "_error")
	hist := filepath.Join(root, "history.log")

	mkdirAll(t, drop)
	mkdirAll(t, movies)
	mkdirAll(t, shows)
	mkdirAll(t, errDir)

	cfg := Config{
		DropFolder:  drop,
		MoviesDir:   movies,
		ShowsDir:    shows,
		ErrorDir:    errDir,
		HistoryFile: hist,

		MainMediaExtensions:      []string{".mkv", ".mp4", ".avi", ".mov", ".wmv", ".flv", ".webm"},
		AssociatedFileExtensions: []string{".srt", ".sub", ".ass", ".idx", ".vtt", ".nfo"},

		MediaTagBlacklist: []string{
			"2160p",
			"1080p",
			"720p",
			"480p",
			"web[- ]?dl",
			"webrip",
			"bluray",
			"brrip",
			"hdrip",
			"x265",
			"x264",
			"hevc",
			"h\\.264",
			"h\\.265",
		},
	}

	pr, err := New(cfg, nil, nil, nil)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	impl, ok := pr.(*processorImpl)
	if !ok {
		t.Fatalf("expected *processorImpl, got %T", pr)
	}
	return impl
}
