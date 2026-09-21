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

`resolution_aware = false` (the default) discards the release resolution once
it has been used to clean up the title. Set `resolution_aware = true` to
re-append it to the sorted name as a ` - <res>` suffix:

```
Interstellar.2014.1080p.BluRay.mkv        -> Interstellar (2014) - 1080p.mkv
Breaking.Bad.S03E07.2160p.4K.WEB-DL.mkv   -> Breaking Bad - S03E07 - 2160p.mkv
```

Detected resolutions are normalized to one of `480p`, `576p`, `720p`, `1080p`,
`1440p`, `2160p` (`4k`/`uhd` and a `1920x1080`-style dimension pair both map
in). The movie folder name is left unchanged -- only the file inside it gets
the suffix -- and the embedded metadata title tag stays resolution-free.

### It's the release name, not the file

The resolution mintmedia writes is only ever read from the release name -- it
never opens the file or probes the video stream. The suffix, and the movie
duplicate-identity decision described below, are therefore only as accurate as
the name the release arrived with. A mislabeled or upscaled `2160p` release
that is really 1080p still sorts in as `- 2160p`, and mintmedia will not notice
two files that share a resolution but differ in source, encode, or bitrate -- a
BluRay remux and a small `x265` WEB-DL both parse to `1080p`, so the second is
treated as an exact duplicate and skipped. Reconciling a name that disagrees
with its contents, or choosing between two same-resolution copies, is left to
you; filename parsing alone can't resolve it cleanly, and real stream probing
is out of scope for this tool.

### Resolution as movie identity

With `resolution_aware = true`, the resolution also becomes part of a **movie's**
identity for duplicate detection, so you can keep more than one resolution of a
film in the same folder:

| Incoming file | The movie's folder already holds | Result |
| --- | --- | --- |
| `… - 2160p` | a `… - 2160p` file (exact match) | skipped as a duplicate |
| `… - 2160p` | only `… - 1080p` / `… - 720p` etc. | sorted in alongside; `--plan` shows an `Alongside:` line, and the completed sort logs an INFO naming the resolution already there |
| `… - 2160p` | an untagged `Movie (Year).mkv`, no `… - 2160p` | sorted in, with a WARNING |
| no resolution detected | an untagged `Movie (Year).mkv` | skipped as a duplicate |
| no resolution detected | only resolution-tagged files | **left in the drop folder for review** (a WARNING, no move -- an untagged release can't be named safely next to a tagged copy) |
| no resolution detected | nothing for that title | sorted in as `Movie (Year).mkv` |

mintmedia only ever **adds** resolutions -- it never deletes or replaces a file
already in the library, so pruning an older/lower resolution is left to you.

## Keeping already-clean episode titles

`preserve_episode_titles = false` (the default) discards any text trailing the
season/episode token in a show's filename. Set `preserve_episode_titles =
true` to keep it when it's already clean:

```
Show Name - S01E06 - Episode Title.mkv        -> Show Name - S01E06 - Episode Title.mkv
Show Name - S01E06 - Episode Title - 1080p.mkv -> Show Name - S01E06 - Episode Title - 1080p.mkv (with resolution_aware also on)
```

This is deliberately strict, not a general release-tag cleanup: the trailing
text must use the `" - "` separator (the naming convention Plex and Jellyfin
themselves recommend, and the one mintmedia's own output already follows),
and it must contain none of the release-tag junk mintmedia already strips
elsewhere (resolution, codec, source, `PROPER`/`REPACK`, etc.). A dot- or
underscore-separated scene name
(`Show.Name.S01E06.Episode.Title.mkv`), or a trailing title with any junk
still attached, is left exactly as it is today -- no title kept, no partial
guess. When combined with `resolution_aware`, the title is inserted before
the resolution suffix, and duplicate detection still matches on season and
episode alone, so a title-free episode already in your library is still
recognized as the same episode as a newly-titled re-download of it.

Once a movie's canonically-named folder (`Title (Year)/`) exists, new resolutions
route into it directly; mintmedia won't reconcile it against a differently-spelled
folder (`Amelie (2001)/` vs `Amélie (2001)/`) of the same film that already
exists (rare edge case).

When the canonical folder is *absent* and an incoming movie is confidently
matched to an existing differently-spelled folder, that folder's spelling is
adopted for the new filename **and**, if metadata tagging is on, the embedded
title tag -- deliberately, so every file in the folder agrees on how the title
is written.

**Shows are unchanged** -- a different-resolution episode re-download is still
skipped as a duplicate. The one addition: when the skipped episode is at a
different resolution than the library copy, a non-blocking WARNING names both
(so a higher-quality season pack bouncing off the library isn't silent).

Two edge cases stay unhandled, both involving files mintmedia did not sort
itself (its own output is always canonically named):

- A library file hand-named with a non-canonical resolution tag
  (`Movie (2020) - 4K.mkv` or `- UHD.mkv` instead of `- 2160p`) isn't seen as
  resolution-tagged, so an incoming copy of that movie isn't compared against
  it: a genuine duplicate can sort in beside it, and an untagged incoming file
  skips the review hold. The folder-level fuzzy match doesn't cover this -- it
  keys on folder names, not files inside an already-canonical folder.
- A diacritic/punctuation-variant *filename* inside a folder reached by the
  fuzzy match (`Amelie (2001).mkv` inside `Amélie (2001)/`) isn't matched by the
  in-folder check and can get a canonically-named sibling.
