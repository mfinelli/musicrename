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

// touchFiles creates empty files at the given paths under dir, with their
// directories.
func touchFiles(t *testing.T, dir string, rels ...string) {
	t.Helper()
	for _, rel := range rels {
		p := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, nil, 0o644))
	}
}

func TestCheckAudioFile(t *testing.T) {
	t.Run("accepts every supported extension, whatever its case", func(t *testing.T) {
		for _, name := range []string{"01 a.flac", "01 a.mp3", "01 a.m4a", "01 a.FLAC", "/some/dir/01 a.Mp3"} {
			assert.NoError(t, CheckAudioFile(name), name)
		}
	})

	t.Run("rejects anything else, naming the file and what is accepted", func(t *testing.T) {
		for _, name := range []string{"folder.jpg", "sums.md5", "noextension", "01 a.flac.bak"} {
			err := CheckAudioFile("/some/dir/" + name)
			require.Error(t, err, name)
			assert.Contains(t, err.Error(), name)
			assert.NotContains(t, err.Error(), "/some/dir/", "only the file's name is shown")
			assert.Contains(t, err.Error(), "supported audio file")
		}
	})

	t.Run("a directory named like audio is judged by its name only", func(t *testing.T) {
		// CheckAudioFile looks at the extension and not at the file system,
		// which is the caller's business.
		assert.NoError(t, CheckAudioFile("/some/dir.flac"))
	})
}

func TestAudioFiles(t *testing.T) {
	t.Run("lists the audio files directly inside, sorted", func(t *testing.T) {
		dir := t.TempDir()
		touchFiles(t, dir, "02 b.flac", "01 a.mp3", "03 c.M4A", "folder.jpg", "sums.md5", "sub/04 d.flac")

		got, err := AudioFiles(dir)
		require.NoError(t, err)
		assert.Equal(t, []string{
			filepath.Join(dir, "01 a.mp3"),
			filepath.Join(dir, "02 b.flac"),
			filepath.Join(dir, "03 c.M4A"),
		}, got)
	})

	t.Run("a directory with no audio has none", func(t *testing.T) {
		dir := t.TempDir()
		touchFiles(t, dir, "folder.jpg")
		got, err := AudioFiles(dir)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("a directory that can't be read is an error", func(t *testing.T) {
		_, err := AudioFiles(filepath.Join(t.TempDir(), "missing"))
		assert.ErrorContains(t, err, "reading")
	})
}

func TestIsAlbumRoot(t *testing.T) {
	t.Run("a directory holding audio is an album", func(t *testing.T) {
		dir := t.TempDir()
		touchFiles(t, dir, "01 a.flac")
		ok, err := IsAlbumRoot(dir)
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("a directory holding only albums is not", func(t *testing.T) {
		dir := t.TempDir()
		touchFiles(t, dir, "Artist/2000 Album/01 a.flac")
		ok, err := IsAlbumRoot(dir)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("audio in a subdirectory doesn't count, nor do other files", func(t *testing.T) {
		dir := t.TempDir()
		touchFiles(t, dir, "folder.jpg", "sub/01 a.flac")
		ok, err := IsAlbumRoot(dir)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("a directory that can't be read is an error", func(t *testing.T) {
		_, err := IsAlbumRoot(filepath.Join(t.TempDir(), "missing"))
		assert.Error(t, err)
	})
}

func TestLibraryAlbums(t *testing.T) {
	t.Run("finds every album, sorted, with sorted files and relative names", func(t *testing.T) {
		root := t.TempDir()
		touchFiles(t, root,
			"Zed/2001 Last/02 b.flac", "Zed/2001 Last/01 a.flac",
			"Abe/1999 First/01 a.flac",
		)

		albums, err := LibraryAlbums(root)
		require.NoError(t, err)
		require.Len(t, albums, 2)

		assert.Equal(t, filepath.Join("Abe", "1999 First"), albums[0].Rel)
		assert.Equal(t, filepath.Join(root, "Abe", "1999 First"), albums[0].RootPath)
		assert.Equal(t, []string{filepath.Join(root, "Abe", "1999 First", "01 a.flac")}, albums[0].Paths)

		assert.Equal(t, filepath.Join("Zed", "2001 Last"), albums[1].Rel)
		assert.Equal(t, []string{
			filepath.Join(root, "Zed", "2001 Last", "01 a.flac"),
			filepath.Join(root, "Zed", "2001 Last", "02 b.flac"),
		}, albums[1].Paths)
	})

	t.Run("a library with no albums is not an error", func(t *testing.T) {
		albums, err := LibraryAlbums(t.TempDir())
		require.NoError(t, err)
		assert.Empty(t, albums)
	})
}
