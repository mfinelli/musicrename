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
- Requires two custom converter presets, "single-disc" and "multi-disc", which
  are identical except for the output name format:
  - **Output**:
    - Output path: "Ask me later (useful for saving presets)"
    - If a file already exists: "Ask"
    - Output style and file name formatting: "Convert each track to an
      individual file"
    - Name format (single-disc):
      `%album artist%/%year% %album%/%tracknumber% %title%`
    - Name format (multi-disc):
      `%album artist%/%year% %album%/%discnumber%-%tracknumber% %title%`
  - **Output format**: FLAC, level 8
  - **Processing**: none configured
  - **Other**:
    - When finished: "Show full status report"
    - Transfer tags, attached pictures, and ReplayGain

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

  A second tagger script, "Move feat. to title", needs to run _after_ the
  Navidrome script above (this is achieved simply by placing it below Navidrome
  in the script list):

  ```
  $set(_feat_regex,\(?i\)\\s+\\\(?\(\(?:feat\\.|featuring\)\\s+[^\)]+\)\\\)?)

  $set(_feat_title,$rsearch(%artist%,%_feat_regex%))
  $set(artist,$rreplace(%artist%,%_feat_regex%,))
  $set(title,$if(%_feat_title%,%title% \(%_feat_title%\),%title%))

  $set(_feat_album,$rsearch(%albumartist%,%_feat_regex%))
  $set(albumartist,$rreplace(%albumartist%,%_feat_regex%,))
  $set(album,$if(%_feat_album%,%album% \(%_feat_album%\),%album%))
  ```

- Under `Options -> Metadata`, select both "Use release relationships" and "Use
  track relationships".

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

## Converting (foobar2000)

Note: if the source isn't lossless FLAC to begin with, skip the convert steps
entirely (e.g. an MP3 downloaded from SoundCloud or an artist's website that
only offers lossy files).

1. Once the rip has been archived, load the `.cue` file into foobar2000. It
   automatically shows the file split into the tracks defined in the cue sheet.
2. Select all of the tracks (the whole album), right-click, and choose
   "Convert", using the "single-disc" or "multi-disc" preset as appropriate.
3. The destination directory is the `WORKING` directory in the home `Music`
   directory.

Note: this is the point where the "rip from CD" and "purchase album online"
workflows unify, since purchased albums ship with tracks already split. For the
purchase workflow, extract the album into that same `WORKING` directory instead
(inside its own album directory, if the archive doesn't already have one).

## Tagging (MusicBrainz Picard)

1. Open Picard and add the recently converted/extracted files. They appear under
   "Unclustered Files".
2. Select them and click "Cluster".
3. Right-click the cluster and select "Lookup in Browser", then navigate to the
   MusicBrainz URL saved earlier (`musicbrainz.txt`).
4. On the MusicBrainz release page, click the "tagger" image/button (usually
   near the top right, by the album art).
5. The album loads into Picard. Drag the tracks from the cluster over onto the
   album.
6. Click Save.

## Finalizing (foobar2000)

1. Add the freshly tagged tracks back into foobar2000.
2. This is where any custom edits can be made, e.g. removing the "ExactAudioCopy
   vX.Y" comment.
3. Right-click the tracks and select `Tagging -> Remove all pictures`.
4. Select all the tracks, right-click, and choose
   `ReplayGain -> Scan as single albums (by tags)`. Occasionally this needs to
   be overridden by directly choosing "Scan as single album" and then updating
   the file tags.

## Lyrics (`mrr`)

1. Switch into the album's `WORKING` directory.
2. Run `mrr lyrics`. This is generally done only for studio albums and live
   albums, remixes, and similar are (usually) excluded.
3. For any tracks where the automatic lookup fails, find a suitable lyric link
   directly on LRCLIB, then pass it in track mode for that single track:
   `mrr lyrics --url https://lrclib.net/tracks/... "./01 track.flac"`.

## Finishing up

1. After adding lyrics and any other tag changes, run the album back through the
   convert preset in foobar2000 again, this time outputting directly into the
   home `Music` directory.
2. Run `mrr rename` on the album.
3. Run `mrr musicbrainz diff` to record the MusicBrainz baseline.
4. Source the highest quality/resolution artwork available. Good sources:
   - [Album Art Exchange](https://albumartexchange.com)
   - [covers.musichoarders.xyz](https://covers.musichoarders.xyz)
   - [fanart.tv](https://fanart.tv)

   On covers.musichoarders.xyz, limiting results to Amazon Music, Apple Music,
   iTunes, and Tidal can also sometimes surface an animated album art variant.

5. Run `mrr sums` on the album.
6. Copy to the NAS and verify checksums.
