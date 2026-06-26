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

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeTestDir creates a temporary directory tree from a map of relative
// path -> file content. Directories are created automatically. The returned
// path is the root of the tree; t.TempDir() ensures cleanup after the test.
func makeTestDir(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
	return root
}

func TestCategorizeRootFile(t *testing.T) {
	tests := []struct {
		filename string
		expected FileCategory
	}{
		// Audio
		{"song.flac", CatAudio},
		{"song.mp3", CatAudio},
		{"song.m4a", CatAudio},
		// Root text
		{"info.log", CatRootText},
		{"album.cue", CatRootText},
		{"playlist.m3u", CatRootText},
		{"playlist.m3u8", CatRootText},
		{"notes.txt", CatRootText},
		{"sums.md5", CatRootText},
		// Primary art (only the exact folder.* names qualify)
		{"folder.jpg", CatPrimaryArt},
		{"folder.jpeg", CatPrimaryArt},
		{"folder.png", CatPrimaryArt},
		// These look similar but are not primary art
		{"folder2.jpg", CatArtwork},
		{"folderbig.png", CatArtwork},
		// Supplementary artwork
		{"cover.jpg", CatArtwork},
		{"back.jpeg", CatArtwork},
		{"booklet.png", CatArtwork},
		// Scans
		{"highres.tiff", CatScan},
		{"scan.tif", CatScan},
		// Unknown
		{"readme.exe", CatUnknown},
		{"data.bin", CatUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			assert.Equal(t, tt.expected, categorizeRootFile(tt.filename))
		})
	}
}

func TestProcessDirectory(t *testing.T) {
	t.Run("empty directory is not an album", func(t *testing.T) {
		dir := t.TempDir()
		album, isAlbum := processDirectory(dir)
		assert.False(t, isAlbum)
		// The album struct itself is still returned (not nil).
		assert.NotNil(t, album)
		assert.Empty(t, album.Tracks)
		assert.Empty(t, album.Assets)
	})

	t.Run("directory with one audio file is an album", func(t *testing.T) {
		dir := makeTestDir(t, map[string]string{
			"01 track one.flac": "",
		})
		album, isAlbum := processDirectory(dir)
		assert.True(t, isAlbum)
		require.Len(t, album.Tracks, 1)
		assert.Equal(t, filepath.Join(dir, "01 track one.flac"), album.Tracks[0].Path)
	})

	t.Run("all supported audio extensions are recognised", func(t *testing.T) {
		dir := makeTestDir(t, map[string]string{
			"track.flac": "",
			"track.mp3":  "",
			"track.m4a":  "",
		})
		album, isAlbum := processDirectory(dir)
		assert.True(t, isAlbum)
		assert.Len(t, album.Tracks, 3)
	})

	t.Run("root-level files are categorised correctly", func(t *testing.T) {
		dir := makeTestDir(t, map[string]string{
			"01 track.flac": "",
			"folder.jpg":    "",
			"cover.jpg":     "",
			"info.log":      "",
			"sums.md5":      "",
			"highres.tiff":  "",
			"random.exe":    "",
		})
		album, isAlbum := processDirectory(dir)
		assert.True(t, isAlbum)
		assert.Len(t, album.Tracks, 1)
		assert.Contains(t, album.Assets[CatPrimaryArt], filepath.Join(dir, "folder.jpg"))
		assert.Contains(t, album.Assets[CatArtwork], filepath.Join(dir, "cover.jpg"))
		assert.Contains(t, album.Assets[CatRootText], filepath.Join(dir, "info.log"))
		assert.Contains(t, album.Assets[CatRootText], filepath.Join(dir, "sums.md5"))
		assert.Contains(t, album.Assets[CatScan], filepath.Join(dir, "highres.tiff"))
		assert.Contains(t, album.Assets[CatUnknown], filepath.Join(dir, "random.exe"))
	})

	t.Run("artwork/ subdirectory files go to CatArtwork", func(t *testing.T) {
		dir := makeTestDir(t, map[string]string{
			"01 track.flac":    "",
			"artwork/back.jpg": "",
		})
		album, _ := processDirectory(dir)
		assert.Contains(t, album.Assets[CatArtwork], filepath.Join(dir, "artwork", "back.jpg"))
	})

	t.Run("scans/ subdirectory files go to CatScan", func(t *testing.T) {
		dir := makeTestDir(t, map[string]string{
			"01 track.flac":    "",
			"scans/inner.tiff": "",
		})
		album, _ := processDirectory(dir)
		assert.Contains(t, album.Assets[CatScan], filepath.Join(dir, "scans", "inner.tiff"))
	})

	t.Run("extras/ subdirectory files go to CatExtras", func(t *testing.T) {
		dir := makeTestDir(t, map[string]string{
			"01 track.flac":      "",
			"extras/booklet.pdf": "",
		})
		album, _ := processDirectory(dir)
		assert.Contains(t, album.Assets[CatExtras], filepath.Join(dir, "extras", "booklet.pdf"))
	})

	t.Run("unknown subdirectory files go to CatUnknown", func(t *testing.T) {
		dir := makeTestDir(t, map[string]string{
			"01 track.flac":      "",
			"misc/something.txt": "",
		})
		album, _ := processDirectory(dir)
		assert.Contains(t, album.Assets[CatUnknown], filepath.Join(dir, "misc", "something.txt"))
	})

	t.Run("subdirectory name matching is case-insensitive", func(t *testing.T) {
		dir := makeTestDir(t, map[string]string{
			"01 track.flac":     "",
			"Artwork/cover.jpg": "",
			"SCANS/scan.tiff":   "",
			"Extras/notes.pdf":  "",
		})
		album, _ := processDirectory(dir)
		assert.Contains(t, album.Assets[CatArtwork], filepath.Join(dir, "Artwork", "cover.jpg"))
		assert.Contains(t, album.Assets[CatScan], filepath.Join(dir, "SCANS", "scan.tiff"))
		assert.Contains(t, album.Assets[CatExtras], filepath.Join(dir, "Extras", "notes.pdf"))
	})

	t.Run("nested subdirectory inside artwork/ is skipped", func(t *testing.T) {
		dir := makeTestDir(t, map[string]string{
			"01 track.flac":           "",
			"artwork/cover.jpg":       "",
			"artwork/nested/deep.jpg": "",
		})
		album, _ := processDirectory(dir)
		// Only the immediate file appears; the nested one is silently ignored.
		assert.Len(t, album.Assets[CatArtwork], 1)
		assert.Contains(t, album.Assets[CatArtwork], filepath.Join(dir, "artwork", "cover.jpg"))
	})

	t.Run("RootPath is set to the scanned directory", func(t *testing.T) {
		dir := makeTestDir(t, map[string]string{
			"01 track.flac": "",
		})
		album, _ := processDirectory(dir)
		assert.Equal(t, dir, album.RootPath)
	})
}

func TestScanLibrary(t *testing.T) {
	t.Run("empty root yields no albums", func(t *testing.T) {
		root := t.TempDir()
		albums, err := ScanLibrary(root)
		assert.NoError(t, err)
		assert.Empty(t, albums)
	})

	t.Run("root itself containing audio is a single album", func(t *testing.T) {
		root := makeTestDir(t, map[string]string{
			"01 track.flac": "",
		})
		albums, err := ScanLibrary(root)
		assert.NoError(t, err)
		require.Len(t, albums, 1)
		assert.Equal(t, root, albums[0].RootPath)
	})

	t.Run("directories with no audio are not returned", func(t *testing.T) {
		root := makeTestDir(t, map[string]string{
			"docs/readme.txt": "",
			"images/art.jpg":  "",
		})
		albums, err := ScanLibrary(root)
		assert.NoError(t, err)
		assert.Empty(t, albums)
	})

	t.Run("finds deeply nested albums", func(t *testing.T) {
		root := makeTestDir(t, map[string]string{
			"b/beyonce/2003 dangerously in love/01 crazy in love.flac": "",
			"b/beyonce/2016 lemonade/01 pray you catch me.flac":        "",
			"0/2pac/1996 all eyez on me/01 ambitionz az a ridah.flac":  "",
		})
		albums, err := ScanLibrary(root)
		assert.NoError(t, err)
		assert.Len(t, albums, 3)
	})

	t.Run("intermediate directories without audio are excluded", func(t *testing.T) {
		// b/ and b/beyonce/ have no audio; only the leaf album does.
		root := makeTestDir(t, map[string]string{
			"b/beyonce/2016 lemonade/01 pray you catch me.flac": "",
		})
		albums, err := ScanLibrary(root)
		assert.NoError(t, err)
		require.Len(t, albums, 1)
		assert.Equal(t, filepath.Join(root, "b", "beyonce", "2016 lemonade"), albums[0].RootPath)
	})

	t.Run("album assets are populated during scan", func(t *testing.T) {
		root := makeTestDir(t, map[string]string{
			"01 track.flac": "",
			"folder.jpg":    "",
			"info.log":      "",
		})
		albums, err := ScanLibrary(root)
		assert.NoError(t, err)
		require.Len(t, albums, 1)
		assert.Len(t, albums[0].Tracks, 1)
		assert.Contains(t, albums[0].Assets[CatPrimaryArt], filepath.Join(root, "folder.jpg"))
		assert.Contains(t, albums[0].Assets[CatRootText], filepath.Join(root, "info.log"))
	})
}
