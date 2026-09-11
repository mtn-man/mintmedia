package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInFlightSet_TryMarkIsClear(t *testing.T) {
	var s inFlightSet
	key := "/tmp/example"

	if !s.TryMark(key) {
		t.Fatalf("expected first mark to succeed")
	}
	if !s.Is(key) {
		t.Fatalf("expected key to be in-flight")
	}
	if s.TryMark(key) {
		t.Fatalf("expected duplicate mark to fail")
	}

	s.Clear(key)

	if s.Is(key) {
		t.Fatalf("expected key to be cleared from in-flight")
	}
	if !s.TryMark(key) {
		t.Fatalf("expected mark after clear to succeed")
	}
}

func TestInFlightSet_KeyCanonicalization(t *testing.T) {
	var s inFlightSet
	base := t.TempDir()

	realDir := filepath.Join(base, "RealCaps")
	subDir := filepath.Join(realDir, "SubCaps")
	mkdirAll(t, subDir)

	linkDir := filepath.Join(base, "LinkCaps")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	targetPath := filepath.Join(linkDir, "SubCaps")
	key := s.Key(targetPath)

	eval, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		t.Fatalf("EvalSymlinks failed: %v", err)
	}

	expected := filepath.Clean(eval)
	if isCaseInsensitiveFS() {
		expected = strings.ToLower(expected)
	}

	if key != expected {
		t.Fatalf("unexpected in-flight key: got %q want %q", key, expected)
	}
}
