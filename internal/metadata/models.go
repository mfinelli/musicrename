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

package metadata

// FileCategory defines the type of file encountered during library scanning.
type FileCategory string

const (
	CatAudio      FileCategory = "Audio"
	CatRootText   FileCategory = "RootText"   // .log, .cue, .m3u, .m3u8, .txt, sums.md5
	CatPrimaryArt FileCategory = "PrimaryArt" // folder.jpg / folder.jpeg / folder.png
	CatArtwork    FileCategory = "Artwork"    // other images at root or in artwork/
	CatScan       FileCategory = "Scan"       // .tiff / .tif, typically in scans/
	CatExtras     FileCategory = "Extras"     // files in extras/
	CatUnknown    FileCategory = "Unknown"    // anything that doesn't fit the above
)

// Track represents a single audio file and its extracted metadata.
type Track struct {
	Path        string
	Artist      string
	AlbumArtist string
	Album       string
	Year        string
	Title       string
	TrackNumber int
	DiscNumber  int
}

// Album represents a directory containing a collection of music and assets.
type Album struct {
	RootPath string
	Tracks   []*Track
	Assets   map[FileCategory][]string // maps category to list of absolute file paths
}

func NewAlbum(path string) *Album {
	return &Album{
		RootPath: path,
		Tracks:   make([]*Track, 0),
		Assets:   make(map[FileCategory][]string),
	}
}
