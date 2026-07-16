package processor

import (
	"context"
	"path/filepath"
	"testing"
)

func TestCountPlans_SumsAcrossExpansionAndSkipsUnplannable(t *testing.T) {
	p := newTestProcessor(t)

	movie := filepath.Join(p.cfg.DropFolder, "Movie.2020.1080p.x265.mkv")
	writeFile(t, movie, "dummy")

	seasonPackRoot := filepath.Join(p.cfg.DropFolder, "Sherlock.Season.1-4.S01-S04.1080p.10bit.BluRay.5.1.x265.HEVC-MZABI")
	writeFile(t, filepath.Join(seasonPackRoot, "Season 01", "Sherlock.S01E01.1080p.x265.mkv"), "dummy")
	writeFile(t, filepath.Join(seasonPackRoot, "Season 04", "Sherlock.S04E00.1080p.x265.mkv"), "dummy")

	nonMedia := filepath.Join(p.cfg.DropFolder, "readme.txt")
	writeFile(t, nonMedia, "not media")

	count, interrupted := CountPlans(context.Background(), p, []string{movie, seasonPackRoot, nonMedia})
	if interrupted {
		t.Fatalf("interrupted = true, want false")
	}
	if count != 3 {
		t.Fatalf("count = %d, want 3 (1 movie + 2-episode season pack, non-media skipped)", count)
	}
}

func TestCountPlans_StopsEarlyOnCanceledContext(t *testing.T) {
	p := newTestProcessor(t)

	movie := filepath.Join(p.cfg.DropFolder, "Movie.2020.1080p.x265.mkv")
	writeFile(t, movie, "dummy")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	count, interrupted := CountPlans(ctx, p, []string{movie})
	if !interrupted {
		t.Fatalf("interrupted = false, want true for canceled context")
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0 (canceled before any path was planned)", count)
	}
}
