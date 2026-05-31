package processor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func mkdirAll(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("mkdirAll(%q): %v", p, err)
	}
}

func writeFile(t *testing.T, p string, contents string) {
	t.Helper()
	mkdirAll(t, filepath.Dir(p))
	if err := os.WriteFile(p, []byte(contents), 0o644); err != nil {
		t.Fatalf("writeFile(%q): %v", p, err)
	}
}

func planOne(t *testing.T, p *processorImpl, inputPath string) (Plan, error) {
	t.Helper()

	plans, err := p.Plan(context.Background(), Request{InputPath: inputPath})
	if err != nil {
		return Plan{}, err
	}
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}
	return plans[0], nil
}

func newTestProcessor(t *testing.T) *processorImpl {
	t.Helper()

	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")
	mkdirAll(t, drop)
	mkdirAll(t, movies)
	mkdirAll(t, shows)

	cfg := Config{
		DropFolder: drop,
		MoviesDir:  movies,
		ShowsDir:   shows,

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

	pr, err := New(cfg, nil, nil)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	impl, ok := pr.(*processorImpl)
	if !ok {
		t.Fatalf("expected *processorImpl, got %T", pr)
	}
	return impl
}
