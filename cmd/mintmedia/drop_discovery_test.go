package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mtn-man/mintmedia/internal/processor"
)

// candidateRecorder captures what discoverDropPaths hands to SortCandidates.
type candidateRecorder struct {
	processor.Processor
	got []processor.Candidate
}

func (r *candidateRecorder) SortCandidates(_ context.Context, candidates []processor.Candidate) ([]string, []processor.SortError, error) {
	r.got = candidates
	paths := make([]string, len(candidates))
	for i, c := range candidates {
		paths[i] = c.Path
	}
	return paths, nil, nil
}

func TestDiscoverDropPaths_PassesIsDir(t *testing.T) {
	root := t.TempDir()

	// Scene-style folder name: filepath.Ext would report ".x265-RARBG".
	dir := filepath.Join(root, "Star.Trek.The.Next.Generation.S01.1080p.BluRay.x265-RARBG")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file := filepath.Join(root, "Aliens.1986.1080p.BluRay.x265.mkv")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	rec := &candidateRecorder{}
	if _, _, readErr, sortErr := discoverDropPaths(context.Background(), rec, root); readErr != nil || sortErr != nil {
		t.Fatalf("discoverDropPaths: readErr=%v sortErr=%v", readErr, sortErr)
	}

	isDir := make(map[string]bool, len(rec.got))
	for _, c := range rec.got {
		isDir[c.Path] = c.IsDir
	}
	if len(isDir) != 2 || !isDir[dir] || isDir[file] {
		t.Errorf("candidates = %+v, want %q as directory and %q as file", rec.got, dir, file)
	}
}
