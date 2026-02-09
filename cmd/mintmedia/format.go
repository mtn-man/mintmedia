package main

import (
	"fmt"
	"os"

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

// PrintProcessDropStartup writes process-drop startup output.
func PrintProcessDropStartup(configPath string, verbose bool) {
	if verbose {
		return
	}
	fmt.Println("Mintmedia starting (process-drop)...")
	fmt.Printf("Config file: %s\n\n", configPath)
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
func PrintProcessDropResults(results []processor.Result) {
	if len(results) == 0 {
		return
	}
	PrintResults(results)
}

// PrintProcessDropSummary writes process-drop completion summary to stderr when needed.
func PrintProcessDropSummary(errCount int) {
	if errCount <= 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "process-drop completed with %d error(s).\n", errCount)
}
