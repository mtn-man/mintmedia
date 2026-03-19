# mintmedia

Mintmedia is a macOS drop-folder daemon and CLI that automatically organizes media files into your Movies and Shows libraries. Drop a file or folder into the watch folder, and mintmedia figures out what it is, renames it cleanly, and moves it to the right place — with optional Transmission integration for torrent automation.

## Quick Start

1. Run `mintmedia` once. If no config file exists, one is created automatically at `~/.config/mintmedia/config.toml` with sensible macOS defaults:
   - **Drop folder**: `~/Downloads/MintDrop`
   - **Movies**: `~/Movies/Movies`
   - **Shows**: `~/Movies/Shows`
   - **State/history**: `~/Library/Application Support/mintmedia`

2. Review and adjust the config if needed — in particular, update the destination paths if your media library lives somewhere else.

3. Process your drop folder once:
   ```
   mintmedia --process-drop
   ```

4. Or run as a background daemon that watches for new files automatically:
   ```
   mintmedia --daemon
   ```

That's it. The drop folder and state directories are created automatically on first run. Destination directories are created on first use.

> Use `--config <path>` to point mintmedia at a different config file if you don't want to use the default location.

## How Processing Works

When mintmedia processes a path, it:

1. **Scans for main media** — looks for files matching `media.main_media_extensions` (`.mkv`, `.mp4`, etc.)
2. **Classifies** — determines movie vs. show from the filename using pattern matching and release tag parsing
3. **Plans the move** — computes clean destination paths using parsed title, year, season/episode info
4. **Moves files** — relocates the main file and any associated files (subtitles, etc.) to the destination
5. **Cleans up** — if the input was a directory and everything moved cleanly, moves the source directory to Trash

Associated file failures (e.g., a subtitle that couldn't be moved) are non-fatal — the main media is still moved and a warning is printed. The source directory is not trashed if any associated files failed.

Use `--plan <path>` to preview what mintmedia would do without making any changes.

## CLI Reference

```
mintmedia [flags]
```

**Modes** (choose one; default is `--process-drop` when `features.enable_processing=true`):

| Flag | Description |
|------|-------------|
| `--plan <path>` | Preview the processing plan — no filesystem changes |
| `--apply <path>` | Plan then apply changes for a specific path |
| `--process <path>` | Process a path with policy (silently skips non-media) |
| `-p, --process-drop` | Process everything currently in the drop folder |
| `-d, --daemon` | Run the daemon (watches drop folder, polls clipboard) |

**Other flags:**

| Flag | Description |
|------|-------------|
| `--config <path>` | Config file path (default: `~/.config/mintmedia/config.toml`) |
| `-v, --verbose` | Print config summary at startup |
| `-h, --help` | Show help |

## Configuration

The config file is TOML. Run `mintmedia` once to generate the default, then edit as needed. `config.example.toml` in this repository documents every available option.

**Key settings:**

| Setting | Default | Description |
|---------|---------|-------------|
| `paths.drop_folder` | `~/Downloads/MintDrop` | Folder to watch/process |
| `paths.state_dir` | `~/Library/Application Support/mintmedia` | History and state files |
| `destinations.dest_dir_movies` | `~/Movies/Movies` | Where processed movies land |
| `destinations.dest_dir_shows` | `~/Movies/Shows` | Where processed shows land |
| `features.enable_processing` | `true` | Enable the file processor |
| `system.auto_create_missing_dirs` | `true` | Create drop/state dirs if missing |
| `system.defer_destination_checks` | `true` | Don't require destinations at startup |
| `system.done_notification_mode` | `per_file` | Sound on completion: `per_file`, `per_job`, or `off` |
| `watch.drop_settle_duration` | `3s` | How long a path must be quiet before processing |
| `logging.console_level` | `INFO` | Console verbosity: `DEBUG`, `INFO`, `WARN`, `ERROR` |
| `logging.history_file` | `history.jsonl` | Persistent JSONL log (relative to `state_dir`) |

**Notes:**
- `dest_dir_movies` and `dest_dir_shows` must be different paths and neither may contain the other.
- `defer_destination_checks = true` means the daemon will start even if destinations aren't mounted yet. Files that arrive before the destination is ready are queued and processed once it becomes available — useful for NAS or Tailscale-mounted shares.
- `media.main_media_extensions` and `media.associated_file_extensions` control which files are treated as main media vs. accompanying files.
- `naming.media_tag_blacklist` strips common release tags (codec, resolution, source) from parsed names.

## Logging and History

Mintmedia writes a structured JSONL history log to `logging.history_file` (default: `history.jsonl` inside `state_dir`). This log records every file moved, every skip, and every warning — useful for auditing what the tool has done.

Console output is controlled separately by `logging.console_level`. The history log level is controlled by `logging.history_level`.

If you need to investigate why a file wasn't moved or a subtitle was left behind, the history log is the first place to look.

## Transmission Integration

Mintmedia can monitor the clipboard for magnet links and submit them to Transmission automatically, then remove completed torrents from Transmission after their files have been processed.

Enable in config:
```toml
[features]
enable_torrent_automation = true

[clipboard]
enabled = true

[torrent]
enabled = true
host = "localhost:9091"
transmission_remote_path = "/opt/homebrew/bin/transmission-remote"
auto_cleanup_completed_torrents = true
```

Clipboard polling requires a macOS build with cgo enabled.

## Platform Support

- **Primary target**: macOS. Sound notifications (`afplay`), sleep prevention (`caffeinate`), and clipboard magnet polling (AppKit pasteboard) are macOS-only.
- **Linux**: Supported on a best-effort basis for non-clipboard workflows. Clipboard polling is stubbed out.
- Clipboard magnet polling requires a `darwin` build with `cgo` enabled.
