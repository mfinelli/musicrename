# Design Document: `musicrename`

## 1. Overview

`musicrename` is a Go CLI for maintaining a curated local music library. It
normalizes music files into a predictable directory structure derived from
metadata, audits the resulting library, maintains file integrity information,
checks whether files that claim to be lossless really are, manages playlists,
synchronizes selected music to removable devices, synchronizes playlists with
Navidrome, and detects upstream MusicBrainz metadata drift.

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

A `TITLE` that sanitizes to an empty string (for example, one made up entirely
of punctuation) is treated the same as a missing `TITLE`.

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
authoritative and bypasses the normalization steps above, but it is still
subject to the length limits below.

Length limits are:

- artist names: 60 characters
- album names: 60 characters
- track titles: whatever remains of the 46-character track filename budget
- other filenames: 40 characters for the basename

Filename limits are reduced when necessary for files in `artwork/`, `scans/`, or
`extras/` so that checksum paths remain within the intended path-length bound.

Truncation is a hard character limit rather than a word-boundary operation. If
the cut exposes trailing spaces, they are trimmed, so a truncated name may be
shorter than its limit.

As a final step, every directory or file name component, whether produced by the
normal form or by an override, has any leading or trailing whitespace removed.
No path component ever begins or ends with whitespace.

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

A complete track filename, including its number prefix and extension, is at most
46 characters. A track's filename is also its path in the album's `sums.md5`,
where each line is a 32-character hash, two separator characters, and the path;
the budget keeps every such line to at most 80 characters.

The sanitized title receives whatever the prefix and extension leave. For
example, a single-disc FLAC track title may be 38 characters, a multi-disc FLAC
track title 36, and a track on disc 10 or later 35. No assumption about disc or
track counts is needed.

When the title falls back to the filename stem, a leading copy of the track's
own number prefix is removed from the stem first, so a file that `rename` has
already named (`03 original.flac`) keeps its name rather than gaining another
prefix (`03 03 original.flac`). If neither the title nor the stem yields any
usable characters, the filename is the number prefix alone (`03.flac`).

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

Global playlists are intentionally **not** updated by `rename`. Renaming is
primarily an ingestion-time operation, when the files being moved cannot yet
appear in any global playlist. When a rename does move tracks that playlists
already reference, the playlists are reconciled through Navidrome instead.

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
- inconsistent MusicBrainz release ID (`MUSICBRAINZ_ALBUMID`) across tracks,
  present on some tracks but not others, or two different non-empty values; an
  album with the tag on none of its tracks is not itself a finding

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

`--url` accepts a specific LRCLIB track URL or numeric ID and embeds that exact
track directly, skipping the matching strategy above entirely. It is valid only
in track mode (a single file). This covers cases where on-disk tags (e.g.
additional artists appended to the track title) prevent the normal strategy from
finding the correct match, even though the correct match is known and easily
found by hand on lrclib.net.

---

## 7. Music Videos

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

## 8. Device Synchronization

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

## 9. Playlists

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

## 10. Navidrome Synchronization

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

Local playlist edits are only ever made after a pull, and a push is never made
without pulling first.

### 10.5 Deletion

Deletion is intentionally conservative.

Deleting a local playlist file manually does not delete its remote counterpart.
A subsequent pull recreates the local playlist.

Deleting a playlist remotely is detected through its known `#NAVIDROME-ID`; a
confirmed not-found result causes the local playlist to be removed.

Explicit deletion through `musicrename sync navidrome delete` is the mechanism
for intentionally deleting a playlist both locally and remotely.

Server errors are never interpreted as confirmation that a playlist is absent.

### 10.6 Library Renames

When `rename` moves tracks that global playlists already reference, the local
playlist entries are left pointing at the old paths. Navidrome is responsible
for reconciling them: its scanner recognizes moved files by their tags, which
`rename` doesn't change, so the remote playlists continue to reference the
correct tracks. The next pull, which scans first, writes the new paths back into
the local playlists.

The expected sequence after such a rename is therefore:

1. `rename`
2. `sync navidrome pull`
3. `playlist check`
4. any device synchronization

Device synchronization must not run between the rename and the pull. Global
playlists contribute to device selection, so stale entries would temporarily
deselect those tracks and remove them from the device until the next sync.

Playlists that have never been pushed to Navidrome are not reconciled this way;
`playlist check` reports their broken entries.

---

## 11. MusicBrainz Drift Detection

`musicrename musicbrainz diff` detects changes to a MusicBrainz release since it
was last checked, allowing upstream corrections to be noticed even when local
tags have subsequently been edited independently.
`musicrename musicbrainz check` is a separate, simpler command: a local-only
visibility listing of which albums do or don't have a `MUSICBRAINZ_ALBUMID` tag
at all, with no bearing on drift detection itself.

### 11.1 Scope

The command operates on exactly one album per invocation and never scans the
library. This is an explicit, on-demand check rather than a mechanism for
polling MusicBrainz for changes.

The release is identified by the `MUSICBRAINZ_ALBUMID` tag on the album's first
track, selected using the same rule as `ResolveAlbumArtist`: the track with the
lowest positive `TRACKNUMBER`, falling back to directory order. `check`
separately flags a release-ID mismatch across an album's tracks. A flagged
album's `MUSICBRAINZ_ALBUMID` should be corrected (typically by re-tagging with
Picard) before its `diff` result can be trusted.

### 11.2 Tracked Fields

The MusicBrainz release is fetched with its recordings, artist credits, release
group, labels, ISRCs, genre tags, and recording relationships.

Composer/writer credits are derived from a recording's relationship to a Work
and the Work's writer relationships to artists. Personnel credits such as
engineers, producers, instruments, and vocals are not tracked because they have
no corresponding field in the project's metadata model.

Genres are compared as a set of names rather than positionally. Changes to vote
counts or ordering therefore do not constitute drift; only additions and
removals are reported.

### 11.3 Snapshot Persistence

The fetched release data is persisted as `musicbrainz.json.gz` in the album
directory, keeping the snapshot with the library rather than in machine-local
state.

### 11.4 Comparison Semantics

Drift is the difference between the previous MusicBrainz snapshot and the
current MusicBrainz data, not the difference between local tags and MusicBrainz.
Local metadata edits therefore do not appear as upstream drift.

An album without a snapshot establishes the current data as the baseline and
reports no changes.

When differences are found, the snapshot is updated to the newly fetched data,
so subsequent checks report only further changes. `--dry-run` performs the
comparison without updating the snapshot, allowing the same diff to be reviewed
again later.

### 11.5 Coverage Listing

`musicbrainz check` lists every album under a library root that has no
`MUSICBRAINZ_ALBUMID` tag, or, with `--has-id`, every album that does.

An album missing the tag is not an error condition (it's the expected state for
anything not yet tagged with Picard). `musicbrainz check` is therefore a
visibility tool rather than an audit, and always exits `0` regardless of what it
finds, unlike the `check` family's exit-non-zero-on-findings convention.

---

## 12. Lossless Verification

`musicrename spectrum` examines audio that claims to be lossless and reports
evidence that it is really a lossy transcode, or otherwise not what it claims to
be. This section records the design decisions. The method itself, the measured
behavior of real encoders, and the limits of the technique are explained in
`SPECTRUM.md`.

### 12.1 Scope

The command operates on a single track or on a single album directory. It is an
explicit, on-demand examination of something the curator is specifically looking
at, not a sweep.

Only lossless codecs are analyzed (FLAC, and ALAC in `.m4a`). The decision is
made from the file's codec as reported by `ffprobe`, and not by its extension,
because an `.m4a` may hold either ALAC or AAC. In an album, lossy files are
skipped and counted in the summary. A lossy file named directly is an error,
since there is nothing to find out about it.

The command doesn't modify the album. The only files it writes are spectrogram
images, and only into a directory the user names with `--png`.

### 12.2 Evidence, not certainty

Lossy origin cannot be proven from the audio alone, so the result is a graded
verdict rather than a yes or no:

- `ok` — no evidence of a problem. This is the absence of evidence, not proof of
  losslessness; a high-bitrate lossy encode may leave nothing to find.
- `suspect` — a feature lossy encoding causes, which a genuine lossless source
  can also have.
- `likely fake` — strong evidence of a transcode or an upsample.
- `inconclusive` — too little non-silent audio to judge. Not a finding.

Cases that cannot be resolved are reported as ambiguous. In particular, a sharp
cutoff between 19.5 and 21 kHz in a CD-rate file is never more than `suspect`: a
high-bitrate encode and a deliberate mastering filter produce the same spectrum.
The tool does not name the encoder it thinks was used, because the measured
cutoff of one encoder varies with the audio and different encoders share
cutoffs.

This follows the principle of curator authority: the tool presents what it
measured and how strongly it points to a conclusion, and the curator decides.

### 12.3 Method

The audio is decoded by `ffmpeg` at its native sample rate and channel count
(resampling or downmixing would reshape the spectrum being measured), and
everything after decoding is pure Go with no additional dependencies.

A long-term average power spectrum is computed per channel with Welch's method:
overlapping (50%) frames, each multiplied by a Hann window and Fourier
transformed, with the power spectra averaged. Nearly silent frames are ignored.
The window is needed so that leakage from loud low frequencies does not fill in
the very quiet region above a lossy cutoff and hide it. Its length scales with
the sample rate (4096 samples at 44.1 and 48 kHz, up to 16384 at 192 kHz) so
that the frequency resolution is roughly constant (about 11 Hz per bin).

The spectrum is condensed into bands about 100 Hz wide. A cliff is the
lowest-frequency place above 5 kHz where the mean level of the 1 kHz below
exceeds the mean level of the 1 kHz above by at least 15 dB. The cutoff is where
the curve crosses halfway between the two levels. The lowest such edge is used,
because what matters is where the content first ends.

Three kinds of finding are produced:

- **cutoff** — a sharp lowpass at a frequency typical of lossy encoders.
- **upsampled** — a file at 88.2 kHz or higher whose content ends at or below 27
  kHz, i.e. has nothing beyond what a CD-rate or lossy source could hold.
- **bit depth** — a container wider than 16 bits that only ever uses 16 or
  fewer, such as 16-bit audio padded to 24.

A cutoff at or above 95% of the Nyquist frequency, or at or above 21 kHz at
sample rates below 88.2 kHz, is ordinary anti-alias filtering and is not a
finding.

All channels are analyzed and the verdict is based on the one with the widest
bandwidth, since a single full-bandwidth channel shows the file as a whole is
not band-limited. The LFE channel of a 2.1, 5.1 or 7.1 layout is excluded: it is
deliberately bass-only, has no cutoff to find, and would otherwise be mistaken
for a full-bandwidth channel and mask a transcode in the others. Channels with
too little non-silent audio are excluded as well.

### 12.4 Thresholds

Every tunable number lives in one documented struct, `spectrum.Thresholds`. The
defaults come from measurements of real encoders on synthetic signals
(`SPECTRUM.md` records them). They are not command-line flags: it is better to
learn from real files which of them actually needs adjusting than to design a
configuration surface in advance.

### 12.5 Output and exit status

Each track is reported with its format, a verdict, and the evidence behind it;
`--verbose` adds the per-channel numbers. A summary line gives the number of
tracks analyzed and the number flagged.

The exit status follows the convention of the `check` family: non-zero when any
track is `suspect` or `likely fake`. A track that cannot be decoded is an error
and also exits non-zero. `inconclusive` is not a finding and does not affect the
exit status.

With `--png`, a spectrogram of each track is written for the curator to judge by
eye. By default it is rendered by `ffmpeg` independently of the tool's own
measurement. A failure to write one is only a warning because the analysis it
illustrates has already succeeded and been reported.

With `--annotated` (which requires `--png`), the tool draws the picture itself,
with the detected cutoff marked on it and, for hi-res files, the top of CD
bandwidth. This is a second pass over the audio, made only on request: the
analysis keeps only the time-averaged spectrum, and retaining the full
time-by-frequency grid on every run would cost memory and complexity for an
output that is rarely wanted. The second pass uses the channel, window and
silence handling of the analysis, so the picture shows what the analysis saw,
and its memory is bounded however long the track is, because neighboring time
columns are merged as the audio grows.

By default each annotated picture's color scale is set from the file's loudness,
which shows each file's shape as clearly as possible but makes pictures of
differently loud files look alike. `--fixed-scale` (which requires
`--annotated`) uses one scale for every picture so that a color always means the
same level and tracks can be compared directly. It is not the default because a
quiet recording looks nearly black on a fixed scale, hiding the edge the picture
exists to show. Levels are per FFT bin, so pictures are exactly comparable at
one sample rate and only roughly across rates.

### 12.6 Non-goals

- Identifying which encoder or bitrate produced a file.
- Detecting lossy origin from codec-specific structure (such as MP3 granule
  boundaries) rather than from the spectrum.
- Scanning a library, or remembering results between runs. There is no cache or
  database; the analysis takes about a second per track.

---

## 13. Rip Log Verification

`musicrename riplog` checks an Exact Audio Copy (EAC) rip log against the
settings a rip should have been made with. A rip made with a wrong setting still
completes, and the result usually sounds fine, so the mistake is easy to make
and hard to notice. The log records every setting that affects accuracy, which
makes it possible to check them after the fact.

### 13.1 Scope

The command operates on a single `.log` file or on the `.log` files directly
inside one directory (a multi-disc rip has one per disc). It is meant to be run
on the original rip as archived, before the files are checksummed and before the
image is split for the library.

Only **image rips** from **English-language EAC 1.x** are supported: one disc
ripped as one range, with a cue sheet. This is how the library's CDs are ripped
(see `WORKFLOW.md`). Anything else (a per-track log, a log in another language,
a log from EAC before 1.0) is reported as unsupported rather than judged against
rules written for something else. An unsupported log counts as an error, because
it cannot be verified. Supporting more is possible but unneeded, and would
multiply the number of log layouts the parser has to know.

### 13.2 Policy, not a score

Some other tools attempt to grade a log with a score, deducting points for each
departure from some setting, but that model is a poor fit here.

`riplog` instead runs a fixed list of checks and reports each as one of:

- **ok** — the check holds.
- **note** — informational; needs no action.
- **warning** — something could not be confirmed or looks unusual, but nothing
  is known to be wrong.
- **error** — a setting is wrong, the log shows a problem with the rip, or the
  log cannot be verified as it should be.

### 13.3 The profile

The required settings are the author's own and are hardcoded in the package
(`settingRules` in `internal/riplog/verify.go`), not configurable, for the same
reason device targets are: a short, personally-curated list lives in code rather
than behind a configuration system. A rip with any of them wrong should be
redone, because the mistake is in the audio and not only in the log.

| Setting                                     | Required | Why                                                                      |
| ------------------------------------------- | -------- | ------------------------------------------------------------------------ |
| Read mode                                   | Secure   | Only secure mode re-reads sectors until they agree.                      |
| Utilize accurate stream                     | Yes      | Otherwise the drive's reading can drift unnoticed.                       |
| Defeat audio cache                          | Yes      | Otherwise re-reads may come from the drive's cache and hide errors.      |
| Make use of C2 pointers                     | No       | A drive's own error report can be wrong, and trusting it skips re-reads. |
| Fill up missing offset samples with silence | Yes      | Otherwise audio is shifted rather than padded at the ends.               |
| Delete leading and trailing silent blocks   | No       | It removes audio, so the result no longer matches the disc.              |
| Null samples used in CRC calculations       | Yes      | Otherwise CRCs differ from those AccurateRip and other rippers compute.  |
| Add ID3 tag                                 | No       | ID3 tags do not belong in FLAC files, which use Vorbis comments.         |

Besides these settings it checks that:

- no normalization is recorded;
- the encoder is `flac.exe` and its command line includes `-V`, so the encode is
  verified to decode back to the same audio;
- the read offset and the overread setting are the ones known for the drive
  (13.5);
- both the test and the copy pass were made and their CRCs agree, and the log
  says the copy is OK;
- no errors were reported;
- the log's table of contents has tracks, and AccurateRip results cover all of
  them;
- AccurateRip confirmed every track. A disc that AccurateRip does not know, or a
  pressing that differs, is a warning rather than an error, since neither is
  evidence of a bad rip;
- the CUETools database result, when present, confirms every track. Its absence
  is a warning, since it comes from a plugin that may not be installed;
- the cue sheet and FLAC image that go with the log agree with it (13.6);
- the disc is one the MusicBrainz release in `musicbrainz.txt` has (13.7);
- the log's checksum matches its text (13.8).

Settings outside the profile, such as the interface EAC used or the encoder's
bitrate and quality, are not looked at, whether the log states them or not.

### 13.4 Unverifiable is not verified

A setting the log is expected to state but does not is an error ("not found in
the log, so it cannot be verified"). A check never passes because the
information it needs is missing. "Expected" means part of the profile: a log
with `Read mode` deleted fails, while one with a line deleted that the profile
does not look at is unchanged.

### 13.5 Drive offset and overread

The read offset is a property of the drive: how far its reading is shifted from
where the audio really is. A wrong one shifts every track. The offsets of the
drives actually used are a small table in code (`internal/riplog/drives.go`),
each one a value AccurateRip has confirmed for that drive. A known drive must
match its offset; a mismatch is an error.

Overread (`Overread into Lead-In and Lead-Out`) is also a property of the drive
and is recorded in the same table. To correct for a drive's offset, EAC needs a
few samples from beyond the edge of the audio: past the end of the last track
for a positive offset, before the start of the first track for a negative one. A
drive that can read into the lead-in and lead-out supplies them; for one that
cannot, EAC fills them in with silence, which is why
`Fill up missing offset samples with silence` is required to be on. The log
cannot say which kind of drive it is, and turning overread on for a drive that
cannot do it can cause read errors, so the table records the setting as
determined for each drive (EAC can test for it), and a known drive's log must
match. The difference is a handful of samples at the very ends of the disc.

The log's drive line carries its adapter and ID, which vary, so it is normalized
before lookup (lower-cased, adapter and revision removed). For an unknown drive
the offset result depends on whether AccurateRip confirmed every track at the
offset used: if so, it is a note that the drive and offset can be added to the
table; if not, a warning that the offset cannot be verified. Overread is only
noted, since there is nothing to compare it with. When a drive is added or
replaced, run EAC's AccurateRip configuration, rip a disc AccurateRip knows, and
add the drive to the table, with its overread setting.

### 13.6 Cue sheet and image

An image rip is a log, a cue sheet, and one FLAC file holding the whole disc,
and the three have to belong together. For a log `Album.log`, `riplog` expects
`Album.cue` beside it (the name EAC gives them, and so also for `(disc N)`) and
reads the audio file the cue sheet names. It checks that:

- the cue sheet is there and can be read. EAC writes cue sheets in the system's
  ANSI code page unless told otherwise, so one with an accent in a title is not
  valid UTF-8; text that is not is read as Windows-1252;
- it names exactly one file, which is a `.flac` in the same directory and is a
  well-formed FLAC file. The file's name is also compared with the image the log
  says was ripped; a difference is only a warning, since the files may have been
  renamed;
- the image is CD audio: 44.1 kHz, 16-bit, stereo;
- the image holds exactly the audio the disc has, which is (the last track's end
  sector + 1) × 588 samples, 588 being the samples in each channel of a sector
  (44,100 a second over 75 sectors a second). That catches an image cut short or
  padded, which still decodes and so passes any check on its contents;
- the image's audio is what EAC ripped. The log's Copy CRC is a CRC-32 of the
  audio EAC read, before it was encoded, and FLAC loses nothing, so decoding the
  image must give audio with that CRC. What it decodes to must also agree with
  what the FLAC records of itself, its length and the MD5 of its audio, which is
  how damage to the file shows;
- it has the same tracks as the log, numbered the same, all of them audio;
- every track's `INDEX 01` is exactly where the log's table of contents puts the
  track: the log's start sector, written as minutes, seconds and frames;
- its `REM DISCID` equals the freedb disc ID computed from the log's table of
  contents. EAC computes the one in the cue sheet from that same table, so a cue
  sheet from another disc cannot pass this even if its tracks happen to line up;
- any `INDEX 00` lines make sense: each lies inside the track before it.

The format, the length and the MD5 are read from the FLAC's STREAMINFO block:
the first 42 bytes of every FLAC file hold its sample rate, channel count, bit
depth, exact sample count, and an MD5 of the audio.

The audio is decoded with ffmpeg (the `pcm` package) as raw 16-bit samples with
nothing converted, and its length, CRC-32 and MD5 are taken in one pass, which
for a whole album takes a few seconds. The CRC is the ordinary one, the
polynomial of zlib and gzip, over the samples without any file header. That is
what EAC's Copy CRC is, and the check reproduces it exactly for a real rip, and
for the tracks later split from it, laid end to end. It holds only while the log
says "Null samples used in CRC calculations: Yes", which the profile requires;
with it off EAC leaves silent samples out of the CRC, which is not reproduced,
and the audio is not compared, with a note. Only CD audio is decoded, since
anything else would be converted to 16 bits and what was hashed would be the
conversion.

ffmpeg is not trusted to say whether the audio decoded well. After a damaged
frame it can exit successfully with the full length of wrong audio, and says
nothing unless asked to check each frame's CRC. So it is asked, anything it
writes counts as a problem, and the verdict is the comparison: the CRC with the
log's, and the length and MD5 with the FLAC's own. That catches damage of any
kind, including an image cut short, which a check of the length in the header
cannot see because the header still claims the whole. A missing ffmpeg is only a
warning.

A disc can have audio before its first track: a hidden track in the pregap, or a
short silence. The cue sheet then gives track 1 an `INDEX 00` at `00:00:00` and
an `INDEX 01` at the log's start sector for it, so every position is measured
from the first sector of the disc and is checked like any other. If that audio
was left out of the image, the cue sheet's positions are all smaller than the
log's by exactly its length, which is reported as such (the image starts at
track 1, leaving out the hidden track or pregap) instead of as a list of
unrelated mismatches. The image's expected length follows the cue sheet in
either case, since its `00:00:00` is where the image begins.

`INDEX 00` marks where a track's pregap begins and is what EAC's Detect Gaps
leaves in the cue sheet. The checks can confirm that what is recorded is
consistent, but not that nothing is missing: a disc with no pregaps and a rip
made without detecting them produce the same cue sheet. A cue sheet with none is
therefore a note rather than a failure, and seeing that gaps were detected is
left to the workflow (`WORKFLOW.md`).

### 13.7 MusicBrainz release

The same album is often several releases on MusicBrainz, one per pressing, and
ripping one while tagging another is easy to do and not obvious afterwards. The
disc's MusicBrainz disc ID identifies its track layout, so it can be compared
with the release saved in `musicbrainz.txt` beside the log (see `WORKFLOW.md`),
which holds that release's URL and nothing else.

The disc ID is computed from the log's table of contents the way MusicBrainz's
libdiscid computes it from a disc's: a SHA-1 hash of the first and last track
numbers and the offsets of the lead-out and of each of 99 track slots (zero for
a track the disc does not have), written in hex and base64 encoded with a
URL-safe alphabet. Every offset counts from the physical start of the disc's
audio, 150 sectors before the sector 0 that EAC counts from. The computation
reproduces the test vector MusicBrainz's libraries are tested with, and gives
IDs that resolve to the right release for real discs, including one with audio
before track 1, where the first offset is not the usual 150.

One request asks MusicBrainz for the release's disc IDs, and the result is:

- **listed on the release** — ok, saying which medium of a multi-disc release it
  is;
- **the release has disc IDs, but not this one** — a warning, since it is
  probably a different pressing. One more request finds the releases that do
  have this disc ID and names them, which is usually the answer;
- **the release has no disc IDs yet** — a note: there is nothing to compare;
- **MusicBrainz has no such release** — an error: `musicbrainz.txt` names one
  that does not exist;
- **no `musicbrainz.txt`** — a warning, and a file that is not a release URL (a
  release group's, say) is an error;
- **MusicBrainz cannot be reached** — a warning. Being offline says nothing
  about the rip.

`--offline` makes no request: the disc ID is still computed and shown, and noted
as not compared. The requests follow the rules of the `musicbrainz` package: one
release is asked about once per run, however many discs of it are checked,
within MusicBrainz's rate limit, on demand for one album, and time-limited so
that a machine with no network is told so rather than kept waiting.

### 13.8 Log checksum

EAC ends a log with a checksum that signs the text above it. `riplog` verifies
it: the checksum must be present, well formed, and the one EAC's algorithm gives
for the log's text. A log that fails was edited after EAC wrote it, damaged, cut
short, or written by an old EAC, and is an error.

This is checked even though a log from one's own rip is trusted almost
completely, for the same reason the other checks are made: the log is kept with
the album for years, and it should be possible to tell later that it is still
exactly as EAC wrote it, not changed by accident or damaged in storage.

EAC does not document the scheme; the algorithm follows the descriptions of
other software that verifies the checksums. The text above the checksum line,
with its line endings and byte order marks removed, is encoded as UTF-16
little-endian and run through a CBC-MAC: Rijndael with a 256-bit block and a
fixed 256-bit key, starting from zeros, the last block padded with zeros. AES
has a 128-bit block and Go's standard library has no wider one, so the package
carries its own Rijndael, which is checked against `crypto/aes` where the two
overlap. The key is the same in every copy of EAC, so the checksum guards
against accident and casual editing, not against a determined forgery. It covers
the text and not the bytes, so a log saved again with other line endings or as
UTF-8 still verifies.

The checksum does not cover what follows its own line. Anything there but blank
lines is an error: text added to the log, or several logs in one file, which
this check of one rip's log does not handle. The one exception is the row of 60
hyphens EAC writes to separate a log from the next that it appends to the file,
which says nothing.

### 13.9 Output and exit status

By default each log is shown with the checks that did not pass, followed by a
count of those that did, so a good rip is a single line. `--verbose` lists every
check with what was found.

The exit status is non-zero when any log has an error. Warnings and notes do not
fail the run, so a disc AccurateRip does not know does not stop a script; with
`--strict`, warnings fail it too. The command only reads the logs.

### 13.10 Verifying the split

Once the image is split into tracks, `mrr split --check Album.log [dir]`
verifies that the tracks hold exactly what was ripped. `dir` is the directory of
the tracks, the current directory by default, so it is run from inside the
directory the album was split into, with the log of the archived rip as its
argument. Checking is a flag so that the command can later do the splitting
itself without changing how it is run to check.

The tracks are named as the workflow names them: `01 Title.flac` for a single
disc; `1-01 Title.flac` for disc 1 of several, which share the directory, the
disc coming from the log's name (`Album (disc 1).log`); and `00 Title.flac` for
audio before track 1, such as a hidden track in the pregap (`1-00 Title.flac` on
a disc of several).

It checks that:

- the log's checksum matches (13.8), since the log is what the tracks are
  compared with, and the cue sheet beside it is there and can be read;
- there is a file for each track of the log's table of contents, and a `00` file
  only if the cue sheet puts audio before track 1, with no other file for any of
  them. Other FLAC files in the directory, such as the other discs of a
  multi-disc album, are counted and left out. A disc with audio before track 1
  and no `00` file is a warning, since the audio can be in track 1, and is
  checked as being, but the workflow gives it a file of its own;
- every track is CD audio, as the image is (13.6);
- the tracks, decoded in order as one stretch of audio, have the log's Copy CRC,
  so that together they hold the audio EAC ripped. This was confirmed for the
  tracks of a real rip laid end to end. Each track is also compared with what
  its own FLAC records of it, its length and the MD5 of its audio, so that
  damage is traced to a track. When the CRC is not the log's and no track is
  damaged, the tracks are compared with the rip's image, which the cue sheet
  names and which is beside the log, to say where they differ: the track and the
  place in it of the first and last differing sample, how many differ in all,
  and how much longer or shorter the tracks are than the image;
- each track is as long as the cue sheet says.

The last is needed because the audio of a split can be exactly the rip's while
every boundary is a few samples off: cut the end of one track and the start of
the next at the wrong place and nothing is lost, so no check of the audio as a
whole can see it, while one track starts a moment early. A track's length is
known to the sample, from what it decodes to, or from its header if it was not
decoded.

Where the cue sheet puts a track's boundaries depends on what the split did with
a pregap, which must go somewhere. The workflow appends it to the end of the
track before it, so that every track begins at its `INDEX 01`. The other way, at
the start of the track it leads into, so that it begins at its `INDEX 00`, is
where the cue sheet's `INDEX 00` puts it and loses nothing, but it is not what
the workflow does, so a split cut that way is a warning that says so. Which was
used is found, not assumed: a split that fits the workflow's way for every track
passes, and one that fits the other way for every track is the warning. One that
is cut one way for some tracks and the other way for the rest is an error that
names the two groups, and a track that fits neither is reported with what the
cue sheet gives for both, how far off it is from the nearer, and how many more
are. A way the cue sheet cannot work out, because its `INDEX 00` lines make no
sense (which `riplog` reports), is not one a split can be.

Audio before track 1 is a `00` file from the start of the image to track 1's
`INDEX 01`; without one, track 1 holds it, and begins at the start of the image.

Nothing more is checked of track files that are not right (a missing track, a
file that is not CD audio), and a decoder that is missing leaves the audio
unchecked with a warning while the lengths, which come from the headers, are
still checked. The output and exit status are the same as for `riplog` (13.9).

### 13.11 Comparing audio

`mrr compare A B` compares the audio of two FLAC files, which can be tracks or
images, and says whether it is the same. It is for questions such as whether two
rips of a disc made with different drives hold the same audio, and works for a
single track as it does for an image, because it knows nothing of rips: two
stretches of audio are the same or they are not. What is compared is what the
files decode to, so how they were encoded and their tags do not matter.

Both files must be CD audio, since the decoder gives exactly what was stored
only for that: a 24-bit file would be compared as a conversion of itself, and an
MP3 of a track differs from it in every sample. Anything else is refused.

The audio is compared sample by sample, in step, without holding either. The
result is that it is the same, or how many samples differ, where the first and
last are, the stretches they fall in, and how the lengths compare. Differences
that are all within the first or last 2% of what both have are said to be, which
is a fact about them and not a reason: with no log there is nothing to say
whether a drive that could not read the ends of a disc, or damage, made them.

Where nearly every sample differs, the likely reason is that one is the other
moved along, as a wrong drive offset moves every sample of an image, or a pregap
kept in another place moves the start of a track. A shift is looked for in the
same pass. A few stretches of A, spread through it, are held as it is read, and
each is searched for in the part of B within about three seconds either way of
where it was in A. A stretch is searched for only if it has enough different
samples to be distinctive, so silence is not, and is accepted only if found
exactly once, on a sample boundary, so a sound that repeats is not. A shift is
reported only if every stretch that was found agrees on one that is not zero,
which is how a stretch moved in only part of the audio is told from one that is
moved throughout. The two are then compared again with the shift allowed for, to
say what is left of the difference, and how many samples at each end have none
to compare with in the other.

Every result but the same audio is an error, as is a comparison that could not
be made, so the exit status is zero only for audio that is the same.

### 13.12 Comparing rips

Given two logs, `mrr compare A.log B.log` compares two rips of one disc, such as
made with different drives, which is the question when a drive is new or a rip
is in doubt. Each log's cue sheet, beside it, names the image that is compared,
as in `split --check`. Besides the checks of 13.11 on the images, it uses what
the logs say.

Each log's checksum is checked, since what is taken from them is the drives'
settings, and the logs must be of the same disc: their tables of contents must
be the same, and if not, nothing that follows would mean anything. Each rip is
then described by its drive, read offset and overread setting, and from them, by
what its drive could not read. A drive delivers audio late or early by its
offset, so with a positive offset the last samples of the disc are past the end
of what it reads and with a negative one the first are before the start, and
Exact Audio Copy fills what it could not read with silence, where the image's
length is kept. With overread on, the drive is taken to read past the ends and
to lose nothing, which is a request in the log and not proof that it did, and so
what differs is checked against this and not taken to follow from it.

What differs is then one of:

- nothing: the same audio, a pass;
- the audio moved along: an error, since with both offsets right the images line
  up, so one offset is not its drive's, by as much as the shift. The logs'
  AccurateRip results say which: the one that AccurateRip did not confirm is the
  one to doubt;
- samples that are all within what either drive could not read, and each of
  which is silence in one rip or the other: a note, which does not fail the run,
  since this is what two correct rips of one disc from drives with different
  offsets give. Both the place and the silence are required, since the first
  alone would excuse damage that happens to be near an end;
- samples that are within it but are sound in both: a warning, since the log
  says EAC filled in silence there and it did not;
- anything else, including images of different lengths: an error, with what each
  drive could not read, and, if the difference is at an end of the images where
  a log says overread was on, that this does not show that the drive read past
  it.

---

## 14. Architectural Principles

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

Where possible, state is represented by the files being managed themselves
(`sums.md5`, source-hash sidecars, playlist directives, Navidrome IDs, and
MusicBrainz snapshots, etc.) rather than by a separate database.

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

### Evidence over assertion

A heuristic check reports what it measured and how strongly that points to a
conclusion. It separates ambiguous results from conclusive ones, does not claim
more certainty than it has, and never acts on its own conclusions.

### Unverifiable is not verified

A check that cannot be performed because the information it needs is missing is
reported as such. It is never passed silently, and a verifier does not treat the
absence of evidence of a problem as evidence that there is none.
