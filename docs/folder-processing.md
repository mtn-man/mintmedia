# Folder processing

This is the detailed reference for how mintmedia handles a folder passed as
input (or found in the drop folder), rather than a single file. See the main
[README](../README.md#how-it-works) for the short version.

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
- A movie collection folder (e.g. a Jason Bourne or Harry Potter collection
  with several `.mkv` files side by side)
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
show name and year are pulled from the folder name instead.

A plain single-season folder (`Season 2`, `Season 04`) carries a narrower
version of the same hint: the specific season number it names. This exists
for old-style releases where individual episode files carry no separator at
all between season and episode digits (`Show 201 Episode Title.avi` meaning
season 2, episode 01) -- a shape that's genuinely ambiguous with a numbered
movie title (`101 Dalmations.1961...`), so it's only ever read as an episode
number when the folder has already pinned down which season it must be, and
even then only when the file doesn't also look like a movie (see
[Media detection](media-detection.md#movie-vs-show)). Flat dumps with no
season folder at all don't get this hint, since there's no trusted season
number to anchor the ambiguous digits to -- the show name and season/episode
are expected to be present on each episode filename in that case. This check
looks at each file's own immediate parent folder, not just the top-level
input, so a single-season subfolder nested inside a larger season-range
container (`Show S01-S04/Season 2/Show 201 Title.avi`) is still recognized.

### Keeping one show folder per batch

When a batch mixes numbering styles across seasons -- some episode filenames
carry the show name and season/episode on their own, others only resolve via
a folder hint -- each file is first checked against how the rest of the batch
would parse. If sibling files would otherwise land on two different show
names (e.g. one season's filenames are self-sufficient while another's rely
entirely on the hint), every file in the batch is forced to the folder-hint
name instead, so the whole batch always lands in one show folder. A batch
that already agrees with itself is left alone, since a name parsed from a
clean filename is often less noisy than one built from a release-tag-heavy
folder name.

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

There's no filename-based ignore list (no "sample" or "trailer" detection),
by design. Filtering is purely by extension: anything not in
`main_media_extensions` or `associated_file_extensions` -- `.nfo`, images,
`.txt`, samples, etc. -- is silently left untouched in the source folder.
