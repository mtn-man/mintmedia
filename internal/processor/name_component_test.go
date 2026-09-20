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
			season, idx, ok := parseSeasonComponent(tc.raw)
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
			episode, idx, ok := parseEpisodeComponent(tc.raw)
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
			season, start, end, idx, ok := parseEpisodeRangeComponent(tc.raw)
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
		bl[i] = regexp.MustCompile("(?i)" + p)
	}
	return bl
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
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			if got := cleanReleaseName(bl, tc.raw); got != tc.want {
				t.Errorf("cleanReleaseName(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
