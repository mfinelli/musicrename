# Design Document: `musicrename`

## 1. Overview
`musicrename` is a Go-based CLI tool designed to normalize a local music
library. It transforms inconsistent file structures and naming conventions
into a strict, predictable, and sanitized hierarchy based on internal metadata
tags.

## 2. Goals & Requirements
- **Normalization:** Standardize paths and filenames for a consistent library
  feel.
- **Sanitization:** Remove non-ASCII characters and illegal filesystem
  characters.
- **Portability:** Support Linux (case-sensitive) and Windows/macOS
  (case-insensitive) filesystems.
- **Integrity:** Generate `sums.md5` files for every album to track file
  integrity.
- **Auditing:** Ability to scan for library "misconfigurations" or unwanted
  attributes.
- **Safety:** Provide a `--dry-run` mode to preview all filesystem changes.

## 3. Technical Specifications

### 3.1 Directory Hierarchy
Files are organized using a tiered structure to avoid overly large root
directories: `/[First Letter of Artist]/[Artist]/[Year] [Album Name]/`

**Example:** `b/beyonce/[2003] dangerously in love/`

**Album Folder Contents:**
- **Root:**
  - Audio files (`.flac`, `.mp3`, `.m4a`)
  - Primary Art: `folder.jpg` or `folder.png`
  - Text files: `.log`, `.cue`, `.m3u8`
  - `sums.md5`
- **`/artwork/`**: Additional image files.
- **`/scans/`**: High-resolution scans (typically `.tiff`).
- **`/extras/`**: All other non-audio/non-art files.

### 3.2 The Sanitization Pipeline
All strings used in folder and filenames (Artist, Album, Title) must pass
through this sequence:

1. **Manual Overrides:** Hardcoded replacements (e.g., `AC/DC` -> `ac⁄dc`,
   `P!nk` -> `pink`).
2. **Transliteration:** Convert Unicode characters to ASCII via
   `github.com/alexsergivan/transliterator`.
3. **Casing:** Convert all characters to lowercase.
4. **Regex Strip:** Keep only `a-z`, `0-9`, and `space`. All other characters
   are stripped.
5. **Truncation:**
   - **Artist:** Max 60 characters.
   - **Album:** Max 60 characters.
   - **Files (Tracks/Art/Extras):** Max 40 characters.
     - *Note:* For files inside subdirectories (`artwork/`, `scans/`,
       `extras/`), the limit is 40 characters **minus the length of the
       directory name** to ensure the full path in `sums.md5` remains <= 80
       characters.

### 3.3 Metadata & Naming Logic
- **Source of Truth:** Internal tags (FLAC, MP3, M4A).
- **Compilation Handling:** Use the `ALBUMARTIST` tag for the directory
  structure; fall back to the `ARTIST` tag of the first track if empty.
- **Track Naming Pattern:**
  - **Single Disc:** `[Track#] Title.ext` (e.g., `01 track one.flac`)
  - **Multi-Disc:** `[Disc-][Track#] Title.ext` (e.g., `1-01 track one.flac`)
  - The disc prefix is only included if the album contains more than one disc.

### 3.4 MD5 Sum Generation
The tool generates a `sums.md5` file in each album root.
- **Format:**
  - Binary files (audio/images): Prefixed with `*` (e.g.,
    `*md5hash filename.flac`).
  - Text files (`.log`, `.cue`, `.m3u8`, `.txt`): Standard prefix space (e.g.,
    ` md5hash lyrics.txt`).
- **Detection:** Based on a predefined list of known text extensions.

## 4. Architecture

### 4.1 Command Interface
The tool uses a command-based structure (via `spf13/cobra`):
- `musicrename rename`: Scans metadata, sanitizes, and moves files.
- `musicrename sums`: Recalculates/updates `sums.md5` files.
- `musicrename check`: Audits the library for misconfigurations (e.g.,
  detecting embedded artwork, missing tags, or naming inconsistencies).
- `musicrename lyrics`: (Future) Fetches and embeds lyrics.

### 4.2 Workflow Execution
1. **Scan Phase:**
   - Recursively locate music files.
   - Identify "unknown" files (files that don't fit known categories) and log a
     warning.
2. **Analysis Phase:**
   - Read tags -> Apply Sanitization Pipeline -> Determine destination path.
3. **Validation Phase:**
   - Calculate necessary directory creations.
   - Verify if `oldPath == newPath` (case-insensitive) to avoid redundant
     moves.
4. **Execution Phase:**
   - Create folders -> Move files -> Generate `sums.md5`.
   - *Skipped if `--dry-run` is passed.*

## 5. Implementation Notes (Go)
- **Filesystem:** Use `os.Rename` for efficiency.
- **Concurrency:** Use a worker pool for reading tags and computing MD5 hashes
  to maximize CPU utilization.
- **Key Dependencies:**
  - `github.com/alexsergivan/transliterator`: For ASCII normalization.
  - `github.com/dhowden/tag`: For cross-format metadata reading.
  - `github.com/spf13/cobra`: For CLI command management.
