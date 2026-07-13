# Show folder matching

This is the detailed reference for how mintmedia picks a destination folder for
an incoming show episode. See the main [README](README.md#library-awareness)
for the short version.

Before checking any of the rules below, mintmedia parses a show name and
(optionally) a year from the episode's filename, then reads the existing
folders in your Shows directory looking for one whose name matches.

## Resolution order

| If... | mintmedia... |
|---|---|
| A folder with just the show name already exists (no year, no qualifier) | Uses it, regardless of any year in the filename |
| The filename has a year, and a `Show Name (YYYY)` folder with that exact year exists | Uses that folder |
| The filename has no year, and exactly one `Show Name (YYYY)` folder exists | Uses that folder |
| The filename has no year, and *multiple* `Show Name (YYYY)` folders exist | Skips the file and reports it -- won't guess which year is right |
| None of the above matched, but exactly one folder exists with some other qualifier (e.g. `Show Name (UK)`) | Uses it as a best-effort match and reports it, in case the guess was wrong |
| None of the above matched, and *multiple* other-qualifier folders exist | Skips the file and reports it -- won't guess which one is right |
| Nothing matches at all | Creates a new plain `Show Name` folder |

## Examples

- Shows has `Survivor (2000)/`. A file parses as `Survivor` with no year. →
  Routed to `Survivor (2000)/` (rule 1: exact name match always wins).
- Shows has `Fallout (1997)/` and `Fallout (2024)/`. A file parses as
  `Fallout` with no year. → Skipped and reported (rule 4: ambiguous, no year
  to disambiguate).
- Shows has `The Office (UK)/`. A file parses as `The Office` with no year
  and no existing plain or year-qualified folder. → Routed to
  `The Office (UK)/` as a best-effort guess (rule 5), and reported so you can
  correct it if that guess was wrong -- e.g. it should have gone to a new
  `The Office (US)/` folder instead.
- Shows has `The Office (UK)/` and `The Office (US)/`. A file parses as
  `The Office` with no year. → Skipped and reported (rule 6: multiple
  qualified folders, can't tell which one is right).
- Shows is empty. A file parses as `Fringe (2008)`. → Creates
  `Fringe (2008)/` (rule 2, no existing folder to match).
