# Configuration

This covers the config knowledge that goes beyond day-one setup. See the main
[README](../README.md#configuration) for the settings you'll actually touch
first, and `config.example.toml` for the fully annotated reference of every
setting.

## Movies and Shows destinations must be separate

`dest_dir_movies` and `dest_dir_shows` must be different directories, and
neither can be nested inside the other. mintmedia relies on this to avoid
misfiling a show episode into the movies tree (or vice versa) when it scans
existing library folders.

## Destinations on a NAS or other mounted filesystem

`defer_destination_checks = true` (the default) lets the daemon start even if
your library destinations aren't mounted yet. Files that arrive while a
destination is unavailable are queued and processed once it comes back --
useful for NAS shares or Tailscale-mounted drives that might not be up the
moment the daemon starts.
