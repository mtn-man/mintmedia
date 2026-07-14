package daemon

import (
	"strings"

	"github.com/mtn-man/mintmedia/internal/console"
	"github.com/mtn-man/mintmedia/internal/logging"
)

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
