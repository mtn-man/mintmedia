package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoad_MinimalProcessingConfig(t *testing.T) {
	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	state := filepath.Join(root, "state")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")

	toml := fmt.Sprintf(`
[paths]
drop_folder = %q
state_dir = %q

[destinations]
dest_dir_movies = %q
dest_dir_shows = %q

[features]
enable_processing = true
enable_torrent_automation = false

[system]
auto_create_missing_dirs = true
defer_destination_checks = false

[media]
main_media_extensions = [".mkv"]
associated_file_extensions = [".srt"]
`, drop, state, movies, shows)

	cfgPath := writeConfigFile(t, root, toml)
	_, res, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if res == nil {
		t.Fatalf("expected resolved config, got nil")
	}

	if res.DropFolderAbs != drop {
		t.Fatalf("DropFolderAbs = %q, want %q", res.DropFolderAbs, drop)
	}
	if res.StateDirAbs != state {
		t.Fatalf("StateDirAbs = %q, want %q", res.StateDirAbs, state)
	}
	wantHistory := filepath.Join(state, "history.jsonl")
	if res.HistoryFileAbs != wantHistory {
		t.Fatalf("HistoryFileAbs = %q, want %q", res.HistoryFileAbs, wantHistory)
	}
	if res.ConsoleLogLevel != "INFO" {
		t.Fatalf("ConsoleLogLevel = %q, want %q", res.ConsoleLogLevel, "INFO")
	}
	if res.HistoryLogLevel != "WARN" {
		t.Fatalf("HistoryLogLevel = %q, want %q", res.HistoryLogLevel, "WARN")
	}

	for _, dir := range []string{drop, state, movies, shows} {
		st, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("expected dir to exist (%s): %v", dir, err)
		}
		if !st.IsDir() {
			t.Fatalf("expected dir (%s), got file", dir)
		}
	}
}

func TestLoad_LoggingConfigOverridesDefaults(t *testing.T) {
	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	state := filepath.Join(root, "state")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")

	toml := fmt.Sprintf(`
[paths]
drop_folder = %q
state_dir = %q

[destinations]
dest_dir_movies = %q
dest_dir_shows = %q

[features]
enable_processing = true
enable_torrent_automation = false

[system]
auto_create_missing_dirs = true

[media]
main_media_extensions = [".mkv"]

[logging]
console_level = "error"
history_level = "info"
history_file = "ops/history.jsonl"
`, drop, state, movies, shows)

	cfgPath := writeConfigFile(t, root, toml)
	_, res, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if res.ConsoleLogLevel != "ERROR" {
		t.Fatalf("ConsoleLogLevel = %q, want %q", res.ConsoleLogLevel, "ERROR")
	}
	if res.HistoryLogLevel != "INFO" {
		t.Fatalf("HistoryLogLevel = %q, want %q", res.HistoryLogLevel, "INFO")
	}
	wantHistory := filepath.Join(state, "ops", "history.jsonl")
	if res.HistoryFileAbs != wantHistory {
		t.Fatalf("HistoryFileAbs = %q, want %q", res.HistoryFileAbs, wantHistory)
	}
}

func TestLoad_TorrentEnabledMissingHost(t *testing.T) {
	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	state := filepath.Join(root, "state")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")

	toml := fmt.Sprintf(`
[paths]
drop_folder = %q
state_dir = %q

[destinations]
dest_dir_movies = %q
dest_dir_shows = %q

[features]
enable_processing = false
enable_torrent_automation = true

[system]
auto_create_missing_dirs = true

[torrent]
enabled = true
host = ""
`, drop, state, movies, shows)

	cfgPath := writeConfigFile(t, root, toml)
	_, _, err := Load(cfgPath)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "torrent.host is required") {
		t.Fatalf("expected host error, got: %v", err)
	}
}

func TestLoad_RejectsLegacyProcessingHistoryFile(t *testing.T) {
	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	state := filepath.Join(root, "state")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")

	toml := fmt.Sprintf(`
[paths]
drop_folder = %q
state_dir = %q

[destinations]
dest_dir_movies = %q
dest_dir_shows = %q

[features]
enable_processing = true
enable_torrent_automation = false

[system]
auto_create_missing_dirs = true

[media]
main_media_extensions = [".mkv"]

[processing]
history_file = "history.log"
`, drop, state, movies, shows)

	cfgPath := writeConfigFile(t, root, toml)
	_, _, err := Load(cfgPath)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "processing.history_file has been removed") {
		t.Fatalf("expected processing.history_file removal error, got: %v", err)
	}
}

func TestLoad_RejectsLegacyProcessingSection(t *testing.T) {
	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	state := filepath.Join(root, "state")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")

	toml := fmt.Sprintf(`
[paths]
drop_folder = %q
state_dir = %q

[destinations]
dest_dir_movies = %q
dest_dir_shows = %q

[features]
enable_processing = true
enable_torrent_automation = false

[system]
auto_create_missing_dirs = true

[media]
main_media_extensions = [".mkv"]

[processing]
`, drop, state, movies, shows)

	cfgPath := writeConfigFile(t, root, toml)
	_, _, err := Load(cfgPath)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "[processing] section has been removed") {
		t.Fatalf("expected processing section removal error, got: %v", err)
	}
}

func TestLoad_TorrentRemotePathExpands(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	remote := filepath.Join(binDir, "tx-remote")
	if err := os.WriteFile(remote, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write remote: %v", err)
	}

	drop := filepath.Join(root, "drop")
	state := filepath.Join(root, "state")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")

	t.Setenv("MINTMEDIA_TESTROOT", root)
	toml := fmt.Sprintf(`
[paths]
drop_folder = %q
state_dir = %q

[destinations]
dest_dir_movies = %q
dest_dir_shows = %q

[features]
enable_processing = false
enable_torrent_automation = true

[system]
auto_create_missing_dirs = true

[torrent]
enabled = true
host = "localhost:9091"
transmission_remote_path = "$MINTMEDIA_TESTROOT/bin/tx-remote"
`, drop, state, movies, shows)

	cfgPath := writeConfigFile(t, root, toml)
	_, res, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if res.TransmissionRemoteAbs != remote {
		t.Fatalf("TransmissionRemoteAbs = %q, want %q", res.TransmissionRemoteAbs, remote)
	}
}

func TestLoad_DefaultTransmissionRemoteWhenEmpty(t *testing.T) {
	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	state := filepath.Join(root, "state")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")

	toml := fmt.Sprintf(`
[paths]
drop_folder = %q
state_dir = %q

[destinations]
dest_dir_movies = %q
dest_dir_shows = %q

[features]
enable_processing = false
enable_torrent_automation = true

[system]
auto_create_missing_dirs = true

[torrent]
enabled = true
host = "localhost:9091"
`, drop, state, movies, shows)

	cfgPath := writeConfigFile(t, root, toml)
	_, res, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if res.TransmissionRemoteAbs != "transmission-remote" {
		t.Fatalf("TransmissionRemoteAbs = %q, want %q", res.TransmissionRemoteAbs, "transmission-remote")
	}
}

func TestLoad_TorrentAutoCleanupDefaultsFalseWhenOmitted(t *testing.T) {
	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	state := filepath.Join(root, "state")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")

	toml := fmt.Sprintf(`
[paths]
drop_folder = %q
state_dir = %q

[destinations]
dest_dir_movies = %q
dest_dir_shows = %q

[features]
enable_processing = false
enable_torrent_automation = true

[system]
auto_create_missing_dirs = true

[torrent]
enabled = true
host = "localhost:9091"
`, drop, state, movies, shows)

	cfgPath := writeConfigFile(t, root, toml)
	cfg, _, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Torrent.AutoCleanupCompletedTorrents == nil {
		t.Fatalf("AutoCleanupCompletedTorrents = nil, want non-nil default")
	}
	if *cfg.Torrent.AutoCleanupCompletedTorrents {
		t.Fatalf("AutoCleanupCompletedTorrents = %v, want false", *cfg.Torrent.AutoCleanupCompletedTorrents)
	}
}

func TestLoad_TorrentAutoCleanupCanBeDisabled(t *testing.T) {
	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	state := filepath.Join(root, "state")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")

	toml := fmt.Sprintf(`
[paths]
drop_folder = %q
state_dir = %q

[destinations]
dest_dir_movies = %q
dest_dir_shows = %q

[features]
enable_processing = false
enable_torrent_automation = true

[system]
auto_create_missing_dirs = true

[torrent]
enabled = true
host = "localhost:9091"
auto_cleanup_completed_torrents = false
`, drop, state, movies, shows)

	cfgPath := writeConfigFile(t, root, toml)
	cfg, _, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Torrent.AutoCleanupCompletedTorrents == nil {
		t.Fatalf("AutoCleanupCompletedTorrents = nil, want explicit false")
	}
	if *cfg.Torrent.AutoCleanupCompletedTorrents {
		t.Fatalf("AutoCleanupCompletedTorrents = %v, want false", *cfg.Torrent.AutoCleanupCompletedTorrents)
	}
}

func TestLoad_TorrentAutoCleanupCanBeEnabled(t *testing.T) {
	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	state := filepath.Join(root, "state")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")

	toml := fmt.Sprintf(`
[paths]
drop_folder = %q
state_dir = %q

[destinations]
dest_dir_movies = %q
dest_dir_shows = %q

[features]
enable_processing = false
enable_torrent_automation = true

[system]
auto_create_missing_dirs = true

[torrent]
enabled = true
host = "localhost:9091"
auto_cleanup_completed_torrents = true
`, drop, state, movies, shows)

	cfgPath := writeConfigFile(t, root, toml)
	cfg, _, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Torrent.AutoCleanupCompletedTorrents == nil {
		t.Fatalf("AutoCleanupCompletedTorrents = nil, want explicit true")
	}
	if !*cfg.Torrent.AutoCleanupCompletedTorrents {
		t.Fatalf("AutoCleanupCompletedTorrents = %v, want true", *cfg.Torrent.AutoCleanupCompletedTorrents)
	}
}

func TestLoad_DoneNotificationMode_DefaultsToPerFile(t *testing.T) {
	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	state := filepath.Join(root, "state")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")

	toml := fmt.Sprintf(`
[paths]
drop_folder = %q
state_dir = %q

[destinations]
dest_dir_movies = %q
dest_dir_shows = %q

[features]
enable_processing = false
enable_torrent_automation = false

[system]
auto_create_missing_dirs = true
`, drop, state, movies, shows)

	cfgPath := writeConfigFile(t, root, toml)
	cfg, res, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.System.DoneNotificationMode != "per_file" {
		t.Fatalf("DoneNotificationMode = %q, want %q", cfg.System.DoneNotificationMode, "per_file")
	}
	if res.DoneNotificationMode != "per_file" {
		t.Fatalf("Resolved DoneNotificationMode = %q, want %q", res.DoneNotificationMode, "per_file")
	}
}

func TestLoad_DoneNotificationMode_NormalizesPerJob(t *testing.T) {
	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	state := filepath.Join(root, "state")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")

	toml := fmt.Sprintf(`
[paths]
drop_folder = %q
state_dir = %q

[destinations]
dest_dir_movies = %q
dest_dir_shows = %q

[features]
enable_processing = false
enable_torrent_automation = false

[system]
auto_create_missing_dirs = true
done_notification_mode = "PER_JOB"
`, drop, state, movies, shows)

	cfgPath := writeConfigFile(t, root, toml)
	cfg, res, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.System.DoneNotificationMode != "per_job" {
		t.Fatalf("DoneNotificationMode = %q, want %q", cfg.System.DoneNotificationMode, "per_job")
	}
	if res.DoneNotificationMode != "per_job" {
		t.Fatalf("Resolved DoneNotificationMode = %q, want %q", res.DoneNotificationMode, "per_job")
	}
}

func TestLoad_DoneNotificationMode_Off(t *testing.T) {
	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	state := filepath.Join(root, "state")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")

	toml := fmt.Sprintf(`
[paths]
drop_folder = %q
state_dir = %q

[destinations]
dest_dir_movies = %q
dest_dir_shows = %q

[features]
enable_processing = false
enable_torrent_automation = false

[system]
auto_create_missing_dirs = true
done_notification_mode = "off"
`, drop, state, movies, shows)

	cfgPath := writeConfigFile(t, root, toml)
	cfg, res, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.System.DoneNotificationMode != "off" {
		t.Fatalf("DoneNotificationMode = %q, want %q", cfg.System.DoneNotificationMode, "off")
	}
	if res.DoneNotificationMode != "off" {
		t.Fatalf("Resolved DoneNotificationMode = %q, want %q", res.DoneNotificationMode, "off")
	}
}

func TestLoad_DoneNotificationMode_InvalidFails(t *testing.T) {
	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	state := filepath.Join(root, "state")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")

	toml := fmt.Sprintf(`
[paths]
drop_folder = %q
state_dir = %q

[destinations]
dest_dir_movies = %q
dest_dir_shows = %q

[features]
enable_processing = false
enable_torrent_automation = false

[system]
auto_create_missing_dirs = true
done_notification_mode = "loud"
`, drop, state, movies, shows)

	cfgPath := writeConfigFile(t, root, toml)
	_, _, err := Load(cfgPath)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "system.done_notification_mode") {
		t.Fatalf("expected done_notification_mode validation error, got: %v", err)
	}
}

func TestLoad_ShutdownDurations_Defaults(t *testing.T) {
	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	state := filepath.Join(root, "state")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")

	toml := fmt.Sprintf(`
[paths]
drop_folder = %q
state_dir = %q

[destinations]
dest_dir_movies = %q
dest_dir_shows = %q

[features]
enable_processing = false
enable_torrent_automation = false

[system]
auto_create_missing_dirs = true
`, drop, state, movies, shows)

	cfgPath := writeConfigFile(t, root, toml)
	cfg, res, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.System.ShutdownGraceDuration != "10m0s" {
		t.Fatalf("ShutdownGraceDuration = %q, want %q", cfg.System.ShutdownGraceDuration, "10m0s")
	}
	if cfg.System.ShutdownForceTimeout != "15s" {
		t.Fatalf("ShutdownForceTimeout = %q, want %q", cfg.System.ShutdownForceTimeout, "15s")
	}
	if res.ShutdownGraceDuration != 10*time.Minute {
		t.Fatalf("Resolved ShutdownGraceDuration = %s, want %s", res.ShutdownGraceDuration, 10*time.Minute)
	}
	if res.ShutdownForceTimeout != 15*time.Second {
		t.Fatalf("Resolved ShutdownForceTimeout = %s, want %s", res.ShutdownForceTimeout, 15*time.Second)
	}
}

func TestLoad_ShutdownGraceDuration_InvalidFails(t *testing.T) {
	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	state := filepath.Join(root, "state")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")

	toml := fmt.Sprintf(`
[paths]
drop_folder = %q
state_dir = %q

[destinations]
dest_dir_movies = %q
dest_dir_shows = %q

[features]
enable_processing = false
enable_torrent_automation = false

[system]
auto_create_missing_dirs = true
shutdown_grace_duration = "0s"
`, drop, state, movies, shows)

	cfgPath := writeConfigFile(t, root, toml)
	_, _, err := Load(cfgPath)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "system.shutdown_grace_duration") {
		t.Fatalf("expected shutdown_grace_duration validation error, got: %v", err)
	}
}

func TestLoad_ShutdownForceTimeout_InvalidFails(t *testing.T) {
	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	state := filepath.Join(root, "state")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")

	toml := fmt.Sprintf(`
[paths]
drop_folder = %q
state_dir = %q

[destinations]
dest_dir_movies = %q
dest_dir_shows = %q

[features]
enable_processing = false
enable_torrent_automation = false

[system]
auto_create_missing_dirs = true
shutdown_force_timeout = "nope"
`, drop, state, movies, shows)

	cfgPath := writeConfigFile(t, root, toml)
	_, _, err := Load(cfgPath)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "system.shutdown_force_timeout") {
		t.Fatalf("expected shutdown_force_timeout validation error, got: %v", err)
	}
}

func writeConfigFile(t *testing.T, dir string, contents string) string {
	t.Helper()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(contents)+"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
