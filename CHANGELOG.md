# musicrename changelog

This is a personal tool and may not follow
[semantic versioning](https://semver.org), but I'll track major changes here for
my own reference.

## v3.3.0 — 2026-08-26

Support `folder.webp` and `folder.mp4` for animated primary album artwork
separate from static `folder.jpg`/`folder.png` art. `check` allows one static
image plus one animated file as a fallback pair (for players without
animated-artwork support) without warning.

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
