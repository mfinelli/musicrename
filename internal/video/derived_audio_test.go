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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeDummy(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte("d"), 0o644))
}

func TestDerivedAudioFiles(t *testing.T) {
	t.Run("no derived audio file yet", func(t *testing.T) {
		dir := t.TempDir()
		videoPath := filepath.Join(dir, "crazy in love.mp4")
		writeDummy(t, videoPath)

		matches, err := DerivedAudioFiles(videoPath)
		require.NoError(t, err)
		assert.Empty(t, matches)
	})

	t.Run("finds a single matching derived audio file", func(t *testing.T) {
		dir := t.TempDir()
		videoPath := filepath.Join(dir, "crazy in love.mp4")
		audioPath := filepath.Join(dir, "crazy in love.m4a")
		writeDummy(t, videoPath)
		writeDummy(t, audioPath)

		matches, err := DerivedAudioFiles(videoPath)
		require.NoError(t, err)
		assert.Equal(t, []string{audioPath}, matches)
	})

	t.Run("ignores sibling files with a different stem", func(t *testing.T) {
		dir := t.TempDir()
		videoPath := filepath.Join(dir, "crazy in love.mp4")
		writeDummy(t, videoPath)
		writeDummy(t, filepath.Join(dir, NFOFilename))
		writeDummy(t, filepath.Join(dir, "info.txt"))
		writeDummy(t, filepath.Join(dir, "sums.md5"))
		writeDummy(t, filepath.Join(dir, "some other title.m4a"))

		matches, err := DerivedAudioFiles(videoPath)
		require.NoError(t, err)
		assert.Empty(t, matches)
	})

	t.Run("ignores a same-stem file with an unrecognized extension", func(t *testing.T) {
		dir := t.TempDir()
		videoPath := filepath.Join(dir, "crazy in love.mp4")
		writeDummy(t, videoPath)
		// Same stem, but .wav isn't a remux target extension at all.
		writeDummy(t, filepath.Join(dir, "crazy in love.wav"))

		matches, err := DerivedAudioFiles(videoPath)
		require.NoError(t, err)
		assert.Empty(t, matches)
	})

	t.Run("returns every match when more than one exists (anomalous state)", func(t *testing.T) {
		dir := t.TempDir()
		videoPath := filepath.Join(dir, "crazy in love.mp4")
		writeDummy(t, videoPath)
		// Simulates a stale file left behind by a codec change across a
		// re-extraction that didn't clean up (or manual tampering) and
		// not collapsed to one result.
		writeDummy(t, filepath.Join(dir, "crazy in love.m4a"))
		writeDummy(t, filepath.Join(dir, "crazy in love.opus"))

		matches, err := DerivedAudioFiles(videoPath)
		require.NoError(t, err)
		assert.Equal(t, []string{
			filepath.Join(dir, "crazy in love.m4a"),
			filepath.Join(dir, "crazy in love.opus"),
		}, matches)
	})

	t.Run("extension matching is case-insensitive", func(t *testing.T) {
		dir := t.TempDir()
		videoPath := filepath.Join(dir, "crazy in love.mp4")
		audioPath := filepath.Join(dir, "crazy in love.M4A")
		writeDummy(t, videoPath)
		writeDummy(t, audioPath)

		matches, err := DerivedAudioFiles(videoPath)
		require.NoError(t, err)
		assert.Equal(t, []string{audioPath}, matches)
	})

	t.Run("errors if the directory can't be read", func(t *testing.T) {
		_, err := DerivedAudioFiles(filepath.Join(t.TempDir(), "nonexistent", "video.mp4"))
		assert.Error(t, err)
	})
}
