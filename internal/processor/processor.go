// internal/processor/processor.go
package processor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/mtn-man/mintmedia/internal/logging"
)

// Processor implementation notes (v1):
// - Plan() and Apply() are implemented in separate files (plan.go / apply.go).
// - Process() is the high-level orchestration entrypoint.
// - This file wires config + dependencies and prepares compiled helpers (regexes, extension sets).
// - Keep this file "boring": constructor + internal helpers only.

// New constructs a Processor with the provided dependencies. metaTagger may
// be nil, meaning metadata title tagging is disabled.
// cfg should already contain absolute, resolved paths.
func New(cfg Config, xfer Transferer, metaTagger MetadataTagger, logger logging.Logger) (Processor, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	p := &processorImpl{
		cfg:        cfg,
		xfer:       xfer,
		metaTagger: metaTagger,
		log:        logging.NewEmitter(logger, "processor"),
	}

	// Normalize extension lists for predictable comparisons.
	p.mainExtSet = make(map[string]struct{}, len(cfg.MainMediaExtensions))
	for _, ext := range cfg.MainMediaExtensions {
		n := normalizeExt(ext)
		if n == "" {
			continue
		}
		p.mainExtSet[n] = struct{}{}
	}

	p.assocExtSet = make(map[string]struct{}, len(cfg.AssociatedFileExtensions))
	for _, ext := range cfg.AssociatedFileExtensions {
		n := normalizeExt(ext)
		if n == "" {
			continue
		}
		p.assocExtSet[n] = struct{}{}
	}

	// Compile naming blacklist patterns (case-insensitive).
	if len(cfg.MediaTagBlacklist) > 0 {
		p.blacklist = make([]*regexp.Regexp, 0, len(cfg.MediaTagBlacklist))
		for _, pat := range cfg.MediaTagBlacklist {
			pat = strings.TrimSpace(pat)
			if pat == "" {
				continue
			}
			re, err := compileBlacklistPattern(pat)
			if err != nil {
				return nil, fmt.Errorf(
					"invalid naming.media_tag_blacklist pattern %q: %w",
					pat,
					err,
				)
			}
			p.blacklist = append(p.blacklist, re)
		}
	}

	return p, nil
}

// compileBlacklistPattern compiles a naming.media_tag_blacklist pattern
// (built-in or user-supplied) case-insensitively, anchored to whole-word
// matches. The pattern is wrapped in a non-capturing group before the \b
// anchors so a pattern containing alternation (e.g. "foo|bar") gets anchored
// as a whole rather than per-side. Without the anchors, a bare substring
// pattern like "proper" would also match inside an unrelated word like
// "Property", corrupting titles that have nothing to do with a PROPER
// release tag.
func compileBlacklistPattern(pat string) (*regexp.Regexp, error) {
	return regexp.Compile(`(?i)\b(?:` + pat + `)\b`)
}

type processorImpl struct {
	cfg        Config
	xfer       Transferer
	metaTagger MetadataTagger
	log        logging.Emitter

	// Prepared helpers
	mainExtSet  map[string]struct{}
	assocExtSet map[string]struct{}
	blacklist   []*regexp.Regexp

	// skipWarningsMu guards skipWarningsSeen, the set of unparseable-file
	// paths already surfaced via a PartialPlanError issue. Process() may be
	// invoked repeatedly for the same path over the processor's lifetime
	// (e.g. the daemon rescanning a pack folder that's still receiving
	// files), so this dedupes the WARNING/SKIPPED lines to once per path.
	skipWarningsMu   sync.Mutex
	skipWarningsSeen map[string]struct{}
}

// firstSkipWarning reports whether path has not yet been warned about as an
// unparseable-file skip, marking it seen as a side effect. Safe for
// concurrent use even though Process is currently only driven by a single
// worker goroutine.
func (p *processorImpl) firstSkipWarning(path string) bool {
	p.skipWarningsMu.Lock()
	defer p.skipWarningsMu.Unlock()
	if p.skipWarningsSeen == nil {
		p.skipWarningsSeen = make(map[string]struct{})
	}
	if _, ok := p.skipWarningsSeen[path]; ok {
		return false
	}
	p.skipWarningsSeen[path] = struct{}{}
	return true
}

// Plan computes deterministic plan(s) for an input path.
// Implementation lives in plan.go.
func (p *processorImpl) Plan(ctx context.Context, req Request) ([]Plan, error) {
	return plan(ctx, p, req)
}

// Apply executes the plan(s) (moves/history).
// Implementation lives in apply.go.
func (p *processorImpl) Apply(ctx context.Context, plans []Plan) ([]Result, error) {
	return apply(ctx, p, plans)
}

// Process is the high-level orchestration entrypoint.
// Policy (v1):
// - Non-media files and directories with no main media are treated as handled and ignored.
// - All other errors are returned to the caller.
// Results are delivered via req.OnResult as they are produced.
func (p *processorImpl) Process(ctx context.Context, req Request) error {
	emit := func(res Result) {
		if req.OnResult != nil {
			req.OnResult(res)
		}
	}

	plans, err := p.Plan(ctx, req)
	var partial *PartialPlanError
	var destErr *DestinationUnavailableError
	isPartial := errors.As(err, &partial)
	isDestUnavailable := errors.As(err, &destErr)
	if err != nil && !isPartial && !isDestUnavailable {
		var noMediaErr *NoMainMediaFoundError
		if errors.As(err, &noMediaErr) && noMediaErr.DepthHit {
			logHistoryWarn(p, logging.EventProcessorInputMaxDepthNoMedia, nil, logging.Fields{
				"input_path": noMediaErr.Path,
				"depth":      noMediaErr.MaxDepth,
			})
		}
		if errors.Is(err, os.ErrNotExist) {
			logHistoryInfo(p, logging.EventProcessorInputSkippedInputMissing, logging.Fields{
				"input_path": req.InputPath,
			})
			emit(Result{Handled: true, Applied: false, Reason: ErrInputMissing.Error()})
			return nil
		}
		var pse *ParseShowError
		var pme *ParseMovieError
		if errors.As(err, &pse) || errors.As(err, &pme) {
			logHistoryInfo(p, logging.EventProcessorInputSkippedParseError, logging.Fields{
				"input_path": req.InputPath,
				"reason":     err.Error(),
			})
			emit(Result{Handled: true, Applied: false, Reason: err.Error(), NeedsReview: true})
			return nil
		}
		if errors.Is(err, ErrNotMedia) || errors.Is(err, ErrNoMainMediaFound) || errors.Is(err, ErrAmbiguousShow) {
			switch {
			case errors.Is(err, ErrNotMedia):
				logHistoryInfo(p, logging.EventProcessorInputSkippedNotMedia, logging.Fields{
					"input_path": req.InputPath,
				})
			case errors.Is(err, ErrNoMainMediaFound):
				logHistoryInfo(p, logging.EventProcessorInputSkippedNoMainMedia, logging.Fields{
					"input_path": req.InputPath,
				})
			default:
				logHistoryInfo(p, logging.EventProcessorInputSkippedParseError, logging.Fields{
					"input_path": req.InputPath,
					"reason":     err.Error(),
				})
			}
			// ErrAmbiguousShow is a genuine "come look" -- mintmedia refused to
			// guess a folder. ErrNotMedia / ErrNoMainMediaFound are non-events
			// (IsSuppressedResult drops them before any count anyway).
			emit(Result{Handled: true, Applied: false, Reason: err.Error(), NeedsReview: errors.Is(err, ErrAmbiguousShow)})
			return nil
		}
		return err
	}

	if _, applyErr := applyWithEmitter(ctx, p, plans, emit); applyErr != nil {
		return applyErr
	}

	if partial != nil && len(partial.Issues) > 0 {
		for _, issue := range partial.Issues {
			// The file is intentionally left in place for manual review, so
			// it keeps resurfacing in every future Plan of this folder (e.g.
			// each time a sibling file elsewhere in the same pack triggers a
			// rescan) until a human moves or removes it. Warn about it once
			// per processor lifetime instead of repeating the same
			// WARNING/SKIPPED lines on every rescan.
			if !p.firstSkipWarning(issue.Path) {
				continue
			}
			var pme *ParseMovieError
			var pse *ParseShowError
			switch {
			case errors.As(issue.Err, &pme):
				msg := fmt.Sprintf("WARNING  movie pack skipped (unparseable filename): %s: %v", issue.Path, issue.Err)
				logWarn(p, logging.EventProcessorMoviePackSkipUnparseable, msg, issue.Err, logging.Fields{
					"input_path": issue.Path,
				})
			case errors.As(issue.Err, &pse):
				// Most often hit when a folder hint (e.g. a "Season N" name)
				// forces every file inside it to be planned as a show, but
				// this particular file doesn't parse as an episode at all --
				// e.g. a movie that happens to sit in a season folder. Rather
				// than guess whether it's really a movie, this is surfaced as
				// a visible warning for human review instead of a silent skip.
				msg := fmt.Sprintf("WARNING  show file skipped (doesn't parse as an episode): %s: %v", issue.Path, issue.Err)
				logWarn(p, logging.EventProcessorShowFileSkipUnparseable, msg, issue.Err, logging.Fields{
					"input_path": issue.Path,
				})
			}
			logHistoryInfo(p, logging.EventProcessorInputSkippedParseError, logging.Fields{
				"input_path": issue.Path,
				"reason":     issue.Err.Error(),
			})
			emit(Result{
				Plan:        Plan{InputPath: issue.Path},
				Handled:     true,
				Applied:     false,
				Reason:      issue.Err.Error(),
				NeedsReview: true,
			})
		}
	}

	if isDestUnavailable {
		// Whatever plans were already computed for this input just applied
		// successfully above; destErr still propagates so the caller (the
		// daemon) knows this destination is unavailable and defers the rest
		// of this input for retry once it recovers.
		return destErr
	}
	return nil
}

// --- Internal helpers -------------------------------------------------------

func validateConfig(cfg Config) error {
	var missing []string

	if strings.TrimSpace(cfg.DropFolder) == "" {
		missing = append(missing, "DropFolder")
	}
	if strings.TrimSpace(cfg.MoviesDir) == "" {
		missing = append(missing, "MoviesDir")
	}
	if strings.TrimSpace(cfg.ShowsDir) == "" {
		missing = append(missing, "ShowsDir")
	}
	if len(cfg.MainMediaExtensions) == 0 {
		missing = append(missing, "MainMediaExtensions")
	}

	if len(missing) > 0 {
		return fmt.Errorf(
			"processor config missing/empty: %s",
			strings.Join(missing, ", "),
		)
	}

	return nil
}

func normalizeExt(ext string) string {
	ext = strings.TrimSpace(ext)
	if ext == "" {
		return ""
	}
	ext = strings.ToLower(ext)
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return ext
}

func isExtInSet(ext string, set map[string]struct{}) bool {
	_, ok := set[strings.ToLower(ext)]
	return ok
}

// ProcessEach calls proc.Process with onResult wired as the result callback.
// The caller must not set req.OnResult.
// Returns the error from Process.
func ProcessEach(ctx context.Context, proc Processor, req Request, onResult func(Result)) error {
	req.OnResult = onResult
	return proc.Process(ctx, req)
}
