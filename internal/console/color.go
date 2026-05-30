package console

import (
	"os"
	"strings"
)

const (
	Reset  = "\033[0m"
	Red    = "\033[31m"
	Green  = "\033[32m"
	Yellow = "\033[33m"
	Cyan   = "\033[36m"
)

// colorEnabled is true when stdout is a terminal.
var colorEnabled = IsTerminal(os.Stdout)

// IsTerminal reports whether f is an interactive terminal.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// prefixColors maps known label prefixes to their ANSI color.
var prefixColors = []struct {
	prefix string
	color  string
}{
	{"STARTED  ", Green},
	{"SORTED   ", Green},
	{"REMOVED  ", Green},
	{"SORTING  ", Yellow},
	{"SKIPPED  ", Yellow},
	{"WARNING  ", Yellow},
	{"ERROR    ", Red},
	{"TORRENT  ", Cyan},
}

// Colorize wraps text in the given ANSI color if color is enabled.
func Colorize(text, color string) string {
	if !colorEnabled {
		return text
	}
	return color + text + Reset
}

// ColorizePrefix detects a known label prefix at the start of line
// and wraps it in the appropriate ANSI color. Returns the line
// unchanged if color is disabled or no prefix matches.
func ColorizePrefix(line string) string {
	if !colorEnabled {
		return line
	}
	// Strip leading newlines, colorize the prefix, then re-prepend them.
	leading := 0
	for leading < len(line) && line[leading] == '\n' {
		leading++
	}
	rest := line[leading:]
	for _, p := range prefixColors {
		if strings.HasPrefix(rest, p.prefix) {
			return line[:leading] + p.color + p.prefix + Reset + rest[len(p.prefix):]
		}
	}
	return line
}
