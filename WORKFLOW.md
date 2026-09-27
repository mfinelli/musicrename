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
- Optional: on first setup, the "Visualization + Cover Art + Tabs" layout can
  be selected, but this is purely cosmetic and can be changed later via
  `View -> Layout -> Quick setup`.
- Requires the FLAC command-line tools for encoding, downloaded directly from
  Xiph: grab the latest `flac-X.Y.Z-win.zip` from the
  [FLAC for Windows](https://downloads.xiph.org/releases/flac/) link on the
  [FLAC downloads page](https://xiph.org/flac/download.html) and unzip it.
  The first time foobar2000 needs it, it will prompt for the path to the
  unzipped `flac.exe`. To upgrade to a newer version later, download and
  unzip the new release, then manually update the path in foobar2000 via
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
- **TODO:** document exact scan configuration.

### musicrename (`mrr`)

Homepage: <https://github.com/mfinelli/musicrename>

- Used at various stages of the workflow.
