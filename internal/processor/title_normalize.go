package processor

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// normalizeTitleKey produces a diacritic-, punctuation-, and
// case-insensitive comparison key for fuzzy title matching (movies and
// shows), e.g. "Amélie" and "Amelie" both become "amelie", and "Spider-Man"
// and "Spider Man" both become "spiderman". It never drops words (including
// leading articles) -- only characters within words are normalized, so
// "The Amazing Spiderman" and "Amazing Spiderman" must remain distinct keys.
// An empty result means no match is possible.
func normalizeTitleKey(s string) string {
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

// yearMatchTier reports how confidently two years on either side of a fuzzy
// title match (an existing library entry vs. an incoming file) represent
// the same work.
type yearMatchTier int

const (
	yearMatchDisagree   yearMatchTier = iota // both have years, and they differ -- different works
	yearMatchAsymmetric                      // exactly one side has a year -- ambiguous
	yearMatchAgree                           // same year, or neither has one -- same work
)

// classifyYearMatch compares the years on either side of a fuzzy title
// match. Shared by movie and show fuzzy-duplicate detection -- only the
// action taken per tier differs between the two.
func classifyYearMatch(a, b string) yearMatchTier {
	switch {
	case a == "" && b == "":
		return yearMatchAgree
	case a != "" && b != "" && a == b:
		return yearMatchAgree
	case a == "" || b == "":
		return yearMatchAsymmetric
	default:
		return yearMatchDisagree
	}
}
