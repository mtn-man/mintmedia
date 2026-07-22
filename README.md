# mintmedia

**mintmedia** turns messy downloads into a clean library, automatically -- simple to set up, simple to use, and built to stay out of your way.

```
input:   Stranger.Things.S04E07.2160p.BluRay.x265.mkv
output:  Shows/Stranger Things/Season 04/Stranger Things - S04E07.mkv
```
Whether your library lives on your computer, a NAS, or any other mounted filesystem, mintmedia organizes it the same way.
Drop a file or folder into the MintDrop folder (typically ~/Downloads/MintDrop) and mintmedia will figure out what it is, rename it cleanly, and move it to your Movies or Shows library - "automagically."

Want to automate the whole thing?

Once you've enabled Transmission integration and started the daemon (mintmedia -d), all you have to do is copy a magnet link, and mintmedia handles the rest: queuing the download, organizing the files when it finishes, and sorting them into a library structure ready for media servers like Plex, Infuse, Jellyfin, or any local media player.

By design, mintmedia keeps things simple:

- No web UI -- everything is CLI and daemon
- No outside internet connection -- sorting is done entirely from filenames, on your machine
- No database -- state lives in a plain JSONL history log
- Single binary -- one executable, no runtime dependencies to install

> **Beta software.** mintmedia is pre-1.0 -- CLI flags, config format, and defaults may still change between releases. Always use --plan or --dry-run before pointing mintmedia at important files, and check release notes before upgrading.

## Installation

### [Homebrew](https://brew.sh/) Install

```
brew install mtn-man/tap/mintmedia
```

### [Go](https://go.dev/) Install

If you'd rather not add a Homebrew tap (or it's not available on your platform), install directly with Go:

```
go install github.com/mtn-man/mintmedia/cmd/mintmedia@latest
```

This requires Go 1.25.5 or newer ([install Go](https://go.dev/doc/install) if you don't have it), and installs to `$(go env GOPATH)/bin` (or `$GOBIN` if set) -- make sure that directory is on your `PATH`. To pin to a specific release instead of the latest commit on `main`, use a tag in place of `@latest`, e.g. `@v1.3.0`.

On Linux, clipboard-based magnet link detection additionally requires a Wayland session with `wl-clipboard` installed (`wl-paste` must be on `PATH`) -- see [Transmission Integration](#transmission-integration).

## Quick Start

1. Run `mintmedia`. A default config is written to `~/.config/mintmedia/config.toml` with sensible defaults (drop folder `~/Downloads/MintDrop`, movies and shows libraries under `~/Movies` on macOS or `~/Videos` on Linux). Edit the config file if you want different paths.
2. Drop some media into the drop folder, then preview what mintmedia would do with it before touching anything:
   ```
   mintmedia --plan
   ```
3. Happy with the plan? Run `mintmedia` again (no flags) to actually process it.
4. To run continuously in the background, watching for new files as they arrive:
   ```
   mintmedia --daemon
   ```

## How It Works

When mintmedia processes a file or folder, it:

1. Finds the main media file (by extension -- `.mkv`, `.mp4`, etc.), searching recursively so season folders, whole-show dumps, and movie collections all work the same as a single file
2. Figures out whether it's a movie or a show from the filename
3. Parses the title, year, and season/episode information
4. Moves everything -- main file and any subtitles -- to the right destination with a clean name
5. If the input was a folder and everything moved successfully, sends it to Trash (using whatever trash mechanism your OS provides)

If a subtitle or other accompanying file can't be moved, the main media is still moved and you'll see a warning. The source folder is left in place if anything was left behind.

Use `--plan` to preview what mintmedia would do without touching anything -- see [CLI Reference](#cli-reference) for the path syntax.

See [Media detection](docs/media-detection.md) for exactly which filename patterns are recognized, and [Folder processing](docs/folder-processing.md) for how season packs, movie collections, and mixed folders are handled.

### Output naming

Shows are organized by season, with the episode renamed to a clean `Show Name - SxxExx` format:

```
input:   Breaking.Bad.S03E07.1080p.HEVC.x265-[GROUP].mkv
output:  Shows/Breaking Bad/Season 03/Breaking Bad - S03E07.mkv
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

mintmedia uses a few heuristics to match an incoming episode to the right existing folder. If it doesn't find a match, it creates a new folder for the show. If it's ever unsure which folder is correct, it won't guess -- it skips the file and reports it so you can sort it manually. This lets mintmedia slot into an existing library without renaming folders or creating duplicates. See [Show folder matching](docs/show-folder-matching.md) for the exact rules.

## CLI Reference

```
mintmedia [flags]
```

| Flag | Description |
|------|-------------|
| `-d` / `--daemon` | Run continuously, watching for new files |
| `-p` / `--process-drop` | Process everything currently in the drop folder (default when no flag is given) |
| `--process <path>` | Process a specific path -- non-media and empty directories are silently skipped |
| `--plan[=path]` / `--dry-run[=path]` | Preview what would happen -- no changes made; omit path to preview the drop folder. Use `=` to pass a path (`--plan=/path`, not `--plan /path`) |
| `-s` / `--status` | Check whether the daemon is running |
| `-S` / `--stop` | Gracefully stop the running daemon |
| `--config <path>` | Use a different config file |
| `-v` / `--verbose` | Print config summary at startup |
| `-V` / `--version` | Show version and exit |
| `-h` / `--help` | Show help |

## Configuration

The config file lives at `~/.config/mintmedia/config.toml` and is created automatically on first run.

**The settings you'll actually touch on day one:**

| Setting | Default | What it does |
|---------|---------|--------------|
| `drop_folder` | `~/Downloads/MintDrop` | Where to look for incoming media |
| `dest_dir_movies` | `~/Movies/Movies` (macOS), `~/Videos/Movies` (Linux) | Where processed movies go |
| `dest_dir_shows` | `~/Movies/Shows` (macOS), `~/Videos/Shows` (Linux) | Where processed shows go |
| `done_notification_mode` | `per_file` | Sound after processing: `per_file`, `per_job`, or `off` |

Everything else -- log levels, drop-folder settle timing, and more -- is documented inline in `config.example.toml`, which is the fully annotated reference for every setting.

**A few things worth knowing:**
- Movies and Shows destinations must be different directories -- one can't be inside the other.
- `defer_destination_checks = true` (the default) lets the daemon start before your library destinations are mounted. Files that arrive while they're unavailable are queued and processed once they come back -- useful for NAS or Tailscale-mounted shares.

## Logs

mintmedia keeps a structured JSONL log of everything it does. The default location depends on your platform:

- **macOS**: `~/Library/Application Support/mintmedia/history.jsonl`
- **Linux**: `~/.local/state/mintmedia/history.jsonl`

If a file ended up somewhere unexpected, or a subtitle was left behind, that's the first place to look. The log location is controlled by `history_file` in your config (relative paths resolve under `state_dir`).

## Transmission Integration

See [Transmission integration](docs/transmission-integration.md) for the config and platform requirements needed to enable the hands-free magnet-link-to-library workflow described above.

## License

mintmedia is licensed under the [GNU General Public License v3.0](LICENSE.txt).
