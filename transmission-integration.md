# Transmission integration

With Transmission integration enabled, the whole workflow -- from magnet link to organized library -- is hands-free:

```
copy magnet link  ->  mintmedia detects it on the clipboard  ->  torrent added to Transmission
  ->  download completes  ->  files organized into your library  ->  completed torrent removed
```

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

Clipboard monitoring requires macOS (cgo-enabled build) or Linux with a Wayland session and `wl-clipboard` installed (`wl-paste` must be on PATH). The Transmission RPC endpoint is reached directly over HTTP -- no `transmission-remote` binary required.
