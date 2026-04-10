// internal/processor/sort.go
package processor

import (
	"context"
	"errors"
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

// sortKeyForPath calls Plan() for path and derives its sort key.
// Returns a non-nil error for any condition that should influence the caller's
// decision about whether to include the path (parse errors, not-media, etc.).
//
// Plan() will be called a second time when the path is actually processed.
// The duplication is acceptable: Plan() is read-only (no filesystem writes),
// cheap relative to the file moves that follow, and keeping sort logic
// self-contained avoids coupling the sort key representation to Plan internals.
func sortKeyForPath(ctx context.Context, proc Processor, path string) (sortKey, error) {
	plans, err := proc.Plan(ctx, Request{InputPath: path})

	var partial *PartialPlanError
	if err != nil && !errors.As(err, &partial) {
		return sortKey{}, err
	}

	if len(plans) == 0 {
		if partial != nil && len(partial.Issues) > 0 {
			return sortKey{}, partial.Issues[0].Err
		}
		return sortKey{tier: tierFallback, path: path}, nil
	}

	// Use plans[0]: plan.go sorts mainPaths via sort.Strings, so for a season
	// pack directory plans[0] is the lexicographically first episode.
	pl := plans[0]
	switch pl.Category {
	case CategoryMovie:
		return sortKey{tier: tierMovie, title: pl.MovieTitle, path: path}, nil
	case CategoryShow:
		return sortKey{tier: tierShow, title: pl.ShowName, season: pl.Season, episode: pl.Episode, path: path}, nil
	default:
		return sortKey{tier: tierFallback, path: path}, nil
	}
}

// SortCandidates returns paths in media-aware order: movies first (alphabetical
// by title), then shows (alphabetical by name, then season, then episode).
// Non-media paths are silently dropped. Paths that appear to be media but whose
// names cannot be parsed are omitted from sorted and reported in errs. A non-nil
// err signals a fatal failure (e.g. context canceled); in that case both sorted
// and errs are nil.
func SortCandidates(ctx context.Context, proc Processor, paths []string) ([]string, []SortError, error) {
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

		key, err := sortKeyForPath(ctx, proc, path)
		if err != nil {
			var pse *ParseShowError
			var pme *ParseMovieError
			if errors.As(err, &pse) || errors.As(err, &pme) {
				errs = append(errs, SortError{Path: path, Err: err})
				continue
			}
			if errors.Is(err, ErrNotMedia) || errors.Is(err, ErrNoMainMediaFound) {
				continue // not a media file; silently skip
			}
			// Unexpected errors (ambiguous show, I/O, etc.): pass through so
			// Process() can report them with full context.
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
