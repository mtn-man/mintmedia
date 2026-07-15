// Mintmedia is a drop-folder daemon and CLI for organizing media into Movies/Shows libraries.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	pflag "github.com/spf13/pflag"
	"golang.org/x/term"

	"github.com/mtn-man/mintmedia/internal/config"
	"github.com/mtn-man/mintmedia/internal/console"
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
			fmt.Println(console.ColorizePrefixOut("STATUS   daemon not running"))
			os.Exit(exitNotRunning)
		}
		if info.Started.IsZero() {
			fmt.Println(console.ColorizePrefixOut(fmt.Sprintf("STATUS   daemon running (pid=%d)", info.PID)))
		} else {
			uptime := time.Since(info.Started).Truncate(time.Second)
			fmt.Println(console.ColorizePrefixOut(fmt.Sprintf("STATUS   daemon running (pid=%d, uptime %s)", info.PID, uptime)))
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
			fmt.Fprintln(os.Stderr, console.ColorizePrefixErr("WARNING  daemon not running"))
			return
		}
		p, err := os.FindProcess(info.PID)
		if err != nil {
			die(err, exitError)
		}
		if err := p.Signal(syscall.SIGTERM); err != nil {
			// Process exited in the window between CheckLock and Signal — already stopped.
			if errors.Is(err, syscall.ESRCH) {
				fmt.Println(console.ColorizePrefixOut("STOPPED  daemon"))
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
		fmt.Println(console.ColorizePrefixOut("STOPPED  daemon"))
		return
	}

	for _, dir := range resolved.CreatedDirs {
		fmt.Println(console.ColorizePrefixOut(fmt.Sprintf("CREATED  %s", dir)))
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

	if bootstrapped {
		fmt.Println(console.ColorizePrefixOut("CREATED  config file"))
		printConfigSummary(cfg, resolved)
		switch {
		case mode.ExplicitCount == 0:
			if !confirmProcessDrop(resolved.DropFolderAbs) {
				fmt.Println("Review the paths above -- especially destinations.dest_dir_movies/dest_dir_shows if you have an existing library -- then re-run with -p/--process-drop or -d/--daemon when ready.")
				return
			}
			fmt.Println()
		case mode.Daemon:
			if isInteractiveStdin() && !confirmDaemonStart(resolved.DropFolderAbs) {
				fmt.Println("Review the paths above -- especially destinations.dest_dir_movies/dest_dir_shows if you have an existing library -- then re-run with -d/--daemon when ready.")
				return
			}
			fmt.Println()
		}
	} else if *verbose {
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

// isInteractiveStdin reports whether stdin is an interactive terminal.
// term.IsTerminal (not console.IsTerminal's char-device heuristic) is
// required for gating a blocking read: /dev/null is itself a character
// device, so the cheap heuristic would misreport it as a terminal and block
// (or print an unanswerable prompt into logs) on a systemd unit or script
// whose stdin defaults to /dev/null.
func isInteractiveStdin() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// promptYesNo prints prompt and reads a line from stdin, treating "y"/"yes"
// (case-insensitive) as acceptance and everything else -- including empty
// input or EOF -- as decline. Callers are responsible for only invoking this
// when isInteractiveStdin() is true; it does not check itself, since the
// safe non-interactive default differs by call site (see confirmProcessDrop
// vs. the daemon bootstrap confirmation).
func promptYesNo(prompt string) bool {
	fmt.Print(prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

// confirmProcessDrop asks the user, on a bare first-run bootstrap, whether
// to go ahead and process the drop folder using the freshly-written default
// paths. A non-interactive stdin (systemd unit, script, CI) means there's no
// one to answer, so it declines rather than blocking -- matching this call
// site's existing bootstrap behavior of stopping and printing next steps
// when no explicit mode flag was given. This is unreviewed config; silence
// must never be read as consent to move files.
func confirmProcessDrop(dropFolder string) bool {
	if !isInteractiveStdin() {
		return false
	}
	return promptYesNo(fmt.Sprintf("Process the drop folder (%s) using these destinations now? [y/N] ", dropFolder))
}

// confirmDaemonStart asks the user, on a bare first-run bootstrap with an
// explicit -d/--daemon flag, whether to start the daemon now against the
// freshly-written default paths. Unlike confirmProcessDrop, a non-interactive
// stdin must NOT decline here -- callers only invoke this after already
// checking isInteractiveStdin(), so daemon startup under systemd (stdin from
// /dev/null) proceeds unprompted exactly as it did before this confirmation
// existed. The daemon is long-running and unattended, so an interactive first
// start still deserves a chance to bail before it silently organizes files
// on unreviewed paths indefinitely.
func confirmDaemonStart(dropFolder string) bool {
	return promptYesNo(fmt.Sprintf("Start the daemon now, watching %s and organizing into these destinations? [y/N] ", dropFolder))
}

func printConfigSummary(cfg *config.Config, resolved *config.Resolved) {
	fmt.Println(console.ColorizePrefixOut("STARTED       mintmedia"))
	fmt.Printf("Version:      %s\n", resolveVersion(version, mainModuleVersion()))
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
		fmt.Printf("  Blacklist:          %d\n", len(cfg.Naming.MediaTagBlacklist))
	} else {
		fmt.Println("Processing: disabled")
	}
	fmt.Println()
}
