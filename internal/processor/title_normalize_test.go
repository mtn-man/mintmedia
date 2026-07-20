package processor

import "testing"

func TestNormalizeMovieTitleKey(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want bool // whether a and b should normalize to the same key
	}{
		{"diacritic", "Amélie", "Amelie", true},
		{"punctuation hyphen vs space", "Spider-Man", "Spider Man", true},
		{"case fold", "SURVIVOR", "survivor", true},
		{"identical", "Fringe", "Fringe", true},
		{"article not dropped", "The Amazing Spiderman", "Amazing Spiderman", false},
		{"different word", "Survivor", "Survivor UK", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ka := normalizeMovieTitleKey(tt.a)
			kb := normalizeMovieTitleKey(tt.b)
			got := ka == kb
			if got != tt.want {
				t.Fatalf("normalizeMovieTitleKey(%q)=%q, normalizeMovieTitleKey(%q)=%q, equal=%v, want %v", tt.a, ka, tt.b, kb, got, tt.want)
			}
		})
	}
}

func TestNormalizeMovieTitleKey_Empty(t *testing.T) {
	if got := normalizeMovieTitleKey(""); got != "" {
		t.Fatalf("normalizeMovieTitleKey(\"\") = %q, want empty", got)
	}
}
