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
	p.logger.ConsoleWarn("processor", event, console.ColorizePrefix(msg), err, fields)
}

func logWarn(p *processorImpl, event logging.Event, msg string, err error, fields logging.Fields) {
	if p == nil || p.logger == nil {
		return
	}
	p.logger.Warn("processor", event, msg, err, fields)
}
