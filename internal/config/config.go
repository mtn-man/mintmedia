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
)

const (
	// DefaultConfigPathRel is relative to the user's home directory.
	DefaultConfigPathRel = ".config/mintmedia/config.toml"

	// Defaults (opinionated for reliability).
	defaultMaxConcurrentProcessors = 1
	defaultLogLevel                = "INFO"
	defaultClipboardPollInterval   = 1 * time.Second
	defaultDropSettleDuration      = 3 * time.Second

	// State file defaults (relative to state_dir unless absolute).
	defaultHistoryFile  = "history.log"
	defaultErrorDirName = "error"
)

// Config is the decoded TOML configuration (pre-normalization).
type Config struct {
	Paths        Paths        `toml:"paths"`
	Destinations Destinations `toml:"destinations"`
	Features     Features     `toml:"features"`
	System       System       `toml:"system"`
	Watch        Watch        `toml:"watch"`
	Clipboard    Clipboard    `toml:"clipboard"`
	Torrent      Torrent      `toml:"torrent"`
	Processing   Processing   `toml:"processing"`
	Media        Media        `toml:"media"`
	Naming       Naming       `toml:"naming"`
}

type Paths struct {
	DropFolder string `toml:"drop_folder"`
	StateDir   string `toml:"state_dir"`
	// Optional; if empty defaults to state_dir + "/error"
	ErrorDir string `toml:"error_dir"`
}

type Destinations struct {
	DestDirMovies string `toml:"dest_dir_movies"`
	DestDirShows  string `toml:"dest_dir_shows"`
}

type Features struct {
	EnableTorrentAutomation bool `toml:"enable_torrent_automation"`
	EnableProcessing        bool `toml:"enable_processing"`
}

type System struct {
	AutoCreateMissingDirs   bool   `toml:"auto_create_missing_dirs"`
	DeferDestinationChecks  bool   `toml:"defer_destination_checks"`
	MaxConcurrentProcessors int    `toml:"max_concurrent_processors"`
	LogLevel                string `toml:"log_level"`
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

// Processing is still present for things like history_file and optional legacy worker.
// In Go-primary mode, worker_script is optional.
type Processing struct {
	// Optional (legacy). If provided, can be used for --smoke-worker, etc.
	WorkerScript string `toml:"worker_script"`

	// Optional. If empty defaults to "history.log" under state_dir.
	HistoryFile string `toml:"history_file"`
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
	ErrorDirAbs   string

	DestDirMoviesAbs string
	DestDirShowsAbs  string

	DropSettleDuration    time.Duration
	ClipboardPollInterval time.Duration

	TransmissionRemoteAbs string

	// Optional legacy worker (may be empty)
	WorkerScriptAbs string

	HistoryFileAbs string

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
	if _, err := toml.DecodeFile(cfgPathAbs, &cfg); err != nil {
		return nil, nil, fmt.Errorf("parse TOML (%s): %w", cfgPathAbs, err)
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
	if strings.TrimSpace(cfg.System.LogLevel) == "" {
		cfg.System.LogLevel = defaultLogLevel
	}

	// Watch defaults
	if strings.TrimSpace(cfg.Watch.DropSettleDuration) == "" {
		cfg.Watch.DropSettleDuration = defaultDropSettleDuration.String()
	}

	// Clipboard defaults
	if strings.TrimSpace(cfg.Clipboard.PollInterval) == "" {
		cfg.Clipboard.PollInterval = defaultClipboardPollInterval.String()
	}

	// Processing defaults
	if strings.TrimSpace(cfg.Processing.HistoryFile) == "" {
		cfg.Processing.HistoryFile = defaultHistoryFile
	}

	// Torrent defaults
	if cfg.Torrent.AutoCleanupCompletedTorrents == nil {
		v := false
		cfg.Torrent.AutoCleanupCompletedTorrents = &v
	}
}

func normalizeAndValidate(cfg *Config, cfgPathAbs string) (*Resolved, error) {
	var errs []error

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

	// Error dir (optional)
	errorDir := strings.TrimSpace(cfg.Paths.ErrorDir)
	if errorDir == "" {
		errorDir = filepath.Join(stateAbs, defaultErrorDirName)
	}
	errorAbs, err := expandPath(errorDir)
	if err != nil {
		errs = append(errs, fmt.Errorf("paths.error_dir: %w", err))
	} else if errorAbs == "" {
		errs = append(errs, errors.New("paths.error_dir is empty after expansion"))
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

	// History file (required if processing enabled)
	historyAbs := ""
	if cfg.Features.EnableProcessing {
		hf := strings.TrimSpace(cfg.Processing.HistoryFile)
		if hf == "" {
			hf = defaultHistoryFile
		}
		if filepath.IsAbs(hf) || strings.HasPrefix(hf, "~") || strings.Contains(hf, "$") {
			historyAbs, err = expandPath(hf)
		} else {
			historyAbs, err = expandPath(filepath.Join(stateAbs, hf))
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("processing.history_file: %w", err))
		} else if historyAbs == "" {
			errs = append(errs, errors.New("processing.history_file is empty after expansion"))
		}

		// Media extensions are required for Go processing.
		if len(cfg.Media.MainMediaExtensions) == 0 {
			errs = append(errs, errors.New("media.main_media_extensions is required and must be non-empty when processing is enabled"))
		}
	}

	// Optional legacy worker: resolve & validate only if provided.
	workerAbs := ""
	if strings.TrimSpace(cfg.Processing.WorkerScript) != "" {
		workerAbs, err = expandPath(cfg.Processing.WorkerScript)
		if err != nil {
			errs = append(errs, fmt.Errorf("processing.worker_script: %w", err))
		} else if workerAbs == "" {
			errs = append(errs, errors.New("processing.worker_script is empty after expansion"))
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
		dirs := []string{dropAbs, stateAbs, errorAbs}
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
		checkDir("paths.error_dir", errorAbs)
		if !cfg.System.DeferDestinationChecks {
			checkDir("destinations.dest_dir_movies", moviesAbs)
			checkDir("destinations.dest_dir_shows", showsAbs)
		}
	}

	// Validate legacy worker script only if provided.
	if workerAbs != "" {
		st, err := os.Stat(workerAbs)
		if err != nil {
			errs = append(errs, fmt.Errorf("processing.worker_script not found: %s", workerAbs))
		} else if st.IsDir() {
			errs = append(errs, fmt.Errorf("processing.worker_script is a directory, expected file: %s", workerAbs))
		} else if st.Mode()&0o111 == 0 {
			errs = append(errs, fmt.Errorf("processing.worker_script not executable: %s", workerAbs))
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

	// Persist derived error_dir back into cfg for transparency
	cfg.Paths.ErrorDir = errorAbs

	if len(errs) > 0 {
		return nil, joinErrors(errs)
	}

	return &Resolved{
		ConfigPathAbs: cfgPathAbs,

		DropFolderAbs: dropAbs,
		StateDirAbs:   stateAbs,
		ErrorDirAbs:   errorAbs,

		DestDirMoviesAbs: moviesAbs,
		DestDirShowsAbs:  showsAbs,

		DropSettleDuration:    settle,
		ClipboardPollInterval: poll,

		TransmissionRemoteAbs: transRemoteAbs,

		WorkerScriptAbs: workerAbs,
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
