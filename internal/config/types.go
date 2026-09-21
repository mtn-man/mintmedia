package config

import "time"

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

// Paths holds the drop folder and state directory locations.
type Paths struct {
	DropFolder string `toml:"drop_folder"`
	StateDir   string `toml:"state_dir"`
}

// Destinations holds the library root directories Plan/Apply move files into.
type Destinations struct {
	DestDirMovies string `toml:"dest_dir_movies"`
	DestDirShows  string `toml:"dest_dir_shows"`
}

// Features toggles optional subsystems.
type Features struct {
	EnableTorrentAutomation    bool `toml:"enable_torrent_automation"`
	EnableProcessing           bool `toml:"enable_processing"`
	EnableMetadataTitleTagging bool `toml:"enable_metadata_title_tagging"`
}

// Logging configures the console/history logging sinks.
type Logging struct {
	// Optional. Defaults to INFO.
	ConsoleLevel string `toml:"console_level"`
	// Optional. Defaults to WARN.
	HistoryLevel string `toml:"history_level"`
	// Optional. If relative, resolved under paths.state_dir.
	HistoryFile string `toml:"history_file"`
}

// System holds process-level behavior settings.
type System struct {
	AutoCreateMissingDirs  bool   `toml:"auto_create_missing_dirs"`
	DeferDestinationChecks bool   `toml:"defer_destination_checks"`
	DoneNotificationMode   string `toml:"done_notification_mode"`
	ShutdownGraceDuration  string `toml:"shutdown_grace_duration"`
	ShutdownForceTimeout   string `toml:"shutdown_force_timeout"`
}

// Watch configures the drop folder filesystem watcher.
type Watch struct {
	// e.g. "3s"
	DropSettleDuration string `toml:"drop_settle_duration"`
}

// Clipboard configures magnet-link detection via clipboard polling.
type Clipboard struct {
	Enabled bool `toml:"enabled"`
	// e.g. "1s"
	PollInterval string `toml:"poll_interval"`
}

// Torrent configures the Transmission JSON-RPC integration.
type Torrent struct {
	Enabled bool `toml:"enabled"`

	// e.g. "localhost:9091"
	Host string `toml:"host"`

	// Optional. If set, passed as "--auth user:pass" (or your chosen scheme later).
	Auth string `toml:"auth"`

	// Optional. Defaults to false.
	AutoCleanupCompletedTorrents bool `toml:"auto_cleanup_completed_torrents"`
}

// Media configures which file extensions are treated as main/associated media.
type Media struct {
	// Required when processing is enabled. Extensions should include the leading dot (e.g. ".mkv").
	MainMediaExtensions []string `toml:"main_media_extensions"`

	// Optional (may be empty). Extensions should include the leading dot (e.g. ".srt").
	AssociatedFileExtensions []string `toml:"associated_file_extensions"`
}

// Naming configures release-name cleanup during title parsing.
type Naming struct {
	// Additional regex patterns (strings) to strip from release names, on top
	// of mintmedia's built-in defaults (resolution/codec/source tags). This
	// list is additive, not a replacement -- see resolveMediaTagBlacklist.
	MediaTagBlacklist []string `toml:"media_tag_blacklist"`

	// ResolutionAware, when true, re-appends the release resolution detected
	// from the source filename to the final sorted name as a " - <res>" suffix
	// (e.g. "Movie (2020) - 1080p.mkv"). Off by default.
	ResolutionAware bool `toml:"resolution_aware"`

	// PreserveEpisodeTitles, when true, recognizes an already-clean episode
	// title trailing the season/episode token in a show's source filename
	// (e.g. "Bad Optics" in "Lanterns - S01E06 - Bad Optics.mkv") and keeps
	// it in the sorted name and embedded metadata tag instead of discarding
	// it. Only a filename that already uses the " - " separator (the
	// Plex/Jellyfin-recommended naming convention) with no other release-tag
	// text qualifies -- a dot/underscore-style scene name, or one with any
	// other release-tag junk trailing the title, is left exactly as today
	// (no title kept). Off by default.
	PreserveEpisodeTitles bool `toml:"preserve_episode_titles"`
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

	ConsoleLogLevel string
	HistoryLogLevel string
	HistoryFileAbs  string

	// Copy of the TOML lists (normalized/validated).
	MainMediaExtensions      []string
	AssociatedFileExtensions []string

	// Naming patterns passed to Go processor.
	MediaTagBlacklist []string

	// CustomMediaTagBlacklistCount is how many of MediaTagBlacklist's patterns
	// came from naming.media_tag_blacklist rather than the built-in defaults.
	// Display-only: the processor always receives the merged MediaTagBlacklist.
	CustomMediaTagBlacklistCount int

	// ResolutionAware mirrors naming.resolution_aware.
	ResolutionAware bool

	// PreserveEpisodeTitles mirrors naming.preserve_episode_titles.
	PreserveEpisodeTitles bool

	// Directories that didn't exist before this Load call and were created
	// because auto_create_missing_dirs is true. Empty when nothing was created.
	CreatedDirs []string

	AutoCleanupCompletedTorrents bool

	EnableMetadataTitleTagging bool

	EnableProcessing bool

	// TorrentEnabled is features.enable_torrent_automation && torrent.enabled --
	// the single "is torrent automation actually on" flag, computed once here
	// rather than re-derived by each caller.
	TorrentEnabled bool
	TorrentHost    string
	TorrentAuth    string

	ClipboardEnabled bool

	DeferDestinationChecks bool
}
