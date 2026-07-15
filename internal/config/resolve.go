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

// defaultMediaTagBlacklist is the built-in set of release-tag patterns
// stripped from parsed titles. It always applies; naming.media_tag_blacklist
// in the user's config is additive on top of this list, not a replacement
// for it -- stripping resolution/codec/source tags is core to how mintmedia
// derives a clean title, not an opt-in feature.
var defaultMediaTagBlacklist = []string{
	"2160p",
	"1080p",
	"720p",
	"480p",
	"x265",
	"x264",
	"hevc",
	"avc",
	"av1",
	"xvid",
	"h\\.264",
	"h\\.265",
	"web[- ]?dl",
	"webrip",
	"bluray",
	"bdrip",
	"brrip",
	"hdrip",
	"hdtv",
	"dvdrip",
	"aac",
	"ac3",
	"dts",
	"dd5\\.1",
	"atmos",
	"truehd",
	"hdr10?",
	"dolby[ .]?vision",
}

// resolveMediaTagBlacklist merges the built-in defaults with any
// user-supplied patterns from naming.media_tag_blacklist.
func resolveMediaTagBlacklist(user []string) []string {
	merged := make([]string, 0, len(defaultMediaTagBlacklist)+len(user))
	merged = append(merged, defaultMediaTagBlacklist...)
	merged = append(merged, user...)
	return merged
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

	doneNotificationMode := strings.ToLower(strings.TrimSpace(cfg.System.DoneNotificationMode))
	switch doneNotificationMode {
	case "per_file", "per_job", "off":
		cfg.System.DoneNotificationMode = doneNotificationMode
	default:
		errs = append(errs, fmt.Errorf(
			"system.done_notification_mode: invalid value %q (allowed: %q, %q, %q)",
			cfg.System.DoneNotificationMode, "per_file", "per_job", "off",
		))
	}

	// Durations
	settle, _ := parseDurationFieldMin(&errs, "watch.drop_settle_duration", cfg.Watch.DropSettleDuration, "3s", 500*time.Millisecond)
	poll, _ := parseDurationFieldMin(&errs, "clipboard.poll_interval", cfg.Clipboard.PollInterval, "1s", 250*time.Millisecond)
	shutdownGrace, _ := parseDurationFieldPositive(&errs, "system.shutdown_grace_duration", cfg.System.ShutdownGraceDuration, "10m")
	shutdownForce, _ := parseDurationFieldPositive(&errs, "system.shutdown_force_timeout", cfg.System.ShutdownForceTimeout, "15s")

	// Required base paths
	dropAbs, err := expandPath(cfg.Paths.DropFolder)
	if err != nil {
		errs = append(errs, fmt.Errorf("paths.drop_folder: %w", err))
	} else if dropAbs == "" {
		errs = append(errs, errors.New("paths.drop_folder is required (e.g. \"~/Downloads/mintmedia-drop\")"))
	}

	stateAbs, err := expandPath(cfg.Paths.StateDir)
	if err != nil {
		errs = append(errs, fmt.Errorf("paths.state_dir: %w", err))
	} else if stateAbs == "" {
		errs = append(errs, errors.New("paths.state_dir is required (e.g. \"~/.local/state/mintmedia\")"))
	}

	// Destinations (required)
	moviesAbs, err := expandPath(cfg.Destinations.DestDirMovies)
	if err != nil {
		errs = append(errs, fmt.Errorf("destinations.dest_dir_movies: %w", err))
	} else if moviesAbs == "" {
		errs = append(errs, errors.New("destinations.dest_dir_movies is required (e.g. \"~/Movies\")"))
	}
	showsAbs, err := expandPath(cfg.Destinations.DestDirShows)
	if err != nil {
		errs = append(errs, fmt.Errorf("destinations.dest_dir_shows: %w", err))
	} else if showsAbs == "" {
		errs = append(errs, errors.New("destinations.dest_dir_shows is required (e.g. \"~/TV Shows\")"))
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
				errs = append(errs, fmt.Errorf("%s[%d]: %q must start with a dot (e.g. \".mkv\")", fieldName, i, ext))
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
		return nil, formatConfigError(cfgPathAbs, errs...)
	}

	// Directory creation / existence checks
	var createdDirs []string
	if cfg.System.AutoCreateMissingDirs {
		dirs := []string{dropAbs, stateAbs}
		if !cfg.System.DeferDestinationChecks {
			dirs = append(dirs, moviesAbs, showsAbs)
		}
		mkdirTracked := func(dir string) {
			if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
				createdDirs = append(createdDirs, dir)
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				errs = append(errs, fmt.Errorf("failed to create directory %q: %w", dir, err))
			}
		}
		for _, dir := range dirs {
			mkdirTracked(dir)
		}
		if historyAbs != "" {
			mkdirTracked(filepath.Dir(historyAbs))
		}
	} else {
		checkDir := func(name, dir string) {
			st, err := os.Stat(dir)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", name, err))
			} else if !st.IsDir() {
				errs = append(errs, fmt.Errorf("%s is not a directory: %q", name, dir))
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
		return nil, formatConfigError(cfgPathAbs, errs...)
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

		MediaTagBlacklist: resolveMediaTagBlacklist(cfg.Naming.MediaTagBlacklist),

		CreatedDirs: createdDirs,
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

// parseDurationField parses a duration-valued config field, appending a
// hinted error to errs and returning ok=false on failure. Prefer
// parseDurationFieldMin/parseDurationFieldPositive below when the field also
// has a bound; use this directly only for fields with no bound to check.
func parseDurationField(errs *[]error, field, raw, example string) (d time.Duration, ok bool) {
	d, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s: invalid duration %q (e.g. %q): %w", field, raw, example, err))
		return 0, false
	}
	return d, true
}

// parseDurationFieldMin is parseDurationField plus a minimum-value check,
// appending a single "too small" error when the parsed duration is below min.
func parseDurationFieldMin(errs *[]error, field, raw, example string, min time.Duration) (d time.Duration, ok bool) {
	d, ok = parseDurationField(errs, field, raw, example)
	if !ok {
		return d, false
	}
	if d < min {
		*errs = append(*errs, fmt.Errorf("%s: %s is too small (minimum %s)", field, d, min))
		return d, false
	}
	return d, true
}

// parseDurationFieldPositive is parseDurationField plus a must-be-positive
// check, appending a single error when the parsed duration is <= 0.
func parseDurationFieldPositive(errs *[]error, field, raw, example string) (d time.Duration, ok bool) {
	d, ok = parseDurationField(errs, field, raw, example)
	if !ok {
		return d, false
	}
	if d <= 0 {
		*errs = append(*errs, fmt.Errorf("%s: must be > 0 (got %s)", field, d))
		return d, false
	}
	return d, true
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

// configError is a user-facing error that names the config file the problem
// was found in, so a raw error printed straight to stderr still tells the
// reader which file to open. A single error is inlined; multiple errors are
// rendered as a bulleted list so each is easy to scan. It implements
// Unwrap() []error so errors.Is/errors.As can still reach the underlying
// errors, matching what a single wrapped error would provide.
type configError struct {
	cfgPathAbs string
	errs       []error
}

func (e *configError) Error() string {
	if len(e.errs) == 1 {
		return fmt.Sprintf("config error in %s: %s", e.cfgPathAbs, e.errs[0])
	}
	var b strings.Builder
	fmt.Fprintf(&b, "config error in %s:", e.cfgPathAbs)
	for _, err := range e.errs {
		fmt.Fprintf(&b, "\n  - %s", err)
	}
	return b.String()
}

func (e *configError) Unwrap() []error {
	return e.errs
}

// formatConfigError builds a configError; see its docs for the rendering.
func formatConfigError(cfgPathAbs string, errs ...error) error {
	if len(errs) == 0 {
		return nil
	}
	return &configError{cfgPathAbs: cfgPathAbs, errs: errs}
}
