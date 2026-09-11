package processor

import (
	"github.com/mtn-man/mintmedia/internal/logging"
)

// These wrappers exist only to spare every call site the p.log receiver and to
// keep one name set across this package and internal/daemon. The console and
// history split, colorization, component resolution and the nil-guard all live
// in logging.Emitter -- see its doc comment for why a console message must
// never reach the history sink.

func logHistoryInfo(p *processorImpl, event logging.Event, fields logging.Fields) {
	p.log.HistoryInfo(event, fields)
}

func logHistoryWarn(p *processorImpl, event logging.Event, err error, fields logging.Fields) {
	p.log.HistoryWarn(event, err, fields)
}

func logConsoleInfo(p *processorImpl, event logging.Event, msg string, fields logging.Fields) {
	p.log.ConsoleInfo(event, msg, fields)
}

func logConsoleWarn(p *processorImpl, event logging.Event, msg string, err error, fields logging.Fields) {
	p.log.ConsoleWarn(event, msg, err, fields)
}

func logInfo(p *processorImpl, event logging.Event, msg string, fields logging.Fields) {
	p.log.Info(event, msg, fields)
}

func logWarn(p *processorImpl, event logging.Event, msg string, err error, fields logging.Fields) {
	p.log.Warn(event, msg, err, fields)
}
