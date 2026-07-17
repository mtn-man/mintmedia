# Show folder matching

This is the detailed reference for how mintmedia picks a destination folder for
an incoming show episode. See the main [README](../README.md#library-awareness)
for the short version.

Before checking any of the rules below, mintmedia parses a show name and
(optionally) a year from the episode's filename, then reads the existing
folders in your Shows directory looking for one whose name matches.

## Why these rules exist

mintmedia would rather skip a file than guess wrong. If more than one existing
folder could plausibly match an episode, the file is left untouched and
reported instead of being routed automatically -- a wrong guess means a
misfiled episode and a folder that's now harder to trust, so every rule below
resolves to either a confident match or an explicit skip.

## Resolution order

| Rule | If... | mintmedia... |
|---|---|---|
| 1 | A folder with just the show name already exists (no year, no qualifier) | Uses it, regardless of any year in the filename |
| 2 | The filename has a year, and a `Show Name (YYYY)` folder with that exact year exists | Uses that folder |
| 2 (fallback) | The filename has a year, no exact-year folder exists, but exactly one folder exists with some other qualifier (e.g. `Show Name (UK)`) | Uses it as a best-effort match and reports it, in case the guess was wrong |
| 2 (fallback, ambiguous) | The filename has a year, no exact-year folder exists, and *multiple* other-qualifier folders exist | Skips the file and reports it -- won't guess which one is right |
| 2 (create) | The filename has a year, and none of the above matched | Creates a new `Show Name (YYYY)` folder using the filename's year |
| 3 | The filename has no year, and exactly one `Show Name (YYYY)` folder exists | Uses that folder |
| 3 (ambiguous) | The filename has no year, and *multiple* `Show Name (YYYY)` folders exist | Skips the file and reports it -- won't guess which year is right |
| 4 | The filename has no year, no year-qualified folder matched, but exactly one folder exists with some other qualifier (e.g. `Show Name (UK)`) | Uses it as a best-effort match and reports it, in case the guess was wrong |
| 4 (ambiguous) | The filename has no year, no year-qualified folder matched, and *multiple* other-qualifier folders exist | Skips the file and reports it -- won't guess which one is right |
| 5 | Nothing matches at all | Creates a new plain `Show Name` folder |

## Examples

- Shows has `Survivor (2000)/`. A file parses as `Survivor` with no year. →
  Routed to `Survivor (2000)/` (rule 3: no year in the filename, and exactly
  one year-qualified folder exists to match against).
- Shows has `Fallout (1997)/` and `Fallout (2024)/`. A file parses as
  `Fallout` with no year. → Skipped and reported (rule 3, ambiguous case:
  no year to disambiguate between the two).
- Shows has `The Office (UK)/`. A file parses as `The Office` with no year
  and no existing plain or year-qualified folder. → Routed to
  `The Office (UK)/` as a best-effort guess (rule 4), and reported so you can
  correct it if that guess was wrong -- e.g. it should have gone to a new
  `The Office (US)/` folder instead.
- Shows has `The Office (UK)/` and `The Office (US)/`. A file parses as
  `The Office` with no year. → Skipped and reported (rule 4, ambiguous case:
  multiple qualified folders, can't tell which one is right).
- Shows is empty. A file parses as `Fringe (2008)`. → Creates
  `Fringe (2008)/` (rule 2, create case: the filename has a year and nothing
  matched, so the new folder keeps that year rather than falling back to a
  plain `Fringe/`).
