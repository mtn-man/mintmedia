# mintmedia

**mintmedia** is a lightweight automation tool for organizing downloaded media — a simpler, local alternative to heavier media automation tools for setups that don't need a full media server stack. Drop a file or folder into the watch folder and mintmedia figures out what it is, renames it cleanly, and moves it to your Movies or Shows library. No web UI, no database, no infrastructure — just a binary that runs on macOS or Linux.

With the daemon running and Transmission integration enabled, the full workflow is hands-free: copy a magnet link, and mintmedia handles the rest — queuing the download, organizing the files when it finishes, and dropping them into a library structure ready for Plex, Infuse, Jellyfin, or any other player.

> **Beta software.** mintmedia is pre-1.0 — CLI flags, config format, and defaults may still change between releases. Back up any media libraries before pointing mintmedia at them, and check release notes before upgrading.

## Installation

```
brew install mtn-man/tap/mintmedia
```

### Linux without Homebrew

If Homebrew isn't available (or you'd rather not add a tap), install directly with Go:

```
go install github.com/mtn-man/mintmedia/cmd/mintmedia@latest
```

This requires Go 1.25.5 or newer, and installs to `$(go env GOPATH)/bin` (or `$GOBIN` if set) — make sure that directory is on your `PATH`. To pin to a specific release instead of the latest commit on `main`, use a tag in place of `@latest`, e.g. `@v0.1.0`.

Clipboard-based magnet link detection additionally requires a Wayland session with `wl-clipboard` installed (`wl-paste` must be on `PATH`) — see [Transmission Integration](#transmission-integration) below.

## Quick Start

1. Run `mintmedia`. A default config is written to `~/.config/mintmedia/config.toml` with sensible defaults:
   - **Drop folder**: `~/Downloads/MintDrop`
   - **Movies**: `~/Movies/Movies` (macOS) or `~/Videos/Movies` (Linux)
   - **Shows**: `~/Movies/Shows` (macOS) or `~/Videos/Shows` (Linux)

2. Drop some media into `~/Downloads/MintDrop`, then run `mintmedia` again to process it.

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

Use `--plan` to preview what mintmedia would do without touching anything. Pass a path to preview a specific item, or omit it to preview the whole drop folder.

### Output naming

Shows are organized by season, with the episode renamed to a clean `Show Name - SxxExx` format:

```
input:   Stranger.Things.S04E07.2160p.BluRay.x265.mkv
output:  Shows/Stranger Things/Season 04/Stranger Things - S04E07.mkv
```

Movies get their own subdirectory named after the title:

```
input:   Get.Smart.2008.1080p.BluRay.mkv
output:  Movies/Get Smart (2008)/Get Smart (2008).mkv
```

Subtitles and other associated files are renamed to match and moved alongside the main file:

```
Stranger.Things.S04E07.en.srt  →  Stranger Things - S04E07.en.srt
```

### Library awareness

For shows, mintmedia reads your existing library folder before deciding on a destination. If your Shows directory already has a `Survivor (2000)` folder, a new episode that parses as `Survivor` will be routed there -- no duplicate folders, no year guessing.

mintmedia uses a few heuristics to match an incoming episode to the right existing folder. If it doesn't find a match, it creates a new folder for the show. If it's ever unsure which folder is correct, it won't guess -- it skips the file and reports it so you can sort it manually. See [Show folder matching](show-folder-matching.md) for the exact rules.

## CLI Reference

```
mintmedia [flags]
```

| Flag | Description |
|------|-------------|
| `-d` / `--daemon` | Run continuously, watching for new files |
| `-p` / `--process-drop` | Process everything currently in the drop folder (default when no flag is given) |
| `--process <path>` | Process a specific path — non-media and empty directories are silently skipped |
| `--plan [path]` | Preview what would happen — no changes made; omit path to preview the drop folder |
| `-s` / `--status` | Check whether the daemon is running |
| `-S` / `--stop` | Gracefully stop the running daemon |
| `--config <path>` | Use a different config file |
| `-v` / `--verbose` | Print config summary at startup |
| `-V` / `--version` | Show version and exit |
| `-h` / `--help` | Show help |

## Configuration

The config file lives at `~/.config/mintmedia/config.toml` and is created automatically on first run. A fully annotated reference is in `config.example.toml`.

**Commonly changed settings:**

| Setting | Default | What it does |
|---------|---------|--------------|
| `drop_folder` | `~/Downloads/MintDrop` | Where to look for incoming media |
| `dest_dir_movies` | `~/Movies/Movies` (macOS), `~/Videos/Movies` (Linux) | Where processed movies go |
| `dest_dir_shows` | `~/Movies/Shows` (macOS), `~/Videos/Shows` (Linux) | Where processed shows go |
| `done_notification_mode` | `per_file` | Sound after processing: `per_file`, `per_job`, or `off` |
| `drop_settle_duration` | `3s` | How long to wait after a file stops changing before processing it |
| `console_level` | `INFO` | How much to print: `DEBUG`, `INFO`, `WARN`, or `ERROR` |

**A few things worth knowing:**
- Movies and Shows destinations must be different directories — one can't be inside the other.
- `defer_destination_checks = true` (the default) lets the daemon start before your library destinations are mounted. Files that arrive while they're unavailable are queued and processed once they come back — useful for NAS or Tailscale-mounted shares.

## Logs

mintmedia keeps a structured JSONL log of everything it does. The default location depends on your platform:

- **macOS**: `~/Library/Application Support/mintmedia/history.jsonl`
- **Linux**: `~/.local/state/mintmedia/history.jsonl`

If a file ended up somewhere unexpected, or a subtitle was left behind, that's the first place to look. The log location is controlled by `history_file` in your config (relative paths resolve under `state_dir`).

## Transmission Integration

With Transmission integration enabled, the whole workflow — from magnet link to organized library — is hands-free. The daemon watches your clipboard for magnet links; copy one and it's queued in Transmission automatically. When the download finishes, mintmedia organizes the files and removes the completed torrent.

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

Clipboard monitoring requires macOS (cgo-enabled build) or Linux with a Wayland session and `wl-clipboard` installed (`wl-paste` must be on PATH). The Transmission RPC endpoint is reached directly over HTTP — no `transmission-remote` binary required.

## License

mintmedia is licensed under the [GNU General Public License v3.0](LICENSE.txt).
