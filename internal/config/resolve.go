package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mtn-man/mintmedia/internal/logging"
)

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

	doneNotificationMode := strings.ToLower(strings.TrimSpace(cfg.System.DoneNotificationMode))
	switch doneNotificationMode {
	case "per_file", "per_job", "off":
		cfg.System.DoneNotificationMode = doneNotificationMode
	default:
		errs = append(errs, fmt.Errorf(
			"system.done_notification_mode: invalid value %q (allowed: \"per_file\", \"per_job\", \"off\")",
			cfg.System.DoneNotificationMode,
		))
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

	// Destinations must not overlap.
	if moviesAbs != "" && showsAbs != "" {
		if err := validateDestinationSeparation(moviesAbs, showsAbs); err != nil {
			errs = append(errs, err)
		}
	}

	// History file path (for logger persistence)
	hf := strings.TrimSpace(cfg.Logging.HistoryFile)
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
	torrentOn := cfg.Features.EnableTorrentAutomation && cfg.Torrent.Enabled

	if torrentOn {
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

		ConsoleLogLevel: cfg.Logging.ConsoleLevel,
		HistoryLogLevel: cfg.Logging.HistoryLevel,
		HistoryFileAbs:  historyAbs,

		MainMediaExtensions:      append([]string(nil), cfg.Media.MainMediaExtensions...),
		AssociatedFileExtensions: append([]string(nil), cfg.Media.AssociatedFileExtensions...),

		MediaTagBlacklist: append([]string(nil), cfg.Naming.MediaTagBlacklist...),
	}, nil
}

// validateDestinationSeparation returns an error if the two destination paths
// are identical or if one is an ancestor of the other. Either condition would
// cause movies and shows to be written into the same directory tree.
func validateDestinationSeparation(moviesAbs, showsAbs string) error {
	if moviesAbs == showsAbs {
		return fmt.Errorf("destinations.dest_dir_movies and destinations.dest_dir_shows must be different directories (both resolve to %q)", moviesAbs)
	}
	sep := string(filepath.Separator)
	if strings.HasPrefix(showsAbs, moviesAbs+sep) {
		return fmt.Errorf("destinations.dest_dir_shows (%q) must not be inside destinations.dest_dir_movies (%q)", showsAbs, moviesAbs)
	}
	if strings.HasPrefix(moviesAbs, showsAbs+sep) {
		return fmt.Errorf("destinations.dest_dir_movies (%q) must not be inside destinations.dest_dir_shows (%q)", moviesAbs, showsAbs)
	}
	return nil
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
