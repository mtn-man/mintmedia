package notify

import (
	"sync"
	"time"
)

// Debouncer suppresses rapid repeated triggers within a cooldown window,
// coalescing many near-simultaneous "done" signals into a single audible
// play instead of overlapping playback (e.g. a fast, same-filesystem batch
// that finishes many files within milliseconds of each other). Zero value
// is ready to use.
type Debouncer struct {
	mu   sync.Mutex
	last time.Time
}

// Allow reports whether a trigger at now should proceed, given cooldown.
// The first call always allows. A call within cooldown of the last allowed
// call is suppressed (returns false) without updating state, so the next
// call is judged against the same last-allowed timestamp.
func (d *Debouncer) Allow(now time.Time, cooldown time.Duration) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.last.IsZero() && now.Sub(d.last) < cooldown {
		return false
	}
	d.last = now
	return true
}
