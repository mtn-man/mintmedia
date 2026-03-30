package transfer

import (
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

const (
	ansiReset  = "\033[0m"
	ansiCyan   = "\033[36m"
	ansiYellow = "\033[33m"
	ansiGreen  = "\033[32m"
)

// IsTerminal is a best-effort check for whether the given file is a terminal.
// It is intentionally simple and does not require third-party packages.
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

// Snapshot is a structured progress update emitted by the transfer layer.
// It is renderer-friendly and avoids embedding presentation concerns in transfer.go.
type Snapshot struct {
	Name     string        // base filename
	Copied   int64         // bytes copied so far
	Total    int64         // total bytes; <=0 if unknown
	RateMBps float64       // instantaneous/interval MB/s
	Elapsed  time.Duration // elapsed since start
}

// Reporter consumes progress snapshots and renders them to an output stream.
// Implementations should be safe for concurrent calls.
type Reporter interface {
	Update(s Snapshot)
	Done(s Snapshot)
}

// ReportOptions controls rendering behavior.
type ReportOptions struct {
	// EnableBar controls whether a progress bar may be rendered when conditions are met.
	EnableBar bool

	// SuppressDoneLine suppresses final "COPY DONE" renderer output while still
	// allowing progress updates and in-place line cleanup.
	SuppressDoneLine bool

	// BarMinBytes is the minimum total size required to show a bar.
	// Typical use: avoid bar for small/local copies.
	BarMinBytes int64

	// BarMinElapsed is the minimum elapsed time before a bar is shown.
	// Typical use: avoid flicker on near-instant operations.
	BarMinElapsed time.Duration

	// BarWidth is the width (in characters) of the bar.
	BarWidth int

	// EnableETA controls whether a best-effort ETA token may be rendered.
	// ETA is only shown when the progress bar is shown.
	EnableETA bool

	// EtaMinElapsed is the minimum elapsed time before ETA is shown.
	// Typical use: avoid noisy early estimates.
	EtaMinElapsed time.Duration
}

// NewTerminalReporter returns a reporter that:
// - If stdout is a terminal: renders progress in-place (single line).
// - If stdout is not a terminal: renders newline-delimited progress lines.
//
// It can optionally render a progress bar for large/slow transfers.
func NewTerminalReporter(out *os.File, opts ReportOptions) Reporter {
	inPlace := IsTerminal(out)

	// Defaults
	if opts.BarMinBytes <= 0 {
		opts.BarMinBytes = 200 * 1024 * 1024 // 200MB
	}
	if opts.BarMinElapsed <= 0 {
		opts.BarMinElapsed = 500 * time.Millisecond
	}
	if opts.BarWidth <= 0 {
		opts.BarWidth = 24
	}
	if opts.EtaMinElapsed <= 0 {
		opts.EtaMinElapsed = 500 * time.Millisecond
	}

	return &terminalReporter{out: out, inPlace: inPlace, opts: opts}
}

type terminalReporter struct {
	out     *os.File
	inPlace bool
	opts    ReportOptions

	mu sync.Mutex
}

func (r *terminalReporter) Update(s Snapshot) {
	if r.out == nil {
		return
	}
	name := strings.TrimSpace(s.Name)
	if name == "" {
		name = "(unknown)"
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	line := r.renderCopyingLine(name, s)
	if r.inPlace {
		_, _ = fmt.Fprintf(r.out, "\r\033[2K%s", line)
		return
	}
	_, _ = fmt.Fprintln(r.out, line)
}

func (r *terminalReporter) Done(s Snapshot) {
	if r.out == nil {
		return
	}
	name := strings.TrimSpace(s.Name)
	if name == "" {
		name = "(unknown)"
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// End any in-place line cleanly.
	if r.inPlace {
		_, _ = fmt.Fprint(r.out, "\r\033[2K")
	}

	if r.opts.SuppressDoneLine {
		return
	}

	// Print completion line.
	line := r.renderDoneLine(name, s)
	_, _ = fmt.Fprintln(r.out, line)
}

func (r *terminalReporter) shouldShowBar(s Snapshot) bool {
	if !r.inPlace {
		return false
	}
	if !r.opts.EnableBar {
		return false
	}
	if s.Total <= 0 {
		return false
	}
	if s.Total < r.opts.BarMinBytes {
		return false
	}
	if s.Elapsed < r.opts.BarMinElapsed {
		return false
	}
	return true
}

func (r *terminalReporter) renderCopyingLine(name string, s Snapshot) string {
	// If bar conditions are met, render bar line; otherwise render the classic line.
	if r.shouldShowBar(s) {
		return r.renderBarLine(name, s)
	}

	return r.renderClassicCopyingLine(name, s)
}

func (r *terminalReporter) renderClassicCopyingLine(name string, s Snapshot) string {
	// Classic line format (compatible with existing logs/colors).
	if s.Total > 0 {
		pct := (float64(s.Copied) / float64(s.Total)) * 100
		line := fmt.Sprintf(
			"SORTING  %s %.1f%% (%s / %s) %.1f MB/s",
			name,
			pct,
			humanBytes(s.Copied),
			humanBytes(s.Total),
			s.RateMBps,
		)
		return colorizeCopyingLine(line)
	}

	line := fmt.Sprintf(
		"SORTING  %s (%s copied) %.1f MB/s",
		name,
		humanBytes(s.Copied),
		s.RateMBps,
	)
	return colorizeCopyingLine(line)
}

func (r *terminalReporter) renderDoneLine(name string, s Snapshot) string {
	// Keep the existing DONE format for consistency.
	elapsed := s.Elapsed.Round(time.Second)
	if s.Total > 0 {
		return colorizeCopyDoneLine(fmt.Sprintf(
			"COPY DONE: %s (%s) in %s",
			name,
			humanBytes(s.Total),
			elapsed,
		))
	}
	return colorizeCopyDoneLine(fmt.Sprintf(
		"COPY DONE: %s in %s",
		name,
		elapsed,
	))
}

func (r *terminalReporter) renderBarLine(name string, s Snapshot) string {
	pct := (float64(s.Copied) / float64(s.Total)) * 100
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}

	w := r.opts.BarWidth
	if w <= 0 {
		return r.renderClassicCopyingLine(name, s)
	}
	filled := int((pct / 100) * float64(w))
	if filled < 0 {
		filled = 0
	}
	if filled > w {
		filled = w
	}

	barRaw := strings.Repeat("█", filled) + strings.Repeat("░", w-filled)
	bar := ansiCyan + barRaw + ansiReset

	// Color conventions:
	// - "SORTING " label in yellow
	// - progress bar in teal (cyan)
	// - percentage token in cyan
	labelRaw := "SORTING "
	label := ansiYellow + labelRaw + ansiReset
	pctTokRaw := fmt.Sprintf("%.0f%%", pct)
	pctTok := ansiCyan + pctTokRaw + ansiReset

	copiedStr := humanBytes(s.Copied)
	totalStr := humanBytes(s.Total)
	rateStr := fmt.Sprintf("%.1f MB/s", s.RateMBps)
	etaStr, etaOK := r.etaToken(s)

	nameToUse := name
	if width, ok := terminalWidth(r.out); ok {
		bytesStr := copiedStr + "/" + totalStr
		fixed := len(labelRaw) + 1 + 1 + 1 + w + 1 + 1 + len(pctTokRaw) + 1 + len(bytesStr) + 1 + len(rateStr)
		if etaOK {
			fixed += 1 + len(etaStr)
		}
		avail := width - fixed
		if avail < 4 {
			if etaOK {
				etaOK = false
				etaStr = ""
				fixed = len(labelRaw) + 1 + 1 + 1 + w + 1 + 1 + len(pctTokRaw) + 1 + len(bytesStr) + 1 + len(rateStr)
				avail = width - fixed
			}
			if avail < 4 {
				return r.renderClassicCopyingLine(name, s)
			}
		}
		nameToUse = truncateWithEllipsis(name, avail)
	}

	etaTail := ""
	if etaOK {
		etaTail = " " + etaStr
	}
	return fmt.Sprintf(
		"%s %s [%s] %s %s/%s %s%s",
		label,
		nameToUse,
		bar,
		pctTok,
		copiedStr,
		totalStr,
		rateStr,
		etaTail,
	)
}

func colorizeCopyingLine(line string) string {
	// Color the title "SORTING  " yellow, and the first percentage token (e.g. "76.0%") cyan.
	out := line
	if strings.HasPrefix(out, "SORTING  ") {
		out = ansiYellow + "SORTING  " + ansiReset + strings.TrimPrefix(out, "SORTING  ")
	}

	// Find the first percentage token and color it cyan.
	pctEnd := strings.Index(out, "%")
	if pctEnd == -1 {
		return out
	}

	// Walk backwards to find the start of the numeric token (digits or '.')
	start := pctEnd - 1
	for start >= 0 {
		c := out[start]
		if (c >= '0' && c <= '9') || c == '.' {
			start--
			continue
		}
		break
	}
	start++
	if start >= pctEnd {
		return out
	}

	pctToken := out[start : pctEnd+1]
	return out[:start] + ansiCyan + pctToken + ansiReset + out[pctEnd+1:]
}

func colorizeCopyDoneLine(line string) string {
	// Color the title "COPY DONE:" green.
	if strings.HasPrefix(line, "COPY DONE:") {
		return ansiGreen + "COPY DONE:" + ansiReset + strings.TrimPrefix(line, "COPY DONE:")
	}
	return line
}

func (r *terminalReporter) etaToken(s Snapshot) (string, bool) {
	if !r.opts.EnableETA {
		return "", false
	}
	if s.Total <= 0 || s.Copied <= 0 {
		return "", false
	}
	if s.Elapsed <= 0 {
		return "", false
	}
	if s.Elapsed < r.opts.EtaMinElapsed {
		return "", false
	}
	remaining := s.Total - s.Copied
	if remaining <= 0 {
		return "", false
	}
	avgBps := float64(s.Copied) / s.Elapsed.Seconds()
	if avgBps <= 0 {
		return "", false
	}
	etaSeconds := float64(remaining) / avgBps
	maxSeconds := float64(math.MaxInt64) / float64(time.Second)
	if etaSeconds <= 0 || etaSeconds > maxSeconds {
		return "", false
	}
	eta := time.Duration(etaSeconds * float64(time.Second)).Round(time.Second)
	if eta <= 0 {
		return "", false
	}
	return "ETA " + eta.String(), true
}

func terminalWidth(out *os.File) (int, bool) {
	if out == nil {
		return 0, false
	}
	width, _, err := term.GetSize(int(out.Fd()))
	if err != nil || width <= 1 {
		return 0, false
	}
	// Leave a 1-column safety margin to avoid wrap on some terminals.
	return width - 1, true
}

func truncateWithEllipsis(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}
