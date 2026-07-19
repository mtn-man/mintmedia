// internal/processor/processor.go
package processor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/mtn-man/mintmedia/internal/logging"
)

// Processor implementation notes (v1):
// - Plan() and Apply() are implemented in separate files (plan.go / apply.go).
// - Process() is the high-level orchestration entrypoint.
// - This file wires config + dependencies and prepares compiled helpers (regexes, extension sets).
// - Keep this file "boring": constructor + internal helpers only.

// New constructs a Processor with the provided dependencies.
// cfg should already contain absolute, resolved paths.
func New(cfg Config, xfer Transferer, logger logging.Logger) (Processor, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	p := &processorImpl{
		cfg:    cfg,
		xfer:   xfer,
		logger: logger,
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
			re, err := regexp.Compile("(?i)" + pat)
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

type processorImpl struct {
	cfg    Config
	xfer   Transferer
	logger logging.Logger

	// Prepared helpers
	mainExtSet  map[string]struct{}
	assocExtSet map[string]struct{}
	blacklist   []*regexp.Regexp
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
			logWarnHistoryOnly(p, logging.EventProcessorInputMaxDepthNoMedia, nil, logging.Fields{
				"input_path": noMediaErr.Path,
				"depth":      noMediaErr.MaxDepth,
			})
		}
		if errors.Is(err, os.ErrNotExist) {
			logInfoHistoryOnly(p, logging.EventProcessorInputSkippedInputMissing, logging.Fields{
				"input_path": req.InputPath,
			})
			emit(Result{Handled: true, Applied: false, Reason: ErrInputMissing.Error()})
			return nil
		}
		var pse *ParseShowError
		var pme *ParseMovieError
		if errors.As(err, &pse) || errors.As(err, &pme) {
			logInfoHistoryOnly(p, logging.EventProcessorInputSkippedParseError, logging.Fields{
				"input_path": req.InputPath,
				"reason":     err.Error(),
			})
			emit(Result{Handled: true, Applied: false, Reason: err.Error()})
			return nil
		}
		if errors.Is(err, ErrNotMedia) || errors.Is(err, ErrNoMainMediaFound) || errors.Is(err, ErrAmbiguousShow) {
			switch {
			case errors.Is(err, ErrNotMedia):
				logInfoHistoryOnly(p, logging.EventProcessorInputSkippedNotMedia, logging.Fields{
					"input_path": req.InputPath,
				})
			case errors.Is(err, ErrNoMainMediaFound):
				logInfoHistoryOnly(p, logging.EventProcessorInputSkippedNoMainMedia, logging.Fields{
					"input_path": req.InputPath,
				})
			default:
				logInfoHistoryOnly(p, logging.EventProcessorInputSkippedParseError, logging.Fields{
					"input_path": req.InputPath,
					"reason":     err.Error(),
				})
			}
			emit(Result{Handled: true, Applied: false, Reason: err.Error()})
			return nil
		}
		return err
	}

	if _, applyErr := applyWithEmitter(ctx, p, plans, emit); applyErr != nil {
		return applyErr
	}

	if partial != nil && len(partial.Issues) > 0 {
		for _, issue := range partial.Issues {
			var pme *ParseMovieError
			var pse *ParseShowError
			switch {
			case errors.As(issue.Err, &pme):
				msg := fmt.Sprintf("movie pack skipped (unparseable filename): %s: %v", issue.Path, issue.Err)
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
				msg := fmt.Sprintf("show file skipped (doesn't parse as an episode): %s: %v", issue.Path, issue.Err)
				logWarn(p, logging.EventProcessorShowFileSkipUnparseable, msg, issue.Err, logging.Fields{
					"input_path": issue.Path,
				})
			}
			logInfoHistoryOnly(p, logging.EventProcessorInputSkippedParseError, logging.Fields{
				"input_path": issue.Path,
				"reason":     issue.Err.Error(),
			})
			emit(Result{
				Plan:    Plan{InputPath: issue.Path},
				Handled: true,
				Applied: false,
				Reason:  issue.Err.Error(),
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
