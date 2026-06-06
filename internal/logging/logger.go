package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	componentSystem = "system"
	schemaV1        = "mintmedia.log.v1"
)

// Options configure the runtime logger and sinks.
type Options struct {
	Stdout io.Writer
	Stderr io.Writer

	ConsoleLevel string
	HistoryLevel string
	HistoryFile  string

	HistoryInfoAllowlist []Event
}

// RuntimeLogger fans out operational logs to console and history sinks.
type RuntimeLogger struct {
	consoleSink *ConsoleSink
	historySink *HistorySink

	consoleMin Level
	historyMin Level

	historyInfoAllow map[Event]struct{}
	pid              int

	stderr io.Writer
}

// New builds a runtime logger with a console sink and JSONL history sink.
func New(opts Options) (*RuntimeLogger, error) {
	consoleMin, err := ParseLevel(opts.ConsoleLevel)
	if err != nil {
		return nil, fmt.Errorf("console level: %w", err)
	}
	historyMin, err := ParseLevel(opts.HistoryLevel)
	if err != nil {
		return nil, fmt.Errorf("history level: %w", err)
	}
	if strings.TrimSpace(opts.HistoryFile) == "" {
		return nil, fmt.Errorf("history file path is empty")
	}

	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	infoAllow := make(map[Event]struct{}, len(opts.HistoryInfoAllowlist))
	for _, event := range opts.HistoryInfoAllowlist {
		event = Event(strings.TrimSpace(string(event)))
		if event == "" {
			continue
		}
		infoAllow[event] = struct{}{}
	}

	l := &RuntimeLogger{
		consoleSink:      NewConsoleSink(stdout, stderr),
		historySink:      NewHistorySink(opts.HistoryFile),
		consoleMin:       consoleMin,
		historyMin:       historyMin,
		historyInfoAllow: infoAllow,
		pid:              os.Getpid(),
		stderr:           stderr,
	}
	return l, nil
}

func (l *RuntimeLogger) Debug(component string, event Event, msg string, fields Fields) {
	l.Log(Entry{
		Level:     LevelDebug,
		Component: component,
		Event:     event,
		Message:   msg,
		Fields:    fields,
	})
}

func (l *RuntimeLogger) Info(component string, event Event, msg string, fields Fields) {
	l.Log(Entry{
		Level:     LevelInfo,
		Component: component,
		Event:     event,
		Message:   msg,
		Fields:    fields,
	})
}

func (l *RuntimeLogger) Warn(component string, event Event, msg string, err error, fields Fields) {
	l.Log(Entry{
		Level:     LevelWarn,
		Component: component,
		Event:     event,
		Message:   msg,
		Fields:    fields,
		Err:       ErrorFieldFrom(err),
	})
}

func (l *RuntimeLogger) Error(component string, event Event, msg string, err error, fields Fields) {
	l.Log(Entry{
		Level:     LevelError,
		Component: component,
		Event:     event,
		Message:   msg,
		Fields:    fields,
		Err:       ErrorFieldFrom(err),
	})
}

func (l *RuntimeLogger) ConsoleInfo(component string, event Event, msg string, fields Fields) {
	l.logConsoleOnly(Entry{
		Level:     LevelInfo,
		Component: component,
		Event:     event,
		Message:   msg,
		Fields:    fields,
	})
}

func (l *RuntimeLogger) ConsoleWarn(component string, event Event, msg string, err error, fields Fields) {
	l.logConsoleOnly(Entry{
		Level:     LevelWarn,
		Component: component,
		Event:     event,
		Message:   msg,
		Fields:    fields,
		Err:       ErrorFieldFrom(err),
	})
}

func (l *RuntimeLogger) ConsoleError(component string, event Event, msg string, err error, fields Fields) {
	l.logConsoleOnly(Entry{
		Level:     LevelError,
		Component: component,
		Event:     event,
		Message:   msg,
		Fields:    fields,
		Err:       ErrorFieldFrom(err),
	})
}

func (l *RuntimeLogger) HistoryInfo(component string, event Event, fields Fields) {
	l.logHistoryOnly(Entry{
		Level:     LevelInfo,
		Component: component,
		Event:     event,
		Fields:    fields,
	})
}

func (l *RuntimeLogger) HistoryWarn(component string, event Event, err error, fields Fields) {
	l.logHistoryOnly(Entry{
		Level:     LevelWarn,
		Component: component,
		Event:     event,
		Fields:    fields,
		Err:       ErrorFieldFrom(err),
	})
}

func (l *RuntimeLogger) HistoryError(component string, event Event, err error, fields Fields) {
	l.logHistoryOnly(Entry{
		Level:     LevelError,
		Component: component,
		Event:     event,
		Fields:    fields,
		Err:       ErrorFieldFrom(err),
	})
}

func (l *RuntimeLogger) Log(entry Entry) {
	if l == nil {
		return
	}
	entry, ok := l.normalizeAndValidate(entry)
	if !ok {
		return
	}
	if entry.Level.gte(l.consoleMin) {
		if err := l.consoleSink.Write(entry); err != nil {
			l.fallbackError(fmt.Errorf("console sink: %w", err))
		}
	}
	if l.shouldWriteHistory(entry) {
		if err := l.historySink.Write(entry); err != nil {
			l.fallbackError(fmt.Errorf("history sink: %w", err))
		}
	}
}

func (l *RuntimeLogger) logHistoryOnly(entry Entry) {
	if l == nil {
		return
	}
	entry, ok := l.normalizeAndValidate(entry)
	if !ok {
		return
	}
	if l.shouldWriteHistory(entry) {
		if err := l.historySink.Write(entry); err != nil {
			l.fallbackError(fmt.Errorf("history sink: %w", err))
		}
	}
}

func (l *RuntimeLogger) logConsoleOnly(entry Entry) {
	if l == nil {
		return
	}
	rawMessage := entry.Message
	entry = l.normalizeEntry(entry)
	// Console-only calls preserve caller formatting exactly (for compatibility
	// with existing user-facing lines), while still using shared normalization.
	entry.Message = rawMessage
	if !l.validateEntry(entry) {
		return
	}
	if entry.Level.gte(l.consoleMin) {
		if err := l.consoleSink.Write(entry); err != nil {
			l.fallbackError(fmt.Errorf("console sink: %w", err))
		}
	}
}

func (l *RuntimeLogger) normalizeAndValidate(entry Entry) (Entry, bool) {
	entry = l.normalizeEntry(entry)
	if !l.validateEntry(entry) {
		return Entry{}, false
	}
	return entry, true
}

func (l *RuntimeLogger) validateEntry(entry Entry) bool {
	if entry.Level == "" || !entry.Level.valid() {
		l.fallbackError(fmt.Errorf("invalid level: %q", entry.Level))
		return false
	}
	if !validEventName(entry.Event) {
		l.fallbackError(fmt.Errorf("invalid event name: %q", entry.Event))
		return false
	}
	return true
}

func (l *RuntimeLogger) normalizeEntry(entry Entry) Entry {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	} else {
		entry.Timestamp = entry.Timestamp.UTC()
	}
	entry.Component = strings.TrimSpace(entry.Component)
	entry.Event = Event(strings.TrimSpace(string(entry.Event)))
	entry.Message = strings.TrimSpace(entry.Message)
	entry.PID = l.pid
	entry.Fields = normalizeFields(entry.Fields)
	if entry.Event == EventSystemStartup {
		if entry.Fields == nil {
			entry.Fields = make(Fields, 1)
		}
		if _, ok := entry.Fields["schema"]; !ok {
			entry.Fields["schema"] = schemaV1
		}
		if entry.Component == "" {
			entry.Component = componentSystem
		}
	}
	return entry
}

func (l *RuntimeLogger) shouldWriteHistory(entry Entry) bool {
	// INFO history behavior:
	// - If min level is INFO or DEBUG, all INFO entries persist.
	// - If min level is WARN, only allowlisted INFO entries persist.
	// - If min level is ERROR, INFO entries do not persist.
	if entry.Level == LevelInfo {
		if l.historyMin == LevelInfo || l.historyMin == LevelDebug {
			return true
		}
		if l.historyMin == LevelWarn {
			_, ok := l.historyInfoAllow[entry.Event]
			return ok
		}
		return false
	}
	return entry.Level.gte(l.historyMin)
}

func (l *RuntimeLogger) fallbackError(err error) {
	if err == nil {
		return
	}
	if l.stderr == nil {
		return
	}
	_, _ = fmt.Fprintf(l.stderr, "LOGGING ERROR: %v\n", err)
}

// --- normalization helpers --------------------------------------------------

var (
	dotCaseEventRe = regexp.MustCompile(`^[a-z0-9]+(?:\.[a-z0-9]+)*$`)

	reservedPathFields = map[string]struct{}{
		"path":         {},
		"src":          {},
		"dst":          {},
		"input_path":   {},
		"source_path":  {},
		"dest_path":    {},
		"drop_folder":  {},
		"movies_dir":   {},
		"shows_dir":    {},
		"history_file": {},
	}
)

func validEventName(event Event) bool {
	return dotCaseEventRe.MatchString(strings.TrimSpace(string(event)))
}

func isReservedPathField(key string) bool {
	_, ok := reservedPathFields[key]
	return ok
}

func normalizeFields(fields Fields) Fields {
	if len(fields) == 0 {
		return nil
	}
	out := make(Fields, len(fields))
	for k, v := range fields {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		if isReservedPathField(key) {
			out[key] = normalizePathValue(v)
			continue
		}
		out[key] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizePathValue(v any) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	p := filepath.Clean(s)
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return filepath.Clean(abs)
}
