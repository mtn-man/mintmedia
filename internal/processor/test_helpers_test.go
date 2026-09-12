package processor

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtn-man/mintmedia/internal/logging"
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
	pl := plans[0]
	// MetadataTitle is meant to always be DestRadix's resolution-free form for
	// any plan that will actually reach Apply's move (a Skip()==true plan's
	// DestRadix can legitimately diverge from it -- e.g. the non-resolution_aware
	// fuzzy-match branch overwrites DestRadix with an adopted folder's on-disk
	// spelling without updating MetadataTitle, since that plan's move never
	// happens and MetadataTitle is never read for it). Checked here, once, so
	// every one of this helper's callers guards the invariant for free.
	if !pl.DupVerdict.Skip() {
		if want := stripTrailingResolution(pl.DestRadix); pl.MetadataTitle != want {
			t.Fatalf("MetadataTitle = %q, want %q (stripTrailingResolution(DestRadix)) for a non-skipped plan", pl.MetadataTitle, want)
		}
	}
	return pl, nil
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

	pr, err := New(cfg, nil, nil, nil)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	impl, ok := pr.(*processorImpl)
	if !ok {
		t.Fatalf("expected *processorImpl, got %T", pr)
	}
	return impl
}

// resolutionAwareTestConfig builds the Config shared by the resolution_aware
// test processors: same layout as newTestProcessor plus the 4k/uhd blacklist
// entries the real resolved config carries, and ResolutionAware enabled.
func resolutionAwareTestConfig(t *testing.T) Config {
	t.Helper()

	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")
	mkdirAll(t, drop)
	mkdirAll(t, movies)
	mkdirAll(t, shows)

	return Config{
		DropFolder: drop,
		MoviesDir:  movies,
		ShowsDir:   shows,

		MainMediaExtensions:      []string{".mkv", ".mp4", ".avi", ".mov", ".wmv", ".flv", ".webm"},
		AssociatedFileExtensions: []string{".srt", ".sub", ".ass", ".idx", ".vtt", ".nfo"},

		MediaTagBlacklist: []string{
			"2160p", "1080p", "720p", "480p", "4k", "uhd",
			"web[- ]?dl", "webrip", "bluray", "brrip", "hdrip",
			"x265", "x264", "hevc", "h\\.264", "h\\.265",
		},
		ResolutionAware: true,
	}
}

func mustProcessorImpl(t *testing.T, cfg Config, logger logging.Logger) *processorImpl {
	t.Helper()
	pr, err := New(cfg, nil, nil, logger)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	impl, ok := pr.(*processorImpl)
	if !ok {
		t.Fatalf("expected *processorImpl, got %T", pr)
	}
	return impl
}

// newTestProcessorResolutionAware mirrors newTestProcessor but with
// ResolutionAware enabled, so tests exercise the resolution-suffix path.
func newTestProcessorResolutionAware(t *testing.T) *processorImpl {
	t.Helper()
	return mustProcessorImpl(t, resolutionAwareTestConfig(t), nil)
}

// newTestProcessorResolutionAwareWithLog is newTestProcessorResolutionAware
// wired to a capturing logger. The returned buffer receives all console output
// (stdout INFO + stderr WARN, merged), so a test can assert on lines emitted
// during Plan regardless of level.
func newTestProcessorResolutionAwareWithLog(t *testing.T) (*processorImpl, *bytes.Buffer) {
	t.Helper()
	p, console, _ := newTestProcessorResolutionAwareWithSinks(t)
	return p, console
}

// newTestProcessorResolutionAwareWithSinks is newTestProcessorResolutionAwareWithLog
// that also returns the history file path. The two sinks deliberately record
// the same event differently -- the console gets a labeled, colorized message,
// history gets fields and no message -- so a test asserting that contract needs
// to read both.
func newTestProcessorResolutionAwareWithSinks(t *testing.T) (*processorImpl, *bytes.Buffer, string) {
	t.Helper()
	var console bytes.Buffer
	historyPath := filepath.Join(t.TempDir(), "history.jsonl")
	lg, err := logging.New(logging.Options{
		Stdout:       &console,
		Stderr:       &console,
		ConsoleLevel: "info",
		HistoryLevel: "warn",
		HistoryFile:  historyPath,
	})
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}
	return mustProcessorImpl(t, resolutionAwareTestConfig(t), lg), &console, historyPath
}

// readHistoryEvent returns the single history entry for event, failing the test
// if there is not exactly one.
func readHistoryEvent(t *testing.T, historyPath string, event logging.Event) logging.Entry {
	t.Helper()
	raw, err := os.ReadFile(historyPath) //nolint:gosec // test-owned temp path
	if err != nil {
		t.Fatalf("read history %q: %v", historyPath, err)
	}
	var found []logging.Entry
	for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var entry logging.Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("unmarshal history line %q: %v", line, err)
		}
		if entry.Event == event {
			found = append(found, entry)
		}
	}
	if len(found) != 1 {
		t.Fatalf("history has %d entries for %q, want 1; file:\n%s", len(found), event, raw)
	}
	return found[0]
}
