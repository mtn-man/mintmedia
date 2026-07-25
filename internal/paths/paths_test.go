package paths

import (
	"os"
	"path/filepath"
	"reflect"
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

func TestRelComponents(t *testing.T) {
	tests := []struct {
		name      string
		root      string
		path      string
		wantParts []string
		wantOK    bool
	}{
		{"root equals path", "/library", "/library", nil, true},
		{"root equals path with trailing slash", "/library", "/library/", nil, true},
		{"nested child", "/library", "/library/Shows/Foo/Season 01", []string{"Shows", "Foo", "Season 01"}, true},
		{"direct child", "/library", "/library/Movies", []string{"Movies"}, true},
		{"path outside root", "/library", "/other/Movies", nil, false},
		{"exact dotdot escape", "/library", "/library/..", nil, false},
		{"nested escape", "/library", "/library/Movies/../../etc", nil, false},
		{"empty root", "", "/library/Movies", nil, false},
		{"empty path", "/library", "", nil, false},
		{"dirty uncleaned input", "/library//", "/library/./Movies/../Movies/Foo", []string{"Movies", "Foo"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotParts, gotOK := RelComponents(tt.root, tt.path)
			if gotOK != tt.wantOK {
				t.Fatalf("RelComponents(%q, %q) ok = %v, want %v", tt.root, tt.path, gotOK, tt.wantOK)
			}
			if gotOK && !reflect.DeepEqual(gotParts, tt.wantParts) {
				t.Fatalf("RelComponents(%q, %q) parts = %v, want %v", tt.root, tt.path, gotParts, tt.wantParts)
			}
		})
	}
}

func TestDirDepthFromRoot(t *testing.T) {
	tests := []struct {
		name      string
		root      string
		dir       string
		wantDepth int
		wantOK    bool
	}{
		{"at root", "/library", "/library", 0, true},
		{"one level down", "/library", "/library/Movies", 1, true},
		{"several levels down", "/library", "/library/Shows/Foo/Season 01/extras", 4, true},
		{"outside root", "/library", "/other", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDepth, gotOK := DirDepthFromRoot(tt.root, tt.dir)
			if gotOK != tt.wantOK || gotDepth != tt.wantDepth {
				t.Fatalf("DirDepthFromRoot(%q, %q) = (%d, %v), want (%d, %v)", tt.root, tt.dir, gotDepth, gotOK, tt.wantDepth, tt.wantOK)
			}
		})
	}
}

func TestWithinMaxDepth(t *testing.T) {
	tests := []struct {
		name     string
		root     string
		dir      string
		maxDepth int
		want     bool
	}{
		{"at limit", "/library", "/library/a/b/c", 3, true},
		{"under limit", "/library", "/library/a", 3, true},
		{"over limit", "/library", "/library/a/b/c/d", 3, false},
		{"outside root entirely", "/library", "/other/a", 3, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WithinMaxDepth(tt.root, tt.dir, tt.maxDepth)
			if got != tt.want {
				t.Fatalf("WithinMaxDepth(%q, %q, %d) = %v, want %v", tt.root, tt.dir, tt.maxDepth, got, tt.want)
			}
		})
	}
}
