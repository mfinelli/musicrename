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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/hasher"
)

// messages extracts the Message field from a slice of Warning, joined into a
// single string, for convenient substring assertions.
func messages(warnings []Warning) string {
	msgs := make([]string, len(warnings))
	for i, w := range warnings {
		msgs[i] = w.Message
	}
	return strings.Join(msgs, "; ")
}

// stubSums writes a real sums.md5 into dir covering every file currently
// present there. Named "stub" because tests using it don't care about the
// checksums' correctness beyond the fact that they're consistent with the
// directory listing (Check's [hasher.DiffEntries]-based check would
// otherwise flag any file present on disk but absent from an empty
// sums.md5).
func stubSums(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, hasher.Hash(dir, nil))
}

func TestCheck(t *testing.T) {
	t.Run("a fully correct video has no warnings", func(t *testing.T) {
		root := t.TempDir()
		result, err := Add(root, AddInput{
			SourcePath: mustWriteVideo(t, "raw.mp4"),
			Artist:     "Beyoncé",
			Title:      "Crazy in Love",
		})
		require.NoError(t, err)
		stubSums(t, result.Dir)

		check, err := Check(result.Dir, root)
		require.NoError(t, err)
		assert.False(t, check.HasWarnings())
	})

	t.Run("missing nfo is flagged", func(t *testing.T) {
		dir := setupFiledVideoDir(t, nil)
		check, err := Check(dir, "")
		require.NoError(t, err)
		assert.Contains(t, messages(check.Warnings), "missing musicvideo.nfo")
	})

	t.Run("empty title and artist are each flagged", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "video.mp4"), []byte("d"), 0o644))
		require.NoError(t, writeNFO(nfoPath(dir), NFO{}))

		check, err := Check(dir, "")
		require.NoError(t, err)
		msgs := messages(check.Warnings)
		assert.Contains(t, msgs, "musicvideo.nfo missing title")
		assert.Contains(t, msgs, "musicvideo.nfo missing artist")
	})

	t.Run("no video file is flagged", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, writeNFO(nfoPath(dir), NFO{Artist: "A", Title: "T"}))

		check, err := Check(dir, "")
		require.NoError(t, err)
		assert.Contains(t, messages(check.Warnings), "no video file found")
	})

	t.Run("multiple video files is flagged", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "one.mp4"), []byte("d"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "two.mkv"), []byte("d"), 0o644))
		require.NoError(t, writeNFO(nfoPath(dir), NFO{Artist: "A", Title: "T"}))

		check, err := Check(dir, "")
		require.NoError(t, err)
		assert.Contains(t, messages(check.Warnings), "multiple video files found (expected exactly one)")
	})

	t.Run("missing sums.md5 is flagged", func(t *testing.T) {
		dir := setupFiledVideoDir(t, &NFO{Artist: "A", Title: "T"})
		check, err := Check(dir, "")
		require.NoError(t, err)
		assert.Contains(t, messages(check.Warnings), "missing sums.md5")
	})

	t.Run("file on disk with no recorded entry is flagged", func(t *testing.T) {
		dir := setupFiledVideoDir(t, &NFO{Artist: "A", Title: "T"})
		stubSums(t, dir)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("d"), 0o644))

		check, err := Check(dir, "")
		require.NoError(t, err)
		assert.Contains(t, messages(check.Warnings), "extra.txt not recorded in sums.md5")
	})

	t.Run("recorded entry with no file on disk is flagged", func(t *testing.T) {
		dir := setupFiledVideoDir(t, &NFO{Artist: "A", Title: "T"})
		stubSums(t, dir)
		sums, _, err := hasher.ReadSums(dir, "sums.md5")
		require.NoError(t, err)
		sums["ghost.txt"] = strings.Repeat("0", 32)
		require.NoError(t, hasher.WriteSums(dir, "sums.md5", sums))

		check, err := Check(dir, "")
		require.NoError(t, err)
		assert.Contains(t, messages(check.Warnings), `sums.md5 references "ghost.txt" which does not exist`)
	})

	t.Run("path conformance is skipped when videoRoot is empty", func(t *testing.T) {
		root := t.TempDir()
		dir := place(t, root, filepath.Join("w", "wrong", "wrong"), NFO{
			Artist: "Artist", Title: "Title",
		}, false)

		check, err := Check(dir, "")
		require.NoError(t, err)
		assert.NotContains(t, messages(check.Warnings), "path does not match")
	})

	t.Run("path conformance is flagged when videoRoot is given and drifted", func(t *testing.T) {
		root := t.TempDir()
		dir := place(t, root, filepath.Join("w", "wrong", "wrong"), NFO{
			Artist: "Artist", Title: "Title",
		}, false)

		check, err := Check(dir, root)
		require.NoError(t, err)
		assert.Contains(t, messages(check.Warnings), "path does not match")
	})

	t.Run("correctly-placed video has no path-conformance warning", func(t *testing.T) {
		root := t.TempDir()
		_, err := Add(root, AddInput{
			SourcePath: mustWriteVideo(t, "raw.mp4"),
			Artist:     "Artist",
			Title:      "Title",
		})
		require.NoError(t, err)

		dir := filepath.Join(root, "a", "artist", "title")
		check, err := Check(dir, root)
		require.NoError(t, err)
		assert.NotContains(t, messages(check.Warnings), "path does not match")
	})
}

func TestCheckAll(t *testing.T) {
	t.Run("aggregates warnings across the whole tree", func(t *testing.T) {
		root := t.TempDir()
		place(t, root, filepath.Join("a", "artist one", "title one"), NFO{
			Artist: "Artist One", Title: "Title One",
		}, false)

		orphan := filepath.Join(root, "orphan")
		require.NoError(t, os.MkdirAll(orphan, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(orphan, "video.mp4"), []byte("d"), 0o644))

		result, err := CheckAll(root)
		require.NoError(t, err)
		assert.Equal(t, 2, result.Checked)
		assert.True(t, result.HasWarnings())
		assert.Contains(t, messages(result.Warnings), "missing musicvideo.nfo")
	})

	t.Run("empty root has no warnings and zero checked", func(t *testing.T) {
		root := t.TempDir()
		result, err := CheckAll(root)
		require.NoError(t, err)
		assert.Equal(t, 0, result.Checked)
		assert.False(t, result.HasWarnings())
	})
}
