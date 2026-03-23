// internal/processor/processor.go
package processor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/Mtn-Man/mintmedia/internal/logging"
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
func (p *processorImpl) Process(ctx context.Context, req Request) ([]Result, error) {
	emit := func(res Result) {
		if req.OnResult == nil {
			return
		}
		req.OnResult(res)
	}

	plans, err := p.Plan(ctx, req)
	var partial *PartialPlanError
	if err != nil && !errors.As(err, &partial) {
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
			out := Result{
				Handled: true,
				Applied: false,
				Reason:  ErrInputMissing.Error(),
			}
			emit(out)
			return []Result{out}, nil
		}
		var pse *ParseShowError
		var pme *ParseMovieError
		if errors.As(err, &pse) || errors.As(err, &pme) {
			logInfoHistoryOnly(p, logging.EventProcessorInputSkippedParseError, logging.Fields{
				"input_path": req.InputPath,
				"reason":     err.Error(),
			})
			out := Result{
				Handled: true,
				Applied: false,
				Reason:  err.Error(),
			}
			emit(out)
			return []Result{out}, nil
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
			out := Result{
				Handled: true,
				Applied: false,
				Reason:  err.Error(),
			}
			emit(out)
			return []Result{out}, nil
		}
		return nil, err
	}

	res, err := applyWithEmitter(ctx, p, plans, emit)
	if err != nil {
		return res, err
	}

	for i := range res {
		if !res[i].Handled {
			res[i].Handled = true
		}
		if res[i].Reason == "" {
			res[i].Reason = "applied"
		}
	}

	if partial != nil && len(partial.Issues) > 0 {
		for _, issue := range partial.Issues {
			var pme *ParseMovieError
			if errors.As(issue.Err, &pme) {
				msg := fmt.Sprintf("movie pack skipped (unparseable filename): %s: %v", issue.Path, issue.Err)
				logWarn(p, logging.EventProcessorMoviePackSkipUnparseable, msg, issue.Err, logging.Fields{
					"input_path": issue.Path,
				})
			}
			logInfoHistoryOnly(p, logging.EventProcessorInputSkippedParseError, logging.Fields{
				"input_path": issue.Path,
				"reason":     issue.Err.Error(),
			})
			reason := issue.Err.Error()
			out := Result{
				Plan:    Plan{InputPath: issue.Path},
				Handled: true,
				Applied: false,
				Reason:  reason,
			}
			res = append(res, out)
			emit(out)
		}
	}
	return res, nil
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
