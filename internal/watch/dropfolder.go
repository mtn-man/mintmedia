package watch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Mtn-Man/mintmedia/internal/paths"

	"github.com/fsnotify/fsnotify"
)

// DropFolderWatcher watches a directory tree and emits "stable" paths after a settle period.
// It is designed for macOS-style bursty events (CREATE/WRITE/RENAME storms).
type DropFolderWatcher struct {
	root           string
	settleDuration time.Duration

	watcher *fsnotify.Watcher

	events chan string
	errs   chan error

	// pending maps a "root path" -> last event time.
	mu      sync.Mutex
	pending map[string]time.Time

	// optional: dedupe emissions so we don't emit same path repeatedly in short windows
	lastEmitted  map[string]time.Time
	dedupeWindow time.Duration

	startMu sync.Mutex
	started bool
}

// NewDropFolderWatcher creates a recursive watcher rooted at rootDir.
// settleDuration determines how long a path must be quiet before it is emitted.
func NewDropFolderWatcher(rootDir string, settleDuration time.Duration) (*DropFolderWatcher, error) {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		return nil, errors.New("drop folder root is empty")
	}
	abs, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve root dir: %w", err)
	}

	st, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat root dir: %w", err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("root is not a directory: %s", abs)
	}

	if settleDuration <= 0 {
		return nil, errors.New("settleDuration must be > 0")
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("fsnotify.NewWatcher: %w", err)
	}

	d := &DropFolderWatcher{
		root:           abs,
		settleDuration: settleDuration,
		watcher:        w,

		events: make(chan string, 128),
		errs:   make(chan error, 32),

		pending:     make(map[string]time.Time),
		lastEmitted: make(map[string]time.Time),

		// Avoid emitting the same path repeatedly if multiple independent settle cycles occur quickly.
		dedupeWindow: 2 * time.Second,
	}

	return d, nil
}

// Events returns a channel of stable paths that should be processed.
func (d *DropFolderWatcher) Events() <-chan string { return d.events }

// Errors returns a channel of watcher errors.
func (d *DropFolderWatcher) Errors() <-chan error { return d.errs }

// Start begins watching. It returns immediately; the watcher runs in goroutines.
// The watcher loops exit on ctx cancellation; Close() must be called to release underlying fsnotify resources.
func (d *DropFolderWatcher) Start(ctx context.Context) error {
	d.startMu.Lock()
	if d.started {
		d.startMu.Unlock()
		return nil
	}
	d.started = true
	d.startMu.Unlock()

	// Add watches for existing directories under root.
	if err := d.addWatchesRecursively(d.root); err != nil {
		return err
	}

	// Seed initial pending paths so pre-existing items get processed.
	if err := d.seedExisting(); err != nil {
		return err
	}

	// Goroutine 1: consume fsnotify events and update pending set.
	go d.loopEvents(ctx)

	// Goroutine 2: periodically check pending entries and emit stable ones.
	go d.loopSettle(ctx)

	return nil
}

func (d *DropFolderWatcher) Close() error {
	// Closing the watcher will cause fsnotify event/error channels to close
	// and our loops to exit. Do not close output channels here; callers stop
	// consuming based on context cancellation.
	return d.watcher.Close()
}

// --- Internals --------------------------------------------------------------

func (d *DropFolderWatcher) loopEvents(ctx context.Context) {
	defer func() {
		// If ctx triggers shutdown, Close() should be called by the orchestrator.
	}()

	for {
		select {
		case <-ctx.Done():
			return

		case ev, ok := <-d.watcher.Events:
			if !ok {
				return
			}
			// We care about create/write/rename primarily.
			if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) == 0 {
				continue
			}

			// If a directory is created, add a watch on it (and subdirs if any).
			// Note: Stat may fail transiently; that's OK.
			if ev.Op&fsnotify.Create != 0 {
				if st, err := os.Stat(ev.Name); err == nil && st.IsDir() {
					_ = d.addWatchesRecursively(ev.Name)
				}
			}

			root := d.processingRoot(ev.Name)

			if root == "" {
				continue
			}

			if IsIgnorableDropEntry(filepath.Base(root)) {
				continue
			}

			d.mu.Lock()
			d.pending[root] = time.Now()
			d.mu.Unlock()

		case err, ok := <-d.watcher.Errors:
			if !ok {
				return
			}
			select {
			case d.errs <- err:
			default:
				// drop if channel is full to avoid deadlock
			}
		}
	}
}

func (d *DropFolderWatcher) loopSettle(ctx context.Context) {
	// Tick faster than settleDuration to be responsive but not busy.
	tick := d.settleDuration / 3
	if tick < 250*time.Millisecond {
		tick = 250 * time.Millisecond
	}
	t := time.NewTicker(tick)
	defer t.Stop()

	type pendingItem struct {
		path string
		last time.Time
	}

	lastPrune := time.Now()

	for {
		select {
		case <-ctx.Done():
			return

		case now := <-t.C:
			var (
				candidates []pendingItem
				toEmit     []string
			)

			d.mu.Lock()

			// Periodically prune lastEmitted to avoid unbounded growth.
			if now.Sub(lastPrune) >= time.Minute {
				cutoff := now.Add(-5 * d.dedupeWindow)
				for p, ts := range d.lastEmitted {
					if ts.Before(cutoff) {
						delete(d.lastEmitted, p)
					}
				}
				lastPrune = now
			}

			for path, last := range d.pending {
				if now.Sub(last) < d.settleDuration {
					continue
				}
				candidates = append(candidates, pendingItem{path: path, last: last})
			}
			d.mu.Unlock()

			for _, item := range candidates {
				// If the path no longer exists, drop it silently.
				// This is expected when Mintmedia moves a file out of the drop folder.
				if _, err := os.Stat(item.path); err != nil {
					d.mu.Lock()
					if cur, ok := d.pending[item.path]; ok && cur.Equal(item.last) {
						delete(d.pending, item.path)
					}
					d.mu.Unlock()
					continue
				}

				d.mu.Lock()
				cur, ok := d.pending[item.path]
				if !ok || !cur.Equal(item.last) {
					d.mu.Unlock()
					continue
				}
				// Dedupe emits to avoid spam.
				if le, ok := d.lastEmitted[item.path]; ok && now.Sub(le) < d.dedupeWindow {
					delete(d.pending, item.path)
					d.mu.Unlock()
					continue
				}
				delete(d.pending, item.path)
				d.mu.Unlock()

				toEmit = append(toEmit, item.path)
			}

			for _, pth := range toEmit {
				emitTime := time.Now()

				// Mark as emitted before releasing the lock so new fsnotify events during
				// the send window are still deduped. If the send fails (channel full),
				// roll back the marker and requeue.
				d.mu.Lock()
				d.lastEmitted[pth] = emitTime
				d.mu.Unlock()

				select {
				case <-ctx.Done():
					return
				case d.events <- pth:
					// sent; keep lastEmitted as-is
				default:
					// If consumer is slow, requeue so we retry on the next tick and
					// remove the lastEmitted mark to allow re-emit.
					d.mu.Lock()
					delete(d.lastEmitted, pth)
					if cur, ok := d.pending[pth]; !ok || cur.Before(emitTime) {
						d.pending[pth] = time.Now().Add(-d.settleDuration)
					}
					d.mu.Unlock()
				}
			}
		}
	}
}

// addWatchesRecursively adds watches for rootDir and any subdirectories.
func (d *DropFolderWatcher) addWatchesRecursively(rootDir string) error {
	rootDir = filepath.Clean(rootDir)

	if !paths.WithinMaxDepth(d.root, rootDir, paths.MaxDepth) {
		return nil
	}

	// Always add the directory itself if possible.
	if err := d.watcher.Add(rootDir); err != nil {
		// Some directories may be transient; return error for root, ignore for subdirs.
		// We'll treat all adds as best-effort except for the main root, which is handled by caller.
		// Here: we return err; caller decides.
		return fmt.Errorf("watcher.Add(%s): %w", rootDir, err)
	}

	// Walk subdirectories.
	return filepath.WalkDir(rootDir, func(path string, de os.DirEntry, err error) error {
		if err != nil {
			// Ignore traversal errors; filesystem may be in flux.
			return nil
		}
		if !de.IsDir() {
			return nil
		}
		// Skip the root itself; already added.
		if samePath(path, rootDir) {
			return nil
		}
		if !paths.WithinMaxDepth(d.root, path, paths.MaxDepth) {
			return filepath.SkipDir
		}
		// Add subdir watch best-effort.
		_ = d.watcher.Add(path)
		return nil
	})
}

// seedExisting adds current top-level entries to pending so they emit after the normal settle delay.
func (d *DropFolderWatcher) seedExisting() error {
	entries, err := os.ReadDir(d.root)
	if err != nil {
		return fmt.Errorf("readdir root: %w", err)
	}

	emitAt := time.Now()

	d.mu.Lock()
	defer d.mu.Unlock()

	for _, ent := range entries {
		if IsIgnorableDropEntry(ent.Name()) {
			continue
		}
		path := filepath.Join(d.root, ent.Name())
		st, err := os.Stat(path)
		if err != nil {
			continue
		}
		if !st.IsDir() && !st.Mode().IsRegular() {
			continue
		}
		d.pending[path] = emitAt
	}

	return nil
}

// processingRoot decides what "unit of work" to emit for a given path.
// v1 policy:
// - If the path is directly under the drop folder root, process that path.
// - If the path is nested deeper (e.g., a file inside a torrent folder), process the top-level folder.
func (d *DropFolderWatcher) processingRoot(path string) string {
	path = filepath.Clean(path)

	rel, err := filepath.Rel(d.root, path)
	if err != nil {
		return path
	}
	rel = filepath.Clean(rel)

	sep := string(os.PathSeparator)
	if rel == "." {
		// The drop folder root itself is not a unit of work.
		// We only process files or directories *inside* the drop folder.
		return ""
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+sep) {
		// Outside root; should not happen, but fail safe.
		return path
	}

	parts := strings.Split(rel, sep)
	if len(parts) == 0 {
		return path
	}
	top := parts[0]
	if top == "" || top == "." {
		return path
	}

	// If nested deeper than one component, coalesce to the top-level folder.
	if len(parts) > 1 {
		return filepath.Join(d.root, top)
	}

	// Direct child (file or directory).
	return filepath.Join(d.root, top)
}

func IsIgnorableDropEntry(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return true
	}
	// Common macOS/Windows filesystem noise
	switch name {
	case ".DS_Store", ".localized", "desktop.ini":
		return true
	}
	// AppleDouble and other dot/underscore metadata files
	if strings.HasPrefix(name, "._") {
		return true
	}
	return false
}

// samePath compares paths case-insensitively to match default macOS filesystem behavior.
func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
