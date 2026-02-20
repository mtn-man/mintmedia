package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	wantErrorDir := filepath.Join(state, "error")
	if res.ErrorDirAbs != wantErrorDir {
		t.Fatalf("ErrorDirAbs = %q, want %q", res.ErrorDirAbs, wantErrorDir)
	}
	wantHistory := filepath.Join(state, "history.log")
	if res.HistoryFileAbs != wantHistory {
		t.Fatalf("HistoryFileAbs = %q, want %q", res.HistoryFileAbs, wantHistory)
	}

	for _, dir := range []string{drop, state, movies, shows, wantErrorDir} {
		st, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("expected dir to exist (%s): %v", dir, err)
		}
		if !st.IsDir() {
			t.Fatalf("expected dir (%s), got file", dir)
		}
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

func TestLoad_TorrentAutoCleanupDefaultsTrueWhenOmitted(t *testing.T) {
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
	if !*cfg.Torrent.AutoCleanupCompletedTorrents {
		t.Fatalf("AutoCleanupCompletedTorrents = %v, want true", *cfg.Torrent.AutoCleanupCompletedTorrents)
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

func writeConfigFile(t *testing.T, dir string, contents string) string {
	t.Helper()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(contents)+"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
