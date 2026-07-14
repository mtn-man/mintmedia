// Mintmedia is a drop-folder daemon and CLI for organizing media into Movies/Shows libraries.
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
	"github.com/mtn-man/mintmedia/internal/state"
	"github.com/mtn-man/mintmedia/internal/transfer"
)

const (
	exitError       = 1
	exitUsage       = 2
	exitNotRunning  = 3
	exitInterrupted = 130

	defaultMagnetTimeout   = 10 * time.Second
	defaultCleanupCooldown = 2 * time.Minute

	defaultReportEvery = 250 * time.Millisecond
)

// die prints err in the tool's standard labeled/colorized error voice and
// exits with code. Every fatal one-shot CLI error goes through this instead
// of a bare fmt.Fprintln(os.Stderr, err.Error()) so nothing reaches the user
// as an unstyled raw Go error chain.
func die(err error, code int) {
	PrintFatalError(err)
	os.Exit(code)
}

func main() {
	configPath := pflag.String(
		"config",
		"",
		"Path to config.toml (default: ~/.config/mintmedia/config.toml)",
	)

	// One-shot processor harness flags
	planPath := pflag.String("plan", "", "Compute and print the processing plan for a path (no changes)")
	pflag.Lookup("plan").NoOptDefVal = "" // makes the path argument optional; no-arg form plans the drop folder
	processPath := pflag.String("process", "", "Process a path with policy (ignore non-media/no-media dirs)")
	processDrop := pflag.BoolP("process-drop", "p", false, "Process all paths currently in the drop folder (one-shot)")
	daemonFlag := pflag.BoolP("daemon", "d", false, "Run the daemon (watch/poll/automations)")
	statusFlag := pflag.BoolP("status", "s", false, "Check whether the daemon is running")
	stopFlag := pflag.BoolP("stop", "S", false, "Gracefully stop the running daemon")
	verbose := pflag.BoolP("verbose", "v", false, "Verbose startup output (prints config summary)")
	versionFlag := pflag.BoolP("version", "V", false, "Show version and exit")
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
		writeln("  --plan [path]        Preview what would happen (no changes); omit path to preview drop folder")
		writeln("  --process <path>     Process a path with policy (ignore non-media/no-media dirs)")
		writeln("  -p, --process-drop   Process all paths currently in the drop folder (one-shot)")
		writeln("  -d, --daemon         Run the daemon (watch/poll/automations)")
		writeln("\nDaemon control:")
		writeln("  -s, --status         Check whether the daemon is running (exit 0 = running, exit 3 = stopped)")
		writeln("  -S, --stop           Gracefully stop the running daemon (exit 0 = stopped or not running, exit 1 = error)")
		writeln("\nOther flags:")
		writeln("  --config <path>      Path to config.toml (default: ~/.config/mintmedia/config.toml)")
		writeln("  -v, --verbose        Verbose startup output (prints config summary)")
		writeln("  -V, --version        Show version and exit")
		writeln("  -h, --help           Show help")
	}

	pflag.Parse()
	if *help {
		pflag.Usage()
		return
	}
	if *versionFlag {
		fmt.Print(formatVersionLine(resolveVersion(version, mainModuleVersion())))
		return
	}

	cfg, resolved, bootstrapped, err := config.Load(*configPath)
	if err != nil {
		die(err, exitError)
	}
	if *statusFlag {
		lockPath := filepath.Join(resolved.StateDirAbs, lockFilename)
		info, running, err := state.CheckLock(lockPath)
		if err != nil {
			die(err, exitError)
		}
		if !running {
			fmt.Println("daemon not running")
			os.Exit(exitNotRunning)
		}
		if info.Started.IsZero() {
			fmt.Printf("daemon running (pid=%d)\n", info.PID)
		} else {
			uptime := time.Since(info.Started).Truncate(time.Second)
			fmt.Printf("daemon running (pid=%d, uptime %s)\n", info.PID, uptime)
		}
		return
	}

	if *stopFlag {
		lockPath := filepath.Join(resolved.StateDirAbs, lockFilename)
		info, running, err := state.CheckLock(lockPath)
		if err != nil {
			die(err, exitError)
		}
		if !running {
			fmt.Println("daemon not running")
			return
		}
		p, err := os.FindProcess(info.PID)
		if err != nil {
			die(err, exitError)
		}
		if err := p.Signal(syscall.SIGTERM); err != nil {
			// Process exited in the window between CheckLock and Signal — already stopped.
			if errors.Is(err, syscall.ESRCH) {
				fmt.Println("daemon stopped")
				return
			}
			die(err, exitError)
		}
		timeout := resolved.ShutdownGraceDuration + resolved.ShutdownForceTimeout + 5*time.Second
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if err := state.WaitUntilReleased(ctx, lockPath, info, 250*time.Millisecond); err != nil {
			die(fmt.Errorf("timed out waiting for daemon to stop (pid=%d)", info.PID), exitError)
		}
		fmt.Println("daemon stopped")
		return
	}

	if bootstrapped {
		fmt.Printf("No config file found. A default config has been written to: %s\n", resolved.ConfigPathAbs)
	}

	planDrop := pflag.Lookup("plan").Changed && *planPath == ""
	mode, err := resolveModePolicy(
		*planPath,
		planDrop,
		*processPath,
		*processDrop,
		*daemonFlag,
		cfg.Features.EnableProcessing,
	)
	if err != nil {
		die(err, exitUsage)
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
		die(err, exitError)
	}

	proc, err := newGoProcessor(resolved, logger)
	if err != nil {
		die(err, exitError)
	}

	// One-shot modes short-circuit daemon.
	ctx := context.Background()
	if mode.PlanPath != "" {
		plans, err := proc.Plan(ctx, processor.Request{InputPath: mode.PlanPath})
		if err != nil {
			die(err, exitError)
		}
		PrintPlans(plans)
		return
	}

	if mode.PlanDrop {
		if errCount := planDropFolder(ctx, proc, resolved.DropFolderAbs); errCount > 0 {
			os.Exit(exitError)
		}
		return
	}

	if mode.ProcessPath != "" {
		var res []processor.Result
		err := withCaffeinate(func() error {
			return proc.Process(ctx, processor.Request{
				InputPath: mode.ProcessPath,
				OnResult:  func(r processor.Result) { res = append(res, r) },
			})
		})
		if err != nil {
			die(err, exitError)
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
			resolved.DestDirMoviesAbs,
			resolved.DestDirShowsAbs,
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
		code := exitError
		if errors.Is(err, daemon.ErrShutdownTimedOut) {
			code = exitInterrupted
		}
		die(err, code)
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
	fmt.Printf("Version:      mintmedia %s\n", resolveVersion(version, mainModuleVersion()))
	fmt.Printf("Config file:  %s\n\n", resolved.ConfigPathAbs)

	fmt.Println("Resolved paths:")
	fmt.Printf("  Drop folder:        %s\n", resolved.DropFolderAbs)
	fmt.Printf("  State dir:          %s\n", resolved.StateDirAbs)
	fmt.Printf("  Movies dir:         %s\n", resolved.DestDirMoviesAbs)
	fmt.Printf("  Shows dir:          %s\n", resolved.DestDirShowsAbs)
	fmt.Println()

	fmt.Println("Runtime settings:")
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
