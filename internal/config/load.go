// Package config loads, normalizes, and validates Mintmedia configuration.
//
// Design goals (v1):
// - TOML is the on-disk format (user-edited).
// - Go code defines the schema, applies defaults, expands paths/env vars, and validates.
// - TOML uses snake_case keys/tables; Go uses CamelCase fields.
// - Backwards compatibility with the legacy bash config is intentionally NOT a goal.
package config

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

//go:embed defaults_darwin.toml
var defaultConfigDarwin []byte

//go:embed defaults_linux.toml
var defaultConfigLinux []byte

func platformDefaultConfig() []byte {
	if runtime.GOOS == "darwin" {
		return defaultConfigDarwin
	}
	return defaultConfigLinux
}

const (
	// DefaultConfigPathRel is relative to the user's home directory.
	DefaultConfigPathRel = ".config/mintmedia/config.toml"

	// Defaults (opinionated for reliability).
	defaultClipboardPollInterval = 1 * time.Second
	defaultDropSettleDuration    = 3 * time.Second
	defaultShutdownGraceDuration = 10 * time.Minute
	defaultShutdownForceTimeout  = 15 * time.Second

	// State file defaults (relative to state_dir unless absolute).
	defaultHistoryFile = "history.jsonl"

	defaultConsoleLevel = "INFO"
	defaultHistoryLevel = "WARN"
)

// tomlSyntaxError reports a TOML syntax error with the config path and the
// library's line/column-annotated snippet, while still wrapping the
// underlying error so errors.As(err, &toml.ParseError{}) keeps working.
type tomlSyntaxError struct {
	cfgPathAbs string
	err        error
	display    string
}

func (e *tomlSyntaxError) Error() string {
	return fmt.Sprintf("config error in %s:\n%s", e.cfgPathAbs, e.display)
}

func (e *tomlSyntaxError) Unwrap() error {
	return e.err
}

// Load reads TOML from disk, applies defaults, expands paths/env vars,
// validates, and returns both the raw Config and a Resolved view.
// The bool return value is true when no config file was found at the default
// path and a fresh one was written from the embedded defaults.
// An explicitly-provided configPath that does not exist is always an error.
func Load(configPath string) (*Config, *Resolved, bool, error) {
	usingDefault := strings.TrimSpace(configPath) == ""

	if usingDefault {
		p, err := defaultConfigPath()
		if err != nil {
			return nil, nil, false, err
		}
		configPath = p
	}

	cfgPathAbs, err := expandPath(configPath)
	if err != nil {
		return nil, nil, false, fmt.Errorf("expand config path: %w", err)
	}

	bootstrapped := false
	if usingDefault {
		if _, statErr := os.Stat(cfgPathAbs); os.IsNotExist(statErr) {
			if writeErr := writeDefaultConfig(cfgPathAbs); writeErr != nil {
				return nil, nil, false, writeErr
			}
			bootstrapped = true
		}
		// Non-IsNotExist stat errors fall through to DecodeFile for a useful error.
	}

	var cfg Config
	md, err := toml.DecodeFile(cfgPathAbs, &cfg)
	if err != nil {
		var parseErr toml.ParseError
		if errors.As(err, &parseErr) {
			return nil, nil, false, &tomlSyntaxError{cfgPathAbs: cfgPathAbs, err: err, display: parseErr.ErrorWithPosition()}
		}
		return nil, nil, false, formatConfigError(cfgPathAbs, err)
	}
	if md.IsDefined("processing", "history_file") {
		return nil, nil, false, formatConfigError(cfgPathAbs, errors.New("processing.history_file has been removed; use logging.history_file"))
	}
	if md.IsDefined("processing") {
		return nil, nil, false, formatConfigError(cfgPathAbs, errors.New("[processing] section has been removed; use [logging]"))
	}
	if unknown := formatUndecodedKeys(md.Undecoded()); len(unknown) > 0 {
		return nil, nil, false, formatConfigError(cfgPathAbs, fmt.Errorf("unknown config key(s): %s", strings.Join(unknown, ", ")))
	}

	applyDefaults(&cfg)

	res, err := normalizeAndValidate(&cfg, cfgPathAbs)
	if err != nil {
		return nil, nil, false, err
	}

	return &cfg, res, bootstrapped, nil
}

func defaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, DefaultConfigPathRel), nil
}

func applyDefaults(cfg *Config) {
	// System defaults
	if strings.TrimSpace(cfg.System.DoneNotificationMode) == "" {
		cfg.System.DoneNotificationMode = "per_file"
	}
	if strings.TrimSpace(cfg.System.ShutdownGraceDuration) == "" {
		cfg.System.ShutdownGraceDuration = defaultShutdownGraceDuration.String()
	}
	if strings.TrimSpace(cfg.System.ShutdownForceTimeout) == "" {
		cfg.System.ShutdownForceTimeout = defaultShutdownForceTimeout.String()
	}

	// Watch defaults
	if strings.TrimSpace(cfg.Watch.DropSettleDuration) == "" {
		cfg.Watch.DropSettleDuration = defaultDropSettleDuration.String()
	}

	// Clipboard defaults
	if strings.TrimSpace(cfg.Clipboard.PollInterval) == "" {
		cfg.Clipboard.PollInterval = defaultClipboardPollInterval.String()
	}

	// Logging defaults
	if strings.TrimSpace(cfg.Logging.ConsoleLevel) == "" {
		cfg.Logging.ConsoleLevel = defaultConsoleLevel
	}
	if strings.TrimSpace(cfg.Logging.HistoryLevel) == "" {
		cfg.Logging.HistoryLevel = defaultHistoryLevel
	}
	if strings.TrimSpace(cfg.Logging.HistoryFile) == "" {
		cfg.Logging.HistoryFile = defaultHistoryFile
	}
}

// writeDefaultConfig writes the embedded defaults.toml to path,
// creating any missing parent directories.
func writeDefaultConfig(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(path, platformDefaultConfig(), 0o644); err != nil {
		return fmt.Errorf("write default config: %w", err)
	}
	return nil
}

func formatUndecodedKeys(keys []toml.Key) []string {
	if len(keys) == 0 {
		return nil
	}
	candidates := make([]string, 0, len(keys))
	for _, key := range keys {
		parts := make([]string, 0, len(key))
		for _, part := range key {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			parts = append(parts, part)
		}
		if len(parts) == 0 {
			continue
		}
		candidates = append(candidates, strings.Join(parts, "."))
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.Strings(candidates)

	// Keep only the most specific keys to avoid redundant parent paths such
	// as both "legacy" and "legacy.error_dir".
	out := make([]string, 0, len(candidates))
	for i, key := range candidates {
		prefix := key + "."
		hasChild := false
		for j := i + 1; j < len(candidates); j++ {
			if strings.HasPrefix(candidates[j], prefix) {
				hasChild = true
				break
			}
		}
		if hasChild {
			continue
		}
		out = append(out, key)
	}
	return out
}
