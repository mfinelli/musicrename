# CD Ripping Workflow

## Prerequisites

The workflow relies on several Windows tools run under WINE, plus a couple of
native Linux/macOS utilities.

### Exact Audio Copy (EAC)

Homepage: <https://www.exactaudiocopy.de/>

- Runs under WINE.
- Requires a pre-configured EAC config file (brought along from a previous
  setup) covering drive offset, secure ripping options, etc.
- **TODO:** document the exact EAC configuration/settings.

### foobar2000

Homepage: <https://www.foobar2000.org/>

- Runs under WINE.
- No special configuration required for the ripping workflow itself.
- Optional: on first setup, the "Visualization + Cover Art + Tabs" layout can be
  selected, but this is purely cosmetic and can be changed later via
  `View -> Layout -> Quick setup`.
- Requires the FLAC command-line tools for encoding, downloaded directly from
  Xiph rather than bundled: grab the latest `flac-X.Y.Z-win.zip` from the
  [FLAC for Windows](http://downloads.xiph.org/releases/flac/) link on the
  [FLAC downloads page](https://xiph.org/flac/download.html) and unzip it. The
  first time foobar2000 needs it, it will prompt for the path to the unzipped
  `flac.exe`. To upgrade to a newer version later, download and unzip the new
  release, then manually update the path in foobar2000 via
  `File -> Preferences -> Advanced`, scroll down to `Tools -> Converter`, and
  double-click "Additional command-line encoder paths".

### MusicBrainz Picard

Homepage: <https://picard.musicbrainz.org/>

- Used for metadata tagging/lookup.
- Requires a custom tagger script (adapted from the Navidrome project),
  saved/named "Navidrome" in Picard's scripting options:

  ```
  # Multiple artists
  $setmulti(albumartists,%_albumartists%)
  $setmulti(albumartistssort,%_albumartists_sort%)
  $setmulti(artistssort,%_artists_sort%)

  # Album Version
  $set(musicbrainz_albumcomment,%_releasecomment%)
  $if(%_recordingcomment%, $set(subtitle,%_recordingcomment%))

  # Release and Original dates
  $set(releasedate,%date%)
  $set(date,%_recording_firstreleasedate%)
  $set(originaldate,%originaldate%)
  $delete(originalyear)
  ```

### gscan2pdf

Homepage: <http://gscan2pdf.sourceforge.net/>

- Used for scanning album artwork/liner notes.

### musicrename (`mrr`)

Homepage: <https://github.com/mfinelli/musicrename>

- Used at various stages of the workflow.

## Ripping (Exact Audio Copy)

1. Open EAC and insert the CD.
2. Click the "Get CD Metadata from metadata provider" button.
3. `Action -> Detect Gaps`.
4. `Action -> Test & Copy Image and Create Cue Sheet (Compressed)`.
5. Rip directly into the home `Music` directory. Filename is just the album
   name, e.g. `Dangerously in Love`; for multidisc albums, append `(disc N)`.

## Staging the rip

1. Create a new folder in the `RIP` directory of the `Music` directory named
   `YEAR Name of album`, e.g. `2003 Dangerously in Love`.
2. Copy the ripped files into that directory.
3. Find the release on MusicBrainz (creating it if it doesn't already exist) and
   save its URL in a file named `musicbrainz.txt` inside that directory.

## Scanning album art (gscan2pdf)

1. Open gscan2pdf and make sure "OCR scanned pages" is **not** selected (check
   this every time because it's not a one-off setting).
2. Set the scan resolution per item:
   - Main album cover: 1200 ppi
   - Back/inside: 600 ppi
   - Disc and booklet: 300 ppi
3. As each item is scanned, crop it down and/or rotate as necessary directly in
   gscan2pdf. No other post-processing is done.
4. Save as TIFF, with LZW compression.
5. Save the TIFFs into a `scans` directory alongside the music files, inside
   that same `RIP` album directory. Sample filenames: `front`, `back`, `inside`,
   `disc`, `booklet-XX-YY`, etc.

   Note: if the front artwork is part of the booklet, the booklet scans start on
   the second page (the front cover is not rescanned at the lower booklet
   resolution).

## Checksumming

Once the scans are done, calculate the MD5 sums of everything in the album
directory. The checksum file is named `Album name.md5` (matching the other files
for the album):

```
touch "Album name.md5"
md5sum *.cue >> *.md5
md5sum -b *.flac *.jpg >> *.md5
md5sum *.log *.txt >> *.md5
md5sum -b scans/* >> *.md5
```

## Archival

Once checksumming is done, copy the whole album directory to both Dropbox and
the NAS for long-term archival. Once copied to the NAS verify the checksums with
`md5sum -c *.md5`.

### Purchased (digitally downloaded) albums

Purchased albums usually arrive as a zip or other archive, so the process
differs from a physical CD rip:

- The archive is kept as-is (original filename included) and is not extracted or
  renamed.
- MD5 sum the archive directly, rather than the individual audio files.
- Include a cover image, if one was provided with the purchase or could be saved
  from the purchase page.
- Create a `source.txt` file containing the link to the purchase page.
- Create an `info.txt` file containing any information/description taken from
  the purchase page.
- Add the archive, cover image, `source.txt`, and `info.txt` to the checksum
  file. Note that for purchased albums this file is named `sums.md5` (rather
  than `Album name.md5` used for rips).
- Copy everything to Dropbox and the NAS, same as the physical rip flow.
