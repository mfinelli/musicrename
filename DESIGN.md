# Design Document: `musicrename`

## 1. Overview

`musicrename` is a Go CLI for maintaining a curated local music library. It
normalizes music files into a predictable directory structure derived from
metadata, audits the resulting library, maintains file integrity information,
manages playlists, synchronizes selected music to removable devices, and
synchronizes playlists with Navidrome.

The library is **curator-managed rather than heuristic-driven**. Metadata and
explicit selections are authoritative; the tool avoids trying to infer a user's
intent from ambiguous filenames or library contents.

The primary supported platforms are Linux and macOS. Windows is not supported.

The project has three related but distinct domains:

1. **The audio library** — normalized and maintained from audio metadata.
2. **The video library** — a separate tree for music videos, with its own
   metadata model.
3. **Synchronization** — selected audio/video and playlists can be synchronized
   to devices or Navidrome.

---

## 2. Audio Library

### 2.1 Directory Structure

Albums are organized as:

```text
[artist bucket]/[artist]/[year] [album]/
```

For example:

```text
b/beyonce/[2003] dangerously in love/
0/2pac/[1996] all eyez on me/
b/beyonce/lemonade/
```

The year prefix is omitted when no year is available.

The artist directory is derived from the sanitized `ALBUMARTIST`. The bucket is
normally determined from the sanitized artist name, but may instead use
`ALBUMARTISTSORT`. A small set of explicit artist bucket overrides takes
precedence over both.

The resulting hierarchy is intentionally deterministic: the same metadata always
produces the same location.

An album directory may contain:

- audio tracks: `.flac`, `.mp3`, `.m4a`
- primary artwork: `folder.jpg`, `folder.png`, `folder.webp`, or `folder.mp4`
- text files such as `.log`, `.cue`, `.m3u`, and `.m3u8`
- `sums.md5`
- `artwork/` for additional artwork
- `scans/` for high-resolution scans
- `extras/` for other files

`folder.jpeg` is normalized to `folder.jpg`.

### 2.2 Metadata

Audio metadata is the source of truth for naming.

Each source directory is treated as one album. Tracks are not globally regrouped
according to their tags.

`ALBUMARTIST` determines the album's artist. If it is absent, the `ARTIST` of
the track with the lowest track number is used.

`DATE` may contain a complete date or a year-month value; only its four-digit
year is used.

`TRACKNUMBER` is expected to contain a single integer. Track number `0` is valid
and represents a pre-gap or hidden track; it is distinct from an absent track
number.

If a required value cannot be determined, the tool prefers a predictable
fallback or an explicit error over guessing.

| Missing metadata                | Behavior                                                   |
| ------------------------------- | ---------------------------------------------------------- |
| `YEAR` / `DATE`                 | Omit the year prefix and warn                              |
| `TITLE`                         | Use the original filename stem and warn                    |
| `TRACKNUMBER`                   | Sort among other unnumbered tracks alphabetically and warn |
| both `ARTIST` and `ALBUMARTIST` | Cannot construct a valid destination; skip with an error   |

If any track in an album has `DISCNUMBER`, every track must have one. Partial
disc-number metadata invalidates the album.

### 2.3 Sanitization

All metadata used in paths and filenames passes through the same normalization
rules.

The resulting normal form:

- transliterates Unicode to ASCII
- converts to lowercase
- normalizes whitespace
- removes characters other than `a-z`, `0-9`, and spaces
- collapses repeated spaces
- trims surrounding whitespace
- applies length limits

Known exceptional names may have explicit overrides. An override is
authoritative and bypasses the remainder of the sanitization process.

Length limits are:

- artist names: 60 characters
- album names: 60 characters
- filenames: 40 characters for the basename

Filename limits are reduced when necessary for files in `artwork/`, `scans/`, or
`extras/` so that checksum paths remain within the intended path-length bound.

Truncation is a hard character limit rather than a word-boundary operation.

### 2.4 Track Names

Single-disc albums use:

```text
[track] [title].ext
```

Multi-disc albums use:

```text
[disc]-[track] [title].ext
```

Track numbers are two digits by default. If an album contains a track numbered
above 99, all tracks in that album use three-digit padding.

---

## 3. Renaming

`musicrename rename` reconciles an existing tree with the layout defined by the
metadata rules.

The operation is planned before files are moved. Sanitization collisions and
existing destination conflicts prevent execution rather than causing an
overwrite.

`--dry-run` displays the planned changes without modifying the filesystem.

Unknown files are reported but are never moved automatically.

Renaming is idempotent: running it again on an already-normalized library should
produce no changes.

Case-only renames are handled explicitly so that they work correctly on
case-insensitive filesystems.

When a file is renamed, existing checksum and album-selection references are
updated as appropriate without unnecessarily changing the file's recorded
content hash.

After successful moves, empty source directories are removed on a best-effort
basis.

---

## 4. Integrity Checksums

Every album may have a `sums.md5` containing checksums for the files in that
album.

The format is compatible with the standard `md5sum` command. Binary files use
the normal `*filename` form; known text files use the text-file form.

Paths are relative to the album root and entries are sorted for stable output.

`sums.md5` itself is never included in its own checksum set.

The checksum file represents a **record of known file content**, not merely a
way to produce a new checksum. Consequently, operations that only rename a file
update its filename in `sums.md5` without recalculating the hash. Operations
that actually rewrite a file recalculate only that file's hash.

This distinction is important because recalculating untouched files would
destroy the ability of `sums.md5` to detect later corruption of those files.

`musicrename sums` can operate on an individual album or recursively over a
library.

---

## 5. Auditing

`musicrename check` is read-only and reports all findings rather than fixing
them. It exits non-zero when findings exist.

It can operate on:

- a single track
- an album
- a library

Checks include:

### Metadata

- missing title
- missing track number
- missing date/year
- missing both artist and album artist

### Album consistency

- inconsistent album artist
- inconsistent album
- partial disc-number metadata
- duplicate track numbers

### Audio quality

- missing ReplayGain track gain
- missing ReplayGain album gain
- embedded artwork

### Artwork

- missing primary artwork
- multiple static primary-art files
- multiple animated primary-art files

One static and one animated primary-art file are allowed so that players with
different artwork capabilities can use an appropriate fallback.

### Integrity

- missing `sums.md5`

### Path conformance

For a library, the tool verifies that album and file paths match what `rename`
would produce.

Checksum contents are not verified by `check`; `md5sum -c` remains the mechanism
for verifying actual file integrity.

---

## 6. Lyrics

`musicrename lyrics` retrieves lyrics from LRCLIB and embeds them into supported
audio files.

It can operate on a track, album, or library.

Lookup proceeds from increasingly relaxed matching:

1. title, artist, album, and duration
2. duration ±1 second
3. duration ±2 seconds
4. fuzzy title/artist/album search

Requests are rate-limited to 5 requests per second.

Synced and unsynced lyrics are treated independently.

| Format | Synced lyrics | Unsynced lyrics  |
| ------ | ------------- | ---------------- |
| FLAC   | `LYRICS`      | `UNSYNCEDLYRICS` |
| MP3    | not embedded  | `USLT`           |
| M4A    | not embedded  | `©lyr`           |

For MP3 and M4A, synced lyrics are not converted into unsynced lyrics merely to
make them fit the format.

Existing lyrics are preserved unless `--force` is used.

LRC timestamps are normalized before being embedded, including normalization of
offsets and timestamp precision.

---

# 7. Music Videos

Music videos are maintained separately from the audio library.

They have their own root and do not participate in the audio-library metadata or
album model.

A video is organized as:

```text
[video-root]/[bucket]/[artist]/[title]/
    [title].ext
    musicvideo.nfo
    info.txt
    [derived audio, if any]
```

Exactly one recognized video is expected per directory.

Supported source formats are `.mp4`, `.webm`, and `.mkv`. The library itself
does not transcode these source files.

### 7.1 Video Metadata

Because video files cannot be relied upon to contain useful music metadata,
metadata is stored in `musicvideo.nfo`.

The NFO contains:

```xml
<musicvideo>
  <title>Crazy in Love</title>
  <artist>Beyoncé</artist>
  <album>Dangerously in Love</album>
  <year>2003</year>
</musicvideo>
```

`title` and `artist` are required. `album` and `year` are optional.

The NFO is the authoritative metadata source for the video and is generated and
edited by `musicrename`.

Album and year are informational and do not affect the video's path.

### 7.2 Video Commands

The video command family provides:

- `video fetch` — download a video with `yt-dlp` and create a human-readable
  `info.txt`
- `video add` — ingest a video and create its NFO
- `video edit` — create or modify its NFO
- `video rename` — reconcile its path with its NFO
- `video sums` — create per-video checksums
- `video check` — audit video directories
- `video inspect` — display raw and sanitized video metadata
- `video extract-audio` — derive an audio file from a video's audio stream
- `video select` — select videos for device synchronization

`info.txt` contains useful source information such as the URL, title, uploader,
upload date, and description. It is user-owned after creation.

`video rename` moves the video, NFO, `info.txt`, checksum data, and any derived
audio together.

### 7.3 Video Audio Extraction

A video may have a derived audio file when a song exists only as a video.

Extraction is explicit and curator-triggered; the tool does not attempt to
discover such tracks automatically.

The derived audio remains alongside its source video rather than becoming a
normal library album.

The audio stream is remuxed without re-encoding. Its extension follows the
source codec.

Metadata is derived from the video's NFO. ReplayGain is generated for the
resulting audio.

The derived file is included in the video's checksum set.

If the source video changes after extraction, the derived audio can be detected
as stale. Metadata changes in the NFO can likewise be detected independently
from source-content changes.

---

# 8. Device Synchronization

Device synchronization copies a curated subset of the library to removable
storage.

The initial targets are:

- `ipod` — an iPod running Rockbox
- `sdcard` — a generic removable-storage target such as a car head unit

The device filesystem mirrors the library-root structure.

For example:

```text
main/a/artist/album/...
christmas/b/artist/album/...
playlists/...
```

The `videos` tree is separate from ordinary audio-library synchronization.

There is intentionally **no synchronization database**. Device state is
self-describing through checksum files stored on the device.

### 8.1 Library Roots

Multiple audio library roots can coexist beneath a common library-root-root:

```text
library/
    main/
    christmas/
    classical/
    videos/
    playlists/
```

Library roots are discovered automatically. `videos` and `playlists` are
reserved names.

Playlist entries are expressed relative to the common parent, so an entry is
unambiguous across multiple roots.

### 8.2 Targets

Targets define:

- which audio formats can be copied unchanged
- which format is used when transcoding is required
- how artwork is delivered
- artwork size constraints
- whether video is supported

Targets are intentionally a small hardcoded set rather than a general-purpose
configuration system.

Current targets:

**`ipod`**

- audio formats are copied without transcoding
- artwork is external
- artwork is resized to a maximum dimension of 400px
- video is supported
- video is converted to the Rockbox-compatible MPEG video format

**`sdcard`**

- MP3 is the native format
- other audio is transcoded to MP3
- artwork is embedded at 500px
- video is not supported

Target-specific encoding policy is part of the target definition rather than
being inferred from the source library.

### 8.3 Desired Selection

An album may contain a target-specific manifest:

```text
ipod.m3u8
sdcard.m3u8
```

These files list the tracks selected for that target. Their order has no
semantic meaning.

Global playlists can also imply device selection: any track referenced by a
playlist that applies to a target is included in that target's desired set.

Thus selection is the union of album-local selection and applicable global
playlists.

### 8.4 Device State and Drift

The device contains its own `sums.md5` files.

For files copied byte-for-byte, the source and device hashes can be compared
directly.

For transformed files—such as transcoded audio or resized artwork—the device
additionally records the source hash in:

```text
[target].src.md5
```

This records which source content produced the derived device file.

No host-side sync database is required.

If a source file has no recorded source checksum, the sync cannot safely
determine that it is unchanged and therefore treats it as needing
synchronization.

### 8.5 Reconciliation

A sync computes:

1. the desired state from library selections and playlists
2. the current state from the device's checksum data
3. the changes required to reconcile them
4. whether sufficient device capacity exists

Each desired file is classified as:

- **add** — absent from the device
- **skip** — already represents the current source content
- **regenerate** — present but stale or unverifiable

Files no longer desired are deleted.

Transformed device files use their source-hash records to determine whether
regeneration is necessary.

The sync operates on the resulting plan rather than maintaining a separate
persistent database.

`--dry-run` displays the plan without modifying the device.

---

# 9. Playlists

There are two distinct playlist concepts.

### Album-local selection manifests

These are `{target}.m3u8` files inside albums and represent selection for a
particular device target.

### Global playlists

These live beneath:

```text
playlists/
```

and represent ordered playlists intended for playback or synchronization.

Global playlists are standard extended-M3U files with additional directives:

```text
#PLAYLIST:Name
#NAVIDROME-ID:...
#TARGETS:ipod,sdcard
#SORT:artist,album
```

`#PLAYLIST` gives the human-readable playlist name.

`#NAVIDROME-ID` correlates the local playlist with its Navidrome counterpart.

`#TARGETS` limits the playlist to particular device targets. If absent, the
playlist applies to all targets.

`#SORT` records the last explicit sorting criteria or the `shuffle` operation.
It is a remembered operation, not a guarantee that the playlist remains in that
order.

Playlist entries are paths relative to the library-root-root.

Unlike album-local selection manifests, **global playlist order is meaningful**.

### 9.1 Playlist Management

The CLI supports:

- creating playlists
- setting or clearing target scope
- adding entries
- removing entries
- interactively reordering entries
- sorting entries by metadata
- shuffling entries
- removing duplicate entries
- renaming playlist files from their `#PLAYLIST` names
- checking playlist consistency
- generating `playlists/sums.md5`

Sorting supports:

```text
artist
albumartist
album
year
disc
track
title
```

Sorting is stable. Missing or unresolvable metadata sorts after entries with
usable values.

A playlist can explicitly remember its sort criteria and later reapply them.

Adding entries does **not** automatically re-sort a playlist. This preserves the
distinction between remembered sorting criteria and deliberate manual ordering.

Deduplication keeps the first occurrence of each entry. Sorting performs
deduplication by default; explicit `entries dedupe` does not otherwise alter
order.

Playlist operations update `playlists/sums.md5` when it already exists.

---

# 10. Navidrome Synchronization

Navidrome synchronization concerns playlists only. Audio files remain in the
shared library and are not copied by `musicrename`.

The local `playlists/` tree is synchronized with Navidrome in both directions.

Authentication uses a single configured Navidrome server. Credentials are stored
locally with restrictive permissions.

The Subsonic API is used for playlist and library operations.

### 10.1 Scan Before Sync

A Navidrome library scan is initiated before playlist synchronization so that
recently changed filesystem contents are visible to track resolution.

This can be skipped when the caller knows the server is already current.

### 10.2 Track Resolution

Local tracks are identified by their library-root-relative paths.

For pushes, the Navidrome catalog is indexed by path so playlist entries can be
resolved efficiently.

For pulls, Navidrome playlist entries already contain the relevant paths.

A path that cannot be resolved is reported rather than causing the entire
synchronization to fail.

### 10.3 Playlist Correlation

Local and remote playlists are correlated exclusively by `#NAVIDROME-ID`.

The filename and display name are not identifiers.

The Navidrome playlist comment carries `#TARGETS` and `#SORT` information so
those properties survive synchronization between machines. The tool manages only
its own structured suffix and preserves the user's ordinary comment text.

### 10.4 Synchronization Model

Navidrome synchronization is intentionally a **pull/edit/push session**, rather
than a continuously merged or three-way-synchronized system.

Pull makes the local playlist reflect the remote state.

The user may then edit the local playlist.

Push makes the remote playlist reflect the local state.

There is no persistent synchronization database.

This is appropriate for a single-user workflow but is not intended to provide
concurrent multi-writer conflict resolution.

### 10.5 Deletion

Deletion is intentionally conservative.

Deleting a local playlist file manually does not delete its remote counterpart.
A subsequent pull recreates the local playlist.

Deleting a playlist remotely is detected through its known `#NAVIDROME-ID`; a
confirmed not-found result causes the local playlist to be removed.

Explicit deletion through `musicrename sync navidrome delete` is the mechanism
for intentionally deleting a playlist both locally and remotely.

Server errors are never interpreted as confirmation that a playlist is absent.

---

# 11. Architectural Principles

Several principles govern the design:

### Deterministic normalization

Metadata produces one predictable filesystem representation. The tool should not
invent alternate representations based on incidental source filenames or
filesystem state.

### Curator authority

Where metadata or selection is ambiguous, explicit curator data wins over
heuristics.

### Read-only auditing

`check` commands report problems but do not silently repair them.

### No unnecessary persistent state

Where possible, state is represented by the files being managed
themselves—`sums.md5`, source-hash sidecars, playlist directives, and Navidrome
IDs—rather than by a separate database.

### Preserve integrity information

A recorded checksum is changed only when the corresponding file's content
changes. Renaming a file must not destroy its existing corruption-detection
value.

### Separate source and derived content

Whenever synchronization transforms a file, the system records which source
content produced the transformed result rather than pretending the transformed
bytes are equivalent to the source.

### Explicit synchronization semantics

Device synchronization is reconciliation against a self-describing device state.
Navidrome synchronization is a pull/edit/push session. Neither relies on an
opaque host-side synchronization database.

### Shared normalization rules

Audio, video, and playlist filenames use the same sanitization model wherever
their semantics permit it, keeping the library predictable and avoiding multiple
competing naming conventions.
