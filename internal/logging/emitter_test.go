package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmitter_ComponentForEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fallback string
		event    Event
		want     string
	}{
		// A recognized prefix wins over the fallback, so an event is attributed
		// to the component that owns it even when another package emits it.
		{"system prefix beats daemon fallback", "daemon", EventSystemStartup, "system"},
		{"system prefix beats processor fallback", "processor", EventSystemStartup, "system"},
		{"processor prefix beats daemon fallback", "daemon", EventProcessorMoveMainApplied, "processor"},
		{"processor prefix with processor fallback", "processor", EventProcessorMoveMainApplied, "processor"},
		// No recognized prefix: each package keeps its own name. This is the
		// case the pre-Emitter daemon hardcoded to "daemon", which would have
		// mislabeled a processor event that lacked the prefix.
		{"daemon event falls back", "daemon", EventDaemonWatchError, "daemon"},
		{"unprefixed event falls back to processor", "processor", Event("weird"), "processor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := NewEmitter(nil, tt.fallback)
			if got := e.componentFor(tt.event); got != tt.want {
				t.Fatalf("componentFor(%q) with fallback %q = %q, want %q",
					tt.event, tt.fallback, got, tt.want)
			}
		})
	}
}

// The composites must send a message to the console and none to history --
// history carries Fields only. One entry carrying a single message to both is
// how colorized text would reach history.jsonl; the Logger interface makes that
// unrepresentable, and these composites are the supported way to reach both.
func TestEmitter_WarnSendsMessageToConsoleOnlyAndFieldsToHistory(t *testing.T) {
	t.Parallel()

	e, console, historyPath := newTestEmitter(t, "processor")

	e.Warn(EventProcessorMovieDuplicateNotice, "WARNING  something worth saying",
		errors.New("boom"), Fields{"incoming": "Alpha (2020)"})

	if got := console.String(); !strings.Contains(got, "WARNING  something worth saying") {
		t.Fatalf("console output = %q, want it to carry the message", got)
	}

	entry := readOneHistoryEntry(t, historyPath)
	if entry.Message != "" {
		t.Fatalf("history msg = %q, want empty -- Fields are the record", entry.Message)
	}
	if entry.Component != "processor" {
		t.Fatalf("history component = %q, want %q", entry.Component, "processor")
	}
	if got := entry.Fields["incoming"]; got != "Alpha (2020)" {
		t.Fatalf("history field incoming = %v, want %q", got, "Alpha (2020)")
	}
	if entry.Err == nil || entry.Err.Message != "boom" {
		t.Fatalf("history err = %+v, want the wrapped error", entry.Err)
	}
}

func TestEmitter_NilSafe(t *testing.T) {
	t.Parallel()

	var zero Emitter

	// Neither form may panic: processors and daemons are constructible without
	// a logger (tests do exactly that), and callers carry no guard of their own.
	for name, e := range map[string]Emitter{"zero value": zero, "nil logger": NewEmitter(nil, "processor")} {
		t.Run(name, func(_ *testing.T) {
			e.Info(EventProcessorMoveMainApplied, "INFO     hi", nil)
			e.Warn(EventProcessorMoveMainApplied, "WARNING  hi", errors.New("x"), nil)
			e.Error(EventDaemonWatchError, "ERROR    hi", errors.New("x"), nil)
			e.ConsoleInfo(EventSystemStartup, "INFO     hi", nil)
			e.HistoryInfo(EventSystemStartup, nil)
		})
	}
}

func newTestEmitter(t *testing.T, fallback string) (Emitter, *bytes.Buffer, string) {
	t.Helper()
	var console bytes.Buffer
	historyPath := filepath.Join(t.TempDir(), "history.jsonl")
	lg, err := New(Options{
		Stdout:       &console,
		Stderr:       &console,
		ConsoleLevel: "info",
		HistoryLevel: "info",
		HistoryFile:  historyPath,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return NewEmitter(lg, fallback), &console, historyPath
}

func readOneHistoryEntry(t *testing.T, historyPath string) Entry {
	t.Helper()
	raw, err := os.ReadFile(historyPath) //nolint:gosec // test-owned temp path
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 1 {
		t.Fatalf("history has %d entries, want 1:\n%s", len(lines), raw)
	}
	var entry Entry
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("unmarshal %q: %v", lines[0], err)
	}
	return entry
}
