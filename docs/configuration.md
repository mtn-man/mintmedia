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

## Keeping the resolution in the filename

`append_resolution = false` (the default) discards the release resolution once
it has been used to clean up the title. Set `append_resolution = true` to
re-append it to the sorted name as a ` - <res>` suffix:

```
Interstellar.2014.1080p.BluRay.mkv        -> Interstellar (2014) - 1080p.mkv
Breaking.Bad.S03E07.2160p.4K.WEB-DL.mkv   -> Breaking Bad - S03E07 - 2160p.mkv
```

Detected resolutions are normalized to one of `480p`, `576p`, `720p`, `1080p`,
`1440p`, `2160p` (`4k`/`uhd` and a `1920x1080`-style dimension pair both map
in). The movie folder name is left unchanged -- only the file inside it gets
the suffix -- and the embedded metadata title tag stays resolution-free. A
re-download at a different resolution is still recognized as a duplicate of the
library copy and skipped.
