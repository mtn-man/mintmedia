package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mtn-man/mintmedia/internal/console"
	"github.com/mtn-man/mintmedia/internal/processor"
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
	for _, res := range results {
		fmt.Println(console.ColorizePrefix(processDropCompactLine(res)))
	}
}

// PrintProcessDropNoFiles writes process-drop no-op output when no candidates are found.
func PrintProcessDropNoFiles() {
	fmt.Println("INFO     No files detected.")
}

// PrintProcessDropCandidates writes process-drop candidate discovery output.
func PrintProcessDropCandidates(count int) {
	noun := "files"
	if count == 1 {
		noun = "file"
	}
	fmt.Printf("INFO     Discovered %d %s.\n\n", count, noun)
}

// PrintProcessDropStatError writes a process-drop stat error to stderr.
func PrintProcessDropStatError(path string, err error) {
	fmt.Fprintln(os.Stderr, console.ColorizePrefix(fmt.Sprintf("ERROR    stat %s: %v", path, err)))
}

// PrintProcessDropItemError writes a process-drop item error to stderr.
func PrintProcessDropItemError(path string, err error) {
	fmt.Fprintln(os.Stderr, console.ColorizePrefix(fmt.Sprintf("ERROR    %s: %v", path, err)))
}

// PrintProcessDropResults writes process-drop results to stdout.
func PrintProcessDropResults(results []processor.Result, verbose bool) {
	if len(results) == 0 {
		return
	}
	if !verbose {
		for _, res := range results {
			fmt.Println(console.ColorizePrefix(processDropCompactLine(res)))
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
}

func processDropCompactLine(res processor.Result) string {
	if res.Applied {
		dest := strings.TrimSpace(res.Plan.DestMainPath)
		name := filepath.Base(strings.TrimSpace(res.Plan.MainSourcePath))
		if name == "." || name == string(os.PathSeparator) || strings.TrimSpace(name) == "" {
			name = "(unknown)"
		}
		if dest == "" {
			return fmt.Sprintf("SORTED   %s", name)
		}
		return fmt.Sprintf("SORTED   %s\n    ->   %s", name, dest)
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
	return fmt.Sprintf("SKIPPED  %s — %s", name, reason)
}

func processDropSummaryLine(s ProcessDropSummary) string {
	noun := "files"
	if s.Candidates == 1 {
		noun = "file"
	}
	elapsed := s.Elapsed.Round(time.Second)

	parts := []string{fmt.Sprintf("%d sorted", s.Applied)}
	if s.Skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", s.Skipped))
	}
	if s.Errors > 0 {
		errNoun := "error"
		if s.Errors != 1 {
			errNoun = "errors"
		}
		parts = append(parts, fmt.Sprintf("%d %s", s.Errors, errNoun))
	}

	return fmt.Sprintf(
		"INFO     %d %s — %s (%s)",
		s.Candidates,
		noun,
		strings.Join(parts, ", "),
		elapsed,
	)
}
