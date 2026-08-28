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

Rules 2 (create) and 5 are the only rules that create a folder. Both run one
extra check first -- see below.

## Possible-duplicate warning when creating a new folder

Just before rule 2 (create) or rule 5 makes a folder that didn't exist
before, mintmedia compares the show name against every existing Shows folder
using a *fuzzy* key that ignores diacritics, punctuation, and case -- so
`Pokemon` is seen as a possible match for `Pokémon`, and `Marvels Daredevil`
for `Marvel's Daredevil`. Whole words are never ignored: `The Bear` and
`Bear` stay distinct.

If that turns up a match, mintmedia still creates the new folder exactly as
the rule computed it, but also reports a warning naming the existing
folder(s) it might be a spelling or encoding variant of, so you can merge
them by hand if the match is real. A candidate is ignored when its folder
name and the filename both carry an explicit year and those years differ --
that's treated as a reboot or a genuinely different show, not a variant.

This check only ever warns; it never changes which folder is used. That is
weaker on purpose than the equivalent for movies, where a confident fuzzy
match reroutes the file into the existing folder. Rerouting a show episode
on a bad guess would file it under a different show entirely -- worse than
leaving one movie unsorted -- so rules 1-4's exact-match routing is left
completely untouched by this check.

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
- Shows has `Pokémon (1997)/`. A file parses as `Pokemon` with no year, and
  nothing matches rules 1-4. → Creates a new plain `Pokemon/` folder (rule
  5) *and* reports a warning that it may be a duplicate of `Pokémon (1997)/`,
  so you can reconcile the two. The episode is still filed -- the folder is
  created as computed, not rerouted.
