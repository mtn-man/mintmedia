package resultformat

import (
	"strings"
	"testing"
	"time"

	"github.com/mtn-man/mintmedia/internal/processor"
)

func TestCleanName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: "", want: "(unknown)"},
		{name: "dot", raw: ".", want: "(unknown)"},
		{name: "normal path", raw: "/drop/Movie.2020.mkv", want: "Movie.2020.mkv"},
		{name: "trailing slash", raw: "/drop/Show S01E02/", want: "Show S01E02"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := CleanName(tt.raw); got != tt.want {
				t.Fatalf("CleanName(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestCompactLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		res         processor.Result
		nameArg     string
		dur         time.Duration
		wantPrefix  string
		wantContain []string
		wantExact   string
	}{
		{
			name: "applied with destination",
			res: processor.Result{
				Applied: true,
				Plan:    processor.Plan{DestMainPath: "/movies/Movie (2020)/Movie (2020).mkv"},
			},
			nameArg:    "Movie.2020.mkv",
			dur:        0,
			wantPrefix: "SORTED   Movie.2020.mkv\n    ",
			wantContain: []string{
				"/movies/Movie (2020)/Movie (2020).mkv",
			},
		},
		{
			// Regression test: this is exactly the case that used to be
			// unguarded in daemon.go, producing a stray "-> " with nothing
			// after it.
			name: "applied with empty destination",
			res: processor.Result{
				Applied: true,
				Plan:    processor.Plan{DestMainPath: ""},
			},
			nameArg:   "Movie.2020.mkv",
			dur:       0,
			wantExact: "SORTED   Movie.2020.mkv",
		},
		{
			name: "applied with sub-second duration has no suffix",
			res: processor.Result{
				Applied: true,
				Plan:    processor.Plan{DestMainPath: ""},
			},
			nameArg:   "Movie.2020.mkv",
			dur:       500 * time.Millisecond,
			wantExact: "SORTED   Movie.2020.mkv",
		},
		{
			name: "applied with multi-second duration has suffix",
			res: processor.Result{
				Applied: true,
				Plan:    processor.Plan{DestMainPath: ""},
			},
			nameArg:   "Movie.2020.mkv",
			dur:       3 * time.Second,
			wantExact: "SORTED   Movie.2020.mkv  (3s)",
		},
		{
			// Regression test: the duration suffix must use the same
			// trailing-zero-unit-free formatting as the rest of the tool
			// (shutdown.FormatDurationCompact), not time.Duration's default
			// String(), which would render "1m0s" instead of "1m".
			name: "applied with whole-minute duration omits trailing zero seconds",
			res: processor.Result{
				Applied: true,
				Plan:    processor.Plan{DestMainPath: ""},
			},
			nameArg:   "Movie.2020.mkv",
			dur:       60 * time.Second,
			wantExact: "SORTED   Movie.2020.mkv  (1m)",
		},
		{
			name: "skipped with reason",
			res: processor.Result{
				Applied: false,
				Reason:  "ambiguous show folder match",
			},
			nameArg:   "Show.S01E02.mkv",
			dur:       0,
			wantExact: "SKIPPED  Show.S01E02.mkv -- ambiguous show folder match",
		},
		{
			name: "skipped with empty reason falls back",
			res: processor.Result{
				Applied: false,
				Reason:  "",
			},
			nameArg:   "Show.S01E02.mkv",
			dur:       0,
			wantExact: "SKIPPED  Show.S01E02.mkv -- not applied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CompactLine(tt.res, tt.nameArg, tt.dur)
			if tt.wantExact != "" && got != tt.wantExact {
				t.Fatalf("CompactLine() = %q, want %q", got, tt.wantExact)
			}
			if tt.wantPrefix != "" && !strings.HasPrefix(got, tt.wantPrefix) {
				t.Fatalf("CompactLine() = %q, want prefix %q", got, tt.wantPrefix)
			}
			for _, s := range tt.wantContain {
				if !strings.Contains(got, s) {
					t.Fatalf("CompactLine() = %q, want to contain %q", got, s)
				}
			}
		})
	}
}
