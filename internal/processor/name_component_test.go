package processor

import (
	"regexp"
	"strings"
	"testing"
)

func TestParseSeasonComponent(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantSeason int
		wantOK     bool
		wantIdxOf  string // substring whose Index(raw, ...) should equal the returned idx
	}{
		{name: "SxxExx", raw: "Show.S01E02.mkv", wantSeason: 1, wantOK: true, wantIdxOf: "S01E02"},
		{name: "SxxSpaceExx", raw: "Show - S01 E02 - Title.mkv", wantSeason: 1, wantOK: true, wantIdxOf: "S01 E02"},
		{name: "SxxDotExx_NoZeroPad", raw: "The Magicians - S5.E1 - Do Something Crazy.mkv", wantSeason: 5, wantOK: true, wantIdxOf: "S5.E1"},
		{name: "SeasonWord", raw: "Show.Season.5.mkv", wantSeason: 5, wantOK: true, wantIdxOf: "Season.5"},
		{name: "SeasonRange_UsesStartOnly", raw: "Show.S01-S04.mkv", wantSeason: 1, wantOK: true, wantIdxOf: "S01-S04"},
		{name: "SeasonWordRange_UsesStartOnly", raw: "Show.Season.1-4.mkv", wantSeason: 1, wantOK: true, wantIdxOf: "Season.1-4"},
		{name: "LowercaseSxxExx", raw: "show.s01e02.mkv", wantSeason: 1, wantOK: true, wantIdxOf: "s01e02"},
		{name: "NoMatch", raw: "Show.Movie.Cut.mkv", wantOK: false},
		{
			// reSeasonWord must win over reSeasonRange/reSeasonWordRange for this
			// ambiguous fixture (matches all three patterns) -- pattern order is
			// load-bearing, see plan_test.go:123.
			name:       "AmbiguousMultiMatch_SeasonWordWins",
			raw:        "Sherlock.Season.1-4.S01-S04.1080p.10bit.BluRay.5.1.x265.HEVC-MZABI",
			wantSeason: 1,
			wantOK:     true,
			wantIdxOf:  "Season.1-4",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			season, idx, _, ok := parseSeasonComponent(tc.raw)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if season != tc.wantSeason {
				t.Errorf("season = %d, want %d", season, tc.wantSeason)
			}
			if tc.wantIdxOf != "" {
				wantIdx := strings.Index(tc.raw, tc.wantIdxOf)
				if idx != wantIdx {
					t.Errorf("idx = %d, want %d (start of %q)", idx, wantIdx, tc.wantIdxOf)
				}
			}
		})
	}
}

func TestParseEpisodeComponent(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantEpisode int
		wantOK      bool
		wantIdxOf   string
	}{
		{name: "SxxExx", raw: "Show.S01E02.mkv", wantEpisode: 2, wantOK: true, wantIdxOf: "S01E02"},
		{name: "SxxSpaceExx", raw: "Show - S01 E02 - Title.mkv", wantEpisode: 2, wantOK: true, wantIdxOf: "S01 E02"},
		{name: "SxxDotExx_NoZeroPad", raw: "The Magicians - S5.E1 - Do Something Crazy.mkv", wantEpisode: 1, wantOK: true, wantIdxOf: "S5.E1"},
		{name: "EpisodeWord", raw: "Show.Episode.7.mkv", wantEpisode: 7, wantOK: true, wantIdxOf: "Episode.7"},
		{name: "LowercaseSxxExx", raw: "show.s01e02.mkv", wantEpisode: 2, wantOK: true, wantIdxOf: "s01e02"},
		{name: "NoMatch", raw: "Show.Movie.Cut.mkv", wantOK: false},
		{
			// SxxExx takes priority over EpisodeWord when both are present.
			name:        "SxxExxWinsOverEpisodeWord",
			raw:         "Show.S01E02.Episode.9.mkv",
			wantEpisode: 2,
			wantOK:      true,
			wantIdxOf:   "S01E02",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			episode, idx, _, ok := parseEpisodeComponent(tc.raw)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if episode != tc.wantEpisode {
				t.Errorf("episode = %d, want %d", episode, tc.wantEpisode)
			}
			if tc.wantIdxOf != "" {
				wantIdx := strings.Index(tc.raw, tc.wantIdxOf)
				if idx != wantIdx {
					t.Errorf("idx = %d, want %d (start of %q)", idx, wantIdx, tc.wantIdxOf)
				}
			}
		})
	}
}

func TestParseEpisodeRangeComponent(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantSeason int
		wantStart  int
		wantEnd    int
		wantOK     bool
		wantIdxOf  string
	}{
		{name: "DashForm", raw: "Show.S03E12-E13.Title.mkv", wantSeason: 3, wantStart: 12, wantEnd: 13, wantOK: true, wantIdxOf: "S03E12-E13"},
		{name: "NoDashForm", raw: "Show.S03E12E13.Title.mkv", wantSeason: 3, wantStart: 12, wantEnd: 13, wantOK: true, wantIdxOf: "S03E12E13"},
		{name: "LowercaseDashForm", raw: "show.s03e12-e13.title.mkv", wantSeason: 3, wantStart: 12, wantEnd: 13, wantOK: true, wantIdxOf: "s03e12-e13"},
		{name: "PeriodForm", raw: "The Magicians - S5.E12-E13 - Some Title.mkv", wantSeason: 5, wantStart: 12, wantEnd: 13, wantOK: true, wantIdxOf: "S5.E12-E13"},
		{name: "SingleEpisode_NoRange", raw: "Show.S03E12.Title.mkv", wantOK: false},
		{name: "ReversedRange_Refuses", raw: "Show.S03E13-E12.Title.mkv", wantOK: false},
		{name: "EqualRange_Refuses", raw: "Show.S03E12-E12.Title.mkv", wantOK: false},
		{name: "RepeatedTokenForm", raw: "Show.S03E12.S03E13.Title.mkv", wantSeason: 3, wantStart: 12, wantEnd: 13, wantOK: true, wantIdxOf: "S03E12.S03E13"},
		{name: "RepeatedTokenForm_DashSeparator", raw: "Show.Name - S03E12 - S03E13 - Title.mkv", wantSeason: 3, wantStart: 12, wantEnd: 13, wantOK: true, wantIdxOf: "S03E12 - S03E13"},
		{name: "RepeatedTokenForm_MismatchedSeason_Refuses", raw: "Show.S03E12.S04E13.Title.mkv", wantOK: false},
		{name: "RepeatedTokenForm_Reversed_Refuses", raw: "Show.S03E13.S03E12.Title.mkv", wantOK: false},
		{name: "AmpersandPair", raw: "Show.S03E12 & S03E13.Title.mkv", wantSeason: 3, wantStart: 12, wantEnd: 13, wantOK: true, wantIdxOf: "S03E12 & S03E13"},
		{name: "AndWordPair", raw: "Show.S03E12 and S03E13.Title.mkv", wantSeason: 3, wantStart: 12, wantEnd: 13, wantOK: true, wantIdxOf: "S03E12 and S03E13"},
		{name: "AmpersandPair_NonConsecutive_Refuses", raw: "Show.S03E12 & S03E14.Title.mkv", wantOK: false},
		{name: "AmpersandPair_MismatchedSeason_Refuses", raw: "Show.S03E12 & S04E13.Title.mkv", wantOK: false},
		{name: "XFormPair", raw: "Show.Name.1x02x03.Title.mkv", wantSeason: 1, wantStart: 2, wantEnd: 3, wantOK: true},
		{name: "XFormPair_NonConsecutive_Refuses", raw: "Show.Name.1x02x04.Title.mkv", wantOK: false},
		{name: "NoMatch", raw: "Show.Movie.Cut.mkv", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			season, start, end, idx, _, ok := parseEpisodeRangeComponent(tc.raw)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if season != tc.wantSeason || start != tc.wantStart || end != tc.wantEnd {
				t.Errorf("season/start/end = %d/%d/%d, want %d/%d/%d", season, start, end, tc.wantSeason, tc.wantStart, tc.wantEnd)
			}
			if tc.wantIdxOf != "" {
				wantIdx := strings.Index(tc.raw, tc.wantIdxOf)
				if idx != wantIdx {
					t.Errorf("idx = %d, want %d (start of %q)", idx, wantIdx, tc.wantIdxOf)
				}
			}
		})
	}
}

func TestDetectRefusedMultiEpisode(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "MismatchedSeasonRepeatedTokens_Refused", raw: "Show.S01E24.S02E01.Title.mkv", want: true},
		{name: "MatchedSeasonRepeatedTokens_NotRefused", raw: "Show.S03E12.S03E13.Title.mkv", want: false},
		{name: "SingleEpisode_NotRefused", raw: "Show.S03E12.Title.mkv", want: false},
		{name: "NoMatch_NotRefused", raw: "Show.Movie.Cut.mkv", want: false},
		{name: "ConsecutiveAmpersandPair_NotRefused", raw: "Show.S03E12 & S03E13.Title.mkv", want: false},
		{name: "ConsecutiveAndWordPair_NotRefused", raw: "Show.S03E12 and S03E13.Title.mkv", want: false},
		{name: "NonConsecutiveAmpersandPair_Refused", raw: "Show.S03E12 & S03E14.Title.mkv", want: true},
		{name: "MismatchedSeasonAmpersandPair_Refused", raw: "Show.S03E12 & S04E13.Title.mkv", want: true},
		{name: "ThreeChain_DotSeparator_Refused", raw: "Show.S03E12.S03E13.S03E14.Title.mkv", want: true},
		{name: "ThreeChain_Ampersand_Refused", raw: "Phineas.and.Ferb.S01E00 & S01E01 & S01E02.Title.mkv", want: true},
		{name: "ThreeChain_AndWord_Refused", raw: "Phineas.and.Ferb.S01E00 and S01E01 and S01E02.Title.mkv", want: true},
		{name: "ConsecutiveXFormPair_NotRefused", raw: "Show.Name.1x02x03.Title.mkv", want: false},
		{name: "NonConsecutiveXFormPair_Refused", raw: "Show.Name.1x02x04.Title.mkv", want: true},
		{name: "ThreeChain_XForm_Refused", raw: "Show_Name.1x02x03x04.HDTV_XViD_Etc-Group.mkv", want: true},
		{name: "ThreeChain_DashSeparator_Refused", raw: "Show.S03E12-E13-E14.Title.mkv", want: true},
		{name: "ThreeChain_NoSeparator_Refused", raw: "Show.S03E12E13E14.Title.mkv", want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectRefusedMultiEpisode(tc.raw); got != tc.want {
				t.Errorf("detectRefusedMultiEpisode(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestEpisodePartLetter(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "SplitPartA", raw: "Regular Show - S03E04a.mkv", want: "a"},
		{name: "NoPartLetter", raw: "Show.S03E04.mkv", want: ""},
		{name: "CaseInsensitiveInput_LowercaseOutput", raw: "show.s03e04A.mkv", want: "a"},
		{
			// False-positive guard: "and" isn't a split-part suffix -- \b
			// fails after either backtrack position since a letter/digit is
			// always a word character.
			name: "TrailingWord_NotMistakenForPartLetter",
			raw:  "Show.S03E04and.The.Rest.mkv",
			want: "",
		},
		{
			// Documents the multi-episode-pack ambiguity in isolation (the
			// same shape reSeasonEpisodeRange resolves properly before this
			// helper is ever reached in the real parsing flow).
			name: "MultiEpisodePack_NotMistakenForPartLetter",
			raw:  "Show.S02E01E02.mkv",
			want: "",
		},
		{name: "NoMatch", raw: "Show.Movie.Cut.mkv", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := episodePartLetter(tc.raw); got != tc.want {
				t.Errorf("episodePartLetter(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestFindYear(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"Movie.2020.1080p", "2020"},
		{"NoYearHere", ""},
		{"Show.1999.S01E01", "1999"},
		{
			// The motivating case: a title that itself embeds a year-looking
			// number ("2049") must not shadow the real release year ("2017")
			// that follows it.
			raw:  "Blade.Runner.2049.2017.1080p.BluRay.x264-[YTS.AG]",
			want: "2017",
		},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			if got := findYear(tc.raw); got != tc.want {
				t.Errorf("findYear(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func compileTestBlacklist(t *testing.T, patterns []string) []*regexp.Regexp {
	t.Helper()
	bl := make([]*regexp.Regexp, len(patterns))
	for i, p := range patterns {
		re, err := compileBlacklistPattern(p)
		if err != nil {
			t.Fatalf("compileBlacklistPattern(%q): %v", p, err)
		}
		bl[i] = re
	}
	return bl
}

func TestCompileBlacklistPattern(t *testing.T) {
	re, err := compileBlacklistPattern("aac")
	if err != nil {
		t.Fatalf("compileBlacklistPattern(%q): %v", "aac", err)
	}
	if re.MatchString("Isaac") {
		t.Errorf("pattern %q matched %q as a bare substring, want whole-word only", "aac", "Isaac")
	}
	if !re.MatchString("Movie.Name.AAC.mkv") {
		t.Errorf("pattern %q should match a standalone token", "aac")
	}
}

func TestCleanReleaseName(t *testing.T) {
	bl := compileTestBlacklist(t, []string{
		"2160p", "1080p", "720p", "480p", "x265", "x264", "bluray", "brrip", "web[- ]?dl",
		"proper", "repack", "rerip", "remux", "extended", "limited", "uncut", "unrated",
		"restored", "remastered", "theatrical", "deluxe",
	})
	tests := []struct {
		raw  string
		want string
	}{
		{"Get.Smart.2008.1080p.BluRay", "Get Smart 2008"},
		{"Spider-Man.Into.the.Spider-Verse", "Spider-Man Into the Spider-Verse"},
		{"[EZTVx.to] Show.Name.720p.x264", "Show Name"},
		{
			// The motivating case: no year present to anchor truncation, so a
			// bare (non-bracketed) release-group tag with nothing recognized
			// to strip it must still be dropped once release metadata starts.
			raw:  "Captain.America.The.First.Avenger.1080p.BrRip.x264.YIFY",
			want: "Captain America The First Avenger",
		},
		{
			// PROPER/REPACK sit before any resolution/codec tag the old list
			// recognized, so with no year to truncate at, they used to survive
			// into the title untouched.
			raw:  "Movie.Name.PROPER.REPACK.BluRay.x264-GROUP",
			want: "Movie Name",
		},
		{
			raw:  "Movie.Name.EXTENDED.UNRATED.REMUX.x264-GROUP",
			want: "Movie Name",
		},
		{
			// A blacklist word that also occurs as legitimate trailing title
			// text ("Remastered" here) must not truncate the real title text
			// that follows it -- only a genuine trailing metadata block does.
			raw:  "The.Great.Escape.Remastered.Anniversary.Cut",
			want: "The Great Escape Anniversary Cut",
		},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			if got := cleanReleaseName(bl, tc.raw); got != tc.want {
				t.Errorf("cleanReleaseName(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestExtractShowEpisodeTitle(t *testing.T) {
	bl := compileTestBlacklist(t, []string{
		"2160p", "1080p", "720p", "480p", "x265", "x264", "bluray", "brrip", "web[- ]?dl",
		"proper", "repack", "rerip", "remux", "extended", "limited", "uncut", "unrated",
		"restored", "remastered", "theatrical", "deluxe",
	})

	tests := []struct {
		name            string
		stem            string
		resolutionAware bool
		resolution      string
		season, episode int
		episodeEnd      int
		episodePart     string
		wantTitle       string
		wantOK          bool
	}{
		{
			name:      "CleanDashTitle",
			stem:      "Lanterns - S01E06 - Bad Optics",
			season:    1,
			episode:   6,
			wantTitle: "Bad Optics",
			wantOK:    true,
		},
		{
			name:            "CleanDashTitleWithResolution",
			stem:            "Lanterns - S01E06 - Bad Optics - 1080p",
			resolutionAware: true,
			resolution:      "1080p",
			season:          1,
			episode:         6,
			wantTitle:       "Bad Optics",
			wantOK:          true,
		},
		{
			// resolution_aware is off, so the trailing "1080p" is never set
			// aside -- it's just another blacklisted token, and the whole
			// candidate is disqualified along with it.
			name:      "ResolutionTagWithoutResolutionAware_Fallback",
			stem:      "Lanterns - S01E06 - Bad Optics - 1080p",
			season:    1,
			episode:   6,
			wantTitle: "",
			wantOK:    false,
		},
		{
			name:      "JunkyTail_Fallback",
			stem:      "Show - S01E06 - Bad Optics PROPER",
			season:    1,
			episode:   6,
			wantTitle: "",
			wantOK:    false,
		},
		{
			name:      "EmptyTail_Fallback",
			stem:      "Show - S01E06",
			season:    1,
			episode:   6,
			wantTitle: "",
			wantOK:    false,
		},
		{
			name:      "DotSeparatedTail_Fallback",
			stem:      "Show.S01E06.Bad.Optics",
			season:    1,
			episode:   6,
			wantTitle: "",
			wantOK:    false,
		},
		{
			name:       "MultiEpisodeRange_OneOpaqueTitle",
			stem:       "Show - S01E12-E13 - Double Feature",
			season:     1,
			episode:    12,
			episodeEnd: 13,
			wantTitle:  "Double Feature",
			wantOK:     true,
		},
		{
			// Simulates a folder-hint/reconciled-batch resolution: the file's
			// own name doesn't parse to a season/episode at all, so there's no
			// tail to read from it.
			name:      "FileNameDoesNotParse_Fallback",
			stem:      "video",
			season:    1,
			episode:   6,
			wantTitle: "",
			wantOK:    false,
		},
		{
			// The file's own name parses, but to a *different* episode than
			// what was actually resolved (e.g. a forced batch-hint name) --
			// no tail is trusted in that case either.
			name:      "FileNameParsesToDifferentEpisode_Fallback",
			stem:      "Show - S01E07 - Something",
			season:    1,
			episode:   6,
			wantTitle: "",
			wantOK:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			title, ok := extractShowEpisodeTitle(bl, tc.resolutionAware, tc.resolution, tc.stem, tc.season, tc.episode, tc.episodeEnd, tc.episodePart)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if title != tc.wantTitle {
				t.Errorf("title = %q, want %q", title, tc.wantTitle)
			}
		})
	}
}

func TestParseAirDate(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantDate string
		wantOK   bool
	}{
		{name: "ISODash", raw: "Show.2021-07-30.mkv", wantDate: "2021-07-30", wantOK: true},
		{name: "ISODot", raw: "Show.2021.07.30.mkv", wantDate: "2021-07-30", wantOK: true},
		{name: "ISOSpace", raw: "Show 2021 07 30.mkv", wantDate: "2021-07-30", wantOK: true},
		{
			name:     "EuropeanDash_PanoramaRepro",
			raw:      "Panorama.15-05-2018.Web-DL.540p.H264.AAC.Subs.mp4",
			wantDate: "2018-05-15",
			wantOK:   true,
		},
		{name: "EuropeanDot", raw: "Show.15.05.2018.mkv", wantDate: "2018-05-15", wantOK: true},
		{
			name:     "MonthNameFullDot_DailyShowRepro",
			raw:      "Show.Name.July.30.2021.1080p.WEB-DL.x264-GRP.mkv",
			wantDate: "2021-07-30",
			wantOK:   true,
		},
		{
			// Single-digit day zero-padded on output.
			name:     "MonthNameAbbreviatedSingleDigitDay",
			raw:      "Nightly News.Jul.3.2021.mkv",
			wantDate: "2021-07-03",
			wantOK:   true,
		},
		{name: "MonthNameSpaceSeparated", raw: "Show July 3 2021.mkv", wantDate: "2021-07-03", wantOK: true},
		{name: "InvalidDay", raw: "Show.2021-07-32.mkv", wantOK: false},
		{name: "InvalidMonth", raw: "Show.2021-13-01.mkv", wantOK: false},
		{name: "UnrelatedDigitRun_PlainYear", raw: "Movie.2021.1080p.mkv", wantOK: false},
		{name: "UnrelatedDigitRun_ResolutionDims", raw: "Show.1920x1080.mkv", wantOK: false},
		{
			// US-style MM-DD-YYYY is deliberately not an accepted shape (see
			// reDateEuropean's doc comment): day=07 is a valid day, but the
			// second group "30" fails the European pattern's month range
			// (01-12), so this correctly falls through to no match under any
			// accepted pattern -- not a gap, a deliberate exclusion.
			name:   "USFormatDeliberatelyExcluded",
			raw:    "Show.07-30-2021.mkv",
			wantOK: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _, ok := parseAirDate(tc.raw)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && got != tc.wantDate {
				t.Errorf("parseAirDate(%q) = %q, want %q", tc.raw, got, tc.wantDate)
			}
		})
	}
}

func TestHasDatedEpisodeSignal(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "EuropeanDate_PanoramaRepro", raw: "Panorama.15-05-2018.Web-DL.540p.H264.AAC.Subs.mp4", want: true},
		{name: "MonthNameDate_DailyShowRepro", raw: "Show.Name.July.30.2021.1080p.WEB-DL.x264-GRP.mkv", want: true},
		{name: "OrdinaryMovieWithPlainYear", raw: "Interstellar.2014.1080p.BluRay.mkv", want: false},
		{name: "OrdinaryShowSxxEyy", raw: "Fallout.S02E07.1080p.mkv", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasDatedEpisodeSignal(tc.raw); got != tc.want {
				t.Errorf("hasDatedEpisodeSignal(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParseDatedShowFromName(t *testing.T) {
	bl := compileTestBlacklist(t, []string{
		"2160p", "1080p", "720p", "480p", "x265", "x264", "bluray", "web[- ]?dl", "h264", "aac",
	})

	tests := []struct {
		name         string
		baseName     string
		fileName     string
		wantShowName string
		wantDate     string
		wantOK       bool
	}{
		{
			name:         "PanoramaRepro",
			baseName:     "drop",
			fileName:     "Panorama.15-05-2018.Web-DL.540p.H264.AAC.Subs.mp4",
			wantShowName: "Panorama",
			wantDate:     "2018-05-15",
			wantOK:       true,
		},
		{
			name:         "DailyShowMonthNameRepro",
			baseName:     "drop",
			fileName:     "Show.Name.July.30.2021.1080p.WEB-DL.x264-GRP.mkv",
			wantShowName: "Show Name",
			wantDate:     "2021-07-30",
			wantOK:       true,
		},
		{
			// A real SxxEyy token takes priority -- parseShowFromName
			// succeeds directly, so resolveShowIdentity never reaches
			// parseDatedShowFromName in production for this shape. Verified
			// here at the parseDatedShowFromName level in isolation: even if
			// called directly, the incidental date-like substring doesn't
			// stop it from still finding a (different, wrong-for-this-case)
			// date match -- the real priority guarantee lives in
			// resolveShowIdentity's err != nil gate, exercised at the Plan
			// level in plan_test.go instead.
			name:         "OrdinaryMovie_NoDate",
			baseName:     "drop",
			fileName:     "Interstellar.2014.1080p.BluRay.mkv",
			wantShowName: "",
			wantDate:     "",
			wantOK:       false,
		},
		{
			// A refused multi-episode SxxEyy chain must not be silently
			// reinterpreted as a dated episode just because the filename
			// also happens to contain a date-like substring.
			name:         "RefusedMultiEpisodeChain_NotReinterpretedAsDated",
			baseName:     "drop",
			fileName:     "Show.S01E01.S01E02.S01E03.July.30.2021.mkv",
			wantShowName: "",
			wantDate:     "",
			wantOK:       false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			showName, _, airDate, ok := parseDatedShowFromName(bl, tc.baseName, tc.fileName)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if showName != tc.wantShowName {
				t.Errorf("showName = %q, want %q", showName, tc.wantShowName)
			}
			if airDate != tc.wantDate {
				t.Errorf("airDate = %q, want %q", airDate, tc.wantDate)
			}
		})
	}
}
