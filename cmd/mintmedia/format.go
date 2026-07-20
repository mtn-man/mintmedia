package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mtn-man/mintmedia/internal/console"
	"github.com/mtn-man/mintmedia/internal/processor"
	"github.com/mtn-man/mintmedia/internal/resultformat"
)

// --- Printing helpers -------------------------------------------------------

// PrintPlans writes plan output to stdout.
func PrintPlans(plans []processor.Plan) {
	if len(plans) == 0 {
		fmt.Println("--- PLAN ---")
		fmt.Println("No plans.")
		return
	}
	for i, pl := range plans {
		if len(plans) > 1 {
			fmt.Printf("\n--- PLAN %d/%d ---\n", i+1, len(plans))
			printPlanBody(pl)
			continue
		}
		printPlan(pl)
	}
}

func printPlan(pl processor.Plan) {
	fmt.Println("--- PLAN ---")
	printPlanBody(pl)
}

func printPlanBody(pl processor.Plan) {
	fmt.Printf("Input:        %s\n", pl.InputPath)
	fmt.Printf("Category:     %s\n", pl.Category)
	fmt.Printf("MainSource:   %s\n", pl.MainSourcePath)
	fmt.Printf("DestMain:     %s\n", pl.DestMainPath)
	fmt.Printf("DestDir:      %s\n", pl.DestDir)
	fmt.Printf("DestRadix:    %s\n", pl.DestRadix)

	if pl.Category == processor.CategoryMovie {
		fmt.Printf("MovieTitle:   %s\n", pl.MovieTitle)
	} else {
		fmt.Printf("ShowName:     %s\n", pl.ShowName)
		fmt.Printf("ShowYear:     %s\n", pl.ShowYear)
		fmt.Printf("Season/Ep:    %d/%d\n", pl.Season, pl.Episode)
	}

	fmt.Printf("Associated:   %d\n", len(pl.Associated))
	for _, mv := range pl.Associated {
		fmt.Printf("  - %s -> %s\n", mv.Source, mv.Dest)
	}

	if pl.Duplicate {
		if pl.DuplicateMatchPath != "" {
			fmt.Printf("Duplicate:    yes (matches existing library entry: %s)\n", pl.DuplicateMatchPath)
		} else {
			fmt.Println("Duplicate:    yes (already exists at DestMain)")
		}
	}
}

// PrintResults writes result output to stdout.
func PrintResults(results []processor.Result) {
	for _, res := range results {
		fmt.Println(console.ColorizePrefixOut(processDropCompactLine(res, 0)))
	}
}

// PrintProcessDropNoFiles writes process-drop no-op output when no candidates are found.
func PrintProcessDropNoFiles() {
	fmt.Println("INFO     No files detected.")
}

// PrintProcessDropCandidates writes process-drop candidate discovery output.
// count is a cheap estimate (extension-only, no naming/hint resolution): the
// real, fully-planned count may end up slightly lower if some files turn out
// unparseable. "Detected" matches the wording PrintProcessDropNoFiles already
// uses for the zero-file case.
func PrintProcessDropCandidates(count int) {
	noun := resultformat.Pluralize(count, "file", "files")
	fmt.Printf("INFO     Detected %d %s.\n\n", count, noun)
}

// PrintFatalError writes a labeled, colorized error line to stderr, matching
// the ERROR/WARNING console voice used elsewhere, instead of a bare
// err.Error() dump. Used for one-shot CLI failures that abort the process.
func PrintFatalError(err error) {
	fmt.Fprintln(os.Stderr, console.ColorizePrefixErr(fmt.Sprintf("ERROR    %v", err)))
}

// PrintProcessDropStatError writes a process-drop stat error to stderr.
func PrintProcessDropStatError(path string, err error) {
	fmt.Fprintln(os.Stderr, console.ColorizePrefixErr(fmt.Sprintf("ERROR    stat %s: %v", path, err)))
}

// PrintProcessDropSortError writes a process-drop sort parse error to stderr.
func PrintProcessDropSortError(path string, err error) {
	fmt.Fprintln(os.Stderr, console.ColorizePrefixErr(fmt.Sprintf("ERROR    cannot sort %s: %v", path, err)))
}

// PrintProcessDropDestinationError writes a process-drop destination unavailable error to stderr.
func PrintProcessDropDestinationError(dir string) {
	fmt.Fprintln(os.Stderr, console.ColorizePrefixErr(
		fmt.Sprintf("ERROR    destination unavailable: %s (directory missing or not yet mounted)", dir)))
}

// PrintProcessDropItemError writes a process-drop item error to stderr.
func PrintProcessDropItemError(path string, err error, dur time.Duration) {
	fmt.Fprintln(os.Stderr, console.ColorizePrefixErr(resultformat.ErrorLine(path, err, dur)))
}

// PrintProcessDropResults writes process-drop results to stdout.
func PrintProcessDropResults(results []processor.Result, verbose bool, dur time.Duration) {
	if len(results) == 0 {
		return
	}
	if !verbose {
		for _, res := range results {
			fmt.Println(console.ColorizePrefixOut(processDropCompactLine(res, dur)))
		}
		return
	}
	PrintResults(results)
}

// ProcessDropSummary captures final process-drop run stats for compact rendering.
type ProcessDropSummary struct {
	Results int
	Applied int
	Skipped int
	Errors  int
	Elapsed time.Duration
}

// PrintProcessDropSummary writes process-drop completion summary.
func PrintProcessDropSummary(s ProcessDropSummary) {
	fmt.Println()
	fmt.Println(processDropSummaryLine(s))
}

func processDropCompactLine(res processor.Result, dur time.Duration) string {
	ref := res.Plan.MainSourcePath
	if !res.Applied {
		if in := strings.TrimSpace(res.Plan.InputPath); in != "" {
			ref = in
		}
	}
	return resultformat.CompactLine(res, resultformat.CleanName(ref), dur)
}

func processDropSummaryLine(s ProcessDropSummary) string {
	total := s.Applied + s.Skipped + s.Errors
	noun := resultformat.Pluralize(total, "file", "files")

	parts := []string{fmt.Sprintf("%d sorted", s.Applied)}
	if s.Skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", s.Skipped))
	}
	if s.Errors > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", s.Errors, resultformat.Pluralize(s.Errors, "error", "errors")))
	}

	// Unlike the per-file duration suffix (resultformat.durationSuffix), the
	// batch summary always shows an elapsed time, even sub-second -- a run
	// that sorted 76 files in 400ms is worth reporting as fast, not silent.
	// Precision scales with magnitude so the number stays meaningful instead
	// of noisy: whole seconds at 1s+, one decimal from 100ms-1s, three
	// decimals below 100ms (where a single decimal would round distinct fast
	// runs down to the same "0.0s").
	var durSuffix string
	switch {
	case s.Elapsed >= time.Second:
		durSuffix = fmt.Sprintf(" (%s)", s.Elapsed.Round(time.Second))
	case s.Elapsed >= 100*time.Millisecond:
		durSuffix = fmt.Sprintf(" (%.1fs)", s.Elapsed.Seconds())
	default:
		durSuffix = fmt.Sprintf(" (%.3fs)", s.Elapsed.Seconds())
	}
	return fmt.Sprintf(
		"INFO     %d %s -- %s%s",
		total,
		noun,
		strings.Join(parts, ", "),
		durSuffix,
	)
}
