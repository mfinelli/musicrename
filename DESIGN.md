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

#### Targeted Single-File Updates

`sums` and the `--force` full-regeneration path always rehash every file in the
album. Other commands that touch just one file in an already-`sums.md5`'d album
(`playlist select`, §7.3; `rename` and `video rename`, §7.10) must **not** go
through a full rehash to update it — doing so would silently recompute and
overwrite the recorded hash of every _other_, untouched file in the album, which
would mask real corruption (bit rot, a failing drive) on any file that happened
to have degraded since the last real `sums` run instead of flagging it.
`sums.md5`'s entire value as a corruption detector depends on a file's recorded
hash only ever changing when that specific file was deliberately rewritten.

Two narrower primitives are needed instead, both operating on a single existing
`sums.md5` by reading it, editing exactly one line, and rewriting the rest
through unchanged (still sorted, still stable):

- **Content changed** (a file was rewritten, e.g. `playlist select` editing
  `ipod.m3u8`): recompute that one file's hash and replace or insert its line.
  This is a real rehash, but scoped to the single file whose bytes actually
  changed.
- **Only the filename changed, content did not** (`rename`/`video rename`
  updating `sums.md5` after a file move/rename, §7.10): rewrite the filename on
  that file's existing line in place, reusing its already-recorded hash
  unchanged. No rehashing at all — the bytes weren't touched, so there is
  nothing to recompute, and doing so anyway would throw away the corruption
  check on that file for no reason.

Both primitives are no-ops (or plain insert/delete) when no `sums.md5` exists
yet in the album — they only ever touch a file that's already there.

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
   override is applied — yt-dlp's default selection and resulting container
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
video's directory) carries no identifying information the way completing on the
video file's unique name, or just being in the right directory already, does.

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
current one (e.g. `video edit` changed artist/title, a video was placed or had
its nfo created by hand, or a bucket-override/sanitization rule changed), the
video file, `musicvideo.nfo`, and `info.txt` (if present) are moved together —
mirroring how audio `rename` moves all of an album's associated assets as a
unit. A directory whose video file has no accompanying `musicvideo.nfo`, or that
contains more than one video file, is skipped with a warning.

Mirrors audio `rename`'s execution behavior: a `--dry-run` flag prints the plan
without touching files; case-only path differences (relevant on case-insensitive
filesystems, i.e. macOS) are handled via a temporary intermediate directory name
rather than a direct move; a destination that already exists at execution time
(a race since planning) is skipped with a warning rather than failing the whole
run; and now-empty source directories are removed afterward, bubbling upward but
never removing or climbing above `video-root` — so a sibling video still filed
under the same artist correctly keeps that artist directory alive. Dry-run and
live-run output are both grouped by bucket/artist, matching audio `rename`'s
presentation.

#### `video sums`

Generates `sums.md5` scoped to a single video's directory, in the same format as
audio's `sums.md5`:

- Video file: binary format (`hash *filename.ext`).
- `musicvideo.nfo` and `info.txt` (if present): text format (`hash  filename`).

Auto-detects single-video vs. library mode the same way audio `sums` does (does
the target directory directly contain a video file, or does it contain
subdirectories to recurse into).

#### `video check`

Rudimentary checks, run per video directory:

- Exactly one recognized video file is present
- Missing `musicvideo.nfo`.
- `musicvideo.nfo` present but missing `title` or `artist`.
- Path does not match what `video rename` would produce (requires a video-root,
  same constraint as audio path-conformance checks — skipped in single-video
  mode, mirroring audio `check`'s posture on a single track with no library root
  available).
- Missing `sums.md5`.

Auto-detects single-video vs. video-root mode the same way `sums` does. Exits
non-zero when any findings are present, for use in scripts, matching audio
`check`.

#### `video inspect`

Reads a single video file's `musicvideo.nfo` and prints Title/Artist alongside
their sanitized equivalents (the values that would be used when filing or
renaming), plus Album/Year shown as-is — they're stored verbatim and never
sanitized, since they never affect placement. Read-only. A video with no
`musicvideo.nfo` yet is a clear error suggesting `add`/`edit`, rather than
printing empty fields.

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

## 7. Device Sync & Playlist Management (Design / Not Yet Implemented)

Two playlist/device use cases exist. The first — Navidrome, accessed over an SMB
share — requires no work from this tool: files are already reachable at their
normal library paths, and playlist management happens entirely inside Navidrome.
This section covers the second use case only: copying a curated subset of the
library onto directly-attached removable storage. The initial target is an iPod
running Rockbox (which exposes itself as plain USB mass storage, no
iTunes/libimobiledevice involved), generalizing to a similar generic-SD-card
target (e.g. for a car head unit), which additionally may require transcoding
since car head units commonly lack FLAC support.

### 7.1 Library Roots and the Library-Root-Root

The audio library is not a single tree: `main`, `christmas`, and (eventually)
further roots such as `classical` are separate, fixed sibling directories under
one common parent, and will remain so indefinitely. The `videos` root (§6) is a
sibling too, but is excluded from all sync operations (see §7.11).

Every relative path used for sync purposes (playlist entries, §7.4) is expressed
relative to this common parent — the "library-root-root" — which means the first
path component of any such relative path is always the library root's name, and
no separate field is needed to record which root an entry belongs to.

On-device, each library root is mirrored as its own top-level directory (e.g.
`/main/...`, `/christmas/...`). This avoids artist/album name collisions between
roots on the device and keeps every path in this design root-qualified and
unambiguous.

Enumerating "every library root" for sync purposes (§7.7 step 1) is implemented
as `internal/devicesync`, `LibraryRoots`: every direct subdirectory of the
library-root-root except the reserved `playlists` (§7.4) and `videos` (§6)
names, auto-discovered rather than configured.

### 7.2 Targets (Implemented)

A "target" is a sync destination with its own constraints: which source audio
formats it can play back as-is, how (and whether) it needs unsupported formats
transcoded, and how it wants artwork delivered. Targets are a small, hardcoded
set in code (`internal/target`) — the same philosophy as the manual sanitization
overrides and `bucketOverrides` (§3.2, §3.1) — not a user-facing config file.

A target's transcode setting names a format (`AudioFormat`, e.g. `"mp3"`), not
raw encoder flags — the actual ffmpeg codec and arguments for a format live in
one shared `EncodeParams` lookup (`internal/target`, `EncodeParamsFor`),
separate from any target `Definition`. This keeps a target's own definition
readable ("transcode to mp3") without needing to repeat or remember specific
encoder settings, and means tuning or adding a format only ever touches one
place, not every target that happens to use it.

| Property               | Description                                                                                       |
| ---------------------- | ------------------------------------------------------------------------------------------------- |
| Accepted audio formats | Source formats copied through unchanged (passthrough).                                            |
| Transcode format       | `AudioFormat` anything outside the accepted set gets transcoded to; empty means never transcodes. |
| Artwork max dimension  | Controlling constraint for resized artwork, in pixels (see §7.8); no separate file-size cap.      |
| Artwork delivery       | External file only, or embedded in the audio file — see §7.8.                                     |

Initial targets:

- **`ipod`:** passthrough for FLAC/MP3/M4A — not a narrowed subset, just every
  format this tool manages at all, so `ipod` never actually transcodes anything.
  External artwork only, resized to 400px (the iPod's screen is 320x240; a
  little headroom over that, not full source resolution).
- **`sdcard`:** MP3 only — anything else is transcoded to MP3 (`libmp3lame`, VBR
  quality V0, via `ffmpeg`). Artwork is _embedded_ rather than external (500px)
  — more portable for a target that's about swapping storage between devices (a
  car head unit) than living permanently in one library layout. Since the
  external file becomes redundant once art travels with the audio file itself,
  sync does not copy `folder.jpg`/`folder.png` to `sdcard` at all — only `ipod`
  ever gets an external artwork file on-device.

A target's accepted-format set only determines _whether_ a given track needs
transcoding, not that the whole target always transcodes — a sync run against
`sdcard` will copy an already-MP3 source track through untouched and transcode a
FLAC track in the same run.

The actual embedding mechanism for `sdcard` (§7.8) — whether via the
`go.senan.xyz/taglib` library already used elsewhere in this project for tag
writes, or via an `ffmpeg` remux — is an open implementation question for when
that piece is built; a passthrough-format track needs art embedded without
needing any transcoding, so this can't simply piggyback on the transcode step
for every track.

### 7.3 Selection: Album-Local Manifests

Each album directory may contain a `{target}.m3u8` file per relevant target
(e.g. `ipod.m3u8`, `sdcard.m3u8`), living alongside `sums.md5` and the primary
artwork. It lists the filenames, from that album, selected for that target.
Despite the `.m3u8` extension, entry order carries no meaning here — this reuses
the playlist file format without its ordering semantics (contrast with §7.4).
These files are a source-side selection input only and are never copied to the
device.

#### Editing via `playlist select`

`musicrename playlist select <target> [album-path]` (§9) is the intended way to
create or update a `{target}.m3u8`: an interactive checkbox list
(`charmbracelet/huh`) of every track in the album, pre-checked against any
existing selection, sorted by `(DiscNumber, TrackNumber)` rather than
filesystem/directory order (the two coincide after `rename`, but the sort is
explicit rather than relying on that). Each row shows track number, disc number
(only when the album has more than one disc), title, and — only when it differs
from the album's resolved artist — that track's artist. A track with no `TITLE`
tag falls back to display by filename stem, since this command is expected to
typically run _before_ the final `rename` pass, unlike most other commands in
this document.

**Stale entries:** if the existing `{target}.m3u8` references a filename that no
longer matches any track currently found in the album (renamed outside the tool,
deleted, etc.), it is still shown — pre-checked, since it's still technically
part of the current selection — but as a bare filename with no tag data and a
visible warning marker, sorted after every real track. It is never silently
dropped; unchecking it removes it from the selection through the exact same save
path as any real track.

**Saving:** the file is (re)written listing every checked entry in the sorted
track order described above (readability/diffability, matching the existing
`sums.md5` stable-sort precedent — not because order is semantically meaningful
here). If every track ends up unchecked, the `{target}.m3u8` file is deleted
rather than left behind empty; an empty file and a missing file are equivalent
to the sync reconciliation in §7.7, so there's no reason to leave clutter
behind. If an existing `sums.md5` is present in the album, it is updated to
match via the targeted single-file primitive described in §3.4 — never a full
rehash — unless a flag (name TBD, e.g. `--skip-md5`) is passed to suppress this.

### 7.4 Playlists

A top-level `playlists/` directory sits as a sibling of the library roots (not
hidden — this simply means no library root may be named `playlists`). Playlists
live flat as `playlists/*.m3u8` — there is exactly one file per playlist, never
a per-target copy. Subdirectories under `playlists/` carry no scoping meaning;
`playlist check` (§7.12) walks the whole tree recursively so purely
organizational subfolders (by genre, mood, whatever) are fine to use, they just
don't do anything special.

Which targets a playlist applies to is declared inside the file itself via a
`#TARGETS:` directive line (§8.4), e.g. `#TARGETS:ipod,sdcard`. A playlist with
no `#TARGETS:` directive applies to every target. This directive exists
specifically so a playlist can be selectively scoped without duplicating the
file — an earlier version of this design used `playlists/{target}/`
subdirectories for that instead, but a playlist scoped to more than one (but not
all) targets forced writing the same file more than once, which meant two
on-disk copies of the same `#NAVIDROME-ID` that could independently drift out of
sync with each other with no warning; storing scope as data inside a single
canonical file removes the duplication (and the drift risk) entirely.

Entries are paths relative to the library-root-root (§7.1), e.g.
`main/a/artist/2020 album/01 track.flac`. Because the device mirrors the same
roots-as-siblings layout, this same relative path resolves unchanged on both
source and device — rewriting a playlist for the device is effectively an
identity operation on the path strings, just against a different base.

Unlike album-local manifests, order here is meaningful (played back as listed).
Playlists are copied onto devices that actually consume them for playback (e.g.
`ipod`/Rockbox).

**Membership implies selection:** a track referenced by any playlist whose
`#TARGETS:` directive includes a given target (or that has no `#TARGETS:`
directive at all) is automatically included in that target's desired sync set,
even if that track's album `{target}.m3u8` doesn't list it. This avoids ever
shipping a playlist with dangling entries.

### 7.5 Device-Side Layout

- Each library root becomes its own top-level directory on-device (§7.1),
  internally mirroring the source directory hierarchy (§3.1). Sanitized
  filenames are already FAT32-safe, since only lowercase `a`-`z`, `0`-`9`, and
  space survive the sanitization pipeline (§3.2).
- Rockbox builds its own tag database (tagcache) by reading embedded tags
  directly; the mirrored directory tree exists for path stability, not because
  Rockbox requires filesystem browsing. Users are expected to enable Rockbox's
  tagcache "autoupdate" setting so the on-device database stays current after a
  sync — this tool does not manage the tagcache itself.
- Copied playlists (§7.4) live at a matching `playlists/` location on-device.

### 7.6 Integrity, Drift Detection, and the "No Database" Approach

On-device `sums.md5` per album always hashes the actual bytes present on the
device — never a passthrough copy of the source's `sums.md5` — so `md5sum -c`
remains valid against a device copy at any time, matching the existing
`sums.md5` guarantee (§3.4).

- **Passthrough files:** device bytes are byte-identical to source, so the
  on-device `sums.md5` entry equals the source `sums.md5` entry for that file.
  Drift detection is a plain string comparison between the two — no re-hashing
  on either side, avoiding slow reads and unnecessary flash wear on the device
  and unnecessary work on the source.
- **Derived files** (transcoded audio, resized artwork, or any future case where
  on-device bytes are not identical to source bytes): the passthrough comparison
  can never match by construction. These get a small additional per-album
  sidecar on-device, `{target}.src.md5`, recording — per derived file — the hash
  of the _source_ file that produced it (not the derived bytes). Drift is then:
  compare the sidecar's recorded source-hash against the current source
  `sums.md5` entry for that file; a mismatch means the source changed since the
  last sync and the derived file needs to be regenerated.

No general-purpose sync-state database (SQLite or otherwise) is used. All state
needed to plan a sync is derived by walking the device tree and reading the
per-album `sums.md5` (and `{target}.src.md5` where present) already sitting on
it — keeping the device fully self-describing and portable, usable from any
machine rather than tied to one host's local state. This is expected to remain
fast even at a library of tens of thousands of files; revisit only if it becomes
a measured bottleneck.

**This has a direct consequence worth being explicit about: if the _source_ side
has no recorded hash to compare against** — the album's `sums.md5` was never
generated (the documented workflow order is `rename → lyrics → check → sums`,
but nothing enforces actually running `sums`), or exists but predates a newer
file — **there is no fallback to consult.** No database means no "last known
good" state to lean on when the primary source (source `sums.md5`) is simply
absent. The only safe default is to treat that file as unverifiable and always
resync it (§7.7 step 3) rather than ever assuming "probably unchanged" —
silently skipping would mean a real source change could go undetected
indefinitely, which is a far worse failure than an occasional unnecessary
recopy.

### 7.7 Sync Reconciliation Algorithm

1. **Desired-state computation (implemented):** union, across every library root
   except `videos`, of every album's `{target}.m3u8` entries plus every entry
   from a playlist whose `#TARGETS:` directive includes that target, or has no
   `#TARGETS:` directive at all (§7.4) — producing a flat list of
   `(root, relative path)`. Lives in `internal/devicesync` (`DesiredState`),
   alongside `PrepareTrack` (§7.9), rather than a separate `internal/sync`
   package as originally sketched — this is the same package that will house the
   rest of this algorithm as it's built out, not a distinct planner.
   `LibraryRoots` enumerates the "every library root" part: every direct
   subdirectory of the library-root-root except the reserved `playlists` (§7.4)
   and `videos` (§6) names — auto-discovered, not configured, matching how those
   two names are already reserved everywhere else in this project rather than
   adding a third way to declare "here are my library roots." An entry that
   doesn't resolve to an actual file (a stale manifest entry, an unresolvable
   playlist entry) is skipped with a warning rather than included or failing the
   whole computation, consistent with per-file misses elsewhere in this project
   (§7.3, §7.12, §8.3); the same file reachable via both an album manifest and a
   global playlist appears exactly once.

   For a target that doesn't embed artwork (§7.2 — currently `ipod`), the
   primary artwork file (§3.1's `CatPrimaryArt`: `folder.jpg`/`folder.jpeg`/
   `folder.png`, never the animated forms) for any album with at least one
   selected track is added to the desired set too — otherwise an external-art
   target would never actually receive a folder image at all. This is also what
   makes cleanup correct with no special-casing needed: an album with zero
   selected tracks contributes no artwork entry either, so if every track from a
   previously-synced album is later deselected, its on-device artwork simply
   stops appearing in the desired set on the next sync and gets removed by step
   3's ordinary "not in the desired set -> delete" rule along with everything
   else — not a distinct code path. An embedding target (`sdcard`) never gets an
   external artwork entry at all, per §7.2/§7.8.

2. **Current-state discovery (implemented):** walk the device tree for the
   target (`internal/devicesync`, `CurrentState`); for each album directory
   found (any directory containing a `sums.md5`), read it — and its
   `{target}.src.md5`, if present — into a map keyed the same way as
   `DesiredState`'s output (`DesiredEntry`, root plus relative path), so the two
   are directly comparable in step 3. Each entry records the on-device
   `sums.md5` hash (`DeviceEntry.Hash`, always present) and, only for a derived
   file, the sidecar's recorded source hash
   (`DeviceEntry.SrcHash`/`HasSrcHash`). No hashing is performed during this
   walk — only `sums.md5`/`{target}.src.md5` are read, never the audio or
   artwork files themselves. A device that hasn't been synced to before (the
   mount path doesn't exist yet) isn't an error, just an empty result; a single
   album's checksum files failing to read is a warning, not a reason to abort
   discovery of the rest of the device — removable flash storage is exactly the
   kind of thing that can have one corrupted file without the rest being
   unusable. This needed two small additions to support it:
   `internal/hasher.ReadSums(dir, filename)`, the first exported _read_
   primitive for a checksum file (everything before this was targeted mutation —
   `UpdateFile`/`RemoveFile`/`RenameFile`), generalized to work for
   `{target}.src.md5` too since it shares the exact same format; and
   `internal/target.SrcSumsFilename(name)` for the sidecar's filename.
3. **Three-way diff (implemented)**, per desired entry (`internal/devicesync`,
   `Diff`, taking `DesiredState`'s and `CurrentState`'s already-computed output
   rather than recomputing either itself, plus `libraryRootRoot` directly — it
   needs to read each entry's own source-side `sums.md5`, which is new I/O
   neither prior step does):
   - Not present on device -> **add**.
   - Present, and _either_ the device's `sums.md5` hash equals the source's
     current `sums.md5` hash directly, _or_ the device's `{target}.src.md5`
     sidecar's recorded source hash equals it -> skip.
   - Present but neither of those matches -> **regenerate and recopy**
     (retranscode/rescale as needed).
   - **Present, but no source hash is available to compare against at all**
     (source `sums.md5` doesn't exist for that album, or exists but has no entry
     for that specific file — e.g. added since the last real `sums` run) ->
     treated exactly like a mismatch, **regenerate and recopy**, never skip.
     There is no third option: without a recorded source hash there is nothing
     to compare against, and no persisted history to fall back on either (§7.6's
     whole design deliberately has none) — "assume unchanged" would mean a real
     source change could go undetected forever, so "unverifiable" has to fail
     toward "recopy," not "skip." This is reported as its own distinct warning
     (not folded into an ordinary "content changed" notice, since it's
     actionable in a way a real change isn't): something like "no sums.md5
     recorded for `<file>`; run `musicrename sums`." It fires on every sync this
     stays true, not just once, since the underlying gap is still real every
     time. The cost is asymmetric depending on what the file needs: for a
     passthrough file this is an extra copy (I/O only); for anything that needs
     transcoding, it means a full re-transcode every single sync run until
     `sums.md5` exists — the warning should say so, since it's a meaningfully
     stronger reason to actually run `sums` than the passthrough case gives. A
     missing _device_-side sidecar entry for a file that genuinely needs one (as
     opposed to a missing _source_-side hash) falls into the same regenerate
     bucket but gets no special warning — that's just normal
     first-sync-of-this-target behavior, not an indication anything's wrong.

   **Deciding "unchanged" is deliberately not based on first classifying an
   entry as passthrough or derived from a static rule** (an accepted audio
   format vs. everything else — an earlier version of this section, and the
   first version of `Diff`, worked exactly this way). That static rule breaks
   down specifically for artwork: once `Resize` can produce byte-identical
   output for an already-small JPEG (§7.8), a "passthrough-ish" artwork entry
   has no sidecar at all — nothing was derived about it — so a static rule that
   forces artwork through a sidecar-only comparison would regenerate it on
   _every_ sync even when nothing changed. Trying both checks and accepting
   either one sidesteps needing to predict in advance which applies: a genuinely
   transformed file's on-device hash can never coincidentally equal the source's
   raw hash (a resize changes dimensions, a transcode changes format entirely),
   so there's no risk of the direct check masking a real change for that kind of
   file — it can only ever help the case a static rule would otherwise miss.

   Each album's source `sums.md5` is read at most once per `Diff` call
   regardless of how many of its files are desired, cached internally by album
   directory.

   Every on-device file _not_ in the desired set -> **delete**; directories left
   empty by deletions are removed too, bubbling upward but never above the
   root's top-level device directory — mirroring `rename`'s existing
   empty-directory cleanup (§4.2).

4. **Capacity check (implemented):** no `du` is needed anywhere.
   `internal/devicesync`, `CheckCapacity` builds a `CapacityReport` from three
   numbers, none requiring a directory-size walk: `NeededBytes` sums each
   add/regenerate entry's _source_ file size (a deliberate approximation — the
   eventual on-device size for a transcode or resize isn't known without doing
   the work, and this tends to overestimate for transcoding targets, which is
   the conservative direction to be wrong in); `FreedBytes` sums each delete
   entry's already-known on-device size (`CurrentState`'s own `os.Stat` during
   its walk, extended with a `Size` field for exactly this); `AvailableBytes`
   comes from one `Statfs` call against the device
   (`golang.org/x/sys/unix.Statfs`, restricted to `linux || darwin` via a build
   tag). `Sufficient()` credits space freed by the plan's own deletions against
   what's needed, since deletions always happen before anything needing that
   room. This step depends on step 3's diff to know how much needs adding — it
   isn't independently useful on its own the way `Statfs`'s raw free-space read
   is.

   `unix.Statfs_t`'s field names (`Bavail`, `Bsize`) are the same on Linux and
   macOS, but their underlying integer types differ by platform, so explicit
   conversions — not a per-OS file split — are what make one implementation safe
   for both; confirmed against a real, shipped cross-platform tool using this
   identical pattern (the `lf` file manager's `df_statfs.go`), and the Linux
   path specifically was compiled and actually run against a real filesystem
   during development, not just reasoned about from documentation. The Darwin
   path is unverified here (no macOS available) but shares the same code, not a
   separate, less-tested implementation.

5. **Output:** dry-run is always available first. Default output is a summary —
   counts and total bytes for add / regenerate / delete-file / delete-dir, plus
   the capacity delta from step 4. `--verbose` itemizes every add and regenerate
   individually; deletions are only itemized under `--verbose` even then, since
   deletion only affects the device copy — source data is never touched, so the
   worst case of an unwanted deletion is a stale `{target}.m3u8`/playlist entry
   to fix and a re-sync.

### 7.8 Artwork Handling (Resize Implemented)

- `ipod` uses external artwork only (400px); `sdcard` embeds artwork instead
  (500px) rather than shipping it externally — more portable for a target that's
  about swapping storage between devices than living permanently in one library
  layout — and does not get an external `folder.jpg`/`folder.png` copied to it
  at all as a result (§7.2).
- On sync, external artwork is resized to the target's fixed max dimension in
  pure Go (`internal/artwork`, `Resize`/`ResizeFile`) — `image/jpeg`,
  `image/png`, and `golang.org/x/image/draw` for the scale itself (`CatmullRom`,
  a quality resampler) — rather than `ffmpeg`; see §7.9 for why `ffmpeg`'s role
  in this project ended up scoped to audio transcoding only. Output is always
  re-encoded as JPEG at a fixed quality (85), even when the source was already
  smaller than the target dimension or already a JPEG — deterministic output
  regardless of the source's format or prior encoding, rather than a conditional
  "sometimes pass through unchanged" special case. Dimension is the controlling
  constraint; file size is whatever falls out of dimension + quality, not an
  independent target. An image already within bounds in both dimensions is never
  upscaled.
- Artwork that's actually resized (not the byte-identical-passthrough case just
  above) is a derived file exactly like transcoded audio (§7.6): it gets a
  `{target}.src.md5` sidecar entry keyed off the _source_ artwork file's hash
  (already tracked in the album's real `sums.md5`), so a source artwork change
  is detected and triggers a recopy of the resized artwork the same way a source
  audio change triggers a recopy of that track. An artwork write that happens to
  produce byte-identical output gets no sidecar entry at all — the same way an
  ordinary audio passthrough never gets one — since §7.7 step 3's diff can
  already confirm "unchanged" with a direct hash comparison in that case;
  writing a sidecar anyway would just be redundant bookkeeping for a file that
  isn't actually derived at all in the sense that matters (§7.6: "derived" means
  on-device bytes aren't identical to source — a definition that's about actual
  outcome, not file type).
- For `sdcard`'s embedded artwork, an artwork change additionally requires
  re-embedding (re-tagging, not re-transcoding) every already-synced track in
  that album for that target — cheaper than a full retranscode, but still a real
  pass over every file. This applies unconditionally for `sdcard` now, rather
  than being a hypothetical gated on some future target's setting.

  This requirement needed a real mechanism, not just a stated intent: §7.7 step
  3's diff has no separate desired entry for `sdcard`'s artwork to compare on
  its own account (embedding targets never get one, per this section), so an
  artwork-only change — the audio itself untouched — would otherwise be
  invisible to a diff that only ever compared each track's own audio hash.
  `CurrentState`'s `AlbumArtHash` (`internal/devicesync`) solves this: a
  per-album record of the artwork hash last used to embed, read from the
  `{target}.src.md5` sidecar's own entry for the artwork filename (e.g.
  `folder.jpg`) — a genuinely valid, correctly-formatted line even though no
  such file exists on-device for an embedding target (the artwork lives inside
  each track, not as a file of its own). This isn't a new kind of impurity:
  every `{target}.src.md5` entry already cross-references a _source_ hash rather
  than the on-device file's own hash, so one more provenance-only line fits the
  same established pattern. `Diff` compares this against the artwork's current
  source hash in addition to the audio's own comparison — both must match for a
  track to be skipped.

  `AlbumArtHash` is deliberately only ever populated for a target whose
  `Definition` has `EmbedArt` set. Nothing would actually break without that
  gate — a non-embedding target's artwork already gets its own ordinary desired
  entry and is tracked through the normal `Hash`/`SrcHash` mechanism like any
  other file, so the field would just sit there unused for `ipod` — but leaving
  it ungated meant it could get incidentally populated whenever a non-embedding
  target's artwork happened to be genuinely resized (which leaves an entirely
  normal-looking `folder.jpg` line in _that_ target's own `{target}.src.md5`
  too), making the field's presence ambiguous about what it actually meant.
  Caught during review after an initial version's doc comment claimed the field
  was "absent... for a non-embedding target, which never writes this entry at
  all" — a claim the code, as first written, didn't actually satisfy.

- The embedding mechanism itself is `go.senan.xyz/taglib`'s `WriteImage`, not an
  `ffmpeg` remux (confirmed against the library's actual source: it exposes
  `WriteImage`/`WriteImageOptions`, backed by `taglib_file_write_image`,
  handling the format-specific frame — ID3v2 `APIC`, FLAC `PICTURE`, MP4 `covr`
  — behind one call). This also cleanly covers a passthrough-format track
  (already MP3) needing art embedded without needing any transcoding, which a
  "ride along with the transcode step" approach couldn't have handled uniformly.
  Not yet implemented — this section covers artwork resizing only.

### 7.9 Transcoding (Audio Implemented)

- Implemented by shelling out to `ffmpeg` (`internal/transcode`, `Audio`),
  mirroring the existing `yt-dlp` shell-out pattern used for music video
  fetching (§6) — including the same injectable-runner test structure, so the
  surrounding logic is testable without a real `ffmpeg` binary — rather than
  calling dedicated encoder binaries (`lame`, `flac`) directly: one external
  dependency instead of several, and already required regardless for future
  video work (§6.5). Most non-minimal distro `ffmpeg` builds link `libmp3lame`,
  so this doesn't give up LAME's encoder, just calls it through `ffmpeg`'s CLI;
  worth confirming with `ffmpeg -encoders | grep libmp3lame` on the target build
  before relying on it.
- Encode parameters are hardcoded, but keyed by format (`AudioFormat`,
  `EncodeParams`, `internal/target`) rather than duplicated per target — a
  target's `Definition` only names the format it wants (e.g. `sdcard` wants
  `mp3`); the actual `libmp3lame`/VBR-quality-V0 settings live once, in the
  format lookup, not repeated per target.
- A target only transcodes tracks whose source format falls outside its
  accepted-formats set (§7.2); accepted-format tracks pass through untouched, so
  a single sync run against a transcoding target can produce a mix of copied and
  transcoded output.
- **Tags and artwork are deliberately excluded from the transcode call itself**
  — `-map_metadata -1` strips whatever `ffmpeg` would otherwise try to carry
  over, and `-vn` drops any embedded picture stream, rather than trusting
  `ffmpeg`'s own Vorbis-comment-to-ID3v2 mapping to cover every tag this project
  cares about. Both are migrated afterward as separate, deliberate steps using
  this project's existing tag mechanism (`go.senan.xyz/taglib`, already used
  everywhere else tags are read or written — `WriteTags` with the same
  normalized cross-format tag representation `check`/`inspect`/`lyrics` already
  use, and `WriteImage` for artwork, §7.8). This guarantees every tag the rest
  of the tool already recognizes migrates consistently through one
  representation, rather than depending on however completely `ffmpeg`'s own
  format-conversion heuristics happen to overlap with this project's own tag
  vocabulary — and avoids `ffmpeg` carrying over a stale, unresized embedded
  picture that a later artwork step would then need to detect and overwrite.
  This is tied together in `internal/devicesync`, `PrepareTrack` — the per-track
  building block the not-yet-built reconciliation algorithm (§7.7) will call
  once per file it decides needs syncing.
- **Tags are migrated only on the transcode path, never for a passthrough
  copy.** A passthrough file is meant to stay byte-for-byte identical to its
  source (§7.6 — that identity is what lets on-device drift detection skip
  rehashing entirely and just compare recorded hashes as strings). Rewriting
  tags on it — even with already-correct values — means `taglib` re-serializing
  the tag block, which is under no obligation to reproduce the source's exact
  original bytes (frame ordering, padding, etc. can differ even with identical
  values); doing that on a passthrough copy would silently break the
  byte-identity guarantee for every passthrough track. A transcode needs tags
  written regardless, since it strips them outright and the destination is
  already a different file by construction — there's no byte-identity property
  to protect there. Artwork embedding is not the same concern and applies on
  both paths when the target embeds: for any `EmbedArt` target (`sdcard`),
  on-device bytes were never meant to be identical to source in the first place,
  and §7.6's derived-file handling (a `{target}.src.md5` sidecar) already
  accounts for that.
- Artwork resizing turned out not to need `ffmpeg` at all: Go's standard library
  (`image/jpeg`, `image/png`) plus `golang.org/x/image/draw` for the resize
  itself cover it, and `go.senan.xyz/taglib`'s `WriteImage` handles embedding
  directly — TagLib's own format-specific frame handling (ID3v2 `APIC`, FLAC
  `PICTURE`, MP4 `covr`) sits behind one uniform call, so no new dependency or
  `ffmpeg` invocation is needed for either half of artwork handling (§7.8).
  `ffmpeg` in this project ends up scoped to audio transcoding only.

### 7.10 Interaction with `rename`

This logic lives in `internal/renamesync`, not in `cmd/rename.go` — the
project's stated split (§4: business logic in `internal/`, testable without a
terminal; user interaction in `cmd/`) applies here too, so the sync pass is a
plain `Sync(plan, skipMD5, skipPlaylists) []string` function `cmd/rename.go`
calls after `executor.Execute`, with its own test suite exercising the edge
cases below directly against `planner.Plan` fixtures rather than through the
CLI.

- **Album-local manifests** (`{target}.m3u8`) and **`sums.md5`**: after a real
  (non-dry-run) `rename` run, for every file whose path relative to its own
  album root actually changed (a real filename change, or a case-only rename — a
  directory-only move needs no follow-up, since these paths are relative to the
  album root, not absolute), `rename` updates `sums.md5` in place if it exists:
  only the renamed entry's filename is rewritten via the targeted
  `hasher.RenameFile` primitive (§3.4) — the hash is left untouched, since the
  file's content didn't change, only its name did. For audio files specifically,
  any `{target}.m3u8` referencing the old filename is updated to the new one the
  same way (`playlist.RenameEntry`). This applies to _any_ moved file (audio or
  asset) for `sums.md5`, but only to audio files for the manifest update, since
  only audio track filenames ever appear in a selection manifest.

  A track's rename rewriting a manifest's _content_ is a different case from the
  track's filename-only rename: the manifest file's bytes genuinely changed (a
  line inside it was rewritten), so its own `sums.md5` entry, if it has one,
  needs a real rehash via `hasher.UpdateFile` — not a `RenameFile` filename swap
  — or `sums.md5` would record a stale hash for a file this same run just
  legitimately edited, producing a false corruption signal on the very next
  verification. So `--skip-md5` isn't quite risk-free in every case as
  originally framed: the audio-file-rename half is pure bookkeeping with zero
  rehash risk, but the manifest-content half is a real, necessary rehash scoped
  to the one file that actually changed — consistent with, not an exception to,
  `sums.md5`'s core guarantee (§3.4). `--skip-md5` and `--skip-playlists` opt
  out of each independently. A move whose destination doesn't actually exist on
  disk afterward (an executor-level race-condition skip) is left alone, so
  nothing ever references a file that was never created. All of this is
  best-effort: failures surface as warnings rather than aborting, since by that
  point every file move has already succeeded.

- **Global playlists** (`playlists/`): out of scope for `rename`, which has no
  visibility outside the single album it is processing at a time. Instead,
  `musicrename playlist check` (§7.12) audits the `playlists/` tree separately
  for dangling entries, since — unlike album-local manifests — it has no
  per-album scope for `check`/`rename` to hook into.
- **`video rename`** (§6): the same `sums.md5` filename-only update applies — a
  video's filename is title-derived and so can change independently of its
  directory move. This surfaced a related gap: `video rename`'s executor
  previously didn't move `sums.md5` along with the rest of a video directory's
  contents at all, orphaning it on any real move. Fixed as a prerequisite:
  `sums.md5` now travels with the directory unconditionally (like
  `musicvideo.nfo` and `info.txt`), with only the _content_ update (the renamed
  entry) gated by `--skip-md5`. There is no manifest/playlist concept for
  videos.

### 7.11 Explicitly Out of Scope (For Now)

- The `videos` library root (§6) is excluded from all sync operations; a
  Rockbox-targeted video pipeline is tracked separately under §6.5 as a later
  phase of this same work.
- No dedicated sync-state database (SQLite or otherwise) — see §7.6.
- The Navidrome/SMB use case is unaffected by any of the above and continues to
  be handled entirely within Navidrome.

### 7.12 Checking Playlists (Implemented)

Auditing splits across two places, matching the same scope boundary used
throughout this document — per-album vs. library-wide:

- **Album-local manifests** (`{target}.m3u8`, §7.3): a new finding category in
  the existing `musicrename check` (§4.3), added alongside its other per-album
  checks. Two things are flagged:
  - A manifest for an unrecognized target name (e.g. a stray `xbox.m3u8` —
    target names are a small, hardcoded set, `internal/target`, so this is
    almost certainly a typo or leftover cruft, not a real target).
  - For a manifest whose target name _is_ recognized, an entry that no longer
    matches any track currently found in the album — the same "stale entry"
    condition `playlist select` (§7.3) detects interactively, surfaced here as a
    passive audit finding instead. Not checked on an unrecognized-target
    manifest, since that manifest is already flagged as a whole.
- **Global playlists** (`playlists/`, §7.4): a new
  `musicrename playlist check [library-root-root]` command (§9), not folded into
  `musicrename check` itself. `check`'s scope model is "a library root, or a
  single album within one" — album-local manifests fit that model directly, but
  global playlists don't: they're not inside any library root, they're a sibling
  of all of them, keyed to the library-root-root (§7.4/§8.1). Teaching `check` a
  second, unrelated scope concept for one feature seemed like the wrong trade
  against a small dedicated command. It walks `playlists/` recursively
  (subdirectories carry no scoping meaning under the flat, `#TARGETS:`-based
  structure in §7.4, but are harmless to use for organization, so the walk
  doesn't assume a flat layout) and flags:
  - An entry whose path doesn't resolve to an actual file anywhere under the
    root (the dangling-entry case originally described as living in `check`
    itself; relocated here instead once the scope mismatch above became clear).
  - An unrecognized target name inside a `#TARGETS:` directive (§8.4) — the same
    typo/cruft-catching reasoning as the album-local unrecognized-target check
    above.
  - Two or more playlist files sharing the same `#NAVIDROME-ID` directive (§8.4,
    §8.9). Under the current one-file-per-playlist structure this is never
    legitimate — an earlier design revision used a directory-per-target layout
    instead, where the same ID appearing on more than one file was the
    _expected_ result of deliberately scoping a playlist to several targets,
    which would have made this check a heuristic (same ID, differing content)
    rather than an unconditional error. Moving target scope into the `#TARGETS:`
    directive (§7.4) removed that legitimate-duplication case entirely, so any
    duplicate ID found today is unambiguously a mistake.

  Reading a library-wide playlist file for this command uses three small new
  `internal/playlist` functions — `ReadEntries` (plain entries, skipping
  `#`-prefixed directive lines and blank lines), `ReadNavidromeID` (extracts a
  `#NAVIDROME-ID:` directive's value if present), and `ReadTargets` (extracts
  and splits a `#TARGETS:` directive's value if present) — distinct from
  `ReadManifest`/`WriteManifest`, which are keyed by an album directory and
  target name and only ever apply to album-local manifests. All three new
  functions take an explicit file path instead, since library-wide playlist
  files live at arbitrary discovered locations rather than a predictable
  per-album name. Neither command modifies anything; both are read-only audits,
  consistent with `check`'s existing behavior, exiting non-zero when findings
  are present.

## 8. Navidrome Playlist Sync (Design / Not Yet Implemented)

This is distinct from §7: the Navidrome use case is SMB-mounted, so audio files
are never copied by this tool — Navidrome reads the library live over its own
(read-only, from Navidrome's side) mount. What needs syncing is playlist
_membership_, bidirectionally — playlists authored locally in `playlists/`
(§7.4) pushed to Navidrome, and playlists created or edited within Navidrome
itself (e.g. from a phone) pulled back down. This section is not a `target` in
the §7.2 sense and shares none of the audio-copy, transcode, or artwork-resize
machinery from that section.

### 8.1 Authentication (Implemented)

Credentials cannot follow the "hardcode it in code" pattern used for §7.2
targets, since the repository is public. Instead, `musicrename login` prompts
for and stores them; `musicrename logout` clears them.

**What's actually stored, and why it's not an "API token":** Navidrome has no
separate, revocable API-token concept distinct from the account password. Two
auth surfaces exist:

- The **native API** (`/api/*`) uses `POST /auth/login` with a
  username/password, returning a JWT that expires in ~48h by default and
  _rotates on every request_ — a session model, a poor fit for a CLI that might
  run once a week.
- The **Subsonic API** (`/rest/*`, already needed regardless for the
  scan-trigger in §8.2 and the playlist CRUD in §8.3) is stateless per request:
  each call carries a username plus a token computed fresh as
  `md5(password + random_salt)`. No login call, no expiry, no rotation to manage
  — just the password on hand to compute a valid signature each time.

Since the Subsonic API is already the natural choice for everything else in this
design, `login` builds on it too: **what's stored is the username and
password**, not a token, and each request computes its own salt/token pair at
call time (`internal/navidrome`, `saltedToken`) rather than reusing a cached
one. This does mean the stored credential is the actual account password, not an
independently scoped or revocable one — worth using a dedicated Navidrome user
for this tool rather than a primary account, purely so the blast radius of that
file is limited.

`saltedToken`'s use of MD5 is a protocol requirement, not a choice — static
analysis (CodeQL's `go/weak-sensitive-data-hashing`) will flag it, since its
underlying concern is normally about an algorithm being too fast to resist
offline brute-forcing of a _stored_ password hash. That doesn't apply here: this
value is computed fresh per request and never stored anywhere, and a stronger
algorithm would simply fail to authenticate against Navidrome (or any other
Subsonic-compatible server), since the server independently computes the same
value to compare against. Suppressed inline at the call site with a
`codeql[go/weak-sensitive-data-hashing]` comment and an explanation, rather than
dismissed silently.

**Storage:** a JSON file (`encoding/json`, no new dependency for something this
small) at `$XDG_CONFIG_HOME/musicrename/navidrome.json` — via Go's
`os.UserConfigDir()`, which already resolves `XDG_CONFIG_HOME` (or `~/.config`)
on Linux and the platform-appropriate equivalent elsewhere, rather than
hand-rolling XDG lookup. The file is written `0600` and its parent directory
`0700`, both owner-only. musicrename supports one configured server at a time
(§8, "single server" decision) — `login` run again simply overwrites whatever
was stored before; there's no profile concept to select between.

**`login`'s shape:** `--url` and `--username` may be passed as flags or left to
be prompted for (`charmbracelet/huh`). The password is never accepted as a flag
under any circumstance — a secret passed as a command-line argument leaks into
shell history and is visible to other users on the same machine via `ps`. By
default it's prompted for interactively, masked (`huh.EchoModePassword`);
`--password-stdin` reads it from stdin instead (reading all of stdin, trimming a
trailing `\r\n`), for scripting — piping from a password manager, or a bootstrap
script — without ever needing an interactive terminal. `--password-stdin` fails
fast if stdin is actually a live terminal rather than something redirected
(checked _before_ any prompting starts, including for `--url`/`--username` if
those are also missing), rather than silently hanging waiting for input that
will never come. `--password-stdin` alone doesn't force a fully non-interactive
invocation — `--url`/`--username` are still prompted for if not also passed as
flags — full automation just means passing all three.

Before writing anything to disk, `login` validates the credentials against the
server via `/rest/ping`, so a typo'd URL or wrong password is caught immediately
rather than surfacing later as a confusing failure mid-sync.

`logout` is a pure local file removal — since there's no server-side session
under the Subsonic auth scheme (see above), there's nothing to invalidate
remotely.

Any other Navidrome sync command errors out immediately if no credentials are
stored, rather than the tool gaining a broader user-facing configuration system.

### 8.2 Scan-Before-Sync (Implemented)

Before any track resolution, sync triggers a manual library scan via
`/rest/startScan` and polls `/rest/getScanStatus` until it reports complete
(`internal/navidrome`, `Scan`). This guarantees Navidrome's view of the
filesystem is current — recently added, renamed, or removed tracks resolve
correctly — before any ID lookups run. This addresses scan staleness only; it is
a separate concern from the playlist-membership handling in §8.5-8.6.

Built on
[`github.com/supersonic-app/go-subsonic`](https://github.com/supersonic-app/go-subsonic)
rather than a hand-rolled client for this and the playlist operations to follow
(§8.3, §8.5-8.7) — an actively maintained library (used by the real Supersonic
desktop client), GPL-3.0 (matching this project's license), whose typed methods
(`StartScan`, `GetScanStatus`, and later the playlist CRUD methods) avoid
re-deriving several endpoints' exact JSON shapes from scratch, including
handling the OpenSubsonic HTTP-POST-vs-GET extension automatically for longer
requests. Its own `Authenticate` generates its salt with `math/rand` rather than
`crypto/rand` — weaker than the `saltedToken`/`Ping` already built for `login`
(§8.1) — and its `salt`/`token` fields are unexported, so there's no way to
inject `saltedToken`'s output instead without forking the library. Accepted
deliberately: the value is still unique per process run, never persisted, and
travels over TLS: a minor, disclosed downside, not a serious one. `login`'s
validation (§8.1) is unaffected — it doesn't use this library at all.

`Scan`'s status is checked immediately after starting, before any waiting — the
common case (an incremental scan where little or nothing changed since the last
sync) often finishes before the first poll would even happen, and there's no
reason to make that case wait a full poll interval for no benefit. The default
poll interval thereafter is 1 second (`DefaultScanPollInterval`): short enough
that a quick scan is noticed within about a second of finishing, without being
so aggressive it's needless chatter against the server for a scan that genuinely
takes a while. `Scan` reports a `ScanProgress{Elapsed, Count}` after every
still-running check via an optional callback, so a caller can show something
concrete rather than apparent silence for however long a longer scan takes — a
sync operation that scans before doing anything else would otherwise look like
it had hung. `internal/navidrome` stays presentation- agnostic (no TTY
detection, no `\r`-based console rendering) per this project's `internal`/`cmd`
split (§4); rendering that progress to the terminal is the concern of the
`sync navidrome` commands that call `Scan` (§8.5-8.7, not yet implemented),
matching the existing TTY-gated `\r` progress pattern already used by
`rename`/`video rename`.

### 8.3 Track Resolution

Local tracks are identified by `(root, relative path)` (§7.1); Navidrome
identifies tracks by an internal song ID, and Subsonic-API song objects carry a
`path` field (relative to the configured music folder) alongside that ID.
Resolution is a lookup in both directions:

- **Push (implemented):** local relative path -> Navidrome song ID. No direct
  "get song by path" endpoint exists, so this enumerates the server's entire
  song catalog once per push run — paginated `search3` calls with an _empty_
  query string (`internal/navidromesync`, `buildSongIndex`) — into an in-memory
  `path -> ID` map, rather than issuing one search per track. This isn't an
  undocumented trick: Navidrome explicitly optimizes empty-query search3
  pagination for exactly this case, describing it as the mechanism clients like
  Symfonium already use to mirror a whole library. The index is built exactly
  once per `push` invocation and reused across every entry in every playlist
  being pushed in that run — a 1,000-track playlist costs a small, fixed number
  of requests (page size 500) rather than 1,000 individual searches, and
  `PushAll` pushing several playlists doesn't rebuild it per file.
- **Pull (implemented):** turns out not to need a separate lookup at all — a
  fetched playlist's `entry` list already carries each track's `path` directly
  (`internal/navidromesync`, `applyRemotePlaylist`), so pull just checks that
  path resolves to a real local file rather than searching for it. This relies
  on an assumption this project can't verify or enforce: Navidrome's configured
  music folder has to be the library-root-root itself (§7.1) — the same parent
  directory `main`/`christmas`/etc. sit under — not, say, a separate music
  folder per library root. If it's configured differently, every entry's `path`
  would be relative to a different base and nothing would resolve. Worth
  confirming on the Navidrome side before relying on this.

A track that fails to resolve is skipped with a warning rather than failing the
whole sync — consistent with how per-file misses are handled elsewhere in this
document (e.g. `rename`, `lyrics`).

### 8.4 Local Playlist File Conventions

Each locally-authored playlist file (§7.4) carries extended-M3U comment lines at
its top:

- `#PLAYLIST:<name>` — the playlist's real display name, independent of its
  (ASCII-sanitized, §3.2) filename. A standard extended-M3U directive, not a
  `musicrename` invention.
- `#NAVIDROME-ID:<id>` — the corresponding Navidrome playlist's internal ID,
  once one exists; absent on a playlist that has never been pushed.
- `#TARGETS:<comma-separated target names>` (§7.4) — which sync targets this
  playlist applies to, e.g. `#TARGETS:ipod,sdcard`. Absent entirely means "every
  target." This is what lets one playlist file be scoped to more than one (but
  not all) targets without needing a second on-disk copy.

**`#TARGETS:` is reconciled bidirectionally through Navidrome's `comment` field
(implemented, `internal/navidromesync`, `comment.go`), not treated as local-only
data.** Navidrome has no directive concept of its own, but does have a plain,
human-editable comment field on every playlist — musicrename manages a
recognizable _suffix_ of it, `[musicrename:targets=ipod,sdcard]`, rather than
owning the whole field, so a real description can still live in the same
comment. Push composes this suffix onto whatever human text is already there
(fetched fresh each time, never assumed); pull parses it back out and uses it as
the source of truth for local `#TARGETS:`, the same way name and entries are
already treated — not preserved from the existing local file. Local `#TARGETS:`
being removed reconciles onto the remote side correctly too: push simply stops
appending a suffix, leaving the human text untouched, and a suffix removed from
the remote side (by hand, in the Navidrome app, or by any other client)
reconciles back to "no `#TARGETS:`" on the next pull.

Wherever a target list is written — the local `#TARGETS:` directive
(`playlist.WriteGlobalPlaylist`) or the comment suffix (`composeComment`) — it's
sorted alphabetically first, so the on-disk/on-server form is always canonical
regardless of the order targets happened to be added or read in. Change
detection on both sides compares target lists (or, on push, the fully-composed
comment string) order-insensitively rather than as raw strings, precisely so a
list that's semantically identical but happened to arrive in a different order —
a hand edit, or content from a version predating this convention — doesn't
register as "changed" and get rewritten for no real reason.

Correlation between a local file and a remote playlist is by the
`#NAVIDROME-ID`, never by filename or display name — renaming a playlist locally
does not orphan or duplicate its remote counterpart. Because there is exactly
one file per playlist (§7.4), a given `#NAVIDROME-ID` should never legitimately
appear on more than one file; `playlist check` (§7.12) treats any duplicate as
an error unconditionally, not a heuristic.

### 8.5 Pull / Edit / Push as a Session, Not a Diff

Sync is a deliberate two-step operation, run as one session: **pull** first
(implemented, `internal/navidromesync`), then — after any local edits — **push**
(also implemented). This is not a three-way diff against remembered prior state
(contrast with the on-device sync in §7.6-7.7, where the device itself is
self-describing): pull overwrites local playlist contents with whatever
Navidrome currently holds; push overwrites the Navidrome side with whatever the
local file now says. Because there is no diffing step, there is no ambiguity
about which side a change originated from, and no persisted sync-state file is
needed for the ordinary create/update case — consistent with §7.6's no-database
principle.

`PullAll` reconciles every playlist in one pass — for each remote playlist:
overwrite an already-correlated local file's content (preserving its `#TARGETS:`
directive, which Navidrome has no concept of and must never be silently stripped
by a pull), or create one at `playlists/<sanitized-name>.m3u8` (flat, no
`#TARGETS:`, per §9.1) if this is the first time it's been seen. `PullOne` does
the same for a single already-correlated local file (§8.7), using a direct
`getPlaylist` lookup instead of the bulk list. A per-playlist detail-fetch
failure during a bulk pull is a warning, not a reason to abort the rest of the
run; the initial `getPlaylists` list call failing outright, or an entry that
can't be resolved locally (§8.3), are handled per that section's and §8.8's
rules respectively.

`PushAll`/`PushOne` mirror this for the opposite direction. A local file with no
`#NAVIDROME-ID` is created remotely (name plus resolved tracks) via a
create-then-populate sequence — `createPlaylist` with just a name, so the server
hands back the new ID directly, then a separate call to add the resolved tracks
— rather than trying to create-with-tracks in one shot, specifically so the new
ID is available to write back into the local file without a second, ambiguous
lookup-by-name. The `#TARGETS:`-as-comment-suffix (§8.4) is set via a follow-up
`updatePlaylist` call too, since a `comment` param at creation time isn't
reliably supported across Subsonic-compatible servers. An already-correlated
file has its remote state fetched first (needed either way, to compare against
local and decide whether anything needs to happen at all — comment included, so
a `#TARGETS:`-only change still counts as a real difference, not silently
ignored) and, if it differs, is brought in line in two steps: remove every
existing track by index, then add the desired tracks back in order — since
Subsonic's `updatePlaylist` has no single "replace all tracks" operation, and
removals are index-based against whatever's already there while additions are
simply appended, doing both in one call wouldn't reliably produce the exact
local order. If remote name, comment, and entries already match local exactly,
no request is made at all.

The tradeoff is explicit: this is a checkout/edit/check-in model, not a
continuously-merged one. An edit made in the Navidrome app _during_ an open
local pull-edit-push session is silently overwritten by that session's eventual
push. Acceptable for a single-user personal tool; not a general-purpose
multi-writer sync.

### 8.6 Deletion Semantics

Deletion is handled asymmetrically, and deliberately so — the two directions
carry different amounts of information:

- **Local file removed by hand (`rm`), not through `musicrename`:** the file and
  its `#NAVIDROME-ID` are simply gone, so nothing distinguishes "this was
  deliberately deleted" from "this was never pushed at all." The default is the
  non-destructive read: the next **pull** treats the still-remote playlist as
  newly discovered and recreates the local file (with its original ID comment
  restored). An accidental `rm` self-heals rather than propagating; a genuine
  deletion requires the explicit delete command (§8.7), never a bare `rm`.
- **Playlist deleted directly on the Navidrome side** (mobile app, web UI): the
  local file still has a concrete `#NAVIDROME-ID` to check. Pull looks that ID
  up; a confirmed **404 / not-found** response is unambiguous — that playlist
  existed and is now gone — so pull deletes the local file to match. This is
  intentionally the more automatic of the two directions: a phone-side deletion
  should "just work" without requiring a `musicrename`-enabled machine to also
  go delete the file by hand.

  This must trigger only on a confirmed not-found response, never on a generic
  request failure (wrong/stale credentials, network error, 5xx) — see §8.8.
  Dry-run always surfaces a pending local deletion before it happens.

  Both halves are implemented (`internal/navidromesync`). `PullAll`'s "recreate
  on rediscovery" behavior for the first case falls out of its general
  reconciliation logic for free — an `rm`'d file is simply absent from the local
  index, so a still-remote playlist looks exactly like one never pulled before
  and gets a fresh local file (a new, sanitized-name file, since the original
  filename itself isn't remembered — only the correlation by ID is restored, not
  the exact prior name). `PullOne`'s confirmed-not-found detection for the
  second case relies on parsing a Subsonic API error code out of the go-subsonic
  library's error message (`internal/navidrome`, `ErrCode`/`ErrCodeNotFound`) —
  the library discards the structured error object it parses internally and
  returns only a formatted string, with no typed error otherwise available to
  check.

### 8.7 Explicit Single-Playlist Operations (Delete Implemented)

A dedicated command allows pulling, pushing, or deleting one playlist by
name/path directly, outside a full sync pass — primarily to correct an
accidental deletion (re-push a playlist that pull just removed locally, or
re-pull one mistakenly deleted remotely) without re-running the whole library
sync. Explicit delete (`internal/navidromesync`, `DeleteOne`) reads the
`#NAVIDROME-ID` out of the local file before removing anything, deletes the
remote playlist by that ID, then removes the local file — this is the only
sanctioned way to perform a real, intended deletion. If the remote delete fails
because the playlist is already gone (a confirmed not-found response, same sense
as §8.6), the local file is still removed — that end state is already
half-achieved — but any other remote failure (§8.8) aborts without touching the
local file at all.

### 8.8 Server-Error Handling

Any operation — bulk sync or the single-playlist commands in §8.7 — aborts
immediately on a 5xx response from the server, especially for destructive
actions (local or remote deletion). A server error must never be interpreted as
a not-found/confirmed-absent result (§8.6); the two are handled completely
differently, and conflating them risks real, unrecoverable local data loss —
something nothing else in this document actually risks, since source library
data is never at stake in the on-device sync design (§7). That makes this the
one place strict error handling is non-negotiable rather than a nicety.

### 8.9 Explicitly Out of Scope (For Now)

- Continuous/live merging — see the session model in §8.5.
- A general sync-state database for playlist correlation — the in-file
  `#NAVIDROME-ID` comment (§8.4) is deliberately the only persisted correlation
  mechanism.
- Two local files sharing the same `#NAVIDROME-ID`, and a pulled playlist entry
  that fails to resolve to a local track (§8.3), are surfaced as new `check`
  (§4.3) finding categories rather than resolved automatically.

## 9. Command-Line Interface for §7/§8 (Design / Not Yet Implemented)

| Command                                                     | Description                                                                                                                                                                                                                                                                                                                                                                                          |
| ----------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `musicrename login [--url] [--username] [--password-stdin]` | **Implemented (§8.1).** Stores the Navidrome server URL, username, and password in a `0600` JSON file under `XDG_CONFIG_HOME` (§8.1). `--url`/`--username` are prompted for if omitted; the password is prompted for (masked) by default, or read from stdin with `--password-stdin` for scripting — never accepted as a flag. Validates via `/rest/ping` before saving.                             |
| `musicrename logout`                                        | **Implemented (§8.1).** Clears stored Navidrome credentials. Pure local file removal; not an error if not logged in.                                                                                                                                                                                                                                                                                 |
| `musicrename playlist select <target> [album-path]`         | Interactive checkbox editor (`charmbracelet/huh`) listing every track in the album, pre-checked against the existing `{target}.m3u8` if one is present; writes the updated selection back, targeted-updating (never fully rehashing) `sums.md5` if present (§7.3, §3.4). `album-path` defaults to the current directory, matching `inspect`/`lyrics`. `--skip-md5` suppresses the `sums.md5` update. |
| `musicrename playlist check [library-root-root]`            | **Implemented (§7.12).** Audits the `playlists/` tree for entries that don't resolve to a file, unrecognized `#TARGETS:` names, and duplicate `#NAVIDROME-ID` values across files. Read-only; exits non-zero on findings, matching `check`'s conventions. Album-local manifest findings live in `musicrename check` instead (§4.3, §7.12), not here.                                                 |
| `musicrename sync ipod <device-path> [library-root-root]`   | Full reconciliation sync to an attached iPod (§7.7). `--dry-run`, `--verbose`.                                                                                                                                                                                                                                                                                                                       |
| `musicrename sync sdcard <device-path> [library-root-root]` | Same, for the `sdcard` target. Any future §7.2 target gets its own sibling subcommand here.                                                                                                                                                                                                                                                                                                          |
| `musicrename sync navidrome pull [playlist]`                | **Implemented.** Pulls all playlists, or one by path if given (§8.5, §8.7). `--dry-run`; `--skip-scan` bypasses the forced library scan (§8.2) when it's known to already be fresh.                                                                                                                                                                                                                  |
| `musicrename sync navidrome push [playlist]`                | **Implemented.** Mirror of `pull`: pushes all playlists, or one by path if given (§8.5, §8.7). Same flags. A file with no `#NAVIDROME-ID` yet gets one created and written back to the local file.                                                                                                                                                                                                   |
| `musicrename sync navidrome delete <playlist>`              | **Implemented.** Explicit single-playlist delete (§8.7) — always requires a specific playlist, never bulk. `--yes` skips the confirmation prompt given it's destructive both locally and remotely. Errors immediately, without attempting anything, if the given playlist has no `#NAVIDROME-ID` (§8.4) — there is nothing remote to delete. No library scan is triggered (§8.2 doesn't apply here). |

### 9.1 Shape Notes

- **`sync` is one parent covering both device and Navidrome flavors of
  syncing.** `ipod`/`sdcard` are direct subcommands of `sync` rather than nested
  under an intermediate `device` level — this keeps every hardcoded §7.2 target
  a flat, independently addable sibling as new targets are added.
  `sync navidrome` reads clearly as the odd one out, consistent with §8's
  explicit statement that Navidrome isn't a `target` in the §7.2 sense.
- **`playlist select` only touches album-local manifests** (`{target}.m3u8`,
  §7.3) — a single album's checkbox-selected track list. It does not touch the
  global `playlists/` tree (§7.4), which stays hand-authored text files using
  the `#PLAYLIST:`/`#NAVIDROME-ID:` header conventions (§8.4).

### 9.2 Deferred: Robust Global Playlist Management (Phase 3)

Tooling beyond `playlist select` for the global `playlists/` tree (§7.4) — e.g.
`playlist create <name> [--targets]` to scaffold a new file with correctly
formatted `#PLAYLIST:`/`#TARGETS:` headers, reordering, editing a playlist's
`#TARGETS:` scope, or repairing malformed headers — is deferred as a later phase
of this same work, alongside the video/Rockbox pipeline (§6.5) and the on-device
sync mechanism (§7) itself. `playlist select`'s narrower album-manifest scope
covers the immediate need; the global-playlist authoring experience remains
manual (a text editor) until this phase is picked up.
