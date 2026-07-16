package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSameDevice_TempDir(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src.mkv")
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}

	same, err := SameDevice(src, root)
	if err != nil {
		t.Fatalf("SameDevice error: %v", err)
	}
	if !same {
		t.Fatalf("expected same device for temp dir paths")
	}
}

func TestSameDevice_StatError(t *testing.T) {
	root := t.TempDir()
	_, err := SameDevice(filepath.Join(root, "missing.mkv"), root)
	if err == nil {
		t.Fatalf("expected error for missing source path")
	}
}
