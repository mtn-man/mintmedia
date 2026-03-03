package logging

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestRuntimeLogger_ConsoleParityNoPrefixes(t *testing.T) {
	t.Parallel()

	var stdout strings.Builder
	var stderr strings.Builder

	history := filepath.Join(t.TempDir(), "history.jsonl")
	l, err := New(Options{
		Stdout:               &stdout,
		Stderr:               &stderr,
		ConsoleLevel:         "INFO",
		HistoryLevel:         "WARN",
		HistoryFile:          history,
		HistoryInfoAllowlist: DefaultHistoryInfoAllowlist(),
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	l.Info("daemon", Event("daemon.started"), "Mintmedia daemon started.", nil)
	l.Warn("daemon", Event("daemon.warning"), "watch error: boom", errors.New("boom"), nil)

	if got := stdout.String(); got != "Mintmedia daemon started.\n" {
		t.Fatalf("stdout = %q", got)
	}
	if got := stderr.String(); got != "watch error: boom\n" {
		t.Fatalf("stderr = %q", got)
	}
}

func TestRuntimeLogger_HistoryJSONLAndErrorField(t *testing.T) {
	t.Parallel()

	history := filepath.Join(t.TempDir(), "history.jsonl")
	l, err := New(Options{
		Stdout:               ioDiscard{},
		Stderr:               ioDiscard{},
		ConsoleLevel:         "INFO",
		HistoryLevel:         "WARN",
		HistoryFile:          history,
		HistoryInfoAllowlist: DefaultHistoryInfoAllowlist(),
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	l.Error("processor", EventDaemonProcessError, "", errors.New("disk full"), Fields{
		"input_path": "relative/file.mkv",
	})

	data, err := os.ReadFile(history)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("unmarshal history line: %v", err)
	}
	if entry["level"] != string(LevelError) {
		t.Fatalf("level = %v", entry["level"])
	}
	errObj, ok := entry["err"].(map[string]any)
	if !ok {
		t.Fatalf("missing err object: %#v", entry["err"])
	}
	if errObj["message"] != "disk full" {
		t.Fatalf("err.message = %v", errObj["message"])
	}
	if _, ok := errObj["type"]; !ok {
		t.Fatalf("err.type missing")
	}
}

func TestRuntimeLogger_InfoAllowlistAtWarnHistoryLevel(t *testing.T) {
	t.Parallel()

	history := filepath.Join(t.TempDir(), "history.jsonl")
	l, err := New(Options{
		Stdout:               ioDiscard{},
		Stderr:               ioDiscard{},
		ConsoleLevel:         "INFO",
		HistoryLevel:         "WARN",
		HistoryFile:          history,
		HistoryInfoAllowlist: DefaultHistoryInfoAllowlist(),
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	noConsole := false
	l.Log(Entry{
		Level:     LevelInfo,
		Component: "processor",
		Event:     EventProcessorMoveMainApplied,
		Fields:    Fields{"src": "a.mkv", "dst": "b.mkv"},
		ToConsole: &noConsole,
	})
	l.Log(Entry{
		Level:     LevelInfo,
		Component: "processor",
		Event:     Event("processor.misc.detail"),
		Fields:    Fields{"foo": "bar"},
		ToConsole: &noConsole,
	})

	data, err := os.ReadFile(history)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 persisted info line, got %d", len(lines))
	}
}

func TestRuntimeLogger_ConcurrentHistoryWritesAreValidJSONL(t *testing.T) {
	t.Parallel()

	history := filepath.Join(t.TempDir(), "history.jsonl")
	l, err := New(Options{
		Stdout:               ioDiscard{},
		Stderr:               ioDiscard{},
		ConsoleLevel:         "INFO",
		HistoryLevel:         "WARN",
		HistoryFile:          history,
		HistoryInfoAllowlist: DefaultHistoryInfoAllowlist(),
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	const n = 64
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			l.Warn("daemon", EventDaemonWatchError, "", errors.New("boom"), Fields{"idx": i})
		}(i)
	}
	wg.Wait()

	data, err := os.ReadFile(history)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != n {
		t.Fatalf("expected %d lines, got %d", n, len(lines))
	}
	for i, line := range lines {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("line %d invalid JSON: %v", i, err)
		}
	}
}

func TestValidEventName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		event Event
		ok    bool
	}{
		{event: EventProcessorMoveMainApplied, ok: true},
		{event: EventSystemStartup, ok: true},
		{event: Event("WARN_EVENT"), ok: false},
		{event: Event("has spaces"), ok: false},
		{event: Event("trailing."), ok: false},
	}
	for _, tc := range tests {
		if got := validEventName(tc.event); got != tc.ok {
			t.Fatalf("validEventName(%q)=%v want %v", tc.event, got, tc.ok)
		}
	}
}

func TestAllOperationalEventsAreValidDotCase(t *testing.T) {
	t.Parallel()

	for _, event := range AllOperationalEvents() {
		if !validEventName(event) {
			t.Fatalf("invalid event constant %q", event)
		}
	}
}

func TestLegacyUnderscoreEventsAreRejected(t *testing.T) {
	t.Parallel()

	legacy := []Event{
		"system.shutdown.grace_elapsed",
		"processor.cleanup.skipped.associated_failed",
		"processor.cleanup.source_failed",
		"processor.input.max_depth_no_media",
		"processor.input.skipped.input_missing",
		"processor.input.skipped.parse_error",
		"processor.input.skipped.not_media",
		"processor.input.skipped.no_main_media",
		"processor.movie_pack.skip_unparseable",
	}
	for _, event := range legacy {
		if validEventName(event) {
			t.Fatalf("expected legacy underscore event to be invalid: %q", event)
		}
	}
}

func TestDefaultHistoryInfoAllowlistUsesKnownOperationalEvents(t *testing.T) {
	t.Parallel()

	all := make(map[Event]struct{}, len(AllOperationalEvents()))
	for _, event := range AllOperationalEvents() {
		all[event] = struct{}{}
	}

	for _, event := range DefaultHistoryInfoAllowlist() {
		if _, ok := all[event]; !ok {
			t.Fatalf("allowlist event is not a known operational constant: %q", event)
		}
		if !validEventName(event) {
			t.Fatalf("allowlist event is not valid dot-case: %q", event)
		}
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
