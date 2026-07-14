// Package resultformat renders a processor.Result as the tool's standard
// single-line SORTED/SKIPPED console status line. It exists so the CLI
// one-shot paths (--process, --process-drop) and the daemon share exactly
// one implementation instead of two independently hand-maintained ones.
package resultformat

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/mtn-man/mintmedia/internal/console"
	"github.com/mtn-man/mintmedia/internal/processor"
	"github.com/mtn-man/mintmedia/internal/shutdown"
)

// CleanName returns filepath.Base(raw), falling back to "(unknown)" for
// paths that resolve to "." or empty, so a status line never shows a bare
// "." when a path was empty or root.
func CleanName(raw string) string {
	name := filepath.Base(strings.TrimSpace(raw))
	if name == "." || name == string(filepath.Separator) || strings.TrimSpace(name) == "" {
		return "(unknown)"
	}
	return name
}

// CompactLine renders a processor.Result as a single-line status line in the
// tool's SORTED/SKIPPED voice. The line is unprefixed and uncolorized apart
// from the destination arrow — callers still apply
// console.ColorizePrefixOut/Err for the label color themselves, since that's
// applied uniformly to every console line, not just these two.
func CompactLine(res processor.Result, name string, dur time.Duration) string {
	if res.Applied {
		dest := strings.TrimSpace(res.Plan.DestMainPath)
		durSuffix := ""
		if dur >= time.Second {
			durSuffix = fmt.Sprintf("  (%s)", shutdown.FormatDurationCompact(dur))
		}
		if dest == "" {
			return fmt.Sprintf("SORTED   %s%s", name, durSuffix)
		}
		return fmt.Sprintf("SORTED   %s\n    %s   %s%s", name, console.ColorizeOut("->", console.Green), dest, durSuffix)
	}

	reason := strings.TrimSpace(res.Reason)
	if reason == "" {
		reason = "not applied"
	}
	return fmt.Sprintf("SKIPPED  %s — %s", name, reason)
}

// Pluralize returns singular when n == 1, plural otherwise.
func Pluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

// ShutdownWaitMessage renders the console WARNING line used for
// shutdown.Hooks.OnWaitStart, so the CLI and daemon shutdown paths share
// identical wording. noun names the in-flight work (e.g. "item", "jobs").
func ShutdownWaitMessage(noun string, grace time.Duration) string {
	return fmt.Sprintf("WARNING  shutdown requested. Waiting up to %s for in-flight %s.", shutdown.FormatDurationCompact(grace), noun)
}

// ShutdownGraceElapsedMessage renders the console WARNING line used for
// shutdown.Hooks.OnGraceElapsed, so the CLI and daemon shutdown paths share
// identical wording. noun names the in-flight work (e.g. "item", "jobs").
func ShutdownGraceElapsedMessage(noun string, force time.Duration) string {
	return fmt.Sprintf("WARNING  shutdown grace elapsed. Canceling in-flight %s (timeout=%s).", noun, shutdown.FormatDurationCompact(force))
}

// ErrorLine renders an item-level failure in the tool's ERROR voice. The line
// is unprefixed and uncolorized apart from the destination arrow convention
// used elsewhere — callers still apply console.ColorizePrefixErr themselves.
// dur is omitted from the line when it's under a second, matching CompactLine.
func ErrorLine(path string, err error, dur time.Duration) string {
	durSuffix := ""
	if dur >= time.Second {
		durSuffix = fmt.Sprintf("  (%s)", shutdown.FormatDurationCompact(dur))
	}
	return fmt.Sprintf("ERROR    %s — %v%s", path, err, durSuffix)
}
