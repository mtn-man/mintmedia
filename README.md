# mintmedia

**Mintmedia** is a lightweight automation tool for organizing downloaded media — a simpler, local alternative to heavier media automation tools for setups that don't need a full media server stack. Drop a file or folder into the watch folder and mintmedia figures out what it is, renames it cleanly, and moves it to your Movies or Shows library. No web UI, no database, no infrastructure — just a binary that runs on macOS or Linux.

With the daemon running and Transmission integration enabled, the full workflow is hands-free: copy a magnet link, and mintmedia handles the rest — queuing the download, organizing the files when it finishes, and dropping them into a library structure ready for Plex, Infuse, Jellyfin, or any other player.

## Installation

```
brew install mtn-man/tools/mintmedia
```

## Quick Start

1. Run `mintmedia` to get started — a config is created automatically on first run at `~/.config/mintmedia/config.toml` with sensible defaults:
   - **Drop folder**: `~/Downloads/MintDrop`
   - **Movies**: `~/Movies/Movies` (macOS) or `~/Videos/Movies` (Linux)
   - **Shows**: `~/Movies/Shows` (macOS) or `~/Videos/Shows` (Linux)

2. Drop some media into `~/Downloads/MintDrop` and run `mintmedia` to process it.

3. To run continuously in the background, watching for new files as they arrive:
   ```
   mintmedia --daemon
   ```

That's it. Edit the config file if you want to use different paths.

## How It Works

When mintmedia processes a file or folder, it:

1. Finds the main media file (by extension — `.mkv`, `.mp4`, etc.)
2. Figures out whether it's a movie or a TV show from the filename
3. Parses the title, year, and season/episode information
4. Moves everything — main file and any subtitles — to the right destination with a clean name
5. If the input was a folder and everything moved successfully, sends it to Trash

If a subtitle or other accompanying file can't be moved, the main media is still moved and you'll see a warning. The source folder is left in place if anything was left behind.

Use `--plan <path>` to preview what mintmedia would do without touching anything.

## CLI Reference

```
mintmedia [flags]
```

With no flags, mintmedia processes everything currently in the drop folder.

| Flag | Description |
|------|-------------|
| `--daemon` / `-d` | Run continuously, watching for new files |
| `-p` / `--process-drop` | Process everything currently in the drop folder (default when no flag is given) |
| `--process <path>` | Process a path with policy — non-media and empty directories are silently skipped |
| `--plan <path>` | Preview what would happen — no changes made |
| `--config <path>` | Use a different config file |
| `--verbose` / `-v` | Print config summary at startup |
| `--help` / `-h` | Show help |

## Configuration

The config file lives at `~/.config/mintmedia/config.toml` and is created automatically on first run. Open it to customize paths, extensions, and behavior. Every option is documented in `config.example.toml`.

**Commonly changed settings:**

| Setting | Default | What it does |
|---------|---------|--------------|
| `paths.drop_folder` | `~/Downloads/MintDrop` | Where to look for incoming media |
| `destinations.dest_dir_movies` | `~/Movies/Movies` (macOS), `~/Videos/Movies` (Linux) | Where processed movies go |
| `destinations.dest_dir_shows` | `~/Movies/Shows` (macOS), `~/Videos/Shows` (Linux) | Where processed shows go |
| `system.done_notification_mode` | `per_file` | Sound after processing: `per_file`, `per_job`, or `off` |
| `watch.drop_settle_duration` | `3s` | How long to wait after a file stops changing before processing it |
| `logging.console_level` | `INFO` | How much to print: `DEBUG`, `INFO`, `WARN`, or `ERROR` |

**A few things worth knowing:**
- Movies and Shows destinations must be different directories — one can't be inside the other.
- With `system.defer_destination_checks = true` (the default), the daemon starts even if your destinations aren't mounted yet. Anything that arrives while they're unavailable is queued and processed once they come back online — handy for NAS or Tailscale-mounted shares.

## Logs

Mintmedia keeps a structured log of everything it does. The default location depends on your platform:

- **macOS**: `~/Library/Application Support/mintmedia/history.jsonl`
- **Linux**: `~/.local/state/mintmedia/history.jsonl`

If a file ended up somewhere unexpected, or a subtitle was left behind, that's the first place to look.

## Transmission Integration

When the daemon is running, mintmedia watches the clipboard for magnet links. Copy one and it's automatically added to Transmission. When the download finishes, mintmedia organizes the files and removes the completed torrent — nothing left to do.

To enable, add this to your config:

```toml
[features]
enable_torrent_automation = true

[clipboard]
enabled = true

[torrent]
enabled = true
host = "localhost:9091"
auto_cleanup_completed_torrents = true
```

Clipboard monitoring requires macOS (cgo-enabled build) or Linux with a Wayland session and `wl-clipboard` installed (`wl-paste` must be on PATH).

## Roadmap

**Batch preview for `--process-drop`** — Currently each item is planned and applied one at a time. A future release will plan all candidates first and then apply, so you can see the full move list before anything changes.
