package processor

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// normalizeMovieTitleKey produces a diacritic-, punctuation-, and
// case-insensitive comparison key for fuzzy movie-title matching, e.g.
// "Amélie" and "Amelie" both become "amelie", and "Spider-Man" and
// "Spider Man" both become "spiderman". It never drops words (including
// leading articles) -- only characters within words are normalized, so
// "The Amazing Spiderman" and "Amazing Spiderman" must remain distinct keys.
// An empty result means no match is possible.
func normalizeMovieTitleKey(s string) string {
	// transform.Transformer is stateful and not safe for concurrent reuse
	// (Plan runs may be called from multiple goroutines), so build a fresh
	// chain per call rather than sharing one.
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	folded, _, err := transform.String(t, s)
	if err != nil {
		folded = s
	}
	folded = strings.ToLower(folded)

	var b strings.Builder
	b.Grow(len(folded))
	for _, r := range folded {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
