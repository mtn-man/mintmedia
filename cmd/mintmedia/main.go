// Mintmedia is a macOS drop-folder daemon and CLI for organizing media into Movies/Shows libraries.
// BETA v1.0: feature-complete for personal use; behavior may change.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	pflag "github.com/spf13/pflag"

	"github.com/mtn-man/mintmedia/internal/config"
	"github.com/mtn-man/mintmedia/internal/daemon"
	"github.com/mtn-man/mintmedia/internal/logging"
	"github.com/mtn-man/mintmedia/internal/processor"
	"github.com/mtn-man/mintmedia/internal/transfer"
)

const (
	exitError       = 1
	exitUsage       = 2
	exitInterrupted = 130

	defaultSoundInput      = "/System/Library/Sounds/Funk.aiff"
	defaultSoundDone       = "/System/Library/Sounds/Glass.aiff"
	defaultMagnetTimeout   = 10 * time.Second
	defaultCleanupCooldown = 2 * time.Minute

	defaultReportEvery = 250 * time.Millisecond
)

func main() {
	configPath := pflag.String(
		"config",
		"",
		"Path to config.toml (default: ~/.config/mintmedia/config.toml)",
	)

	// One-shot processor harness flags
	planPath := pflag.String("plan", "", "Compute and print the processing plan for a path (no changes)")
	applyPath := pflag.String("apply", "", "Plan and apply changes for a path (filesystem writes)")
	processPath := pflag.String("process", "", "Process a path with policy (ignore non-media/no-media dirs)")
	processDrop := pflag.BoolP("process-drop", "p", false, "Process all paths currently in the drop folder (one-shot)")
	daemonFlag := pflag.BoolP("daemon", "d", false, "Run the daemon (watch/poll/automations)")
	verbose := pflag.BoolP("verbose", "v", false, "Verbose startup output (prints config summary)")
	help := pflag.BoolP("help", "h", false, "Show help")

	pflag.Usage = func() {
		out := pflag.CommandLine.Output()
		write := func(format string, args ...any) {
			_, _ = fmt.Fprintf(out, format, args...)
		}
		writeln := func(args ...any) {
			_, _ = fmt.Fprintln(out, args...)
		}
		write("Usage: %s [flags]\n\n", filepath.Base(os.Args[0]))
		writeln("Modes (choose one; default is -p/--process-drop when features.enable_processing=true):")
		writeln("  --plan <path>        Compute and print the processing plan (no changes)")
		writeln("  --apply <path>       Plan and apply changes for a path (filesystem writes)")
		writeln("  --process <path>     Process a path with policy (ignore non-media/no-media dirs)")
		writeln("  -p, --process-drop   Process all paths currently in the drop folder (one-shot)")
		writeln("  -d, --daemon         Run the daemon (watch/poll/automations)")
		writeln("\nOther flags:")
		writeln("  --config <path>      Path to config.toml (default: ~/.config/mintmedia/config.toml)")
		writeln("  -v, --verbose        Verbose startup output (prints config summary)")
		writeln("  -h, --help           Show help")
	}

	pflag.Parse()
	if *help {
		pflag.Usage()
		return
	}

	cfg, resolved, bootstrapped, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(exitError)
	}
	if bootstrapped {
		fmt.Printf("No config file found. A default config has been written to: %s\n", resolved.ConfigPathAbs)
	}

	mode, err := resolveModePolicy(
		*planPath,
		*applyPath,
		*processPath,
		*processDrop,
		*daemonFlag,
		cfg.Features.EnableProcessing,
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(exitUsage)
	}

	if *verbose {
		printConfigSummary(cfg, resolved)
	}

	if !cfg.Features.EnableProcessing {
		fmt.Println("Config smoke test complete.")
		return
	}

	logger, err := logging.New(logging.Options{
		Stdout:               os.Stdout,
		Stderr:               os.Stderr,
		ConsoleLevel:         resolved.ConsoleLogLevel,
		HistoryLevel:         resolved.HistoryLogLevel,
		HistoryFile:          resolved.HistoryFileAbs,
		HistoryInfoAllowlist: logging.DefaultHistoryInfoAllowlist(),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(exitError)
	}

	proc, err := newGoProcessor(resolved, logger)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(exitError)
	}

	// One-shot modes short-circuit daemon.
	ctx := context.Background()
	if mode.PlanPath != "" {
		plans, err := proc.Plan(ctx, processor.Request{InputPath: mode.PlanPath})
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(exitError)
		}
		PrintPlans(plans)
		return
	}

	if mode.ApplyPath != "" {
		plans, err := proc.Plan(ctx, processor.Request{InputPath: mode.ApplyPath})
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(exitError)
		}
		PrintPlans(plans)

		fmt.Println("\n--- APPLY ---")
		var res []processor.Result
		err = withCaffeinate(func() error {
			var runErr error
			res, runErr = proc.Apply(ctx, plans)
			return runErr
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(exitError)
		}
		PrintResults(res)
		return
	}

	if mode.ProcessPath != "" {
		var res []processor.Result
		err := withCaffeinate(func() error {
			var runErr error
			res, runErr = proc.Process(ctx, processor.Request{InputPath: mode.ProcessPath})
			return runErr
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(exitError)
		}
		PrintResults(res)
		return
	}

	if mode.ProcessDrop {
		runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		outcome := processDropFolder(
			runCtx,
			proc,
			resolved.DropFolderAbs,
			defaultSoundDone,
			resolved.DoneNotificationMode,
			*verbose,
			resolved.ShutdownGraceDuration,
			resolved.ShutdownForceTimeout,
		)
		if outcome.Interrupted || outcome.TimedOut {
			os.Exit(exitInterrupted)
		}
		if outcome.ErrorCount > 0 {
			os.Exit(exitError)
		}
		return
	}

	// ---- Daemon mode ---------------------------------------------------------
	interrupted, err := runDaemonMode(cfg, resolved, proc, logger)
	if err != nil {
		if errors.Is(err, daemon.ErrShutdownTimedOut) {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(exitInterrupted)
		}
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(exitError)
	}
	if interrupted {
		os.Exit(exitInterrupted)
	}
}

// --- Go-native processor wiring --------------------------------------------

func newGoProcessor(res *config.Resolved, logger logging.Logger) (processor.Processor, error) {
	pcfg := processor.Config{
		DropFolder: res.DropFolderAbs,
		MoviesDir:  res.DestDirMoviesAbs,
		ShowsDir:   res.DestDirShowsAbs,

		MainMediaExtensions:      res.MainMediaExtensions,
		AssociatedFileExtensions: res.AssociatedFileExtensions,
		MediaTagBlacklist:        res.MediaTagBlacklist,
	}

	xfer := transfer.NewRenameOrCopy(transfer.Options{
		// Structured reporter enables the progress bar for large/slow copies.
		Reporter: transfer.NewTerminalReporter(os.Stdout, transfer.ReportOptions{
			EnableBar: true,
			EnableETA: true,
		}),
		UpdateEvery: defaultReportEvery,
	})

	return processor.New(pcfg, xfer, logger)
}

func printConfigSummary(cfg *config.Config, resolved *config.Resolved) {
	fmt.Println("Mintmedia config loaded successfully.")
	fmt.Printf("Config file:  %s\n\n", resolved.ConfigPathAbs)

	fmt.Println("Resolved paths:")
	fmt.Printf("  Drop folder:        %s\n", resolved.DropFolderAbs)
	fmt.Printf("  State dir:          %s\n", resolved.StateDirAbs)
	fmt.Printf("  Movies dir:         %s\n", resolved.DestDirMoviesAbs)
	fmt.Printf("  Shows dir:          %s\n", resolved.DestDirShowsAbs)
	fmt.Println()

	fmt.Println("Runtime settings:")
	fmt.Printf("  Max processors:     %d\n", cfg.System.MaxConcurrentProcessors)
	fmt.Printf("  Drop settle:        %s\n", resolved.DropSettleDuration)
	fmt.Printf("  Clipboard poll:     %s\n", resolved.ClipboardPollInterval)
	fmt.Printf("  Shutdown grace:     %s\n", resolved.ShutdownGraceDuration)
	fmt.Printf("  Shutdown force:     %s\n", resolved.ShutdownForceTimeout)
	fmt.Printf("  Console log level:  %s\n", resolved.ConsoleLogLevel)
	fmt.Printf("  History log level:  %s\n", resolved.HistoryLogLevel)
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
