package console

import (
	"os"
	"testing"
)

func TestColorizePrefix(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{"started", "STARTED  daemon", Green + "STARTED  " + Reset + "daemon"},
		{"created", "CREATED  file.mkv", Green + "CREATED  " + Reset + "file.mkv"},
		{"sorted", "SORTED   Movie (2024)", Green + "SORTED   " + Reset + "Movie (2024)"},
		{"removed", "REMOVED  old.torrent", Green + "REMOVED  " + Reset + "old.torrent"},
		{"status", "STATUS   running", Green + "STATUS   " + Reset + "running"},
		{"stopped", "STOPPED  daemon", Green + "STOPPED  " + Reset + "daemon"},
		{"sorting", "SORTING  file.mkv 50%", Yellow + "SORTING  " + Reset + "file.mkv 50%"},
		{"skipped", "SKIPPED  duplicate", Yellow + "SKIPPED  " + Reset + "duplicate"},
		{"warning", "WARNING  ambiguous show", Yellow + "WARNING  " + Reset + "ambiguous show"},
		{"error", "ERROR    disk full", Red + "ERROR    " + Reset + "disk full"},
		{"torrent", "TORRENT  queued", Cyan + "TORRENT  " + Reset + "queued"},
		{"unknown prefix unchanged", "INFO     something", "INFO     something"},
		{"empty line unchanged", "", ""},
		{"leading newline preserved", "\nSORTED   file.mkv", "\n" + Green + "SORTED   " + Reset + "file.mkv"},
		{"multiple leading newlines preserved", "\n\nERROR    boom", "\n\n" + Red + "ERROR    " + Reset + "boom"},
		{"prefix substring without trailing spaces not matched", "SORTEDfile.mkv", "SORTEDfile.mkv"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := colorizePrefix(true, tt.line)
			if got != tt.want {
				t.Fatalf("colorizePrefix(true, %q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}

func TestColorizePrefix_Disabled(t *testing.T) {
	line := "SORTED   Movie (2024)"
	got := colorizePrefix(false, line)
	if got != line {
		t.Fatalf("colorizePrefix(false, %q) = %q, want unchanged", line, got)
	}
}

func TestColorize(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		text    string
		color   string
		want    string
	}{
		{"disabled returns unchanged", false, "hello", Red, "hello"},
		{"enabled wraps in color", true, "hello", Red, Red + "hello" + Reset},
		{"enabled with empty text", true, "", Green, Green + "" + Reset},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := colorize(tt.enabled, tt.text, tt.color)
			if got != tt.want {
				t.Fatalf("colorize(%v, %q, color) = %q, want %q", tt.enabled, tt.text, got, tt.want)
			}
		})
	}
}

func TestIsTerminal(t *testing.T) {
	t.Run("nil file", func(t *testing.T) {
		if IsTerminal(nil) {
			t.Fatal("expected IsTerminal(nil) to be false")
		}
	})

	t.Run("regular file is not a terminal", func(t *testing.T) {
		f, err := os.CreateTemp(t.TempDir(), "isterminal")
		if err != nil {
			t.Fatalf("CreateTemp: %v", err)
		}
		defer func() { _ = f.Close() }()

		if IsTerminal(f) {
			t.Fatal("expected IsTerminal(regular file) to be false")
		}
	})
}
