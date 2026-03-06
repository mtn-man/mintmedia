package logging

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var quotedOperationalEventRe = regexp.MustCompile(`"(system|daemon|processor)\.[a-z0-9_]+(?:\.[a-z0-9_]+)*"`)
var forbiddenConsoleWriteRe = regexp.MustCompile(`\bfmt\.(?:Print|Printf|Println|Fprint|Fprintf|Fprintln)\s*\(|\bos\.(?:Stdout|Stderr)\b`)

func TestNoQuotedOperationalEventLiteralsInProductionCallSites(t *testing.T) {
	t.Parallel()

	repoRoot, err := findRepoRootFromCWD()
	if err != nil {
		t.Fatalf("find repo root: %v", err)
	}

	targetDirs := []string{
		filepath.Join(repoRoot, "internal", "daemon"),
		filepath.Join(repoRoot, "internal", "processor"),
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
			assertNoQuotedOperationalEvents(t, path)
		}
	}
}

func TestNoDirectConsoleWritesInScopedInternalPackages(t *testing.T) {
	t.Parallel()

	repoRoot, err := findRepoRootFromCWD()
	if err != nil {
		t.Fatalf("find repo root: %v", err)
	}

	targetDirs := []string{
		filepath.Join(repoRoot, "internal", "daemon"),
		filepath.Join(repoRoot, "internal", "processor"),
	}

	allowlist := map[string]map[int]struct{}{}

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
			assertNoDirectConsoleWrites(t, path, allowlist[path])
		}
	}
}

func assertNoQuotedOperationalEvents(t *testing.T, path string) {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Fatalf("close %s: %v", path, err)
		}
	}()

	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		match := quotedOperationalEventRe.FindString(line)
		if match == "" {
			continue
		}
		t.Fatalf("quoted operational event literal found in %s:%d: %s", path, lineNo, match)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
}

func assertNoDirectConsoleWrites(t *testing.T, path string, allowedLines map[int]struct{}) {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Fatalf("close %s: %v", path, err)
		}
	}()

	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		match := forbiddenConsoleWriteRe.FindString(line)
		if match == "" {
			continue
		}
		if _, ok := allowedLines[lineNo]; ok {
			continue
		}
		t.Fatalf("direct console write found in %s:%d: %q", path, lineNo, strings.TrimSpace(line))
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
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
