package watch

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func TestDropFolderWatcher_SeedExisting(t *testing.T) {
	root := t.TempDir()

	filePath := filepath.Join(root, "movie.mkv")
	writeFile(t, filePath, "data")

	dirPath := filepath.Join(root, "Some.Show.S01E01")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(dirPath, "episode.mkv"), "data")

	w := startWatcher(t, root, 300*time.Millisecond)

	waitForEvents(t, w.Events(), []string{filePath, dirPath}, 3*time.Second)
}

func TestDropFolderWatcher_EmitOnCreate(t *testing.T) {
	root := t.TempDir()

	w := startWatcher(t, root, 300*time.Millisecond)

	filePath := filepath.Join(root, "new_file.mkv")
	writeFile(t, filePath, "data")

	waitForEvents(t, w.Events(), []string{filePath}, 3*time.Second)
}

func TestDropFolderWatcher_DedupeWindow(t *testing.T) {
	root := t.TempDir()

	w := startWatcher(t, root, 200*time.Millisecond)

	filePath := filepath.Join(root, "dedupe.mkv")
	writeFile(t, filePath, "data")
	waitForEvents(t, w.Events(), []string{filePath}, 3*time.Second)

	// Clear any buffered events before the second write.
	drainEvents(w.Events())

	writeFile(t, filePath, "data2")

	// The dedupe window is 2s; ensure we do not emit again within a shorter window.
	expectNoEvent(t, w.Events(), 800*time.Millisecond)
}

func TestDropFolderWatcher_SettleTiming(t *testing.T) {
	root := t.TempDir()

	settle := 400 * time.Millisecond
	w := startWatcher(t, root, settle)

	filePath := filepath.Join(root, "settle.mkv")
	writeFile(t, filePath, "data")

	time.Sleep(120 * time.Millisecond)
	lastWrite := time.Now()
	writeFile(t, filePath, "data2")

	elapsed := waitForEventAfter(t, w.Events(), filePath, 3*time.Second, lastWrite)
	if elapsed < settle-50*time.Millisecond {
		t.Fatalf("event arrived too early: %s (settle=%s)", elapsed, settle)
	}
}

func TestDropFolderWatcher_DropMissingBeforeEmit(t *testing.T) {
	root := t.TempDir()

	settle := 300 * time.Millisecond
	w := startWatcher(t, root, settle)

	filePath := filepath.Join(root, "transient.mkv")
	writeFile(t, filePath, "data")

	time.Sleep(80 * time.Millisecond)
	if err := os.Remove(filePath); err != nil {
		t.Fatalf("remove: %v", err)
	}

	expectNoEvent(t, w.Events(), settle+400*time.Millisecond)
}

func TestDropFolderWatcher_StartRetryAfterInitFailure(t *testing.T) {
	root := t.TempDir()

	w, err := NewDropFolderWatcher(root, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("NewDropFolderWatcher error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		_ = w.Close()
	})

	// Force initialization failure on first Start by removing the watch root.
	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("remove root: %v", err)
	}
	if err := w.Start(ctx); err == nil {
		t.Fatalf("expected first Start() to fail when root is missing")
	}

	// Recreate root and ensure a second Start actually starts loops and emits events.
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("recreate root: %v", err)
	}
	if err := w.Start(ctx); err != nil {
		t.Fatalf("second Start() error: %v", err)
	}

	filePath := filepath.Join(root, "retry-ok.mkv")
	writeFile(t, filePath, "data")
	waitForEvents(t, w.Events(), []string{filePath}, 3*time.Second)
}

func TestDropFolderWatcher_ProcessingRoot(t *testing.T) {
	root := t.TempDir()

	w, err := NewDropFolderWatcher(root, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("NewDropFolderWatcher error: %v", err)
	}

	if got := w.processingRoot(root); got != "" {
		t.Fatalf("processingRoot(root) = %q, want empty", got)
	}

	child := filepath.Join(root, "Movie.mkv")
	if got := w.processingRoot(child); got != child {
		t.Fatalf("processingRoot(child) = %q, want %q", got, child)
	}

	nested := filepath.Join(root, "Show", "Season 01", "ep.mkv")
	wantTop := filepath.Join(root, "Show")
	if got := w.processingRoot(nested); got != wantTop {
		t.Fatalf("processingRoot(nested) = %q, want %q", got, wantTop)
	}
}

func TestIsIgnorableDropEntry(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{name: "", want: true},
		{name: " ", want: true},
		{name: ".DS_Store", want: true},
		{name: ".localized", want: true},
		{name: "desktop.ini", want: true},
		{name: "._metadata", want: true},
		{name: "movie.mkv", want: false},
	}

	for _, c := range cases {
		if got := IsIgnorableDropEntry(c.name); got != c.want {
			t.Fatalf("IsIgnorableDropEntry(%q) = %t, want %t", c.name, got, c.want)
		}
	}
}

func startWatcher(t *testing.T, root string, settle time.Duration) *DropFolderWatcher {
	t.Helper()

	w, err := NewDropFolderWatcher(root, settle)
	if err != nil {
		t.Fatalf("NewDropFolderWatcher error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := w.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Start error: %v", err)
	}

	t.Cleanup(func() {
		cancel()
		_ = w.Close()
	})

	return w
}

func waitForEvents(t *testing.T, ch <-chan string, want []string, timeout time.Duration) {
	t.Helper()

	wantSet := make(map[string]struct{}, len(want))
	for _, w := range want {
		wantSet[w] = struct{}{}
	}

	got := make(map[string]struct{}, len(wantSet))
	deadline := time.After(timeout)

	for len(got) < len(wantSet) {
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for events; missing=%v got=%v", missingKeys(wantSet, got), sortedKeys(got))
		case ev := <-ch:
			if _, ok := wantSet[ev]; ok {
				got[ev] = struct{}{}
			}
		}
	}
}

func expectNoEvent(t *testing.T, ch <-chan string, timeout time.Duration) {
	t.Helper()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case ev := <-ch:
		t.Fatalf("unexpected event: %q", ev)
	case <-timer.C:
	}
}

func waitForEventAfter(t *testing.T, ch <-chan string, want string, timeout time.Duration, start time.Time) time.Duration {
	t.Helper()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case ev := <-ch:
			if ev == want {
				return time.Since(start)
			}
		case <-timer.C:
			t.Fatalf("timeout waiting for event %q", want)
			return 0
		}
	}
}

func drainEvents(ch <-chan string) {
	for {
		select {
		case <-ch:
			continue
		default:
			return
		}
	}
}

func missingKeys(want map[string]struct{}, got map[string]struct{}) []string {
	out := make([]string, 0, len(want))
	for k := range want {
		if _, ok := got[k]; !ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}
