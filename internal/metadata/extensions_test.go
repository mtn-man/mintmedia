// internal/metadata/extensions_test.go
package metadata

import "testing"

func TestSupportsExtension(t *testing.T) {
	tests := []struct {
		ext  string
		want bool
	}{
		{".mp4", true},
		{".MP4", true},
		{".Mkv", true},
		{".mov", true},
		{".m4v", true},
		{".avi", false},
		{".ts", false},
		{".srt", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := SupportsExtension(tt.ext); got != tt.want {
			t.Errorf("SupportsExtension(%q) = %v, want %v", tt.ext, got, tt.want)
		}
	}
}
