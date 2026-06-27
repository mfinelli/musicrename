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

package hasher

import (
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeFile creates a file at path with the given content, creating any
// necessary parent directories. It fails the test immediately on any error.
func makeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}

// md5hex returns the lowercase hex MD5 digest of content, matching the
// format written by Hash. Used to compute expected values in tests without
// duplicating the hashing logic.
func md5hex(content string) string {
	h := md5.Sum([]byte(content))
	return fmt.Sprintf("%x", h)
}

// readSums reads and returns the contents of dir/sums.md5.
func readSums(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, SumsFilename))
	require.NoError(t, err)
	return string(b)
}

func TestHash_OutputFormat(t *testing.T) {
	t.Run("binary file uses space-asterisk separator", func(t *testing.T) {
		dir := t.TempDir()
		makeFile(t, filepath.Join(dir, "01 track one.flac"), "audio")

		require.NoError(t, Hash(dir, nil))

		got := readSums(t, dir)
		assert.Equal(t, md5hex("audio")+" *01 track one.flac\n", got)
	})

	t.Run("text file uses two-space separator", func(t *testing.T) {
		dir := t.TempDir()
		makeFile(t, filepath.Join(dir, "rip.log"), "log content")

		require.NoError(t, Hash(dir, nil))

		got := readSums(t, dir)
		assert.Equal(t, md5hex("log content")+"  rip.log\n", got)
	})

	t.Run("all recognised text extensions use two-space separator", func(t *testing.T) {
		extensions := []string{".cue", ".log", ".m3u", ".m3u8", ".txt"}
		for _, ext := range extensions {
			t.Run(ext, func(t *testing.T) {
				dir := t.TempDir()
				name := "file" + ext
				makeFile(t, filepath.Join(dir, name), "content")

				require.NoError(t, Hash(dir, nil))

				got := readSums(t, dir)
				assert.Contains(t, got, "  "+name+"\n",
					"expected two-space separator for %s", ext)
				assert.NotContains(t, got, " *"+name,
					"did not expect binary separator for %s", ext)
			})
		}
	})

	t.Run("extension check is case-insensitive", func(t *testing.T) {
		dir := t.TempDir()
		makeFile(t, filepath.Join(dir, "rip.LOG"), "log content")

		require.NoError(t, Hash(dir, nil))

		got := readSums(t, dir)
		assert.Contains(t, got, "  rip.LOG\n")
	})

	t.Run("mixed binary and text files produce correct separators", func(t *testing.T) {
		dir := t.TempDir()
		makeFile(t, filepath.Join(dir, "01 track.flac"), "audio")
		makeFile(t, filepath.Join(dir, "rip.log"), "log")

		require.NoError(t, Hash(dir, nil))

		got := readSums(t, dir)
		assert.Contains(t, got, " *01 track.flac\n")
		assert.Contains(t, got, "  rip.log\n")
	})
}

func TestHash_Ordering(t *testing.T) {
	t.Run("files are sorted alphabetically for a stable output", func(t *testing.T) {
		dir := t.TempDir()
		makeFile(t, filepath.Join(dir, "03 track three.flac"), "c")
		makeFile(t, filepath.Join(dir, "01 track one.flac"), "a")
		makeFile(t, filepath.Join(dir, "02 track two.flac"), "b")

		require.NoError(t, Hash(dir, nil))

		got := readSums(t, dir)
		lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
		require.Len(t, lines, 3)
		assert.Contains(t, lines[0], "01 track one.flac")
		assert.Contains(t, lines[1], "02 track two.flac")
		assert.Contains(t, lines[2], "03 track three.flac")
	})

	t.Run("files in subdirectories use relative paths with forward slashes", func(t *testing.T) {
		dir := t.TempDir()
		makeFile(t, filepath.Join(dir, "artwork", "cover.jpg"), "image")

		require.NoError(t, Hash(dir, nil))

		got := readSums(t, dir)
		assert.Contains(t, got, " *artwork/cover.jpg\n")
	})

	t.Run("subdirectory files sort together with root files", func(t *testing.T) {
		dir := t.TempDir()
		makeFile(t, filepath.Join(dir, "01 track.flac"), "audio")
		makeFile(t, filepath.Join(dir, "artwork", "cover.jpg"), "image")
		makeFile(t, filepath.Join(dir, "rip.log"), "log")

		require.NoError(t, Hash(dir, nil))

		got := readSums(t, dir)
		lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
		require.Len(t, lines, 3)
		// "01 track.flac" < "artwork/cover.jpg" < "rip.log" lexicographically.
		assert.Contains(t, lines[0], "01 track.flac")
		assert.Contains(t, lines[1], "artwork/cover.jpg")
		assert.Contains(t, lines[2], "rip.log")
	})
}

func TestHash_SumsFile(t *testing.T) {
	t.Run("sums.md5 is excluded from its own listing", func(t *testing.T) {
		dir := t.TempDir()
		makeFile(t, filepath.Join(dir, "01 track.flac"), "audio")

		require.NoError(t, Hash(dir, nil))

		got := readSums(t, dir)
		assert.NotContains(t, got, SumsFilename)
	})

	t.Run("existing sums.md5 is overwritten on a second run", func(t *testing.T) {
		dir := t.TempDir()
		makeFile(t, filepath.Join(dir, "01 track.flac"), "audio")

		require.NoError(t, Hash(dir, nil))
		first := readSums(t, dir)

		// Add a second file; the new sums.md5 must include both.
		makeFile(t, filepath.Join(dir, "02 track.flac"), "more audio")
		require.NoError(t, Hash(dir, nil))
		second := readSums(t, dir)

		assert.NotEqual(t, first, second)
		assert.Contains(t, second, "01 track.flac")
		assert.Contains(t, second, "02 track.flac")
	})
}

func TestHash_Correctness(t *testing.T) {
	t.Run("MD5 digest matches expected value for known content", func(t *testing.T) {
		dir := t.TempDir()
		content := "hello, world"
		makeFile(t, filepath.Join(dir, "01 track.flac"), content)

		require.NoError(t, Hash(dir, nil))

		got := readSums(t, dir)
		assert.Contains(t, got, md5hex(content)+" *01 track.flac\n")
	})

	t.Run("empty file produces the well-known MD5 of empty input", func(t *testing.T) {
		dir := t.TempDir()
		makeFile(t, filepath.Join(dir, "silence.flac"), "")

		require.NoError(t, Hash(dir, nil))

		// MD5 of zero bytes is always d41d8cd98f00b204e9800998ecf8427e.
		got := readSums(t, dir)
		assert.Contains(t, got, "d41d8cd98f00b204e9800998ecf8427e *silence.flac\n")
	})
}

func TestHash_Progress(t *testing.T) {
	t.Run("progress callback is called once per file in sorted order", func(t *testing.T) {
		dir := t.TempDir()
		makeFile(t, filepath.Join(dir, "02 track two.flac"), "b")
		makeFile(t, filepath.Join(dir, "01 track one.flac"), "a")
		makeFile(t, filepath.Join(dir, "artwork", "cover.jpg"), "img")

		var got []string
		require.NoError(t, Hash(dir, func(rel string) {
			got = append(got, rel)
		}))

		require.Len(t, got, 3)
		assert.Equal(t, "01 track one.flac", got[0])
		assert.Equal(t, "02 track two.flac", got[1])
		assert.Equal(t, "artwork/cover.jpg", got[2])
	})

	t.Run("nil progress callback does not panic", func(t *testing.T) {
		dir := t.TempDir()
		makeFile(t, filepath.Join(dir, "01 track.flac"), "audio")

		assert.NotPanics(t, func() {
			require.NoError(t, Hash(dir, nil))
		})
	})

	t.Run("sums.md5 is not reported to the progress callback", func(t *testing.T) {
		dir := t.TempDir()
		makeFile(t, filepath.Join(dir, "01 track.flac"), "audio")
		// Pre-create a sums.md5 so it exists during the walk.
		makeFile(t, filepath.Join(dir, SumsFilename), "old content")

		var got []string
		require.NoError(t, Hash(dir, func(rel string) {
			got = append(got, rel)
		}))

		assert.NotContains(t, got, SumsFilename)
	})
}

func TestCollectFiles(t *testing.T) {
	t.Run("returns empty slice for an empty directory", func(t *testing.T) {
		dir := t.TempDir()

		files, err := collectFiles(dir)
		require.NoError(t, err)
		assert.Empty(t, files)
	})

	t.Run("excludes sums.md5 from the result", func(t *testing.T) {
		dir := t.TempDir()
		makeFile(t, filepath.Join(dir, SumsFilename), "content")
		makeFile(t, filepath.Join(dir, "01 track.flac"), "audio")

		files, err := collectFiles(dir)
		require.NoError(t, err)
		assert.Equal(t, []string{"01 track.flac"}, files)
	})

	t.Run("returns files from subdirectories with relative paths", func(t *testing.T) {
		dir := t.TempDir()
		makeFile(t, filepath.Join(dir, "01 track.flac"), "audio")
		makeFile(t, filepath.Join(dir, "artwork", "cover.jpg"), "image")

		files, err := collectFiles(dir)
		require.NoError(t, err)
		assert.Contains(t, files, "artwork/cover.jpg")
	})
}

func TestIsTextFile(t *testing.T) {
	t.Run("returns true for recognised text extensions", func(t *testing.T) {
		textFiles := []string{
			"rip.cue", "rip.log", "playlist.m3u", "playlist.m3u8", "notes.txt",
		}
		for _, name := range textFiles {
			t.Run(name, func(t *testing.T) {
				assert.True(t, isTextFile(name))
			})
		}
	})

	t.Run("returns false for binary extensions", func(t *testing.T) {
		binaryFiles := []string{
			"track.flac", "track.mp3", "track.m4a", "cover.jpg", "cover.png",
			"scan.tiff", "booklet.pdf",
		}
		for _, name := range binaryFiles {
			t.Run(name, func(t *testing.T) {
				assert.False(t, isTextFile(name))
			})
		}
	})

	t.Run("extension check is case-insensitive", func(t *testing.T) {
		assert.True(t, isTextFile("rip.LOG"))
		assert.True(t, isTextFile("rip.Log"))
		assert.False(t, isTextFile("track.FLAC"))
	})

	t.Run("returns false for files with no extension", func(t *testing.T) {
		assert.False(t, isTextFile("README"))
	})
}
