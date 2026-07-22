package daemon

import (
	"fmt"
	"strings"

	"github.com/mtn-man/mintmedia/internal/console"
	"github.com/mtn-man/mintmedia/internal/logging"
	"github.com/mtn-man/mintmedia/internal/notify"
)

// caffeinateHooks builds the notify.CaffeinateHooks used for the daemon's
// lifetime sleep-inhibition (see Run), routing through the daemon's own
// console-only logger methods -- identical wording/events to what Run had
// inlined before notify.StartCaffeinate existed, and still gated by the
// user's configured console_level like every other daemon log line.
func (d *Daemon) caffeinateHooks() notify.CaffeinateHooks {
	return notify.CaffeinateHooks{
		OnUnsupported: func() {
			d.logConsoleInfo(logging.EventSystemStartup, "INFO     caffeinate: sleep inhibition not available on this platform", nil)
		},
		OnStartWarn: func(err error) {
			d.logConsoleWarn(logging.EventSystemStartup, fmt.Sprintf("WARNING  caffeinate: %v", err), err, nil)
		},
		OnStopWarn: func(err error) {
			d.logConsoleWarn(logging.EventSystemShutdownComplete, fmt.Sprintf("WARNING  caffeinate stop: %v", err), err, nil)
		},
	}
}

func (d *Daemon) logHistoryInfo(event logging.Event, fields logging.Fields) {
	if d == nil || d.Logger == nil {
		return
	}
	d.Logger.HistoryInfo(componentForEvent(event), event, fields)
}

func (d *Daemon) logConsoleInfo(event logging.Event, msg string, fields logging.Fields) {
	if d == nil || d.Logger == nil {
		return
	}
	d.Logger.ConsoleInfo(componentForEvent(event), event, console.ColorizePrefixOut(msg), fields)
}

func (d *Daemon) logHistoryWarn(event logging.Event, err error, fields logging.Fields) {
	if d == nil || d.Logger == nil {
		return
	}
	d.Logger.HistoryWarn(componentForEvent(event), event, err, fields)
}

func (d *Daemon) logConsoleWarn(event logging.Event, msg string, err error, fields logging.Fields) {
	if d == nil || d.Logger == nil {
		return
	}
	d.Logger.ConsoleWarn(componentForEvent(event), event, console.ColorizePrefixErr(msg), err, fields)
}

func (d *Daemon) logHistoryError(event logging.Event, err error, fields logging.Fields) {
	if d == nil || d.Logger == nil {
		return
	}
	d.Logger.HistoryError(componentForEvent(event), event, err, fields)
}

func (d *Daemon) logConsoleError(event logging.Event, msg string, err error, fields logging.Fields) {
	if d == nil || d.Logger == nil {
		return
	}
	d.Logger.ConsoleError(componentForEvent(event), event, console.ColorizePrefixErr(msg), err, fields)
}

func (d *Daemon) logSortParseError(path string, err error) {
	d.logConsoleWarn(
		logging.EventProcessorInputSkippedParseError,
		"WARNING  skipping "+path+": "+err.Error(),
		err,
		logging.Fields{"path": path},
	)
	d.logHistoryWarn(logging.EventProcessorInputSkippedParseError, err, logging.Fields{"path": path})
}

func componentForEvent(event logging.Event) string {
	if strings.HasPrefix(string(event), "system.") {
		return "system"
	}
	if strings.HasPrefix(string(event), "processor.") {
		return "processor"
	}
	return "daemon"
}
