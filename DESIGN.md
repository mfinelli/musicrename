# Design Document: `musicrename`

## 1. Overview

`musicrename` is a Go-based CLI tool designed to normalize a local music
library. It transforms inconsistent file structures and naming conventions into
a strict, predictable, and sanitized hierarchy based on internal metadata tags.

## 2. Goals & Requirements

- **Normalization:** Standardize paths and filenames for a consistent library
  feel.
- **Sanitization:** Remove non-ASCII characters and illegal filesystem
  characters.
- **Platform Target:** Linux and macOS. Windows is not supported.
- **Integrity:** Generate `sums.md5` files for every album to track file
  integrity. Output is compatible with the system `md5sum` command for
  verification.
- **Auditing:** Ability to scan for library "misconfigurations" or unwanted
  attributes.
- **Safety:** Provide a `--dry-run` mode to preview all filesystem changes.

## 3. Technical Specifications

### 3.1 Directory Hierarchy

Files are organized using a tiered structure to avoid overly large root
directories: `/[First Letter of Artist]/[Artist]/[Year] [Album Name]/`

- The first-letter bucket is a single character: `a`–`z` for artists whose name
  begins with a letter, or `0` for all others (digits, symbols, etc.). If the
  `ALBUMARTISTSORT` tag is present, its sanitized first character determines the
  bucket instead of `ALBUMARTIST`. This allows artists like "The Beatles" to
  file under `b/` rather than `t/`. The artist folder name always comes from the
  sanitized `ALBUMARTIST`; only the bucket is affected by the sort tag.
- Bucket overrides: A small hardcoded map in internal/sanitize allows specific
  raw `ALBUMARTIST` values to be assigned a fixed bucket letter, bypassing both
  the `ALBUMARTISTSORT` tag and the standard first-character derivation.
  Precedence order: bucket override -> `ALBUMARTISTSORT` -> `ALBUMARTIST`.
- Because artist names pass through the full sanitization pipeline before
  bucketing, only lowercase letters and digits are possible first characters by
  the time the bucket is determined.
- If the `YEAR` tag is absent, the year prefix is omitted entirely:
  `/[First Letter of Artist]/[Artist]/[Album Name]/`

**Examples:**

- `b/beyonce/[2003] dangerously in love/`
- `0/2pac/[1996] all eyez on me/`
- `b/beyonce/lemonade/` _(year tag missing)_

**Album Folder Contents:**

- **Root:**
  - Audio files (`.flac`, `.mp3`, `.m4a`)
  - Primary Art (static): `folder.jpg` or `folder.png`
  - Primary Art (animated): `folder.webp` or `folder.mp4`
  - Text files: `.log`, `.cue`, `.m3u`, `.m3u8`
  - `sums.md5`
- **`/artwork/`**: Additional image files.
- **`/scans/`**: High-resolution scans (typically `.tiff`).
- **`/extras/`**: All other non-audio/non-art files.

### 3.2 The Sanitization Pipeline

All strings used in folder and filenames (Artist, Album, Title) must pass
through this sequence:

1. **Manual Overrides:** Hardcoded replacements for a small set of known edge
   cases (e.g., `AC/DC` -> `ac⁄dc` (U+2044 fraction slash), `P!nk` -> `pink`).
   **Overrides return the final sanitized string immediately, skipping all
   subsequent steps including truncation.** The override value is used exactly
   as written.
2. **Transliteration:** Convert Unicode characters to ASCII via
   `github.com/alexsergivan/transliterator`.
3. **Casing:** Convert all characters to lowercase.
4. **Non-standard Whitespace:** Convert tabs, newlines, and other whitespace
   variants to a regular space. This runs before the regex strip so that word
   boundaries in badly-tagged files are preserved (e.g., `"Dark\tSide"` ->
   `"dark side"`, not `"darkside"`).
5. **Regex Strip:** Keep only `a-z`, `0-9`, and space. All other characters are
   removed.
6. **Space Normalisation:** Collapse runs of multiple spaces into a single
   space, then trim leading and trailing spaces.
7. **Truncation:**
   - **Artist:** Max 60 characters.
   - **Album:** Max 60 characters.
   - **Files (Tracks/Art/Extras):** Max 40 characters (applied to the base name
     only, before appending the extension).
     - _Note:_ For files inside subdirectories (`artwork/`, `scans/`,
       `extras/`), the limit is 40 characters **minus the length of the
       subdirectory name plus one** (for the `/`) to ensure the full relative
       path in `sums.md5` remains <= 80 characters.
   - Truncation is mid-word (hard cut at the character limit); no word-boundary
     snapping.
   - Truncation is applied after space normalisation, so no result will start or
     end with a space as a result of the cut.

### 3.3 Metadata & Naming Logic

#### Tag Reading

- **Source of Truth:** Internal tags (FLAC/Vorbis Comments, ID3, M4A atoms),
  read via `github.com/deluan/go-taglib`, which normalizes tag names across
  formats. `TRACKNUMBER` is expected to be a single integer (not `track/total`
  form); the library is curator-managed. A `TRACKNUMBER` value of `0` is valid
  and represents a pre-gap or hidden track; it is stored distinctly from an
  absent tag.
- **Album Grouping:** Each source folder is treated as one album. Files are not
  grouped globally by tag values.
- **Compilation Handling:** Use the `ALBUMARTIST` tag for the directory
  structure. If `ALBUMARTIST` is absent, fall back to the `ARTIST` tag of the
  track with the lowest `TRACKNUMBER` value on that album.

#### Missing Tag Behaviour

The tool emits a warning for each missing tag and falls back as follows:

| Missing Tag                  | Fallback                                                     | Severity |
| ---------------------------- | ------------------------------------------------------------ | -------- |
| `YEAR`                       | Omit year prefix from album folder name                      | Warning  |
| `TITLE`                      | Use the original filename stem (passed through the pipeline) | Warning  |
| `TRACKNUMBER`                | Sort the file alphabetically among its untracked peers       | Warning  |
| `ARTIST` _and_ `ALBUMARTIST` | Skip the file; cannot construct a valid path                 | Error    |

The `DATE` tag may contain a full ISO-8601 date (e.g. `2003-01-14`) or a
year-month value (e.g. `2003-01`), as is common with MusicBrainz-sourced tags.
Only the four-character year component is extracted and used as the folder
prefix; the rest is discarded. No validity check is applied on the extracted
year; malformed values (e.g. `0000`) are considered a data entry issue to fix at
the source, not something the tool guards against.

#### Disc Number Handling

If **any** track in an album has a `DISCNUMBER` tag, **all** tracks must have
one. If the tag is missing on even one track, the entire album is skipped with
an error. In practice this is unlikely since metadata is edited per-album.

#### Track Naming Pattern

- **Single Disc:** `[Track#] Title.ext` (e.g., `01 track one.flac`)
- **Multi-Disc:** `[Disc]-[Track#] Title.ext` (e.g., `1-01 track one.flac`)
  - The disc prefix is included only if the album contains more than one disc.
- **Zero-padding:** Track numbers are zero-padded to 2 digits by default. If any
  track number on the album exceeds 99, the entire album switches to 3-digit
  padding for that album only.

### 3.4 MD5 Sum Generation

The tool generates a `sums.md5` file in each album root by computing MD5 digests
directly via Go's `crypto/md5` package. No external tool is required to produce
the file; the output is formatted to be fully compatible with `md5sum -c` for
verification on any system that has `md5sum` installed.

- **Format:** Standard `md5sum` output.
  - Binary files (audio/images): `hash *filename` (asterisk prefix on name).
  - Text files (`.log`, `.cue`, `.m3u`, `.m3u8`, `.txt`): `hash  filename`
    (two-space prefix on name).
- **Paths:** Filenames in `sums.md5` are relative to the album root (e.g.,
  `artwork/cover.jpg`, `01 track one.flac`). Files are listed in sorted order
  for a stable, diffable output across runs.
- **Detection:** Text vs. binary classification is based on a predefined list of
  known text extensions (no magic-byte inspection).
- **Exclusion:** `sums.md5` itself is never included in the checksum file.
- **Scope:** The `sums` command auto-detects its operating mode by checking
  whether the target directory directly contains audio files:
  - **Single-album mode:** The target directory contains audio files. An
    existing `sums.md5` is always an error unless `--force` is passed.
  - **Library mode:** The target directory contains no audio files directly. All
    album directories within it are processed recursively. Albums that already
    have a `sums.md5` are silently skipped; `--force` regenerates them all.

## 4. Architecture

### 4.1 Commands

The tool uses a command-based structure (via `spf13/cobra`):

| Command                             | Description                                                                                                                                                                                                                          |
| ----------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `musicrename rename [library-root]` | Scans metadata, sanitizes, and moves files. Accepts an optional path argument (default: current directory). Use `--dry-run` to preview all planned moves without touching the filesystem.                                            |
| `musicrename sums [path]`           | Generates `sums.md5` for an album or library. Auto-detects mode: single-album if the path directly contains audio files, library otherwise. Defaults to the current directory. Use `--force` to overwrite existing `sums.md5` files. |
| `musicrename check [path]`          | Audits an album or library for misconfigurations; exits non-zero on findings. Auto-detects mode from the path argument (see §4.3). Defaults to the current directory.                                                                |
| `musicrename lyrics [path]`         | Fetches lyrics from LRCLIB and embeds them into audio file tags. Auto-detects mode from the path argument (see §4.5). Defaults to the current directory. Use `--force` to re-fetch and overwrite existing lyrics.                    |
| `musicrename inspect`               | Displays detected and sanitized metadata for a single audio file.                                                                                                                                                                    |

**Note on command independence:** `rename` does **not** generate `sums.md5`. The
intended workflow for a full library update is:

1. `musicrename rename`
2. `musicrename check` _(audit the result before generating checksums)_
3. `musicrename lyrics`
4. `musicrename sums`

### 4.2 `rename` Workflow

1. **Scan Phase:**
   - Recursively locate music files.
   - Identify "unknown" files (files that don't fit known categories), log a
     warning, and leave them in place. Unknown files are never moved by the
     tool.

2. **Analysis Phase:**
   - Read tags -> Apply Sanitization Pipeline -> Determine destination path.

3. **Validation Phase:**
   - Calculate necessary directory creations.
   - Classify each planned move:
     - **No-op** (`oldPath == newPath` exactly): file is already in the correct
       location; no filesystem change required.
     - **Case-only** (`oldPath` and `newPath` differ only in case): a real
       rename is required, but must go via an intermediate temp path to avoid a
       silent no-op on case-insensitive filesystems (macOS default HFS+).
   - Detect sanitization collisions (two source files resolving to the same
     destination path). On the first collision detected: abort the entire run
     with an error.
   - **Overwrite safety:** Check all planned destination paths against the
     filesystem. If any destination file already exists, **abort the entire
     run** and list every conflict. The run is all-or-nothing; no files are
     moved until the pre-flight check passes cleanly.

4. **Execution Phase** _(skipped if `--dry-run` is passed)_:
   - Create folders -> Move files.
   - Use `os.Rename` where source and destination are on the same filesystem.
   - Fall back to copy-then-delete when `os.Rename` returns a cross-device error
     (`syscall.EXDEV`).
   - **Case-only renames:** When the source and destination differ only in case
     (e.g. `Beatles` -> `beatles`), rename via an intermediate temp path to
     avoid silent no-ops on case-insensitive filesystems (macOS default).
   - **Race condition:** If a destination file materializes between the
     pre-flight check and the actual move, skip that file with a warning rather
     than aborting the run.
   - **Empty directory cleanup:** After all moves, attempt to remove any source
     directories that were touched and are now empty, bubbling upward until a
     non-empty directory or the library root is reached. This is best-effort:
     failures are logged but do not affect exit status.
   - **Progress feedback:** On an interactive TTY, the current filename is
     printed with `\r` so each update overwrites the previous line. On non-TTY
     output (pipes, CI) no progress is written.

### 4.3 `check` Command

Audits a music library for metadata and structural issues. Emits all findings to
stdout grouped by album and exits non-zero if any are present, enabling use in
scripts.

#### Operating Modes

The mode is auto-detected from the path argument (default: current directory):

- **Track mode:** The path is an audio file (`.flac`, `.mp3`, `.m4a`). Only
  per-track checks run; directory-level checks (artwork, `sums.md5`, unknown
  files, path conformance) are skipped because album context is unavailable.
- **Album mode:** The path is a directory that directly contains audio files.
  All checks run except path conformance, which requires a library root that
  cannot be reliably inferred from a single album path.
- **Library mode:** The path is a directory with no audio files directly inside.
  All checks run on every album found recursively, including path conformance.

#### Complete Check List

**Metadata completeness** _(track-level; all modes)_

- Missing `TITLE` tag
- Missing `TRACKNUMBER` tag
- Missing `DATE`/year tag
- Missing both `ARTIST` and `ALBUMARTIST` tags

**Album consistency** _(album-level; album and library modes)_

- Inconsistent `ALBUMARTIST` tag across tracks in the same album
- Inconsistent `ALBUM` tag across tracks in the same album
- Partial `DISCNUMBER` coverage (some tracks have the tag, some do not)
- Duplicate track numbers within the same disc

**Audio quality** _(track-level; all modes)_

- Missing `REPLAYGAIN_TRACK_GAIN` tag
- Missing `REPLAYGAIN_ALBUM_GAIN` tag (checked per-track; semantically
  album-level)
- Embedded artwork inside the audio file

**Artwork** _(album-level; album and library modes)_

- Missing primary artwork (`folder.jpg`, `folder.png`, `folder.webp`, or
  `folder.mp4`)
- More than one static (`folder.jpg`/`folder.png`) or more than one animated
  (`folder.webp`/`folder.mp4`) primary artwork file. One static plus one
  animated file is allowed, as a fallback pair for players without
  animated-artwork support.

**Integrity** _(album-level; album and library modes)_

- Missing `sums.md5` for an album

**Naming / path conformance** _(album-level; library mode only)_

- Album directory path does not match what `rename` would produce
- Any file path does not match what `rename` would produce ("would rename move
  this?")

_Note: Verification of `sums.md5` checksums is out of scope. Use
`md5sum -c sums.md5` directly for that._

### 4.4 `inspect` Command

Reads a single audio file and prints its detected metadata alongside the
sanitized values that `rename` would use. Accepts `.flac`, `.mp3`, and `.m4a`
files only; exits with an error for any other input. Shell argument completion
is restricted to those three extensions.

Output format:

```
File:         01 back in black.flac  (FLAC)

Title:        Back In Black
              ↳ back in black
Artist:       AC/DC
              ↳ ac⁄dc  [manual override]
Album Artist: AC/DC
              ↳ ac⁄dc  [manual override]
Album:        Back In Black
              ↳ back in black

Year:         1980  (DATE: "1980-07-25")
Track:        1
Disc:         —
```

- The `↳` line is always shown for non-empty fields (lowercasing alone means the
  sanitized form almost always differs from the raw tag value).
- The `↳` line and `[manual override]` marker are rendered in dim text.
- **Year:** if the `DATE` tag contains a full ISO-8601 date or year-month value,
  the raw tag is shown in parentheses next to the extracted year. If the tag is
  already a bare four-digit year the parenthetical is omitted.
- Absent fields display `—`; no sanitized line is shown for absent fields.
- `inspect` is read-only and makes no filesystem changes.

### 4.5 `lyrics` Command

Fetches lyrics from LRCLIB and embeds them into audio file tags. Operates on a
single file, an album directory, or a library root using the same auto-detection
logic as `sums` and `check`. Defaults to the current directory if no path
argument is given.

#### Operating Modes

- **Track mode:** The path is a single audio file. Only that file is processed.
- **Album mode:** The path is a directory that directly contains audio files.
  All audio files in that directory are processed.
- **Library mode:** The path is a directory with no audio files directly inside.
  All album directories within it are processed recursively.

#### Fetch Strategy

For each track, LRCLIB is queried using title, artist, album, and duration. The
following sequence is attempted in order, stopping at the first hit:

1. Exact match via `/get` (title + artist + album + duration)
2. `/get` with duration relaxed to ±1 second
3. `/get` with duration relaxed to ±2 seconds
4. Fuzzy search via `/search` (title + artist + album, no duration constraint)

If none of the above returns a result, the track is skipped and noted in the
summary. In the worst case this is 4 requests per track, but steps 2–4 are only
reached on a miss, so the common case is a single request.

All requests are rate-limited client-side to 5 requests/second as a courtesy to
the free public API.

#### Embedding Behaviour

Synced (LRC) and unsynced lyrics are handled independently per format:

| Format | Synced lyrics                                                            | Unsynced lyrics                                          |
| ------ | ------------------------------------------------------------------------ | -------------------------------------------------------- |
| FLAC   | Embedded in `LYRICS` (LRC text, timestamps standardized to `[mm:ss.xx]`) | Embedded in `UNSYNCEDLYRICS`                             |
| MP3    | Not embedded                                                             | Embedded in `USLT` via go-taglib normalized `LYRICS` key |
| M4A    | Not embedded                                                             | Embedded in `©lyr` via go-taglib normalized `LYRICS` key |

For MP3 and M4A, if only synced lyrics are available from LRCLIB (no plain
text), the track is skipped (timestamps are never stripped and embedded as
unsynced).

Existing lyrics tags are never overwritten unless `--force` is passed. `--force`
re-fetches and overwrites all lyrics tags for every track regardless of current
state.

#### Summary Output

Follows the same style as `sums` and `rename`: a summary line at the end
reporting counts of embedded, skipped (already have lyrics), not found, and
failed tracks.

#### Implementation Notes (`internal/lyrics`)

- **LRCLIB client:** A small HTTP client wrapping the LRCLIB public API
  (`https://lrclib.net/api`). Implements the four-step fetch sequence above.
  Rate-limited via `golang.org/x/time/rate` token bucket at 5 req/s.
- **Timestamp standardization:** Applied to all LRC text before embedding via a
  four-step pipeline: (1) parse and remember any `[offset:±N]` tag; (2) strip
  all LRC metadata header lines (`ti`, `ar`, `al`, `au`, `lr`, `length`, `by`,
  `offset`, `re`, `tool`, `ve`) and comment lines (`#`); (3) normalize all
  timestamps to `[mm:ss.xx]` / `[hh:mm:ss.xx]` (2-digit centiseconds), applying
  the offset so the embedded result is self-contained; (4) strip any whitespace
  between the closing `]` of a line-level timestamp and the lyric text, as
  required by the LRC spec. Overflow values (e.g. seconds > 59) are corrected
  via duration arithmetic. Negative results from a large negative offset are
  clamped to `[00:00.00]`.
- **Tag writing:** All tag writes use go-taglib's `WriteTags` with the
  normalized `LYRICS` / `UNSYNCEDLYRICS` keys. No additional dependencies
  required beyond go-taglib.
- **Skip logic:** A track is considered to already have lyrics if the relevant
  tag(s) for its format are non-empty. `--force` bypasses this check and
  overwrites both tags.
- **Progress callback:** `Fetch` (the primary entry point) accepts an optional
  `func(path string, status LyricStatus)` callback, called after each track is
  processed. The cobra command layer passes a TTY-gated closure for live
  terminal feedback, including cases where multiple LRCLIB requests are made for
  a single track. `nil` disables all progress output, consistent with
  `hasher.Hash`.

## 5. Implementation Notes (Go)

- **Filesystem moves:** `os.Rename` for same-device moves; copy-then-delete
  fallback for cross-device (`syscall.EXDEV`).
- **Case-only renames:** Rename to a temp path first, then to the final
  destination, to handle case-insensitive filesystems correctly. The temp path
  uses a `UnixNano`-suffixed name in the same parent directory to guarantee it
  stays on the same filesystem and avoid collisions.
- **MD5 generation:** Computed via Go's `crypto/md5` package; no external tool
  required. Output is formatted to be compatible with `md5sum -c` for
  verification.
- **Concurrency:** Worker pool for tag reading. MD5 generation and lyrics
  fetching are sequential, with per-file progress reported via a callback.
- **Manual overrides:** Hardcoded in the binary (small, stable set; no config
  file).
- **Primary target:** Linux (case-sensitive filesystem). macOS is supported but
  is a secondary target.
- **Album artist resolution:** `ProcessLibrary` calls `ResolveAlbumArtist()` on
  each album and stores the result in `Album.ResolvedArtist`. Callers (the
  planner, `inspect`, `check`) read this field directly and do not need to
  invoke `ResolveAlbumArtist()` themselves.
- **Warning collection:** Non-fatal issues are collected rather than printed
  immediately. `ProcessLibrary` appends scan-phase warnings (unreadable tracks,
  unresolvable artists) to `Album.Warnings`. The planner seeds
  `AlbumPlan.Warnings` from this field and then appends its own planning-phase
  warnings (missing tags, unknown files). The display layer (e.g. `--dry-run`
  output) surfaces all warnings grouped together at the top of the output.
- **Progress feedback:** `rename`, `sums`, and `lyrics` accept an optional
  `func`-typed progress callback. The command layer passes a TTY-gated closure
  that writes `\r`-overwriting lines; passing `nil` disables all progress output
  (used in tests and non-TTY contexts). TTY detection uses
  `github.com/mattn/go-isatty`.
- **`internal/checker` second pass:** The checker opens each audio file a second
  time via `taglib.OpenReadOnly` to read `REPLAYGAIN_TRACK_GAIN`,
  `REPLAYGAIN_ALBUM_GAIN`, and embedded image metadata (`Properties().Images`).
  This is a deliberate design choice: `metadata.Track` stays focused on the
  fields needed for path planning; checker-specific audio attributes do not
  belong in the shared data model. The WASM call is read-only and inexpensive.
- **`planner.PlanAlbum`:** An exported single-album wrapper around the private
  `planAlbum` function. It creates a fresh `destMap` per call so that the
  checker can plan albums independently without cross-album collision state
  accumulating. `rename` continues to use `PlanLibrary` with a shared `destMap`
  for global collision detection.
- **`Album.ResolvedArtistSort`:** Populated by `ProcessLibrary` from the
  `ALBUMARTISTSORT` tag of the first track that carries it. Read by the planner
  for bucket determination only; never used for folder naming.
  `AlbumPlan.Bucket` carries the resolved bucket string so the display layer
  does not need to recompute it.

### Key Dependencies

| Package                                  | Purpose                                                                                               |
| ---------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| `github.com/alexsergivan/transliterator` | Unicode -> ASCII transliteration                                                                      |
| `github.com/charmbracelet/lipgloss`      | Terminal styling for CLI output (`inspect`, `rename`, `sums`, `check`, `lyrics`)                      |
| `github.com/charmbracelet/huh`           | Interactive form/prompt fields for `video add`/`video edit` (editable, pre-fillable text inputs)      |
| `github.com/deluan/go-taglib`            | Cross-format metadata reading and writing (maintained fork of `sentriz/go-taglib`, used by Navidrome) |
| `github.com/mattn/go-isatty`             | TTY detection for progress output (`rename`, `sums`, `lyrics`)                                        |
| `github.com/spf13/cobra`                 | CLI command management                                                                                |
| `golang.org/x/time/rate`                 | Token bucket rate limiter for LRCLIB requests (`lyrics`)                                              |

## 6. Music Video Support (Experimental)

Music videos (one video per track, e.g. an official music video) are managed as
a tree that is **completely separate** from the audio library, with its own root
path and its own `video` command family. Source videos are typically downloaded
via `yt-dlp` and carry no reliable embedded metadata, so — unlike audio tracks —
a video's Artist/Title are never read from the file itself. Instead they live in
a sidecar file that `musicrename` owns and writes.

### 6.1 Directory Hierarchy

```
[video-root]/[bucket]/[artist]/[title]/[title].ext
                                        /musicvideo.nfo
                                        /info.txt        (written once by `video fetch`, user-owned after that)
```

- **Bucket and artist** use the exact same first-letter bucketing and
  sanitization pipeline as the audio library (`internal/sanitize`), including
  the hardcoded bucket-override map. There is no sort-tag equivalent for video;
  the artist string in the nfo is used as-is for bucketing.
- **Title** doubles as both the directory name and the file basename (there is
  no track number, disc number, or year in the filename — a video's directory is
  the unit, not an album). Title is sanitized and truncated to 40 characters,
  matching the audio-filename cap so that the relative path in each video's
  `sums.md5` stays comfortably under 80 characters, consistent with the
  reasoning behind the audio filename limit.
- **Extension** is preserved as-is (`.mp4`, `.webm`, `.mkv` are all expected)
  and lowercased, but never transcoded by `rename`/`add`. Format conversion
  (e.g. for iPod/Rockbox compatibility) is out of scope for this phase; see
  §6.5.
- Album/year, when present in the nfo, are stored for informational and
  Jellyfin-scraping purposes only. They never affect directory placement — a
  video is filed under its artist regardless of whether it belongs to an album.
- `info.txt` is generated once by `video fetch` (see §6.3) as a small labeled
  plain-text file (`url`, `title`, `uploader`, `uploaded`, `Description:`) — not
  the raw yt-dlp JSON, which is large, version-fragile, and mostly noise for a
  human-reference file. After creation it is treated as user-owned: `add` never
  creates or modifies it, and you're free to hand-edit it. `rename` carries it
  along during path reconciliation if present (see §6.3).

### 6.2 The `musicvideo.nfo` Sidecar

A minimal subset of the Kodi/Jellyfin `musicvideo` NFO schema — enough for
Jellyfin to pick up correctly, not the full scraper-oriented field set (no
`plot`, `genre`, `director`, etc.):

```xml
<musicvideo>
  <title>Crazy in Love</title>
  <artist>Beyoncé</artist>
  <album>Dangerously in Love</album>
  <year>2003</year>
</musicvideo>
```

- `title` and `artist` are **required**; there is no fallback source for either
  (no embedded tags, no filename parsing), so a video without both is an error
  condition, not a warning.
- `album` and `year` are **optional**, reflecting that a video is often not tied
  to any particular album.
- The nfo is **always machine-written** via `encoding/xml` (marshal, not
  hand-built strings or hand-editing) — this is a deliberate design goal so that
  malformed/typo'd XML is never a concern. `video add` writes a fresh one at
  ingest time; `video edit` rewrites an existing one's fields afterward without
  ever requiring hand-editing.

### 6.3 Commands

| Command                                                                              | Description                                                                                                                                                                                                      |
| ------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `musicrename video fetch <url> [destination]`                                        | Downloads a video via `yt-dlp` and writes a generated `info.txt`. Standalone step; does not file the video into the library.                                                                                     |
| `musicrename video add <file> [video-root] [--artist] [--title] [--album] [--year]`  | Ingests a single raw video file. Prompts interactively for `--artist`/`--title` if not passed as flags; `--album`/`--year` stay optional with no prompt. `video-root` defaults to the current working directory. |
| `musicrename video edit [file-or-directory] [--artist] [--title] [--album] [--year]` | Creates or updates a video's `musicvideo.nfo`. Argument defaults to the current directory; any field not passed as a flag is prompted for, pre-filled with its current value if one exists.                      |
| `musicrename video rename [video-root]`                                              | Idempotent whole-tree reconciliation pass, analogous to audio `rename`.                                                                                                                                          |
| `musicrename video sums [path]`                                                      | Generates a per-video-directory `sums.md5`.                                                                                                                                                                      |
| `musicrename video check [path]`                                                     | Audits the video tree for missing/incomplete nfo files, path conformance, and missing `sums.md5`.                                                                                                                |
| `musicrename video inspect <file>`                                                   | Displays a single video's raw and sanitized metadata; read-only.                                                                                                                                                 |

#### `video fetch`

A thin wrapper around `yt-dlp` so the exact invocation never needs to be
remembered, and so pasting a URL copied from a playlist or a shared link with
tracking parameters doesn't carry that cruft into the library:

1. **Clean the URL.** Parse with `net/url`; extract just the video ID (the `v`
   query parameter for `youtube.com/watch` links, or the path segment for
   `youtu.be/<id>` short links) and rebuild a canonical
   `https://www.youtube.com/watch?v=<id>`. Every other query parameter (`list`,
   `t`, `si`, etc.) is discarded.
2. **Download.** Shell out to `yt-dlp --write-info-json <clean-url>` in the
   target directory (defaults to the current directory, consistent with other
   commands' path-argument defaults). No `--format`/`--merge-output-format`
   override is applied — yt-dlp's own default selection and resulting container
   (commonly `.webm` or `.mp4` depending on source) are accepted as-is,
   consistent with `add`/`rename` already handling multiple extensions.
3. **Extract fields.** Parse the resulting `.info.json` for `webpage_url`,
   `title`, `uploader` (or `channel` as a fallback), `upload_date` (`YYYYMMDD`
   -> reformatted `YYYY-MM-DD`), and `description`.
4. **Write `info.txt`** as small labeled plain-text fields, not the raw JSON:

   ```
   url:      https://www.youtube.com/watch?v=dQw4w9WgXcQ
   title:    Never Gonna Give You Up
   uploader: Rick Astley
   uploaded: 2009-10-25

   Description:
   We're no strangers to love...
   ```

5. **Delete the `.info.json`** once the fields above have been extracted;
   nothing downstream reads it, and leaving it around would reintroduce the
   noise/schema-fragility `info.txt` is deliberately avoiding.

`fetch` only downloads and writes `info.txt` — it does not prompt for
Artist/Title or file anything into the library. Running `add` afterward is a
separate, deliberate step.

#### `video add`

1. Resolve `--artist`/`--title` from flags, prompting for any that are missing.
   `--album`/`--year` are used if passed and left empty otherwise — `add` never
   prompts for them, unlike `edit`.
2. Sanitize artist and title through the shared `sanitize` pipeline; compute the
   destination path (`[video-root]/[bucket]/[artist]/[title]/[title].ext`).
3. **Error and abort if the destination directory already exists** — there is no
   `--force` overwrite path for `add`. A pre-existing destination most likely
   means a duplicate import or an artist/title typo.
4. Move the video file into place, along with `info.txt` if one is sitting
   alongside the source video (the normal case after `video fetch`, keeping the
   fetch → add workflow from requiring a manual copy step). Any other sibling
   files (e.g. a `yt-dlp`-generated thumbnail) are left untouched.
5. Write `musicvideo.nfo` into the destination directory unconditionally, using
   the resolved (unsanitized/raw) field values.

#### `video edit`

Creates or updates a video's `musicvideo.nfo` without hand-editing XML.

The argument can be either the video file or its directory; if omitted it
defaults to the current directory, so running this from inside a video's folder
needs no argument. Unlike `add`/`inspect`'s `<file>` convention, this
flexibility is deliberate: `musicvideo.nfo` has the same filename in every
video's directory, so tab-completing on it specifically (from outside the
video's own directory) carries no identifying information the way completing on
the video file's unique name, or just being in the right directory already,
does.

1. Resolve the target directory (see above), then attempt to read its current
   `musicvideo.nfo`. **A missing nfo is not an error** — `edit` creates one
   fresh in that case (e.g. for a video that was never run through `add`, or
   whose nfo was deleted), so prompts below simply start blank instead of
   pre-filled. As a sanity check against writing an orphaned nfo into the wrong
   directory, this still requires the target directory to contain a recognized
   video file; it does not require that `add` was ever run.
2. For `--artist`/`--title`/`--album`/`--year`: a flag passed explicitly
   (including as an empty string, e.g. `--year ""`) is used as-is and skips
   prompting for that field entirely. Anything not passed is prompted for via a
   `huh` form field pre-filled with its current value (blank if there wasn't
   one), so pressing enter keeps it unchanged; the field can also be edited or
   cleared in place before submitting, since `huh`'s text input is a real
   editable buffer, not a type-to-override default. Prompting is skipped
   entirely (no terminal interaction at all) if every field was passed as a
   flag.
3. Write the resulting `musicvideo.nfo`, applying the same required
   (artist/title)/optional (album/year, `omitempty`) rules as `add`.

`edit` only ever writes the nfo — it never moves the video, renames the file, or
otherwise touches the directory. If artist or title changes (or is set for the
first time), the video's location will no longer match what `add`/`rename` would
compute for the new values; running `video rename` afterward reconciles this,
which is exactly the "nfo was hand-edited to fix a typo" scenario `rename`'s
design already anticipates (see below).

#### `video rename`

Walks `video-root`, reads each directory's `musicvideo.nfo`, and re-derives the
target path the same way `add` does. If the computed path differs from the
current one (e.g. the nfo was hand-edited later to fix a typo, or a
bucket-override/sanitization rule changed), the video file, `musicvideo.nfo`,
and `info.txt` (if present) are moved together — mirroring how audio `rename`
moves all of an album's associated assets as a unit. A directory whose video
file has no accompanying `musicvideo.nfo` is skipped with a warning rather than
guessed at, the same posture as an audio track missing `ARTIST`/ `ALBUMARTIST`.

#### `video sums`

Generates `sums.md5` scoped to a single video's directory, in the same format as
audio's `sums.md5`:

- Video file: binary format (`hash *filename.ext`).
- `musicvideo.nfo` and `info.txt` (if present): text format (`hash  filename`).

Auto-detects single-video vs. library mode the same way audio `sums` does (does
the target directory directly contain a video file, or does it contain
subdirectories to recurse into).

#### `video check`

Rudimentary checks:

- Missing `musicvideo.nfo`.
- `musicvideo.nfo` present but missing `title` or `artist`.
- Path does not match what `video rename` would produce (requires a video-root,
  same constraint as audio path-conformance checks).
- Missing `sums.md5`.

#### `video inspect`

Reads a single video file and its `musicvideo.nfo`, printing raw vs. sanitized
artist/title, in the same visual style as audio `inspect`. Read-only.

### 6.4 Formats and Constraints

- Recognized video extensions: `.mp4`, `.webm`, `.mkv`. No transcoding is
  performed by any `video` command in this phase.
- Exactly one video per directory is assumed for now (no lyric-video/
  alternate-cut handling). The nfo is named `musicvideo.nfo` (fixed, not
  filename-matched) on that basis; revisit if/when multiple videos per track
  become a real need.

### 6.5 Future Work (Not Yet Implemented)

- **iPod/Rockbox conversion pipeline:** transcode videos to a uniform,
  Rockbox-compatible resolution/format/framerate for on-device playback, and
  embed Artist/Title metadata into the converted file (MP4 atoms via go-taglib,
  or via `ffmpeg -metadata` at transcode time — mechanically straightforward,
  same pattern as audio tag writing). **Open question:** whether Rockbox's video
  plugin actually reads and displays embedded metadata during playback, or only
  identifies videos by filename, is unconfirmed and should be verified against
  the target device before this is relied upon.
