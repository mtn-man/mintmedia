package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/term"

	"github.com/mtn-man/mintmedia/internal/config"
	"github.com/mtn-man/mintmedia/internal/console"
)

// isInteractiveStdin reports whether stdin is an interactive terminal.
// term.IsTerminal (not console.IsTerminal's char-device heuristic) is
// required for gating a blocking read: /dev/null is itself a character
// device, so the cheap heuristic would misreport it as a terminal and block
// (or print an unanswerable prompt into logs) on a systemd unit or script
// whose stdin defaults to /dev/null.
func isInteractiveStdin() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// promptYesNo prints question on its own line followed by a [Y/n]/[y/N] hint
// (capitalized letter marks the default), then reads a line from stdin.
// Empty input (bare Enter) or EOF takes defaultYes; otherwise "n"/"no" always
// declines and "y"/"yes" always accepts regardless of defaultYes. Callers are
// responsible for only invoking this when isInteractiveStdin() is true; it
// does not check itself, since the safe non-interactive fallback differs by
// call site (see confirmProcessDrop vs. the daemon bootstrap confirmation).
func promptYesNo(question string, defaultYes bool) bool {
	hint := "[y/N] "
	if defaultYes {
		hint = "[Y/n] "
	}
	fmt.Printf("%s\n%s", question, hint)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return defaultYes
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer == "" {
		return defaultYes
	}
	if answer == "n" || answer == "no" {
		return false
	}
	if answer == "y" || answer == "yes" {
		return true
	}
	return defaultYes
}

// confirmProcessDrop asks the user, on a bare first-run bootstrap, whether
// to go ahead and process the drop folder using the freshly-written default
// paths. The interactive prompt defaults to accepting on a bare Enter,
// matching confirmDaemonStart's Y/n convention. A non-interactive stdin is a
// separate case, not just "no answer given": there's no one to answer at
// all, so it declines outright rather than blocking or silently treating
// absence of a human as consent to move files.
func confirmProcessDrop(dropFolder string) bool {
	if !isInteractiveStdin() {
		return false
	}
	return promptYesNo(fmt.Sprintf("Process the drop folder (%s) using these destinations now?", dropFolder), true)
}

// confirmDaemonStart asks the user, on a bare first-run bootstrap with an
// explicit -d/--daemon flag, whether to start the daemon now against the
// freshly-written default paths. Defaults to accepting: passing -d already
// signals clear intent to run the daemon, so this is just a last chance to
// bail on unreviewed default paths, not a question about intent. A
// non-interactive stdin must NOT decline here -- callers only invoke this
// after already checking isInteractiveStdin(), so daemon startup under
// systemd (stdin from /dev/null) proceeds unprompted exactly as it did
// before this confirmation existed.
func confirmDaemonStart(dropFolder string) bool {
	return promptYesNo(fmt.Sprintf("Start the daemon now, watching %s and organizing into these destinations?", dropFolder), true)
}

// offerEditConfig runs after the user declines a first-run bootstrap
// confirmation. If stdin is interactive and $EDITOR is set, it offers
// (Y/n, default yes) to open the just-written config file in $EDITOR before
// exiting. It always ends by printing nextStepsMsg, whether or not an editor
// was offered or opened, so there's a single source of truth for the final
// guidance line regardless of which path got there.
func offerEditConfig(configPath, nextStepsMsg string) {
	editor := strings.TrimSpace(os.Getenv("EDITOR"))
	if isInteractiveStdin() && editor != "" && promptYesNo(fmt.Sprintf("Open the config file in %s now?", editor), true) {
		parts := strings.Fields(editor)
		cmd := exec.Command(parts[0], append(parts[1:], configPath)...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Println(console.ColorizePrefixErr(fmt.Sprintf("WARNING  failed to launch %s: %v", editor, err)))
		}
		fmt.Println()
	}
	fmt.Println(nextStepsMsg)
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
		fmt.Printf("  Custom blacklist patterns: %d\n", len(cfg.Naming.MediaTagBlacklist))
	} else {
		fmt.Println("Processing: disabled")
	}
	fmt.Println()
}
