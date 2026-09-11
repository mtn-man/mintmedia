package processor

import (
	"github.com/mtn-man/mintmedia/internal/console"
	"github.com/mtn-man/mintmedia/internal/logging"
)

func logInfoHistoryOnly(p *processorImpl, event logging.Event, fields logging.Fields) {
	if p == nil || p.logger == nil {
		return
	}
	p.logger.HistoryInfo("processor", event, fields)
}

func logWarnHistoryOnly(p *processorImpl, event logging.Event, err error, fields logging.Fields) {
	if p == nil || p.logger == nil {
		return
	}
	p.logger.HistoryWarn("processor", event, err, fields)
}

func logConsoleWarn(p *processorImpl, event logging.Event, msg string, err error, fields logging.Fields) {
	if p == nil || p.logger == nil {
		return
	}
	p.logger.ConsoleWarn("processor", event, console.ColorizePrefixErr(msg), err, fields)
}

func logConsoleInfo(p *processorImpl, event logging.Event, msg string, fields logging.Fields) {
	if p == nil || p.logger == nil {
		return
	}
	p.logger.ConsoleInfo("processor", event, console.ColorizePrefixOut(msg), fields)
}

// logWarn and logInfo emit one logical event to both sinks. They compose the
// two single-sink helpers rather than calling Logger.Warn/Logger.Info, which
// write a single Message to both: the console message is label-prefixed and
// colorized, and colorized text must never reach history.jsonl. That leaves
// history carrying no message at all -- the Fields at each call site are the
// record, so anything the console line states must also appear as a field.

func logWarn(p *processorImpl, event logging.Event, msg string, err error, fields logging.Fields) {
	logConsoleWarn(p, event, msg, err, fields)
	logWarnHistoryOnly(p, event, err, fields)
}

func logInfo(p *processorImpl, event logging.Event, msg string, fields logging.Fields) {
	logConsoleInfo(p, event, msg, fields)
	logInfoHistoryOnly(p, event, fields)
}
