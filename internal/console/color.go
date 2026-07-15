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

// stdoutEnabled/stderrEnabled are true when the respective stream is a
// terminal. WARNING/ERROR console lines are written to stderr while
// everything else goes to stdout (see logging.ConsoleSink.Write), so
// colorization must be decided per stream: redirecting one without the
// other must not leak raw ANSI escapes into whichever stream is redirected.
var (
	stdoutEnabled = IsTerminal(os.Stdout)
	stderrEnabled = IsTerminal(os.Stderr)
)

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
	{"CREATED  ", Green},
	{"SORTED   ", Green},
	{"REMOVED  ", Green},
	{"STATUS   ", Green},
	{"STOPPED  ", Green},
	{"SORTING  ", Yellow},
	{"SKIPPED  ", Yellow},
	{"WARNING  ", Yellow},
	{"ERROR    ", Red},
	{"TORRENT  ", Cyan},
}

// ColorizeOut wraps text in the given ANSI color if stdout is a terminal.
// Use for text that will be written to stdout.
func ColorizeOut(text, color string) string {
	return colorize(stdoutEnabled, text, color)
}

// ColorizeErr wraps text in the given ANSI color if stderr is a terminal.
// Use for text that will be written to stderr.
func ColorizeErr(text, color string) string {
	return colorize(stderrEnabled, text, color)
}

func colorize(enabled bool, text, color string) string {
	if !enabled {
		return text
	}
	return color + text + Reset
}

// ColorizePrefixOut detects a known label prefix at the start of line and
// wraps it in the appropriate ANSI color, for text written to stdout.
// Returns the line unchanged if stdout isn't a terminal or no prefix matches.
func ColorizePrefixOut(line string) string {
	return colorizePrefix(stdoutEnabled, line)
}

// ColorizePrefixErr is ColorizePrefixOut for text written to stderr.
func ColorizePrefixErr(line string) string {
	return colorizePrefix(stderrEnabled, line)
}

func colorizePrefix(enabled bool, line string) string {
	if !enabled {
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
