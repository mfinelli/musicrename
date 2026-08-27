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

func TestFindVideoDirs(t *testing.T) {
	t.Run("finds leaf directories with a video file, nfo or not", func(t *testing.T) {
		root := t.TempDir()

		withNFO := filepath.Join(root, "b", "beyonce", "crazy in love")
		require.NoError(t, os.MkdirAll(withNFO, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(withNFO, "video.mp4"), []byte("d"), 0o644))
		require.NoError(t, writeNFO(nfoPath(withNFO), NFO{Artist: "Beyoncé", Title: "Crazy in Love"}))

		withoutNFO := filepath.Join(root, "orphan")
		require.NoError(t, os.MkdirAll(withoutNFO, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(withoutNFO, "video.webm"), []byte("d"), 0o644))

		dirs, err := FindVideoDirs(root)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{withNFO, withoutNFO}, dirs)
	})

	t.Run("does not descend into a leaf video directory", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "a", "artist", "title")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "video.mp4"), []byte("d"), 0o644))

		// A stray subdirectory under a video leaf shouldn't happen in
		// practice, but confirms FindVideoDirs stops at the leaf rather than
		// reporting it too.
		nested := filepath.Join(dir, "extra")
		require.NoError(t, os.MkdirAll(nested, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(nested, "another.mp4"), []byte("d"), 0o644))

		dirs, err := FindVideoDirs(root)
		require.NoError(t, err)
		assert.Equal(t, []string{dir}, dirs)
	})

	t.Run("empty root returns no directories", func(t *testing.T) {
		root := t.TempDir()
		dirs, err := FindVideoDirs(root)
		require.NoError(t, err)
		assert.Empty(t, dirs)
	})

	t.Run("results are sorted", func(t *testing.T) {
		root := t.TempDir()
		for _, name := range []string{"z-artist", "a-artist"} {
			dir := filepath.Join(root, "x", name, "title")
			require.NoError(t, os.MkdirAll(dir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "video.mp4"), []byte("d"), 0o644))
		}

		dirs, err := FindVideoDirs(root)
		require.NoError(t, err)
		require.Len(t, dirs, 2)
		assert.Less(t, dirs[0], dirs[1])
	})
}
