package clipboard

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mtn-man/mintmedia/internal/magnet"
)

// ErrUnsupportedPlatform indicates no clipboard backend is available on this platform/build.
var ErrUnsupportedPlatform = errors.New("clipboard backend unsupported on this platform")

// Poller polls clipboard content using a platform-specific backend.
// It emits magnet URIs when clipboard content changes.
type Poller struct {
	interval time.Duration

	events chan string
	errs   chan error

	lastClipboard   string
	lastChangeCount int64
	initialized     bool
}

// NewPoller creates a clipboard poller that runs at the given interval.
// interval must be > 0.
func NewPoller(interval time.Duration) (*Poller, error) {
	if !clipboardBackendSupported() {
		req := clipboardBackendRequirement()
		if strings.TrimSpace(req) == "" {
			return nil, ErrUnsupportedPlatform
		}
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedPlatform, req)
	}

	if interval <= 0 {
		return nil, errors.New("clipboard poll interval must be > 0")
	}
	return &Poller{
		interval: interval,
		events:   make(chan string, 64),
		errs:     make(chan error, 16),
	}, nil
}

// Events returns the channel of detected magnet URIs.
func (p *Poller) Events() <-chan string { return p.events }

// Errors returns the channel of backend errors encountered while polling.
func (p *Poller) Errors() <-chan error { return p.errs }

// Start begins polling. It returns immediately; polling happens in a goroutine.
// Stop is handled by ctx cancellation.
// NOTE: Start is safe to call only once per Poller instance. Repeated calls can
// spawn multiple loops that race to close shared channels on shutdown.
func (p *Poller) Start(ctx context.Context) {
	go p.loop(ctx)
}

// loop runs until ctx is cancelled.
// It polls the pasteboard change count and reads clipboard text when changed.
// It emits magnet URIs when clipboard content changes.
func (p *Poller) loop(ctx context.Context) {
	t := time.NewTicker(p.interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			close(p.events)
			close(p.errs)
			return

		case <-t.C:
			cc := pasteboardChangeCount(ctx)

			// First tick: initialize baseline silently
			if !p.initialized {
				p.lastChangeCount = cc
				s := pasteboardReadString(ctx)
				s = strings.TrimSpace(s)
				if s != "" {
					p.lastClipboard = s
				}
				p.initialized = true
				continue
			}

			if cc == p.lastChangeCount {
				continue
			}

			p.lastChangeCount = cc

			txt := pasteboardReadString(ctx)

			txt = strings.TrimSpace(txt)
			if txt == "" {
				continue
			}

			if txt == p.lastClipboard {
				continue
			}

			p.lastClipboard = txt

			magnets := extractMagnetURIs(txt)
			for _, m := range magnets {
				m = strings.TrimSpace(m)
				if !magnet.IsValid(m) {
					continue
				}
				p.trySendEvent(m)
			}
		}
	}
}

func (p *Poller) trySendEvent(magnet string) {
	select {
	case p.events <- magnet:
	default:
		// Drop if consumer is slow to avoid unbounded queue growth.
	}
}

// extractMagnetURIs finds magnet:? URIs inside text. Extraction here is
// deliberately permissive (token-prefix matching, not full validation) --
// callers (see loop()) run each result through magnet.IsValid before acting
// on it, so this only needs to narrow down candidates.
func extractMagnetURIs(text string) []string {
	// Fast path: whole clipboard is a magnet link.
	if strings.HasPrefix(text, "magnet:?") {
		return []string{strings.TrimSpace(text)}
	}

	// Otherwise, scan tokens and pick those starting with magnet:?
	// This handles cases where clipboard contains multiple links or surrounding text.
	fields := strings.Fields(text)
	var out []string
	seen := make(map[string]struct{})

	for _, f := range fields {
		if strings.HasPrefix(f, "magnet:?") {
			// Trim common trailing punctuation copied from chat clients.
			m := strings.TrimRight(f, ".,);]>\"'")
			if _, ok := seen[m]; ok {
				continue
			}
			seen[m] = struct{}{}
			out = append(out, m)
		}
	}

	return out
}
