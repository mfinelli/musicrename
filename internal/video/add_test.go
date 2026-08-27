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
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFakeVideo creates a minimal file at dir/name for Add to move.
func writeFakeVideo(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte("fake video data"), 0o644))
	return path
}

func TestAdd(t *testing.T) {
	t.Run("happy path files the video and writes the nfo", func(t *testing.T) {
		srcDir := t.TempDir()
		videoRoot := t.TempDir()
		src := writeFakeVideo(t, srcDir, "raw-download.mp4")

		result, err := Add(videoRoot, AddInput{
			SourcePath: src,
			Artist:     "Beyoncé",
			Title:      "Crazy in Love",
			Album:      "Dangerously in Love",
			Year:       "2003",
		})
		require.NoError(t, err)

		wantDir := filepath.Join(videoRoot, "b", "beyonce", "crazy in love")
		assert.Equal(t, wantDir, result.Dir)
		assert.Equal(t, filepath.Join(wantDir, "crazy in love.mp4"), result.VideoPath)
		assert.Equal(t, filepath.Join(wantDir, NFOFilename), result.NFOPath)

		// Source file was moved, not copied.
		_, err = os.Stat(src)
		assert.True(t, os.IsNotExist(err))
		assert.FileExists(t, result.VideoPath)

		var nfo NFO
		data, err := os.ReadFile(result.NFOPath)
		require.NoError(t, err)
		require.NoError(t, xml.Unmarshal(data, &nfo))
		assert.Equal(t, "Crazy in Love", nfo.Title)
		assert.Equal(t, "Beyoncé", nfo.Artist)
		assert.Equal(t, "Dangerously in Love", nfo.Album)
		assert.Equal(t, "2003", nfo.Year)

		// The written file carries an XML declaration.
		assert.True(t, strings.HasPrefix(string(data), xml.Header))
	})

	t.Run("optional fields are omitted from the nfo when empty", func(t *testing.T) {
		srcDir := t.TempDir()
		videoRoot := t.TempDir()
		src := writeFakeVideo(t, srcDir, "raw.webm")

		result, err := Add(videoRoot, AddInput{
			SourcePath: src,
			Artist:     "Rick Astley",
			Title:      "Never Gonna Give You Up",
		})
		require.NoError(t, err)

		data, err := os.ReadFile(result.NFOPath)
		require.NoError(t, err)
		assert.NotContains(t, string(data), "<album>")
		assert.NotContains(t, string(data), "<year>")
	})

	t.Run("bucket override is respected", func(t *testing.T) {
		srcDir := t.TempDir()
		videoRoot := t.TempDir()
		src := writeFakeVideo(t, srcDir, "raw.mkv")

		result, err := Add(videoRoot, AddInput{
			SourcePath: src,
			Artist:     "Dave Matthews Band",
			Title:      "Some Song",
		})
		require.NoError(t, err)

		assert.Equal(t,
			filepath.Join(videoRoot, "d", "dave matthews band", "some song"),
			result.Dir,
		)
	})

	t.Run("preserves and lowercases whatever extension yt-dlp chose", func(t *testing.T) {
		srcDir := t.TempDir()
		videoRoot := t.TempDir()
		src := writeFakeVideo(t, srcDir, "raw.MKV")

		result, err := Add(videoRoot, AddInput{
			SourcePath: src,
			Artist:     "Artist",
			Title:      "Title",
		})
		require.NoError(t, err)
		assert.Equal(t, ".mkv", filepath.Ext(result.VideoPath))
	})

	t.Run("missing artist is an error", func(t *testing.T) {
		srcDir := t.TempDir()
		src := writeFakeVideo(t, srcDir, "raw.mp4")

		_, err := Add(t.TempDir(), AddInput{SourcePath: src, Title: "Title"})
		assert.Error(t, err)
	})

	t.Run("missing title is an error", func(t *testing.T) {
		srcDir := t.TempDir()
		src := writeFakeVideo(t, srcDir, "raw.mp4")

		_, err := Add(t.TempDir(), AddInput{SourcePath: src, Artist: "Artist"})
		assert.Error(t, err)
	})

	t.Run("whitespace-only artist/title is treated as missing", func(t *testing.T) {
		srcDir := t.TempDir()
		src := writeFakeVideo(t, srcDir, "raw.mp4")

		_, err := Add(t.TempDir(), AddInput{SourcePath: src, Artist: "  ", Title: "Title"})
		assert.Error(t, err)
	})

	t.Run("missing source file is an error", func(t *testing.T) {
		_, err := Add(t.TempDir(), AddInput{
			SourcePath: filepath.Join(t.TempDir(), "does-not-exist.mp4"),
			Artist:     "Artist",
			Title:      "Title",
		})
		assert.Error(t, err)
	})

	t.Run("unsupported extension is an error", func(t *testing.T) {
		srcDir := t.TempDir()
		src := writeFakeVideo(t, srcDir, "raw.avi")

		_, err := Add(t.TempDir(), AddInput{SourcePath: src, Artist: "Artist", Title: "Title"})
		assert.ErrorContains(t, err, "unsupported video extension")
	})

	t.Run("existing destination is an error and does not overwrite", func(t *testing.T) {
		srcDir := t.TempDir()
		videoRoot := t.TempDir()

		existingDir := filepath.Join(videoRoot, "a", "artist", "title")
		require.NoError(t, os.MkdirAll(existingDir, 0o755))
		existingFile := filepath.Join(existingDir, "title.mp4")
		require.NoError(t, os.WriteFile(existingFile, []byte("original"), 0o644))

		src := writeFakeVideo(t, srcDir, "raw.mp4")
		_, err := Add(videoRoot, AddInput{SourcePath: src, Artist: "Artist", Title: "Title"})
		assert.Error(t, err)

		// The pre-existing file must be untouched, and the source must not
		// have been moved (no --force / overwrite path).
		content, readErr := os.ReadFile(existingFile)
		require.NoError(t, readErr)
		assert.Equal(t, "original", string(content))
		assert.FileExists(t, src)
	})

	t.Run("info.txt alongside the source is carried along", func(t *testing.T) {
		srcDir := t.TempDir()
		videoRoot := t.TempDir()
		src := writeFakeVideo(t, srcDir, "raw.mp4")
		infoTxt := filepath.Join(srcDir, "info.txt")
		require.NoError(t, os.WriteFile(infoTxt, []byte("url: https://example.com\n"), 0o644))

		result, err := Add(videoRoot, AddInput{SourcePath: src, Artist: "Artist", Title: "Title"})
		require.NoError(t, err)

		require.NotEmpty(t, result.InfoPath)
		assert.Equal(t, filepath.Join(result.Dir, "info.txt"), result.InfoPath)
		assert.FileExists(t, result.InfoPath)

		// Moved, not copied.
		_, statErr := os.Stat(infoTxt)
		assert.True(t, os.IsNotExist(statErr))

		content, readErr := os.ReadFile(result.InfoPath)
		require.NoError(t, readErr)
		assert.Equal(t, "url: https://example.com\n", string(content))
	})

	t.Run("missing info.txt is not an error and InfoPath stays empty", func(t *testing.T) {
		srcDir := t.TempDir()
		videoRoot := t.TempDir()
		src := writeFakeVideo(t, srcDir, "raw.mp4")

		result, err := Add(videoRoot, AddInput{SourcePath: src, Artist: "Artist", Title: "Title"})
		require.NoError(t, err)
		assert.Empty(t, result.InfoPath)
	})

	t.Run("other sibling files in the source directory are left untouched", func(t *testing.T) {
		srcDir := t.TempDir()
		videoRoot := t.TempDir()
		src := writeFakeVideo(t, srcDir, "raw.mp4")
		sibling := writeFakeVideo(t, srcDir, "raw.jpg") // e.g. a yt-dlp thumbnail

		_, err := Add(videoRoot, AddInput{SourcePath: src, Artist: "Artist", Title: "Title"})
		require.NoError(t, err)

		assert.FileExists(t, sibling)
	})

	// NOTE: The cross-device (EXDEV) copy-and-delete fallback in
	// moveVideoFile/copyAndDeleteFile cannot be exercised from a standard
	// single-filesystem test environment; it mirrors internal/executor's
	// equivalent, already-tested logic. See internal/executor/executor_test.go
	// for the analogous NOTE on that package's untestable EXDEV path.
}

func TestArtistFolderPath(t *testing.T) {
	t.Run("standard first-letter bucketing", func(t *testing.T) {
		path, err := artistFolderPath("Beyoncé", "beyonce")
		require.NoError(t, err)
		assert.Equal(t, "b/beyonce", path)
	})

	t.Run("bucket override takes precedence", func(t *testing.T) {
		path, err := artistFolderPath("Dave Matthews Band", "dave matthews band")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join("d", "dave matthews band"), path)
	})
}
