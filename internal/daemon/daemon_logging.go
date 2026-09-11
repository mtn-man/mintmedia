package daemon

import (
	"fmt"

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

// log derives the daemon's Emitter from the exported Logger field. Daemon is a
// bare struct literal with no constructor -- callers, including tests, assign
// Logger after construction -- so there is no init point at which to cache one.
// Emitter is a value type precisely so deriving it per call costs nothing.
func (d *Daemon) log() logging.Emitter {
	if d == nil {
		return logging.Emitter{}
	}
	return logging.NewEmitter(d.Logger, "daemon")
}

// These wrappers exist only to spare every call site the d.log() receiver and
// to keep one name set across this package and internal/processor. The console
// and history split, colorization, component resolution and the nil-guard all
// live in logging.Emitter -- see its doc comment.

func (d *Daemon) logHistoryInfo(event logging.Event, fields logging.Fields) {
	d.log().HistoryInfo(event, fields)
}

func (d *Daemon) logConsoleInfo(event logging.Event, msg string, fields logging.Fields) {
	d.log().ConsoleInfo(event, msg, fields)
}

func (d *Daemon) logConsoleWarn(event logging.Event, msg string, err error, fields logging.Fields) {
	d.log().ConsoleWarn(event, msg, err, fields)
}

func (d *Daemon) logHistoryError(event logging.Event, err error, fields logging.Fields) {
	d.log().HistoryError(event, err, fields)
}

func (d *Daemon) logInfo(event logging.Event, msg string, fields logging.Fields) {
	d.log().Info(event, msg, fields)
}

func (d *Daemon) logWarn(event logging.Event, msg string, err error, fields logging.Fields) {
	d.log().Warn(event, msg, err, fields)
}

func (d *Daemon) logError(event logging.Event, msg string, err error, fields logging.Fields) {
	d.log().Error(event, msg, err, fields)
}

func (d *Daemon) logSortParseError(path string, err error) {
	d.logWarn(
		logging.EventProcessorInputSkippedParseError,
		"WARNING  skipping "+path+": "+err.Error(),
		err,
		logging.Fields{"path": path},
	)
}
