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

package playlist

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDir(t *testing.T) {
	t.Run("joins libraryRootRoot with playlists", func(t *testing.T) {
		assert.Equal(t, filepath.Join("/library", "playlists"), Dir("/library"))
	})
}

func TestLibraryRootRootFor(t *testing.T) {
	t.Run("flat playlist file", func(t *testing.T) {
		root := filepath.Join("home", "mario", "music")
		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		assert.Equal(t, root, LibraryRootRootFor(path))
	})

	t.Run("nested organizational subfolder", func(t *testing.T) {
		root := filepath.Join("home", "mario", "music")
		path := filepath.Join(root, "playlists", "roadtrips", "summer.m3u8")
		assert.Equal(t, root, LibraryRootRootFor(path))
	})

	t.Run("deeply nested subfolder", func(t *testing.T) {
		root := filepath.Join("home", "mario", "music")
		path := filepath.Join(root, "playlists", "a", "b", "c", "list.m3u8")
		assert.Equal(t, root, LibraryRootRootFor(path))
	})

	t.Run("is the inverse of Dir for the root it returns", func(t *testing.T) {
		root := filepath.Join("home", "mario", "music")
		path := filepath.Join(Dir(root), "roadtrip.m3u8")
		assert.Equal(t, root, LibraryRootRootFor(path))
	})
}

func TestWalkTree(t *testing.T) {
	t.Run("no playlists/ directory at all is not an error", func(t *testing.T) {
		root := t.TempDir()
		var visited []string
		err := WalkTree(root, func(path string) error {
			visited = append(visited, path)
			return nil
		})
		require.NoError(t, err)
		assert.Empty(t, visited)
	})

	t.Run("visits every .m3u8 file, ignoring other files", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "playlists"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "playlists", "a.m3u8"), []byte(""), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(root, "playlists", "README.md"), []byte(""), 0644))

		var visited []string
		err := WalkTree(root, func(path string) error {
			visited = append(visited, path)
			return nil
		})
		require.NoError(t, err)
		require.Len(t, visited, 1)
		assert.Equal(t, filepath.Join(root, "playlists", "a.m3u8"), visited[0])
	})

	t.Run("recurses into subdirectories", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "playlists", "roadtrips"), 0755))
		require.NoError(t, os.WriteFile(
			filepath.Join(root, "playlists", "roadtrips", "summer.m3u8"), []byte(""), 0644,
		))

		var visited []string
		err := WalkTree(root, func(path string) error {
			visited = append(visited, path)
			return nil
		})
		require.NoError(t, err)
		require.Len(t, visited, 1)
		assert.Equal(t, filepath.Join(root, "playlists", "roadtrips", "summer.m3u8"), visited[0])
	})

	t.Run("a non-nil error from fn stops the walk and is returned", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "playlists"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "playlists", "a.m3u8"), []byte(""), 0644))

		boom := errors.New("boom")
		err := WalkTree(root, func(path string) error { return boom })
		assert.ErrorIs(t, err, boom)
	})
}
