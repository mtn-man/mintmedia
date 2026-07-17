package processor

import (
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
