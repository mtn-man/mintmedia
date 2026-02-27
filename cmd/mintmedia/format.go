package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Mtn-Man/mintmedia/internal/processor"
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
}

// PrintResults writes result output to stdout.
func PrintResults(results []processor.Result) {
	if len(results) == 0 {
		fmt.Println("--- RESULT ---")
		fmt.Println("No results.")
		return
	}
	for i, res := range results {
		if len(results) > 1 {
			fmt.Printf("\n--- RESULT %d/%d ---\n", i+1, len(results))
			printResultBody(res)
			continue
		}
		printResult(res)
	}
}

func printResult(res processor.Result) {
	fmt.Println("--- RESULT ---")
	printResultBody(res)
}

func printResultBody(res processor.Result) {
	fmt.Printf("Handled: %t\n", res.Handled)
	fmt.Printf("Applied: %t\n", res.Applied)
	if res.Reason != "" {
		fmt.Printf("Reason:  %s\n", res.Reason)
	}
	if res.Applied {
		fmt.Printf("Dest:    %s\n", res.Plan.DestMainPath)
	}
}

// PrintProcessDropNoFiles writes process-drop no-op output when no candidates are found.
func PrintProcessDropNoFiles() {
	fmt.Println("No files detected, exiting...")
}

// PrintProcessDropCandidates writes process-drop candidate discovery output.
func PrintProcessDropCandidates(count int, verbose bool) {
	if verbose {
		return
	}
	fmt.Printf("Discovered %d candidate(s).\n\n", count)
}

// PrintProcessDropStatError writes a process-drop stat error to stderr.
func PrintProcessDropStatError(path string, err error) {
	fmt.Fprintf(os.Stderr, "process-drop: stat %s: %v\n", path, err)
}

// PrintProcessDropItemError writes a process-drop item error to stderr.
func PrintProcessDropItemError(path string, err error) {
	fmt.Fprintf(os.Stderr, "process-drop: %s: %v\n", path, err)
}

// PrintProcessDropResults writes process-drop results to stdout.
func PrintProcessDropResults(results []processor.Result, verbose bool) {
	if len(results) == 0 {
		return
	}
	if !verbose {
		for _, res := range results {
			fmt.Println(processDropCompactLine(res))
		}
		return
	}
	PrintResults(results)
}

// ProcessDropSummary captures final process-drop run stats for compact rendering.
type ProcessDropSummary struct {
	Candidates int
	Results    int
	Applied    int
	Skipped    int
	Errors     int
	Elapsed    time.Duration
}

// PrintProcessDropSummary writes process-drop completion summary.
func PrintProcessDropSummary(s ProcessDropSummary) {
	fmt.Println()
	fmt.Println(processDropSummaryLine(s))
	if s.Errors > 0 {
		fmt.Fprintf(os.Stderr, "process-drop completed with %d error(s).\n", s.Errors)
	}
}

func processDropCompactLine(res processor.Result) string {
	if res.Applied {
		dest := strings.TrimSpace(res.Plan.DestMainPath)
		name := filepath.Base(dest)
		if name == "." || name == string(os.PathSeparator) || strings.TrimSpace(name) == "" {
			name = "(unknown)"
		}
		if dest == "" {
			return fmt.Sprintf("OK   %s", name)
		}
		return fmt.Sprintf("OK   %s -> %s", name, dest)
	}

	ref := strings.TrimSpace(res.Plan.InputPath)
	if ref == "" {
		ref = strings.TrimSpace(res.Plan.MainSourcePath)
	}
	name := filepath.Base(ref)
	if name == "." || name == string(os.PathSeparator) || strings.TrimSpace(name) == "" {
		name = "(unknown)"
	}
	reason := strings.TrimSpace(res.Reason)
	if reason == "" {
		reason = "not applied"
	}
	return fmt.Sprintf("SKIP %s (%s)", name, reason)
}

func processDropSummaryLine(s ProcessDropSummary) string {
	elapsed := s.Elapsed.Round(time.Second)
	return fmt.Sprintf(
		"SUMMARY candidates=%d results=%d applied=%d skipped=%d errors=%d elapsed=%s",
		s.Candidates,
		s.Results,
		s.Applied,
		s.Skipped,
		s.Errors,
		elapsed,
	)
}
