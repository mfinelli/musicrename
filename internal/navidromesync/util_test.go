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

package navidromesync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLibraryRootRootFor(t *testing.T) {
	t.Run("flat playlist file", func(t *testing.T) {
		root := "/home/mario/music"
		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		assert.Equal(t, root, libraryRootRootFor(path))
	})

	t.Run("nested organizational subfolder", func(t *testing.T) {
		root := "/home/mario/music"
		path := filepath.Join(root, "playlists", "roadtrips", "summer.m3u8")
		assert.Equal(t, root, libraryRootRootFor(path))
	})

	t.Run("deeply nested subfolder", func(t *testing.T) {
		root := "/home/mario/music"
		path := filepath.Join(root, "playlists", "a", "b", "c", "list.m3u8")
		assert.Equal(t, root, libraryRootRootFor(path))
	})
}

func TestNewPlaylistFilename(t *testing.T) {
	t.Run("sanitizes the name", func(t *testing.T) {
		dir := t.TempDir()
		name := newPlaylistFilename(dir, "Road Trip!")
		assert.Equal(t, "road trip.m3u8", name)
	})

	t.Run("falls back to a generic stem for a name that sanitizes to empty", func(t *testing.T) {
		dir := t.TempDir()
		name := newPlaylistFilename(dir, "!!!")
		assert.Equal(t, "playlist.m3u8", name)
	})

	t.Run("disambiguates a collision against an existing file", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "road trip.m3u8"), []byte(""), 0644))

		name := newPlaylistFilename(dir, "Road Trip")
		assert.Equal(t, "road trip-2.m3u8", name)
	})

	t.Run("keeps disambiguating past multiple collisions", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "list.m3u8"), []byte(""), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "list-2.m3u8"), []byte(""), 0644))

		name := newPlaylistFilename(dir, "List")
		assert.Equal(t, "list-3.m3u8", name)
	})
}

func TestStringSlicesEqual(t *testing.T) {
	assert.True(t, stringSlicesEqual(nil, nil))
	assert.True(t, stringSlicesEqual([]string{}, nil))
	assert.True(t, stringSlicesEqual([]string{"a", "b"}, []string{"a", "b"}))
	assert.False(t, stringSlicesEqual([]string{"a", "b"}, []string{"b", "a"}))
	assert.False(t, stringSlicesEqual([]string{"a"}, []string{"a", "b"}))
}
