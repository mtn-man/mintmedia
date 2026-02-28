# mintmedia

Mintmedia is a macOS drop-folder daemon and CLI for organizing media into Movies/Shows libraries, with optional Transmission automation.

## Quick Start
1. Create a config file from the example:
   - Copy `config.example.toml` to `~/.config/mintmedia/config.toml` (or pass `--config`).
   - Update `paths.drop_folder`, `paths.state_dir`, and destination paths in `[destinations]`.
2. Run in one-shot mode:
   - `mintmedia --process-drop` (default when `features.enable_processing=true`)
3. Run as a daemon:
   - `mintmedia --daemon`

## Platform Support
- Primary target: macOS.
- Linux/non-cgo builds are supported on a best-effort basis for non-clipboard workflows.
- Clipboard magnet polling requires a `darwin` build with `cgo` enabled.

## CLI Usage
```
mintmedia [flags]
```

Modes (choose one; default is `-p/--process-drop` when `features.enable_processing=true`):
- `--plan <path>`: compute and print the processing plan (no changes)
- `--apply <path>`: plan and apply changes for a path (filesystem writes)
- `--process <path>`: process a path with policy (ignore non-media/no-media dirs)
- `-p, --process-drop`: process all paths currently in the drop folder (one-shot)
- `-d, --daemon`: run the daemon (watch/poll/automations)

Other flags:
- `--config <path>`: path to `config.toml` (default: `~/.config/mintmedia/config.toml`)
- `-v, --verbose`: verbose startup output (prints config summary)
- `-h, --help`: show help

## How Processing Works (Brief)
- Input is scanned to find main media files (by extension).
- The processor determines movie vs show and parses naming info.
- Files are moved into Movies/Shows destinations with structured naming.
- Associated files (e.g., subtitles) are moved alongside the main media.
- If the input is a directory, it may be moved to Trash after successful processing (with safety checks).

## Configuration Notes
- `features.enable_processing` controls the Go-native processor.
- If `features.enable_processing=false`, running with no mode flag performs a config-only smoke test.
- `media.main_media_extensions` and `media.associated_file_extensions` drive file detection.
- `naming.media_tag_blacklist` removes common release tags from names.
- `system.defer_destination_checks` can delay processing until destinations are ready.
- `system.done_notification_mode` controls done-sound behavior for both daemon and `--process-drop`:
  - `per_file` (default): one sound per successfully applied main media file.
  - `per_job`: one sound per processed path when at least one main media file is applied.
  - `off`: disables done sounds.
- `system.shutdown_grace_duration` configures how long shutdown waits for in-flight work before forced cancellation.
- `system.shutdown_force_timeout` configures how long shutdown waits after cancellation before giving up.
- `watch.drop_settle_duration` controls how long a path must be quiet before it is processed.
- `clipboard` and `torrent` sections enable optional Transmission automation.
- `torrent.auto_cleanup_completed_torrents` toggles removing completed Transmission entries after successful APPLIED processing (default: disabled).

## Optional macOS Integrations
- `caffeinate` is used to prevent idle sleep while the daemon runs.
- `afplay` is used for sound notifications.
- Transmission automation uses `transmission-remote` if enabled in config.
