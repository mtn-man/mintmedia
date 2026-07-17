# Folder processing

This is the detailed reference for how mintmedia handles a folder passed as
input (or found in the drop folder), rather than a single file. See the main
[README](README.md#how-it-works) for the short version.

## How folders are walked

Given a folder, mintmedia recursively finds every file whose extension is a
main media extension (`.mkv`, `.mp4`, etc., configurable via
`main_media_extensions`), up to 6 levels deep. There's no requirement of
"exactly one media file per folder" -- a folder with 8 episodes or 4 movies in
it produces 8 or 4 separate results from one call, each moved to its own
destination.

This means these all work the same way, with no special-casing required:

- A single movie file, alone or in its own folder
- A whole season folder (`Season 01/` with all its episodes)
- A season-range dump (`Show S01-S04/` with subfolders per season, or all
  episodes flattened directly inside)
- A movie collection folder (e.g. a Bourne or Harry Potter collection with
  several `.mkv` files side by side)
- A mix of the above nested a level or two deep

## Movie collections

When a folder contains two or more main media files, movie titles are parsed
from each **filename only** -- the folder name is never used as a fallback.
This matters because a collection folder's own name (e.g. `The Jason Bourne
Collection 2004-2016 1080p BluRay HEVC x265 5.1`) would otherwise bleed into
every movie's parsed title. With a single file, the folder name is still used
as a fallback when the filename alone doesn't parse cleanly.

## Show hints from folder names

Season-range folders (`S01-S04`, `Season 1-4`) carry a hint: if an episode
filename inside doesn't include the show name (e.g. a bare `S01E01.mkv`), the
show name and year are pulled from the folder name instead. Plain `Season NN`
folders and flat dumps don't need this, since the show name is expected to be
present on each episode filename or a parent folder in that case.

## Subtitles and other sidecars

For each main media file, mintmedia looks for accompanying files (`.srt`,
`.sub`, `.ass`, `.idx`, `.vtt` by default, via `associated_file_extensions`)
in the **same directory only** -- it doesn't search elsewhere for orphaned
subtitles. A sidecar matches if its filename -- minus a trailing language
tag like `.en` -- equals the main file's name. The language tag, if present,
is preserved on the renamed output:

```
Stranger.Things.S04E07.en.srt  →  Stranger Things - S04E07.en.srt
```

## Partial failures

If one file in a multi-file folder can't be parsed or moved, it's skipped and
reported -- the rest of the folder is still processed normally. Skipped files
never fall back to folder-name parsing, even in single-movie mode.

## Cleanup

Once every file in a folder has moved successfully with no issues, the
now-empty source folder is sent to Trash. If anything was skipped or failed
-- a stray unparseable file, a subtitle that couldn't be moved, non-media
files left behind -- the source folder is left in place so nothing is lost.

## What's ignored

There's no filename-based ignore list (no "sample" or "trailer" detection).
Filtering is purely by extension: anything not in `main_media_extensions` or
`associated_file_extensions` -- `.nfo`, images, `.txt`, samples, etc. -- is
silently left untouched in the source folder.
