package processor

import (
	"context"
	"sync"
)

// DestDegradedTracker tracks which destination categories (Movies/Shows) are
// currently refusing writes (disk full, over quota, permission denied).
// Presence as a key means degraded; absent means healthy. The triggering
// error is logged by the caller at the point of detection, not stored here,
// since nothing needs to read it back later.
//
// The zero value is ready to use. Safe for concurrent use by multiple
// goroutines; the mutex's uncontended cost is negligible even for
// single-threaded callers such as a one-shot CLI run.
type DestDegradedTracker struct {
	mu   sync.Mutex
	cats map[Category]struct{}
}

// Mark records cat as degraded. It returns true only the first time this is
// called for a healthy cat (a healthy->degraded transition), so callers can
// log the loud warning exactly once instead of on every subsequent failure.
func (t *DestDegradedTracker) Mark(cat Category) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cats == nil {
		t.cats = make(map[Category]struct{})
	}
	if _, already := t.cats[cat]; already {
		return false
	}
	t.cats[cat] = struct{}{}
	return true
}

// Clear marks cat healthy again. It returns true only when cat was actually
// degraded (a degraded->healthy transition).
func (t *DestDegradedTracker) Clear(cat Category) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.cats[cat]; !ok {
		return false
	}
	delete(t.cats, cat)
	return true
}

// IsDegraded reports whether cat is currently degraded.
func (t *DestDegradedTracker) IsDegraded(cat Category) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.cats[cat]
	return ok
}

// Any reports whether any category is currently degraded, so callers can
// skip the cost of planning a category just to check in the common
// (healthy) case.
func (t *DestDegradedTracker) Any() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.cats) > 0
}

// Degraded returns the categories currently marked degraded.
func (t *DestDegradedTracker) Degraded() []Category {
	t.mu.Lock()
	defer t.mu.Unlock()
	cats := make([]Category, 0, len(t.cats))
	for cat := range t.cats {
		cats = append(cats, cat)
	}
	return cats
}

// Reset clears all tracked state (e.g. at the start of a new daemon Run),
// without reassigning the tracker itself -- reassigning would copy the
// embedded mutex, which go vet's copylocks check forbids.
func (t *DestDegradedTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cats = nil
}

// ClassifyDegraded reports whether path's category is currently degraded,
// folding Any()+CategoryForPath+IsDegraded into a single call so callers
// avoid CategoryForPath's Plan() cost entirely in the common (healthy) case
// -- a known-full disk still costs a real write if attempted, since
// RenameOrCopy's cross-device fallback copies the whole file into a temp
// file on the destination before Sync/Rename would hit ENOSPC. known is
// false when Plan() couldn't determine a category for an unrelated reason,
// in which case degraded is always false too.
func (t *DestDegradedTracker) ClassifyDegraded(ctx context.Context, proc Processor, path string) (cat Category, degraded bool) {
	if !t.Any() {
		return "", false
	}
	cat, known := CategoryForPath(ctx, proc, path)
	if !known {
		return "", false
	}
	return cat, t.IsDegraded(cat)
}

// DirFor maps cat to its configured destination directory.
func DirFor(cat Category, moviesDir, showsDir string) string {
	if cat == CategoryShow {
		return showsDir
	}
	return moviesDir
}
