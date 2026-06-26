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
- **Platform Target:** Linux and macOS. Windows is not supported (no native
  `md5sum`). On macOS, `md5sum` must be available (e.g. via Homebrew:
  `brew install md5sha1sum`).
- **Integrity:** Generate `sums.md5` files for every album to track file
  integrity via the system `md5sum` command.
- **Auditing:** Ability to scan for library "misconfigurations" or unwanted
  attributes.
- **Safety:** Provide a `--dry-run` mode to preview all filesystem changes.

## 3. Technical Specifications

### 3.1 Directory Hierarchy

Files are organized using a tiered structure to avoid overly large root
directories: `/[First Letter of Artist]/[Artist]/[Year] [Album Name]/`

- The first-letter bucket is a single character: `a`–`z` for artists whose name
  begins with a letter, or `0` for all others (digits, symbols, etc.).
- Because artist names pass through the full sanitization pipeline before
  bucketing, only lowercase letters and digits are possible first characters by
  the time the bucket is determined.
- If the `YEAR` tag is absent, the year prefix is omitted entirely:
  `/[First Letter of Artist]/[Artist]/[Album Name]/`

**Examples:**

- `b/beyonce/2003 dangerously in love/`
- `0/2pac/1996 all eyez on me/`
- `b/beyonce/lemonade/` _(year tag missing)_

**Album Folder Contents:**

- **Root:**
     - Audio files (`.flac`, `.mp3`, `.m4a`)
     - Primary Art: `folder.jpg` or `folder.png`
     - Text files: `.log`, `.cue`, `.m3u`, `.m3u8`
     - `sums.md5`
- **`/artwork/`**: Additional image files.
- **`/scans/`**: High-resolution scans (typically `.tiff`).
- **`/extras/`**: All other non-audio/non-art files.

### 3.2 The Sanitization Pipeline

All strings used in folder and filenames (Artist, Album, Title) must pass
through this sequence:

1. **Manual Overrides:** Hardcoded replacements for a small set of known edge
   cases (e.g., `AC/DC` -> `acdc`, `P!nk` -> `pink`). **Overrides return the
   final sanitized string immediately, skipping all subsequent steps including
   truncation.** The override value is used exactly as written.
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
      - **Files (Tracks/Art/Extras):** Max 40 characters (applied to the base
        name only, before appending the extension).
           - _Note:_ For files inside subdirectories (`artwork/`, `scans/`,
             `extras/`), the limit is 40 characters **minus the length of the
             subdirectory name plus one** (for the `/`) to ensure the full
             relative path in `sums.md5` remains ≤ 80 characters.
      - Truncation is mid-word (hard cut at the character limit); no
        word-boundary snapping.
      - Truncation is applied after space normalisation, so no result will start
        or end with a space as a result of the cut.

### 3.3 Metadata & Naming Logic

#### Tag Reading

- **Source of Truth:** Internal tags (FLAC/Vorbis Comments, ID3, M4A atoms),
  read via `github.com/deluan/go-taglib`, which normalizes tag names across
  formats. `TRACKNUMBER` is expected to be a single integer (not `track/total`
  form); the library is curator-managed.
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
year; malformed values (e.g. `0000`) are considered a data entry issue to fix
at the source, not something the tool guards against.

#### Disc Number Handling

If **any** track in an album has a `DISCNUMBER` tag, **all** tracks must have
one. If the tag is missing on even one track, the entire album is skipped with
an error. In practice this is unlikely since metadata is edited per-album.

#### Track Naming Pattern

- **Single Disc:** `[Track#] Title.ext` (e.g., `01 track one.flac`)
- **Multi-Disc:** `[Disc]-[Track#] Title.ext` (e.g., `1-01 track one.flac`)
     - The disc prefix is included only if the album contains more than one
       disc.
- **Zero-padding:** Track numbers are zero-padded to 2 digits by default. If any
  track number on the album exceeds 99, the entire album switches to 3-digit
  padding for that album only.

### 3.4 MD5 Sum Generation

The tool generates a `sums.md5` file in each album root by shelling out to the
system `md5sum` command. This keeps the output fully compatible with `md5sum -c`
for verification.

- **Format:** Standard `md5sum` output.
     - Binary files (audio/images): `hash *filename` (asterisk prefix on name).
     - Text files (`.log`, `.cue`, `.m3u`, `.m3u8`, `.txt`): `hash  filename`
       (two-space prefix on name).
- **Paths:** Filenames in `sums.md5` are relative to the album root (e.g.,
  `artwork/cover.jpg`, `01 track one.flac`).
- **Detection:** Text vs. binary classification is based on a predefined list of
  known text extensions (no magic-byte inspection).
- **Exclusion:** `sums.md5` itself is never included in the checksum file.
- **Scope:** The `sums` command operates on a single album root directory (not
  the whole library). Running it against the library root is not supported.

## 4. Architecture

### 4.1 Commands

The tool uses a command-based structure (via `spf13/cobra`):

| Command              | Description                                                           |
| -------------------- | --------------------------------------------------------------------- |
| `musicrename rename` | Scans metadata, sanitizes, and moves files.                           |
| `musicrename sums`   | Generates/updates `sums.md5` for the given album directory.           |
| `musicrename check`  | Audits the library for misconfigurations; exits non-zero on findings. |
| `musicrename lyrics` | _(Future)_ Fetches and embeds lyrics.                                 |

**Note on command independence:** `rename` does **not** generate `sums.md5`. The
intended workflow for a full library update is:

1. `musicrename rename`
2. `musicrename lyrics` _(once implemented)_
3. `musicrename sums`

### 4.2 `rename` Workflow

1. **Scan Phase:**
      - Recursively locate music files.
      - Identify "unknown" files (files that don't fit known categories) and log
        a warning.

2. **Analysis Phase:**
      - Read tags -> Apply Sanitization Pipeline -> Determine destination path.

3. **Validation Phase:**
      - Calculate necessary directory creations.
      - Verify if `oldPath == newPath` (case-insensitive) to skip no-op moves.
      - Detect sanitization collisions (two source files resolving to the same
        destination path). On collision: skip both files and emit an error.
      - **Overwrite safety:** Check all planned destination paths against the
        filesystem. If any destination file already exists, **abort the entire
        run** and list every conflict. The run is all-or-nothing; no files are
        moved until the pre-flight check passes cleanly.

4. **Execution Phase** _(skipped if `--dry-run` is passed)_:
      - Create folders -> Move files.
      - Use `os.Rename` where source and destination are on the same filesystem.
      - Fall back to copy-then-delete when `os.Rename` returns a cross-device
        error (`syscall.EXDEV`).
      - **Case-only renames:** When the source and destination differ only in
        case (e.g. `Beatles` -> `beatles`), rename via an intermediate temp path
        to avoid silent no-ops on case-insensitive filesystems (macOS default).
      - **Race condition:** If a destination file materializes between the
        pre-flight check and the actual move, skip that file with a warning
        rather than aborting the run.
      - **Empty directory cleanup:** After all moves, attempt to remove any
        source directories that were touched and are now empty. This is
        best-effort: failures are logged but do not affect exit status. Only
        directories that the tool moved files out of are candidates; no other
        directories are touched.

### 4.3 `check` Command

Emits a human-readable list of warnings/errors to stdout. Exits with a non-zero
status code if any findings are present, enabling use in scripts.

Example findings:

- Embedded album artwork detected in audio files
- Missing ReplayGain tags
- Track naming inconsistencies vs. current spec
- Files in unexpected locations

## 5. Implementation Notes (Go)

- **Filesystem moves:** `os.Rename` for same-device moves; copy-then-delete
  fallback for cross-device (`syscall.EXDEV`).
- **Case-only renames:** Rename to a temp path first, then to the final
  destination, to handle case-insensitive filesystems correctly.
- **MD5 generation:** Shell out to `md5sum`; do not reimplement.
- **Concurrency:** Worker pool for tag reading. MD5 generation delegates to
  `md5sum` which handles its own I/O.
- **Manual overrides:** Hardcoded in the binary (small, stable set; no config
  file).
- **Primary target:** Linux (case-sensitive filesystem). macOS is supported but
  is a secondary target.

### Key Dependencies

| Package                                  | Purpose                                                                                   |
| ---------------------------------------- | ----------------------------------------------------------------------------------------- |
| `github.com/alexsergivan/transliterator` | Unicode -> ASCII transliteration                                                          |
| `github.com/deluan/go-taglib`            | Cross-format metadata reading (maintained fork of `sentriz/go-taglib`, used by Navidrome) |
| `github.com/spf13/cobra`                 | CLI command management                                                                    |
