// Mintmedia is a macOS drop-folder daemon and CLI for organizing media into Movies/Shows libraries.
// BETA v1.0: feature-complete for personal use; behavior may change.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/Mtn-Man/mintmedia/internal/clipboard"
	"github.com/Mtn-Man/mintmedia/internal/config"
	"github.com/Mtn-Man/mintmedia/internal/daemon"
	"github.com/Mtn-Man/mintmedia/internal/processor"
	"github.com/Mtn-Man/mintmedia/internal/state"
	"github.com/Mtn-Man/mintmedia/internal/transfer"
	"github.com/Mtn-Man/mintmedia/internal/transmission"
	"github.com/Mtn-Man/mintmedia/internal/watch"
)

const (
	exitError = 1
	exitUsage = 2

	defaultSoundInput      = "/System/Library/Sounds/Funk.aiff"
	defaultSoundDone       = "/System/Library/Sounds/Glass.aiff"
	defaultMagnetTimeout   = 10 * time.Second
	defaultCleanupCooldown = 2 * time.Minute

	defaultReportEvery   = 250 * time.Millisecond
	defaultProgressEvery = 4 * time.Second
)

func main() {
	configPath := flag.String(
		"config",
		"",
		"Path to config.toml (default: ~/.config/mintmedia/config.toml)",
	)

	// One-shot processor harness flags
	planPath := flag.String("plan", "", "Compute and print the processing plan for a path (no changes)")
	applyPath := flag.String("apply", "", "Plan and apply changes for a path (filesystem writes)")
	processPath := flag.String("process", "", "Process a path with policy (ignore non-media/no-media dirs)")
	processDropShort := flag.Bool("p", false, "Process all paths currently in the drop folder (one-shot)")
	processDropLong := flag.Bool("process-drop", false, "Process all paths currently in the drop folder (one-shot)")

	verbose := flag.Bool("verbose", false, "Verbose startup output (prints config summary in daemon mode)")

	flag.Parse()

	cfg, resolved, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(exitError)
	}

	// Determine whether we're in one-shot mode.
	ops := 0
	plan := strings.TrimSpace(*planPath)
	apply := strings.TrimSpace(*applyPath)
	process := strings.TrimSpace(*processPath)
	processDrop := *processDropShort || *processDropLong
	if plan != "" {
		ops++
	}
	if apply != "" {
		ops++
	}
	if process != "" {
		ops++
	}
	if processDrop {
		ops++
	}
	if ops > 1 {
		fmt.Fprintln(os.Stderr, "Use only one of --plan, --apply, --process, or --process-drop at a time.")
		os.Exit(exitUsage)
	}

	if ops > 0 || *verbose {
		printConfigSummary(cfg, resolved)
	}
	if ops == 0 && !*verbose {
		fmt.Println("Mintmedia starting...")
	fmt.Printf("Config file: %s\n\n", resolved.ConfigPathAbs)
	}

	if !cfg.Features.EnableProcessing {
		if ops > 0 {
			fmt.Fprintln(os.Stderr, "Go-native processor requested but features.enable_processing=false in TOML.")
			os.Exit(exitUsage)
		}
		fmt.Println("Config smoke test complete.")
		return
	}

	hist, err := state.NewFileHistoryWriter(resolved.HistoryFileAbs, state.HistoryOptions{Fsync: false})
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(exitError)
	}

	proc, err := newGoProcessor(resolved, hist)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(exitError)
	}

	// One-shot modes short-circuit daemon.
	ctx := context.Background()
	if plan != "" {
		plans, err := proc.Plan(ctx, processor.Request{InputPath: plan})
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(exitError)
		}
		PrintPlans(plans)
		return
	}

	if apply != "" {
		plans, err := proc.Plan(ctx, processor.Request{InputPath: apply})
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(exitError)
		}
		PrintPlans(plans)

		fmt.Println("\n--- APPLY ---")
		res, err := proc.Apply(ctx, plans)
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(exitError)
		}
		PrintResults(res)
		return
	}

	if process != "" {
		res, err := proc.Process(ctx, processor.Request{InputPath: process})
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(exitError)
		}
		PrintResults(res)
		return
	}

	if processDrop {
		errCount := processDropFolder(ctx, proc, resolved.DropFolderAbs)
		if errCount > 0 {
			os.Exit(exitError)
		}
		return
	}

	// ---- Default behavior: run daemon --------------------------------------
	lockPath := filepath.Join(resolved.StateDirAbs, "mintmedia.lock")
	releaseLock, err := state.AcquireLock(lockPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(exitError)
	}
	defer func() { _ = releaseLock() }()

	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Watcher
	w, err := watch.NewDropFolderWatcher(resolved.DropFolderAbs, resolved.DropSettleDuration)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(exitError)
	}

	torrentEnabled := cfg.Features.EnableTorrentAutomation && cfg.Torrent.Enabled

	// Clipboard poller (disabled unless torrent automation is enabled)
	var poller *clipboard.Poller
	if torrentEnabled && cfg.Clipboard.Enabled {
		poller, err = clipboard.NewPoller(resolved.ClipboardPollInterval)
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(exitError)
		}
	}
	// poller has no Stop; it exits on ctx cancellation.

	// Optional Transmission client
	var tx *transmission.Client
	if torrentEnabled {
		tx = &transmission.Client{
			RemotePath: resolved.TransmissionRemoteAbs,
			Host:       cfg.Torrent.Host,
			Auth:       cfg.Torrent.Auth,
		}
	}

	d := &daemon.Daemon{
		Watcher: w,
		Poller:  poller,
		Proc:    proc,
		Tx:      tx,
		History: hist,

		TransmissionHost: cfg.Torrent.Host,

		MaxConcurrent: cfg.System.MaxConcurrentProcessors,

		MoviesDir: resolved.DestDirMoviesAbs,
		ShowsDir:  resolved.DestDirShowsAbs,

		DeferDestinationChecks: cfg.System.DeferDestinationChecks,

		// Sounds (best-effort; empty disables)
		SoundInput: defaultSoundInput,
		SoundDone:  defaultSoundDone,

		MagnetTimeout: defaultMagnetTimeout,

		VerboseMagnets:               false,
		AutoCleanupCompletedTorrents: true,
		CleanupCooldown:              defaultCleanupCooldown,
	}

	if err := d.Run(runCtx); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(exitError)
	}
}

// --- Go-native processor wiring --------------------------------------------

func newGoProcessor(res *config.Resolved, hist state.HistoryWriter) (processor.Processor, error) {
	pcfg := processor.Config{
		DropFolder:  res.DropFolderAbs,
		MoviesDir:   res.DestDirMoviesAbs,
		ShowsDir:    res.DestDirShowsAbs,
		ErrorDir:    res.ErrorDirAbs,
		HistoryFile: res.HistoryFileAbs,

		MainMediaExtensions:      res.MainMediaExtensions,
		AssociatedFileExtensions: res.AssociatedFileExtensions,
		MediaTagBlacklist:        res.MediaTagBlacklist,
	}

	xfer := transfer.NewRenameOrCopy(transfer.Options{
		// Legacy string progress is disabled in favor of the structured reporter.
		Progress: nil,

		// Keep PrintDone enabled so we get a completion line.
		PrintDone: true,

		// Structured reporter enables the progress bar for large/slow copies.
		Reporter: transfer.NewTerminalReporter(os.Stdout, transfer.ReportOptions{
			EnableBar: true,
			EnableETA: true,
		}),
		ReportEvery: defaultReportEvery,

		// Retain the legacy interval value for any remaining string progress paths.
		ProgressEvery: defaultProgressEvery,
	})

	return processor.New(pcfg, xfer, nil, hist)
}

func printConfigSummary(cfg *config.Config, resolved *config.Resolved) {
	fmt.Println("Mintmedia config loaded successfully.")
	fmt.Printf("Config file:  %s\n\n", resolved.ConfigPathAbs)

	fmt.Println("Resolved paths:")
	fmt.Printf("  Drop folder:        %s\n", resolved.DropFolderAbs)
	fmt.Printf("  State dir:          %s\n", resolved.StateDirAbs)
	fmt.Printf("  Error dir:          %s\n", resolved.ErrorDirAbs)
	fmt.Printf("  Movies dir:         %s\n", resolved.DestDirMoviesAbs)
	fmt.Printf("  Shows dir:          %s\n", resolved.DestDirShowsAbs)
	fmt.Println()

	fmt.Println("Runtime settings:")
	fmt.Printf("  Max processors:     %d\n", cfg.System.MaxConcurrentProcessors)
	fmt.Printf("  Drop settle:        %s\n", resolved.DropSettleDuration)
	fmt.Printf("  Clipboard poll:     %s\n", resolved.ClipboardPollInterval)
	fmt.Println()

	if cfg.Features.EnableProcessing {
		fmt.Println("Processing:")
		fmt.Printf("  History file:       %s\n", resolved.HistoryFileAbs)
		fmt.Printf("  Main extensions:    %d\n", len(resolved.MainMediaExtensions))
		fmt.Printf("  Assoc extensions:   %d\n", len(resolved.AssociatedFileExtensions))
		fmt.Printf("  Blacklist patterns: %d\n", len(resolved.MediaTagBlacklist))
	} else {
		fmt.Println("Processing: disabled")
	}
	fmt.Println()
}

type dropCandidate struct {
	path    string
	modTime time.Time
}

func processDropFolder(ctx context.Context, proc processor.Processor, dropRoot string) int {
	entries, err := os.ReadDir(dropRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}

	candidates := make([]dropCandidate, 0, len(entries))
	errCount := 0

	for _, ent := range entries {
		name := ent.Name()
		if watch.IsIgnorableDropEntry(name) {
			continue
		}
		path := filepath.Join(dropRoot, name)

		info, err := ent.Info()
		if err != nil {
			fmt.Fprintf(os.Stderr, "process-drop: stat %s: %v\n", path, err)
			errCount++
			continue
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			continue
		}

		candidates = append(candidates, dropCandidate{
			path:    path,
			modTime: info.ModTime(),
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.Before(candidates[j].modTime)
	})

	for _, item := range candidates {
		res, err := proc.Process(ctx, processor.Request{InputPath: item.path})
		if err != nil {
			fmt.Fprintf(os.Stderr, "process-drop: %s: %v\n", item.path, err)
			errCount++
		}
		if len(res) > 0 {
			PrintResults(res)
		}
	}

	if errCount > 0 {
		fmt.Fprintf(os.Stderr, "process-drop completed with %d error(s).\n", errCount)
	}

	return errCount
}
