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
var eventConstDeclRe = regexp.MustCompile(`^\s*Event\w+\s+Event\s*=\s*"([^"]+)"`)

// consoleLabelWidth is the fixed column width every labeled console line
// pads its label to (see internal/console's prefixColors table). A literal
// that gets this wrong either misaligns (if colorizePrefix's string-prefix
// match still succeeds) or silently loses its color entirely.
const consoleLabelWidth = 9

// consoleLabels is every string that CLAUDE.md and internal/console's
// prefixColors table together recognize as a labeled console line, plus
// INFO -- a real label (same 9-column convention, emitted via logConsoleInfo
// and the caffeinate hooks) that is deliberately left uncolored and so
// appears in neither list.
var consoleLabels = []string{
	"STARTED", "CREATED", "SORTED", "SKIPPED", "WARNING", "ERROR",
	"STATUS", "STOPPED", "TORRENT", "SORTING", "REMOVED", "TAGGING", "INFO",
}

var consoleLabelRe = regexp.MustCompile(`"(` + strings.Join(consoleLabels, "|") + `)( *)`)

func TestConsoleLabelLiteralsArePaddedToNineColumns(t *testing.T) {
	t.Parallel()

	repoRoot, err := findRepoRootFromCWD()
	if err != nil {
		t.Fatalf("find repo root: %v", err)
	}

	targetDirs := []string{
		filepath.Join(repoRoot, "cmd", "mintmedia"),
		filepath.Join(repoRoot, "internal", "daemon"),
		filepath.Join(repoRoot, "internal", "console"),
		filepath.Join(repoRoot, "internal", "resultformat"),
		filepath.Join(repoRoot, "internal", "transfer"),
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
			assertConsoleLabelsPadded(t, filepath.Join(dir, name))
		}
	}
}

func assertConsoleLabelsPadded(t *testing.T, path string) {
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
		for _, m := range consoleLabelRe.FindAllStringSubmatch(line, -1) {
			label, padding := m[1], m[2]
			if width := len(label) + len(padding); width != consoleLabelWidth {
				t.Fatalf("%s:%d: label %q padded to %d columns, want %d: %q",
					path, lineNo, label, width, consoleLabelWidth, strings.TrimSpace(line))
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
}

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

// TestNoDeclaredEventConstantMissingFromAllOperationalEvents guards
// AllOperationalEvents() against silently drifting out of sync with the
// const block it's meant to restate in full: a new Event constant declared
// in events.go but forgotten from the returned slice would otherwise pass
// every other test in this package undetected.
func TestNoDeclaredEventConstantMissingFromAllOperationalEvents(t *testing.T) {
	t.Parallel()

	repoRoot, err := findRepoRootFromCWD()
	if err != nil {
		t.Fatalf("find repo root: %v", err)
	}

	declared := declaredEventConstantValues(t, filepath.Join(repoRoot, "internal", "logging", "events.go"))

	listed := make(map[string]struct{}, len(AllOperationalEvents()))
	for _, e := range AllOperationalEvents() {
		listed[string(e)] = struct{}{}
	}

	for value := range declared {
		if _, ok := listed[value]; !ok {
			t.Errorf("event constant %q is declared in events.go but missing from AllOperationalEvents()", value)
		}
	}
}

func declaredEventConstantValues(t *testing.T, path string) map[string]struct{} {
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

	declared := make(map[string]struct{})
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		m := eventConstDeclRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		declared[m[1]] = struct{}{}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return declared
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
