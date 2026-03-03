package processor

import (
	"fmt"
	"os"

	"github.com/Mtn-Man/mintmedia/internal/logging"
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

func logWarn(p *processorImpl, event logging.Event, msg string, err error, fields logging.Fields) {
	if p == nil {
		return
	}
	if p.logger == nil {
		if msg != "" {
			fmt.Fprintln(os.Stderr, msg)
		}
		return
	}
	p.logger.Warn("processor", event, msg, err, fields)
}
