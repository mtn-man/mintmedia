package processor

import "testing"

func TestNormalizeTitleKey(t *testing.T) {
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
			ka := normalizeTitleKey(tt.a)
			kb := normalizeTitleKey(tt.b)
			got := ka == kb
			if got != tt.want {
				t.Fatalf("normalizeTitleKey(%q)=%q, normalizeTitleKey(%q)=%q, equal=%v, want %v", tt.a, ka, tt.b, kb, got, tt.want)
			}
		})
	}
}

func TestNormalizeTitleKey_Empty(t *testing.T) {
	if got := normalizeTitleKey(""); got != "" {
		t.Fatalf("normalizeTitleKey(\"\") = %q, want empty", got)
	}
}

func TestClassifyYearMatch(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want yearMatchTier
	}{
		{"both empty", "", "", yearMatchAgree},
		{"both equal", "2001", "2001", yearMatchAgree},
		{"both non-empty, differ", "2000", "2006", yearMatchDisagree},
		{"a empty, b set", "", "2000", yearMatchAsymmetric},
		{"a set, b empty", "2000", "", yearMatchAsymmetric},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyYearMatch(tt.a, tt.b); got != tt.want {
				t.Fatalf("classifyYearMatch(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
