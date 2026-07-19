package logging

import (
	"fmt"
	"strings"
	"time"
)

// Fields stores structured metadata for a log entry.
type Fields map[string]any

// Level describes log severity.
type Level string

// Severity levels, ordered low to high.
const (
	LevelDebug Level = "DEBUG"
	LevelInfo  Level = "INFO"
	LevelWarn  Level = "WARN"
	LevelError Level = "ERROR"
)

var levelRank = map[Level]int{
	LevelDebug: 0,
	LevelInfo:  1,
	LevelWarn:  2,
	LevelError: 3,
}

// ParseLevel normalizes and validates a string level.
func ParseLevel(raw string) (Level, error) {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case string(LevelDebug):
		return LevelDebug, nil
	case string(LevelInfo):
		return LevelInfo, nil
	case string(LevelWarn):
		return LevelWarn, nil
	case string(LevelError):
		return LevelError, nil
	default:
		return "", fmt.Errorf("invalid level %q (allowed: DEBUG, INFO, WARN, ERROR)", raw)
	}
}

func (l Level) valid() bool {
	_, ok := levelRank[l]
	return ok
}

func (l Level) gte(other Level) bool {
	return levelRank[l] >= levelRank[other]
}

// ErrorField stores a normalized error payload.
type ErrorField struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

// Entry is the canonical structured log record used by all sinks.
type Entry struct {
	Timestamp time.Time   `json:"ts"`
	Level     Level       `json:"level"`
	Component string      `json:"component,omitempty"`
	Event     Event       `json:"event"`
	Message   string      `json:"msg,omitempty"`
	PID       int         `json:"pid"`
	Fields    Fields      `json:"fields,omitempty"`
	Err       *ErrorField `json:"err,omitempty"`
}

// Logger is the flat logging API used by callers.
type Logger interface {
	Log(entry Entry)
	Debug(component string, event Event, msg string, fields Fields)
	Info(component string, event Event, msg string, fields Fields)
	Warn(component string, event Event, msg string, err error, fields Fields)
	Error(component string, event Event, msg string, err error, fields Fields)
	ConsoleInfo(component string, event Event, msg string, fields Fields)
	ConsoleWarn(component string, event Event, msg string, err error, fields Fields)
	ConsoleError(component string, event Event, msg string, err error, fields Fields)
	HistoryInfo(component string, event Event, fields Fields)
	HistoryWarn(component string, event Event, err error, fields Fields)
	HistoryError(component string, event Event, err error, fields Fields)
}

// ErrorFieldFrom converts a Go error into the canonical structured error field.
func ErrorFieldFrom(err error) *ErrorField {
	if err == nil {
		return nil
	}
	return &ErrorField{
		Message: err.Error(),
		Type:    fmt.Sprintf("%T", err),
	}
}
