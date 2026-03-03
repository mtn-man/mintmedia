// Package config loads, normalizes, and validates Mintmedia configuration.
//
// Design goals (v1):
// - TOML is the on-disk format (user-edited).
// - Go code defines the schema, applies defaults, expands paths/env vars, and validates.
// - TOML uses snake_case keys/tables; Go uses CamelCase fields.
// - Backwards compatibility with the legacy bash config is intentionally NOT a goal.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/Mtn-Man/mintmedia/internal/logging"
	"github.com/Mtn-Man/mintmedia/internal/notify"
)

const (
	// DefaultConfigPathRel is relative to the user's home directory.
	DefaultConfigPathRel = ".config/mintmedia/config.toml"

	// Defaults (opinionated for reliability).
	defaultMaxConcurrentProcessors = 1
	defaultClipboardPollInterval   = 1 * time.Second
	defaultDropSettleDuration      = 3 * time.Second
	defaultShutdownGraceDuration   = 10 * time.Minute
	defaultShutdownForceTimeout    = 15 * time.Second

	// State file defaults (relative to state_dir unless absolute).
	defaultHistoryFile = "history.jsonl"

	defaultConsoleLevel = "INFO"
	defaultHistoryLevel = "WARN"
)

// Config is the decoded TOML configuration (pre-normalization).
type Config struct {
	Paths        Paths        `toml:"paths"`
	Destinations Destinations `toml:"destinations"`
	Features     Features     `toml:"features"`
	Logging      Logging      `toml:"logging"`
	System       System       `toml:"system"`
	Watch        Watch        `toml:"watch"`
	Clipboard    Clipboard    `toml:"clipboard"`
	Torrent      Torrent      `toml:"torrent"`
	Media        Media        `toml:"media"`
	Naming       Naming       `toml:"naming"`
}

type Paths struct {
	DropFolder string `toml:"drop_folder"`
	StateDir   string `toml:"state_dir"`
}

type Destinations struct {
	DestDirMovies string `toml:"dest_dir_movies"`
	DestDirShows  string `toml:"dest_dir_shows"`
}

type Features struct {
	EnableTorrentAutomation bool `toml:"enable_torrent_automation"`
	EnableProcessing        bool `toml:"enable_processing"`
}

type Logging struct {
	// Optional. Defaults to INFO.
	ConsoleLevel string `toml:"console_level"`
	// Optional. Defaults to WARN.
	HistoryLevel string `toml:"history_level"`
	// Optional. If relative, resolved under paths.state_dir.
	HistoryFile string `toml:"history_file"`
}

type System struct {
	AutoCreateMissingDirs   bool   `toml:"auto_create_missing_dirs"`
	DeferDestinationChecks  bool   `toml:"defer_destination_checks"`
	MaxConcurrentProcessors int    `toml:"max_concurrent_processors"`
	DoneNotificationMode    string `toml:"done_notification_mode"`
	ShutdownGraceDuration   string `toml:"shutdown_grace_duration"`
	ShutdownForceTimeout    string `toml:"shutdown_force_timeout"`
}

type Watch struct {
	// e.g. "3s"
	DropSettleDuration string `toml:"drop_settle_duration"`
}

type Clipboard struct {
	Enabled bool `toml:"enabled"`
	// e.g. "1s"
	PollInterval string `toml:"poll_interval"`
}

type Torrent struct {
	Enabled bool `toml:"enabled"`

	// Optional. If empty, relies on PATH lookup for "transmission-remote".
	TransmissionRemotePath string `toml:"transmission_remote_path"`

	// e.g. "localhost:9091"
	Host string `toml:"host"`

	// Optional. If set, passed as "--auth user:pass" (or your chosen scheme later).
	Auth string `toml:"auth"`

	// Optional. If unset, defaults to false.
	AutoCleanupCompletedTorrents *bool `toml:"auto_cleanup_completed_torrents"`
}

type Media struct {
	// Required when processing is enabled. Extensions should include the leading dot (e.g. ".mkv").
	MainMediaExtensions []string `toml:"main_media_extensions"`

	// Optional (may be empty). Extensions should include the leading dot (e.g. ".srt").
	AssociatedFileExtensions []string `toml:"associated_file_extensions"`
}

type Naming struct {
	// Regex patterns (strings) used by the Go processor to strip junk tags from release names.
	// Example: ["2160p","1080p","x265","h\\.264","web[- ]?dl", ...]
	MediaTagBlacklist []string `toml:"media_tag_blacklist"`
}

// Resolved contains normalized, validated, and parsed forms other packages should use.
type Resolved struct {
	ConfigPathAbs string

	DropFolderAbs string
	StateDirAbs   string

	DestDirMoviesAbs string
	DestDirShowsAbs  string

	DropSettleDuration    time.Duration
	ClipboardPollInterval time.Duration
	DoneNotificationMode  string
	ShutdownGraceDuration time.Duration
	ShutdownForceTimeout  time.Duration

	TransmissionRemoteAbs string

	ConsoleLogLevel string
	HistoryLogLevel string
	HistoryFileAbs  string

	// Copy of the TOML lists (normalized/validated).
	MainMediaExtensions      []string
	AssociatedFileExtensions []string

	// Naming patterns passed to Go processor.
	MediaTagBlacklist []string
}

// Load reads TOML from disk, applies defaults, expands paths/env vars,
// validates, and returns both the raw Config and a Resolved view.
func Load(configPath string) (*Config, *Resolved, error) {
	if strings.TrimSpace(configPath) == "" {
		p, err := defaultConfigPath()
		if err != nil {
			return nil, nil, err
		}
		configPath = p
	}

	cfgPathAbs, err := expandPath(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("expand config path: %w", err)
	}

	var cfg Config
	md, err := toml.DecodeFile(cfgPathAbs, &cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("parse TOML (%s): %w", cfgPathAbs, err)
	}
	if md.IsDefined("processing", "history_file") {
		return nil, nil, fmt.Errorf("config validation failed: processing.history_file has been removed; use logging.history_file")
	}
	if md.IsDefined("processing") {
		return nil, nil, fmt.Errorf("config validation failed: [processing] section has been removed; use [logging]")
	}

	applyDefaults(&cfg)

	res, err := normalizeAndValidate(&cfg, cfgPathAbs)
	if err != nil {
		return nil, nil, err
	}

	return &cfg, res, nil
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
	if cfg.System.MaxConcurrentProcessors <= 0 {
		cfg.System.MaxConcurrentProcessors = defaultMaxConcurrentProcessors
	}
	if strings.TrimSpace(cfg.System.DoneNotificationMode) == "" {
		cfg.System.DoneNotificationMode = notify.DoneNotificationPerFile
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

	// Torrent defaults
	if cfg.Torrent.AutoCleanupCompletedTorrents == nil {
		v := false
		cfg.Torrent.AutoCleanupCompletedTorrents = &v
	}
}

func normalizeAndValidate(cfg *Config, cfgPathAbs string) (*Resolved, error) {
	var errs []error

	consoleLevel, err := logging.ParseLevel(cfg.Logging.ConsoleLevel)
	if err != nil {
		errs = append(errs, fmt.Errorf("logging.console_level: %w", err))
	} else {
		cfg.Logging.ConsoleLevel = string(consoleLevel)
	}

	historyLevel, err := logging.ParseLevel(cfg.Logging.HistoryLevel)
	if err != nil {
		errs = append(errs, fmt.Errorf("logging.history_level: %w", err))
	} else {
		cfg.Logging.HistoryLevel = string(historyLevel)
	}

	doneNotificationMode, err := notify.NormalizeDoneNotificationMode(cfg.System.DoneNotificationMode)
	if err != nil {
		errs = append(errs, fmt.Errorf("system.done_notification_mode: %w", err))
	} else {
		cfg.System.DoneNotificationMode = doneNotificationMode
	}

	// Durations
	settle, err := time.ParseDuration(strings.TrimSpace(cfg.Watch.DropSettleDuration))
	if err != nil {
		errs = append(errs, fmt.Errorf("watch.drop_settle_duration invalid: %w", err))
	} else if settle < 500*time.Millisecond {
		errs = append(errs, fmt.Errorf("watch.drop_settle_duration too small (%s)", settle))
	}

	poll, err := time.ParseDuration(strings.TrimSpace(cfg.Clipboard.PollInterval))
	if err != nil {
		errs = append(errs, fmt.Errorf("clipboard.poll_interval invalid: %w", err))
	} else if poll < 250*time.Millisecond {
		errs = append(errs, fmt.Errorf("clipboard.poll_interval too small (%s)", poll))
	}

	shutdownGrace, err := time.ParseDuration(strings.TrimSpace(cfg.System.ShutdownGraceDuration))
	if err != nil {
		errs = append(errs, fmt.Errorf("system.shutdown_grace_duration invalid: %w", err))
	} else if shutdownGrace <= 0 {
		errs = append(errs, fmt.Errorf("system.shutdown_grace_duration must be > 0 (got %s)", shutdownGrace))
	}

	shutdownForce, err := time.ParseDuration(strings.TrimSpace(cfg.System.ShutdownForceTimeout))
	if err != nil {
		errs = append(errs, fmt.Errorf("system.shutdown_force_timeout invalid: %w", err))
	} else if shutdownForce <= 0 {
		errs = append(errs, fmt.Errorf("system.shutdown_force_timeout must be > 0 (got %s)", shutdownForce))
	}

	// Required base paths
	dropAbs, err := expandPath(cfg.Paths.DropFolder)
	if err != nil {
		errs = append(errs, fmt.Errorf("paths.drop_folder: %w", err))
	} else if dropAbs == "" {
		errs = append(errs, errors.New("paths.drop_folder is required"))
	}

	stateAbs, err := expandPath(cfg.Paths.StateDir)
	if err != nil {
		errs = append(errs, fmt.Errorf("paths.state_dir: %w", err))
	} else if stateAbs == "" {
		errs = append(errs, errors.New("paths.state_dir is required"))
	}

	// Destinations (required)
	moviesAbs, err := expandPath(cfg.Destinations.DestDirMovies)
	if err != nil {
		errs = append(errs, fmt.Errorf("destinations.dest_dir_movies: %w", err))
	} else if moviesAbs == "" {
		errs = append(errs, errors.New("destinations.dest_dir_movies is required"))
	}
	showsAbs, err := expandPath(cfg.Destinations.DestDirShows)
	if err != nil {
		errs = append(errs, fmt.Errorf("destinations.dest_dir_shows: %w", err))
	} else if showsAbs == "" {
		errs = append(errs, errors.New("destinations.dest_dir_shows is required"))
	}

	// History file path (for logger persistence)
	hf := strings.TrimSpace(cfg.Logging.HistoryFile)
	if hf == "" {
		hf = defaultHistoryFile
	}
	historyAbs := ""
	if filepath.IsAbs(hf) || strings.HasPrefix(hf, "~") || strings.Contains(hf, "$") {
		historyAbs, err = expandPath(hf)
	} else {
		historyAbs, err = expandPath(filepath.Join(stateAbs, hf))
	}
	if err != nil {
		errs = append(errs, fmt.Errorf("logging.history_file: %w", err))
	} else if historyAbs == "" {
		errs = append(errs, errors.New("logging.history_file is empty after expansion"))
	}

	if cfg.Features.EnableProcessing {
		// Media extensions are required for Go processing.
		if len(cfg.Media.MainMediaExtensions) == 0 {
			errs = append(errs, errors.New("media.main_media_extensions is required and must be non-empty when processing is enabled"))
		}
	}

	// Validate extension formatting (when provided)
	validateExtList := func(fieldName string, exts []string) {
		for i, raw := range exts {
			ext := strings.TrimSpace(raw)
			if ext == "" {
				errs = append(errs, fmt.Errorf("%s[%d] is empty", fieldName, i))
				continue
			}
			if !strings.HasPrefix(ext, ".") {
				errs = append(errs, fmt.Errorf("%s[%d] must start with '.' (got %q)", fieldName, i, ext))
			}
		}
	}
	if len(cfg.Media.MainMediaExtensions) > 0 {
		validateExtList("media.main_media_extensions", cfg.Media.MainMediaExtensions)
	}
	if len(cfg.Media.AssociatedFileExtensions) > 0 {
		validateExtList("media.associated_file_extensions", cfg.Media.AssociatedFileExtensions)
	}

	// Torrent config
	transRemoteAbs := ""

	torrentOn := cfg.Features.EnableTorrentAutomation && cfg.Torrent.Enabled

	if torrentOn {
		if strings.TrimSpace(cfg.Torrent.TransmissionRemotePath) == "" {
			transRemoteAbs = "transmission-remote"
		} else {
			transRemoteAbs, err = expandPath(cfg.Torrent.TransmissionRemotePath)
			if err != nil {
				errs = append(errs, fmt.Errorf("torrent.transmission_remote_path: %w", err))
			} else if transRemoteAbs == "" {
				errs = append(errs, errors.New("torrent.transmission_remote_path is empty after expansion"))
			}
		}

		if strings.TrimSpace(cfg.Torrent.Host) == "" {
			errs = append(errs, errors.New("torrent.host is required when torrent automation is enabled (e.g. \"localhost:9091\")"))
		}
	}

	// Fail early if parsing/expansion produced errors.
	if len(errs) > 0 {
		return nil, joinErrors(errs)
	}

	// Directory creation / existence checks
	if cfg.System.AutoCreateMissingDirs {
		dirs := []string{dropAbs, stateAbs}
		if !cfg.System.DeferDestinationChecks {
			dirs = append(dirs, moviesAbs, showsAbs)
		}
		for _, dir := range dirs {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				errs = append(errs, fmt.Errorf("failed to create directory '%s': %w", dir, err))
			}
		}
		if historyAbs != "" {
			if err := os.MkdirAll(filepath.Dir(historyAbs), 0o755); err != nil {
				errs = append(errs, fmt.Errorf("failed to create history directory '%s': %w", filepath.Dir(historyAbs), err))
			}
		}
	} else {
		checkDir := func(name, dir string) {
			st, err := os.Stat(dir)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", name, err))
			} else if !st.IsDir() {
				errs = append(errs, fmt.Errorf("%s is not a directory: %s", name, dir))
			}
		}
		checkDir("paths.drop_folder", dropAbs)
		checkDir("paths.state_dir", stateAbs)
		if !cfg.System.DeferDestinationChecks {
			checkDir("destinations.dest_dir_movies", moviesAbs)
			checkDir("destinations.dest_dir_shows", showsAbs)
		}
	}

	// Optional: validate transmission-remote path if explicitly provided
	if torrentOn && strings.TrimSpace(cfg.Torrent.TransmissionRemotePath) != "" {
		st, err := os.Stat(transRemoteAbs)
		if err != nil {
			errs = append(errs, fmt.Errorf("torrent.transmission_remote_path: %w", err))
		} else if st.IsDir() {
			errs = append(errs, fmt.Errorf("torrent.transmission_remote_path is a directory, expected file: %s", transRemoteAbs))
		}
	}

	if len(errs) > 0 {
		return nil, joinErrors(errs)
	}

	return &Resolved{
		ConfigPathAbs: cfgPathAbs,

		DropFolderAbs: dropAbs,
		StateDirAbs:   stateAbs,

		DestDirMoviesAbs: moviesAbs,
		DestDirShowsAbs:  showsAbs,

		DropSettleDuration:    settle,
		ClipboardPollInterval: poll,
		DoneNotificationMode:  doneNotificationMode,
		ShutdownGraceDuration: shutdownGrace,
		ShutdownForceTimeout:  shutdownForce,

		TransmissionRemoteAbs: transRemoteAbs,

		ConsoleLogLevel: cfg.Logging.ConsoleLevel,
		HistoryLogLevel: cfg.Logging.HistoryLevel,
		HistoryFileAbs:  historyAbs,

		MainMediaExtensions:      append([]string(nil), cfg.Media.MainMediaExtensions...),
		AssociatedFileExtensions: append([]string(nil), cfg.Media.AssociatedFileExtensions...),

		MediaTagBlacklist: append([]string(nil), cfg.Naming.MediaTagBlacklist...),
	}, nil
}

func expandPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", nil
	}

	// Expand environment variables like $HOME
	p = os.ExpandEnv(p)

	// Expand leading ~
	if strings.HasPrefix(p, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand ~ in path: %w", err)
		}
		switch {
		case p == "~":
			p = home
		case strings.HasPrefix(p, "~/"):
			p = filepath.Join(home, p[2:])
		}
	}

	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	return abs, nil
}

func joinErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return fmt.Errorf("config validation failed: %w", errs[0])
	}
	return fmt.Errorf("config validation failed: %w", errors.Join(errs...))
}
