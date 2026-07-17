# Media detection

This is the detailed reference for how mintmedia decides whether a name is a
movie or a show, and how it parses a title, year, and season/episode out of
it. See the main [README](README.md#how-it-works) for the short version.

## Movie vs. show

A name is treated as a **show** only if it has both a season signal and an
episode signal. Anything else is treated as a **movie**. When a folder and a
file inside it are considered together (e.g. a season-range folder holding
individual episode files), a signal on either one counts.

| Signal | Recognizes |
|---|---|
| Season | `S01`, `s1` (as part of `S01E02`); `Season 1`, `Seasons.01` (season-only folder); `S01-S04`, `Season 1-4` (season-range folder) |
| Episode | `E02`, `e2` (as part of `S01E02`); `Episode 1`, `Episodes.010` |

There is no support for `1x02`-style episode notation -- only `SxxExx`.

## Year

A year is a plain standalone 4-digit token between `1900` and `2099`, found
anywhere in the name -- bare (`Fallout.2024`), dot-separated
(`Fallout.2024.S02E04`), or in parens (`Fallout (2024)`). For movies,
everything after the year is discarded, which is what drops trailing
resolution/codec tags without needing to strip each one individually. For
shows, the year is removed from the title portion as its own token if
present.

## Release-tag cleanup

Before producing a clean title, mintmedia strips:

- Bracketed release-group tags: `[EZTVx.to]`, `[YTS]`
- Leading website-ad prefixes: `www.UIndex.org - `, `EZTVx.to - `
- Resolution/codec/source tags from `naming.media_tag_blacklist` in your
  config -- `2160p`, `1080p`, `x264`, `x265`, `hevc`, `web-dl`, `bluray`,
  `hdtv`, `aac`, `dts`, `atmos`, and similar. This list is documented in full,
  and is additive to your own config -- see `config.example.toml`.

Hyphens that look like compound words (`X-Men`, `Spider-Man`) are preserved;
separator hyphens elsewhere become spaces. The resulting title is title-cased,
with Roman numerals and a small acronym allowlist (`AI`, `CIA`, `DEA`, `EU`,
`FBI`, `NASA`, `NYC`, `UAE`, `UFC`, `UK`, `USA`, `WWE`) kept uppercase, and
short words (`a`, `an`, `and`, `of`, `the`, `to`, ...) lowercased unless first
or last.

## Examples

| Input | Category | Parsed as |
|---|---|---|
| `Stranger.Things.S04E07.2160p.BluRay.x265.mkv` | Show | `Stranger Things`, S04E07 |
| `Get.Smart.2008.1080p.BluRay.mkv` | Movie | `Get Smart`, 2008 |
| `[EZTVx.to] Fallout S01E02 1080p WEB-DL.mkv` | Show | `Fallout`, S01E02 |
| `X-Men.Days.of.Future.Past.2014.mkv` | Movie | `X-Men Days of Future Past`, 2014 |
| `Show.S01E01.mkv` (inside a `Show S01-S04` folder) | Show | `Show` (from folder), S01E01 |
