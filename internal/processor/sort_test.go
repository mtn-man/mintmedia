package processor

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

// --- sortKey.less unit tests (no filesystem) ---

func TestSortKey_Less(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		a, b sortKey
		want bool // want a.less(b)
	}{
		{
			name: "Movie before show",
			a:    sortKey{tier: tierMovie, title: "Aliens", path: "/a"},
			b:    sortKey{tier: tierShow, title: "Breaking Bad", path: "/b"},
			want: true,
		},
		{
			name: "Show before non-media",
			a:    sortKey{tier: tierShow, title: "Breaking Bad", path: "/a"},
			b:    sortKey{tier: tierFallback, path: "/b"},
			want: true,
		},
		{
			name: "Movie before non-media",
			a:    sortKey{tier: tierMovie, title: "Aliens", path: "/a"},
			b:    sortKey{tier: tierFallback, path: "/b"},
			want: true,
		},
		{
			name: "Movies sorted case-insensitively by title",
			a:    sortKey{tier: tierMovie, title: "aliens", path: "/a"},
			b:    sortKey{tier: tierMovie, title: "Zodiac", path: "/b"},
			want: true,
		},
		{
			name: "Movies sorted case-insensitively by title reverse",
			a:    sortKey{tier: tierMovie, title: "Zodiac", path: "/z"},
			b:    sortKey{tier: tierMovie, title: "aliens", path: "/a"},
			want: false,
		},
		{
			name: "Shows sorted by name",
			a:    sortKey{tier: tierShow, title: "Breaking Bad", season: 1, episode: 1, path: "/a"},
			b:    sortKey{tier: tierShow, title: "The Wire", season: 1, episode: 1, path: "/b"},
			want: true,
		},
		{
			name: "Same show, earlier season sorts first",
			a:    sortKey{tier: tierShow, title: "Breaking Bad", season: 1, episode: 1, path: "/a"},
			b:    sortKey{tier: tierShow, title: "Breaking Bad", season: 2, episode: 1, path: "/b"},
			want: true,
		},
		{
			name: "Same show and season, earlier episode sorts first",
			a:    sortKey{tier: tierShow, title: "Fallout", season: 1, episode: 1, path: "/a"},
			b:    sortKey{tier: tierShow, title: "Fallout", season: 1, episode: 2, path: "/b"},
			want: true,
		},
		{
			name: "Same show and season, later episode does not sort first",
			a:    sortKey{tier: tierShow, title: "Fallout", season: 1, episode: 2, path: "/a"},
			b:    sortKey{tier: tierShow, title: "Fallout", season: 1, episode: 1, path: "/b"},
			want: false,
		},
		{
			name: "Non-media sorted by path case-insensitively",
			a:    sortKey{tier: tierFallback, path: "/a/a_file"},
			b:    sortKey{tier: tierFallback, path: "/b/z_file"},
			want: true,
		},
		{
			name: "Equal keys return false (stable sort precondition)",
			a:    sortKey{tier: tierMovie, title: "Aliens", path: "/same"},
			b:    sortKey{tier: tierMovie, title: "Aliens", path: "/same"},
			want: false,
		},
		{
			name: "Path tiebreaker for equal title/season/episode",
			a:    sortKey{tier: tierShow, title: "Fallout", season: 1, episode: 1, path: "/a"},
			b:    sortKey{tier: tierShow, title: "Fallout", season: 1, episode: 1, path: "/b"},
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.a.less(tc.b)
			if got != tc.want {
				t.Errorf("(%+v).less(%+v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// --- SortCandidates integration tests (real filesystem via newTestProcessor) ---

func TestSortCandidates_EmptyInput(t *testing.T) {
	t.Parallel()
	p := newTestProcessor(t)
	sorted, errs, err := SortCandidates(context.Background(), p, nil)
	if sorted != nil || errs != nil || err != nil {
		t.Errorf("SortCandidates(nil) = %v, %v, %v; want nil, nil, nil", sorted, errs, err)
	}
}

func TestSortCandidates_MoviesFirst(t *testing.T) {
	t.Parallel()
	p := newTestProcessor(t)

	movie := filepath.Join(p.cfg.DropFolder, "The.Dark.Knight.2008.1080p.BluRay.x265.mkv")
	show := filepath.Join(p.cfg.DropFolder, "Fallout.S01E01.1080p.x265.mkv")
	writeFile(t, movie, "dummy")
	writeFile(t, show, "dummy")

	// Pass show first to confirm ordering is by media type, not input order.
	sorted, errs, err := SortCandidates(context.Background(), p, []string{show, movie})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("unexpected sort errors: %v", errs)
	}
	if len(sorted) != 2 {
		t.Fatalf("expected 2 sorted paths, got %d: %v", len(sorted), sorted)
	}
	if sorted[0] != movie {
		t.Errorf("sorted[0] = %q, want movie %q", sorted[0], movie)
	}
	if sorted[1] != show {
		t.Errorf("sorted[1] = %q, want show %q", sorted[1], show)
	}
}

func TestSortCandidates_MovieOrder(t *testing.T) {
	t.Parallel()
	p := newTestProcessor(t)

	zodiac := filepath.Join(p.cfg.DropFolder, "Zodiac.2007.1080p.BluRay.x265.mkv")
	aliens := filepath.Join(p.cfg.DropFolder, "Aliens.1986.1080p.BluRay.x265.mkv")
	madMax := filepath.Join(p.cfg.DropFolder, "Mad.Max.1979.1080p.BluRay.x265.mkv")
	writeFile(t, zodiac, "dummy")
	writeFile(t, aliens, "dummy")
	writeFile(t, madMax, "dummy")

	sorted, errs, err := SortCandidates(context.Background(), p, []string{zodiac, madMax, aliens})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("unexpected sort errors: %v", errs)
	}
	want := []string{aliens, madMax, zodiac}
	for i, w := range want {
		if sorted[i] != w {
			t.Errorf("sorted[%d] = %q, want %q", i, sorted[i], w)
		}
	}
}

func TestSortCandidates_ShowOrder(t *testing.T) {
	t.Parallel()
	p := newTestProcessor(t)

	s01e02 := filepath.Join(p.cfg.DropFolder, "Fallout.S01E02.1080p.x265.mkv")
	s01e01 := filepath.Join(p.cfg.DropFolder, "Fallout.S01E01.1080p.x265.mkv")
	s02e01 := filepath.Join(p.cfg.DropFolder, "Fallout.S02E01.1080p.x265.mkv")
	writeFile(t, s01e02, "dummy")
	writeFile(t, s01e01, "dummy")
	writeFile(t, s02e01, "dummy")

	sorted, errs, err := SortCandidates(context.Background(), p, []string{s01e02, s02e01, s01e01})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("unexpected sort errors: %v", errs)
	}
	want := []string{s01e01, s01e02, s02e01}
	for i, w := range want {
		if sorted[i] != w {
			t.Errorf("sorted[%d] = %q, want %q", i, sorted[i], w)
		}
	}
}

func TestSortCandidates_MultipleShows(t *testing.T) {
	t.Parallel()
	p := newTestProcessor(t)

	// Two shows: Fallout (S01E01) and Breaking Bad (S03E07)
	// Plus a movie: Aliens
	// Expected: movie first, then Breaking Bad, then Fallout.
	aliens := filepath.Join(p.cfg.DropFolder, "Aliens.1986.1080p.BluRay.x265.mkv")
	fallout := filepath.Join(p.cfg.DropFolder, "Fallout.S01E01.1080p.x265.mkv")
	bb := filepath.Join(p.cfg.DropFolder, "Breaking.Bad.S03E07.1080p.x265.mkv")
	writeFile(t, aliens, "dummy")
	writeFile(t, fallout, "dummy")
	writeFile(t, bb, "dummy")

	sorted, errs, err := SortCandidates(context.Background(), p, []string{fallout, aliens, bb})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("unexpected sort errors: %v", errs)
	}
	want := []string{aliens, bb, fallout}
	for i, w := range want {
		if sorted[i] != w {
			t.Errorf("sorted[%d] = %q, want %q", i, sorted[i], w)
		}
	}
}

func TestSortCandidates_NonMediaSilentlySkipped(t *testing.T) {
	t.Parallel()
	p := newTestProcessor(t)

	movie := filepath.Join(p.cfg.DropFolder, "Aliens.1986.1080p.BluRay.x265.mkv")
	nonMedia := filepath.Join(p.cfg.DropFolder, "notes.txt")
	writeFile(t, movie, "dummy")
	writeFile(t, nonMedia, "not media")

	sorted, errs, err := SortCandidates(context.Background(), p, []string{nonMedia, movie})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("expected no sort errors for non-media, got: %v", errs)
	}
	if len(sorted) != 1 || sorted[0] != movie {
		t.Errorf("sorted = %v, want [%q] (non-media silently dropped)", sorted, movie)
	}
}

func TestSortCandidates_ParseFailureExcluded(t *testing.T) {
	t.Parallel()
	p := newTestProcessor(t)

	// A file with a media extension but no parseable title.
	unparseable := filepath.Join(p.cfg.DropFolder, "1080p.x265.mkv")
	valid := filepath.Join(p.cfg.DropFolder, "Aliens.1986.1080p.BluRay.x265.mkv")
	writeFile(t, unparseable, "dummy")
	writeFile(t, valid, "dummy")

	sorted, errs, err := SortCandidates(context.Background(), p, []string{unparseable, valid})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 sort error, got %d: %v", len(errs), errs)
	}
	if errs[0].Path != unparseable {
		t.Errorf("errs[0].Path = %q, want %q", errs[0].Path, unparseable)
	}
	if len(sorted) != 1 || sorted[0] != valid {
		t.Errorf("sorted = %v, want [%q]", sorted, valid)
	}
}

func TestSortCandidates_ContextCanceled(t *testing.T) {
	t.Parallel()
	p := newTestProcessor(t)

	valid := filepath.Join(p.cfg.DropFolder, "Aliens.1986.1080p.BluRay.x265.mkv")
	writeFile(t, valid, "dummy")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // canceled before the call

	sorted, errs, err := SortCandidates(ctx, p, []string{valid})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if sorted != nil || errs != nil {
		t.Errorf("expected nil sorted and errs on cancel, got sorted=%v errs=%v", sorted, errs)
	}
}
