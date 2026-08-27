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

package video

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupFiledVideoDir creates a directory containing a fake video file and,
// if nfo is non-nil, a musicvideo.nfo with the given fields (as Add would
// leave behind). Returns the directory path.
func setupFiledVideoDir(t *testing.T, nfo *NFO) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "title.mp4"), []byte("fake video data"), 0o644))
	if nfo != nil {
		require.NoError(t, writeNFO(nfoPath(dir), *nfo))
	}
	return dir
}

func TestReadNFO(t *testing.T) {
	t.Run("reads back what was written", func(t *testing.T) {
		dir := setupFiledVideoDir(t, &NFO{
			Title:  "Crazy in Love",
			Artist: "Beyoncé",
			Album:  "Dangerously in Love",
			Year:   "2003",
		})

		nfo, err := ReadNFO(dir)
		require.NoError(t, err)
		assert.Equal(t, "Crazy in Love", nfo.Title)
		assert.Equal(t, "Beyoncé", nfo.Artist)
		assert.Equal(t, "Dangerously in Love", nfo.Album)
		assert.Equal(t, "2003", nfo.Year)
	})

	t.Run("optional fields read back empty when absent", func(t *testing.T) {
		dir := setupFiledVideoDir(t, &NFO{Title: "T", Artist: "A"})

		nfo, err := ReadNFO(dir)
		require.NoError(t, err)
		assert.Empty(t, nfo.Album)
		assert.Empty(t, nfo.Year)
	})

	t.Run("missing nfo is a wrapped not-exist error", func(t *testing.T) {
		dir := setupFiledVideoDir(t, nil)

		_, err := ReadNFO(dir)
		require.Error(t, err)
		// os.IsNotExist does not unwrap generic fmt.Errorf("...: %w", err)
		// chains (it only recognizes specific types like *PathError), so
		// errors.Is against fs.ErrNotExist is required here to see through
		// ReadNFO's wrapping (this is also what cmd/video_edit.go relies on
		// to detect "no nfo yet" and fall back to blank prompt values).
		assert.ErrorIs(t, err, fs.ErrNotExist)
	})
}

func TestEdit(t *testing.T) {
	t.Run("overwrites all fields on an existing nfo", func(t *testing.T) {
		dir := setupFiledVideoDir(t, &NFO{
			Title:  "Old Title",
			Artist: "Old Artist",
			Album:  "Old Album",
			Year:   "1999",
		})

		result, err := Edit(dir, EditInput{
			Artist: "New Artist",
			Title:  "New Title",
			Album:  "New Album",
			Year:   "2020",
		})
		require.NoError(t, err)
		assert.Equal(t, nfoPath(dir), result.NFOPath)
		assert.False(t, result.Created)

		nfo, err := ReadNFO(dir)
		require.NoError(t, err)
		assert.Equal(t, "New Artist", nfo.Artist)
		assert.Equal(t, "New Title", nfo.Title)
		assert.Equal(t, "New Album", nfo.Album)
		assert.Equal(t, "2020", nfo.Year)
	})

	t.Run("creates a fresh nfo when one doesn't exist yet", func(t *testing.T) {
		dir := setupFiledVideoDir(t, nil)

		result, err := Edit(dir, EditInput{Artist: "Artist", Title: "Title"})
		require.NoError(t, err)
		assert.True(t, result.Created)
		assert.FileExists(t, result.NFOPath)

		nfo, err := ReadNFO(dir)
		require.NoError(t, err)
		assert.Equal(t, "Artist", nfo.Artist)
		assert.Equal(t, "Title", nfo.Title)
	})

	t.Run("refuses to create an nfo in a directory with no video file", func(t *testing.T) {
		dir := t.TempDir() // no video file written

		_, err := Edit(dir, EditInput{Artist: "Artist", Title: "Title"})
		assert.ErrorContains(t, err, "no video file found")

		_, statErr := os.Stat(filepath.Join(dir, NFOFilename))
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("clears optional fields when given empty values", func(t *testing.T) {
		dir := setupFiledVideoDir(t, &NFO{
			Title:  "Title",
			Artist: "Artist",
			Album:  "Old Album",
			Year:   "1999",
		})

		_, err := Edit(dir, EditInput{Artist: "Artist", Title: "Title"})
		require.NoError(t, err)

		nfo, err := ReadNFO(dir)
		require.NoError(t, err)
		assert.Empty(t, nfo.Album)
		assert.Empty(t, nfo.Year)
	})

	t.Run("does not move or rename the video file", func(t *testing.T) {
		dir := setupFiledVideoDir(t, &NFO{Title: "Title", Artist: "Artist"})
		videoPath := filepath.Join(dir, "title.mp4")

		_, err := Edit(dir, EditInput{Artist: "Totally Different Artist", Title: "Title"})
		require.NoError(t, err)

		assert.FileExists(t, videoPath)
	})

	t.Run("missing artist is an error", func(t *testing.T) {
		dir := setupFiledVideoDir(t, &NFO{Title: "Title", Artist: "Artist"})
		_, err := Edit(dir, EditInput{Title: "New Title"})
		assert.Error(t, err)
	})

	t.Run("missing title is an error", func(t *testing.T) {
		dir := setupFiledVideoDir(t, &NFO{Title: "Title", Artist: "Artist"})
		_, err := Edit(dir, EditInput{Artist: "New Artist"})
		assert.Error(t, err)
	})
}
