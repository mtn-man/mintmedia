// internal/processor/sort.go
package processor

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Sort tiers: lower values are processed first.
const (
	tierMovie    = 0
	tierShow     = 1
	tierFallback = 2 // uncategorized or unexpected-error paths; sorted last by path
)

// sortKey holds the parsed sort fields for a single candidate path.
// title holds MovieTitle for movies and ShowName for shows.
// path is used as a stable tiebreaker.
type sortKey struct {
	tier    int
	title   string
	season  int
	episode int
	path    string
}

// less reports whether a should sort before b.
func (a sortKey) less(b sortKey) bool {
	if a.tier != b.tier {
		return a.tier < b.tier
	}
	switch a.tier {
	case tierMovie:
		at, bt := strings.ToLower(a.title), strings.ToLower(b.title)
		if at != bt {
			return at < bt
		}
	case tierShow:
		an, bn := strings.ToLower(a.title), strings.ToLower(b.title)
		if an != bn {
			return an < bn
		}
		if a.season != b.season {
			return a.season < b.season
		}
		if a.episode != b.episode {
			return a.episode < b.episode
		}
	default:
		ap, bp := strings.ToLower(a.path), strings.ToLower(b.path)
		if ap != bp {
			return ap < bp
		}
	}
	return strings.ToLower(a.path) < strings.ToLower(b.path)
}

// parseSortKey derives a sort key from the filename or directory name alone.
// It calls the filename parsers directly with no filesystem I/O.
func parseSortKey(blacklist []*regexp.Regexp, path string) (sortKey, error) {
	name := filepath.Base(path)
	cat := determineCategoryFromNames(name, name)
	switch cat {
	case CategoryShow:
		showName, _, season, episode, err := parseShowFromName(blacklist, name, name)
		if err != nil {
			return sortKey{}, err
		}
		return sortKey{tier: tierShow, title: showName, season: season, episode: episode, path: path}, nil
	default: // CategoryMovie
		title, year, err := parseMovieFromName(blacklist, name, name)
		if err != nil {
			return sortKey{}, err
		}
		if year != "" {
			title = fmt.Sprintf("%s (%s)", title, year)
		}
		return sortKey{tier: tierMovie, title: title, path: path}, nil
	}
}

// SortCandidates returns paths in media-aware order: movies first (alphabetical
// by title), then shows (alphabetical by name, then season, then episode).
// Non-media paths are silently dropped. Paths that appear to be media but whose
// names cannot be parsed are omitted from sorted and reported in errs. A non-nil
// err signals a fatal failure (e.g. context canceled); in that case both sorted
// and errs are nil.
func SortCandidates(ctx context.Context, proc Processor, paths []string) ([]string, []SortError, error) {
	return proc.SortCandidates(ctx, paths)
}

func (p *processorImpl) SortCandidates(ctx context.Context, paths []string) ([]string, []SortError, error) {
	if len(paths) == 0 {
		return nil, nil, nil
	}

	type keyed struct {
		path string
		key  sortKey
	}

	var items []keyed
	var errs []SortError

	for _, path := range paths {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}

		// Files with non-media extensions are silently skipped.
		// Directories (no extension) are always candidates.
		if ext := strings.ToLower(filepath.Ext(path)); ext != "" {
			if !isExtInSet(ext, p.mainExtSet) {
				continue
			}
		}

		key, err := parseSortKey(p.blacklist, path)
		if err != nil {
			var pse *ParseShowError
			var pme *ParseMovieError
			if errors.As(err, &pse) || errors.As(err, &pme) {
				errs = append(errs, SortError{Path: path, Err: err})
				continue
			}
			// Unexpected errors: include as fallback so Process() can report them.
			items = append(items, keyed{path: path, key: sortKey{tier: tierFallback, path: path}})
			continue
		}
		items = append(items, keyed{path: path, key: key})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].key.less(items[j].key)
	})

	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.path
	}
	return out, errs, nil
}
