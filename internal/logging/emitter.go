package logging

import (
	"strings"

	"github.com/mtn-man/mintmedia/internal/console"
)

// Emitter binds a Logger to a component scope and owns the console/history
// split that callers need.
//
// The two sinks deliberately record one event differently. Console messages
// arrive label-prefixed from the caller and are colorized here; history entries
// carry no message at all, only structured Fields. The Logger interface enforces
// that split -- every method targets exactly one sink -- because one entry
// carrying a single message to both would put raw ANSI escapes into
// history.jsonl. Anything a console line states must therefore also appear as a
// Field, or it is not recorded at all.
//
// Emitter is a value: the zero Emitter, and any Emitter over a nil Logger,
// discards everything, so callers need no guard of their own and holders with
// no init point can derive one per call without allocating.
type Emitter struct {
	logger   Logger
	fallback string
}

// NewEmitter returns an Emitter writing through logger.
//
// fallbackComponent names the component for events whose name carries no
// recognized prefix. Events that do carry one are attributed to it regardless
// of the fallback, so a package logging an event it does not own is still
// recorded against the right component.
func NewEmitter(logger Logger, fallbackComponent string) Emitter {
	return Emitter{logger: logger, fallback: strings.TrimSpace(fallbackComponent)}
}

func (e Emitter) enabled() bool {
	return e.logger != nil
}

func (e Emitter) componentFor(event Event) string {
	name := strings.TrimSpace(string(event))
	switch {
	case strings.HasPrefix(name, "system."):
		return "system"
	case strings.HasPrefix(name, "processor."):
		return "processor"
	default:
		return e.fallback
	}
}

// --- console only ------------------------------------------------------------

// ConsoleInfo writes an INFO line to the console sink only.
func (e Emitter) ConsoleInfo(event Event, msg string, fields Fields) {
	if !e.enabled() {
		return
	}
	e.logger.ConsoleInfo(e.componentFor(event), event, console.ColorizePrefixOut(msg), fields)
}

// ConsoleWarn writes a WARN line to the console sink only.
func (e Emitter) ConsoleWarn(event Event, msg string, err error, fields Fields) {
	if !e.enabled() {
		return
	}
	e.logger.ConsoleWarn(e.componentFor(event), event, console.ColorizePrefixErr(msg), err, fields)
}

// ConsoleError writes an ERROR line to the console sink only.
func (e Emitter) ConsoleError(event Event, msg string, err error, fields Fields) {
	if !e.enabled() {
		return
	}
	e.logger.ConsoleError(e.componentFor(event), event, console.ColorizePrefixErr(msg), err, fields)
}

// --- history only ------------------------------------------------------------

// HistoryInfo records an INFO entry in the history sink only.
func (e Emitter) HistoryInfo(event Event, fields Fields) {
	if !e.enabled() {
		return
	}
	e.logger.HistoryInfo(e.componentFor(event), event, fields)
}

// HistoryWarn records a WARN entry in the history sink only.
func (e Emitter) HistoryWarn(event Event, err error, fields Fields) {
	if !e.enabled() {
		return
	}
	e.logger.HistoryWarn(e.componentFor(event), event, err, fields)
}

// HistoryError records an ERROR entry in the history sink only.
func (e Emitter) HistoryError(event Event, err error, fields Fields) {
	if !e.enabled() {
		return
	}
	e.logger.HistoryError(e.componentFor(event), event, err, fields)
}

// --- both sinks --------------------------------------------------------------

// Info emits one logical INFO event: the labeled message to the console, the
// structured record to history.
func (e Emitter) Info(event Event, msg string, fields Fields) {
	e.ConsoleInfo(event, msg, fields)
	e.HistoryInfo(event, fields)
}

// Warn emits one logical WARN event to both sinks.
func (e Emitter) Warn(event Event, msg string, err error, fields Fields) {
	e.ConsoleWarn(event, msg, err, fields)
	e.HistoryWarn(event, err, fields)
}

// Error emits one logical ERROR event to both sinks.
func (e Emitter) Error(event Event, msg string, err error, fields Fields) {
	e.ConsoleError(event, msg, err, fields)
	e.HistoryError(event, err, fields)
}
