package daemon

import (
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// inFlightSet tracks paths currently queued or being processed, so a
// filesystem event for the same file arriving again before the first
// finishes doesn't enqueue a duplicate. The zero value is ready to use.
// Safe for concurrent use by multiple goroutines.
type inFlightSet struct {
	mu sync.Mutex
	m  map[string]struct{}
}

// Key normalizes path to the identity inFlightSet tracks: resolved symlinks,
// case-folded on case-insensitive filesystems. Two paths that reach the same
// file (e.g. via a symlinked drop folder, or a case difference on macOS)
// must map to the same key so they're recognized as the same in-flight item.
func (s *inFlightSet) Key(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = filepath.Clean(path)
	if realPath, err := filepath.EvalSymlinks(path); err == nil {
		path = filepath.Clean(realPath)
	}
	if isCaseInsensitiveFS() {
		path = strings.ToLower(path)
	}
	return path
}

// TryMark records key in-flight. It returns false if key was already
// in-flight (a duplicate), true otherwise.
func (s *inFlightSet) TryMark(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = make(map[string]struct{})
	}
	if _, ok := s.m[key]; ok {
		return false
	}
	s.m[key] = struct{}{}
	return true
}

// Is reports whether key is currently in-flight.
func (s *inFlightSet) Is(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.m[key]
	return ok
}

// Clear removes key from the set.
func (s *inFlightSet) Clear(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
}

// Reset clears all tracked state (e.g. at the start of a new daemon Run),
// without reassigning the set itself -- reassigning would copy the embedded
// mutex, which go vet's copylocks check forbids.
func (s *inFlightSet) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m = nil
}

func isCaseInsensitiveFS() bool {
	switch runtime.GOOS {
	case "darwin", "windows":
		return true
	default:
		return false
	}
}
