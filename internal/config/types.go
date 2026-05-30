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
	AutoCreateMissingDirs  bool   `toml:"auto_create_missing_dirs"`
	DeferDestinationChecks bool   `toml:"defer_destination_checks"`
	DoneNotificationMode   string `toml:"done_notification_mode"`
	ShutdownGraceDuration  string `toml:"shutdown_grace_duration"`
	ShutdownForceTimeout   string `toml:"shutdown_force_timeout"`
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
