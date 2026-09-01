# musicrename changelog

This is a personal tool and may not follow
[semantic versioning](https://semver.org), but I'll track major changes here for
my own reference.

## v4.1.0 — 2026-09-01

Round out global playlist management: `playlist create`/`targets` scaffold and
edit a playlist's `#TARGETS:` scope, `playlist sums` covers the `playlists/`
tree the way `sums` already covers albums, and `playlist check` gains missing/
stale checksum detection, directive-order consistency, and duplicate-directive
findings (the same missing-entry checksum diffing was also added to `check` and
`video check`).

Add a full `playlist entries` command family: `add` (by path, or an interactive
directory browser), `remove` (interactive checkbox, or non-interactive by
`--artist`/`--album`), `reorder` (a full-screen grab-and-move editor), and
`dedupe`. `playlist sort` reorders by metadata fields or shuffles.

Extend Navidrome sync (`sync navidrome pull`/`push`) to carry `#SORT:` alongside
`#TARGETS:` in the remote comment field, so sort criteria round-trip across
machines the same way target scoping already did.

## v4.0.0 — 2026-08-30

Add playlist management. Album-local `{target}.m3u8` manifests (managed with the
new `playlist select`) mark which tracks in an album are included for a device
sync target; library-wide playlists under a new `playlists/` tree can span
albums and library roots. `playlist check` audits the latter for broken
references, unrecognized targets, and duplicate IDs.

Add Navidrome sync. `login`/`logout` store server credentials, and
`sync navidrome pull`/`push`/`delete` keep the `playlists/` tree in sync with a
Navidrome server bidirectionally, over the Subsonic-compatible API.

Add device sync. `sync ipod` and `sync sdcard` copy tracks selected via
`playlist select` onto a mounted iPod or SD card, keeping the device in sync
with the library (adding, updating, and removing files as needed) without ever
touching the library itself. `ipod` copies audio through unchanged and ships
artwork separately; `sdcard` transcodes to MP3 and embeds artwork directly, for
players that need it. Checks free space and asks for confirmation before
changing anything; `--dry-run` and `--verbose` are both supported.

## v3.4.0 — 2026-08-27

Add support for managing music videos as a completely separate library, with its
own root path and a `video` command family (`fetch`, `add`, `edit`, `rename`,
`sums`, `check`, `inspect`). Since videos are typically downloaded via `yt-dlp`
with no reliable embedded metadata, each video's artist, title, and optional
album/year live in a machine-written `musicvideo.nfo` sidecar (a minimal
Kodi/Jellyfin-compatible XML file) instead; `video fetch` also writes a
plain-text `info.txt` with the source URL, title, uploader, and description.

## v3.3.0 — 2026-08-26

Support `folder.webp` and `folder.mp4` for animated primary album artwork
separate from static `folder.jpg`/`folder.png` artwork. The `check` command now
allows one static image plus one animated file as a fallback pair (for players
without animated-artwork support) without warning.

## v3.2.0 — 2026-06-28

Add artist bucket overrides.

## v3.1.1 — 2026-06-28

Fix presentation of artist bucket.

## v3.1.0 — 2026-06-28

Use the album artist sort tag to properly bucket "The" artists.

## v3.0.2 — 2026-06-28

Fix for tracknumber and discnumber that have both current and total separated by
a `/`.

## v3.0.1 — 2026-06-28

Change binary to `mrr` to avoid conflicts.

## v3.0.0 — 2026-06-27

First release of the Go rewrite; v3 because I went through two full rewrites of
the Go version before landing here. A
[Python version](https://github.com/mfinelli/music-rename) preceded all of this.
