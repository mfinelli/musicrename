/*
 * Copyright © 2026 Mario Finelli
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program. If not, see <http://www.gnu.org/licenses/>.
 */

// Package metadata handles the discovery and extraction of music library data.
//
// It provides three main capabilities:
//
//  1. Scanning: [ScanLibrary] and [ProcessLibrary] walk a directory tree and
//     identify album directories (any directory that contains at least one
//     supported audio file). Each directory's contents are categorised into
//     audio tracks, primary artwork, supplementary artwork, scans, text files,
//     and extras.
//
//  2. Tag reading: [Reader.ReadTrack] extracts normalised metadata (title,
//     artist, album, year, track number, disc number) from FLAC, MP3, and M4A
//     files via the go-taglib WASM library.
//
//  3. Artist resolution: [Album.ResolveAlbumArtist] determines the canonical
//     album artist using a defined fallback chain: ALBUMARTIST tag -> artist
//     of the lowest-numbered track -> artist of the first track in slice
//     order.
//
// The package is intentionally read-only with respect to the filesystem; it
// never writes, moves, or deletes files. All mutation is the responsibility of
// the caller (the rename command).
package metadata

// FileCategory defines the type of file encountered during library scanning.
type FileCategory string

const (
	CatAudio      FileCategory = "Audio"      // .flac, .mp3, .m4a
	CatRootText   FileCategory = "RootText"   // .log, .cue, .m3u, .m3u8, .txt, sums.md5
	CatPrimaryArt FileCategory = "PrimaryArt" // folder.jpg / folder.jpeg / folder.png
	CatArtwork    FileCategory = "Artwork"    // other images at root or in artwork/
	CatScan       FileCategory = "Scan"       // .tiff / .tif, typically in scans/
	CatExtras     FileCategory = "Extras"     // files in extras/
	CatUnknown    FileCategory = "Unknown"    // anything that doesn't fit the above
)

// Track represents a single audio file and its extracted metadata.
type Track struct {
	// Path is the absolute path to the audio file on disk.
	Path string

	// Artist is the value of the ARTIST tag; empty string if the tag is absent.
	Artist string

	// AlbumArtist is the value of the ALBUMARTIST tag. When present it takes
	// precedence over Artist for directory naming. Empty string if absent.
	AlbumArtist string

	// Album is the value of the ALBUM tag.
	Album string

	// Year is the four-character year component extracted from the DATE tag.
	// MusicBrainz and other taggers often store a full ISO-8601 date
	// (e.g. "2003-01-14" or "2003-01"); only the year portion is retained.
	// Empty string if the tag is absent. No validity check is applied
	// (malformed values such as "0000" are passed through as-is).
	Year string

	// Title is the value of the TITLE tag. Empty string if the tag is absent;
	// the planner is responsible for applying the filename-stem fallback.
	Title string

	// TrackNumber is the TRACKNUMBER tag parsed as a non-negative integer.
	// Nil means the tag was absent or could not be parsed. Zero is a valid
	// value representing a hidden track (e.g. a pre-gap track on a CD).
	TrackNumber *int

	// DiscNumber is the DISCNUMBER tag parsed as a positive integer.
	// Zero means the tag was absent or could not be parsed.
	DiscNumber int
}

// Album represents a directory containing a collection of music and assets.
type Album struct {
	// RootPath is the absolute path to the source directory on disk.
	RootPath string

	// Tracks contains the audio files found directly in RootPath, in the
	// order returned by the filesystem. Tags are populated by Reader.ReadTrack.
	Tracks []*Track

	// Assets holds non-audio files grouped by FileCategory. A key is only
	// present in the map when at least one file of that category was found.
	// All paths are absolute.
	Assets map[FileCategory][]string

	// ResolvedArtist is the canonical album-level artist name, populated by
	// ProcessLibrary via ResolveAlbumArtist. Empty string means no artist
	// could be determined from any track; callers that require an artist
	// (e.g. the planner) should treat this as an error condition.
	ResolvedArtist string
}

// NewAlbum returns an Album rooted at path with all internal maps and slices
// initialised and ready for use.
func NewAlbum(path string) *Album {
	return &Album{
		RootPath: path,
		Tracks:   make([]*Track, 0),
		Assets:   make(map[FileCategory][]string),
	}
}
