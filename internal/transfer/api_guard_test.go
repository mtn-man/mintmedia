package transfer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoLegacyProgressAPISymbolsInProductionCode(t *testing.T) {
	t.Parallel()

	repoRoot, err := findRepoRootFromCWD()
	if err != nil {
		t.Fatalf("find repo root: %v", err)
	}

	targetDirs := []string{
		filepath.Join(repoRoot, "internal", "transfer"),
		filepath.Join(repoRoot, "cmd", "mintmedia"),
	}

	forbidden := []string{
		"ProgressEvery",
		"PrintDone",
		"ProgressSink",
		"NewTerminalAwareProgressSink",
		"Progress:",
	}

	for _, dir := range targetDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read dir %s: %v", dir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read file %s: %v", path, err)
			}
			text := string(content)
			for _, symbol := range forbidden {
				if strings.Contains(text, symbol) {
					t.Fatalf("legacy progress API symbol %q found in %s", symbol, path)
				}
			}
		}
	}
}

func findRepoRootFromCWD() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
