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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/hasher"
)

func TestCreate(t *testing.T) {
	t.Run("writes headers only, no entries", func(t *testing.T) {
		root := t.TempDir()
		path, warning, err := Create(root, CreateOptions{Name: "Road Trip"})
		require.NoError(t, err)
		assert.Empty(t, warning)
		assert.Equal(t, filepath.Join(root, "playlists", "road trip.m3u8"), path)

		got, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		assert.Equal(t, "#PLAYLIST:Road Trip\n", string(got))
	})

	t.Run("with targets, writes a sorted #TARGETS: directive", func(t *testing.T) {
		root := t.TempDir()
		path, _, err := Create(root, CreateOptions{Name: "Workout", Targets: []string{"sdcard", "ipod"}})
		require.NoError(t, err)

		got, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		assert.Equal(t, "#PLAYLIST:Workout\n#TARGETS:ipod,sdcard\n", string(got))
	})

	t.Run("an empty but non-nil Targets writes an explicit empty #TARGETS:", func(t *testing.T) {
		root := t.TempDir()
		path, _, err := Create(root, CreateOptions{Name: "Nowhere", Targets: []string{}})
		require.NoError(t, err)

		got, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		assert.Equal(t, "#PLAYLIST:Nowhere\n#TARGETS:\n", string(got))
	})

	t.Run("nil Targets omits the directive entirely", func(t *testing.T) {
		root := t.TempDir()
		path, _, err := Create(root, CreateOptions{Name: "Everywhere"})
		require.NoError(t, err)

		got, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		assert.Equal(t, "#PLAYLIST:Everywhere\n", string(got))
	})

	t.Run("sanitizes the name for the filename but preserves it verbatim in the directive", func(t *testing.T) {
		root := t.TempDir()
		path, _, err := Create(root, CreateOptions{Name: "Road Trip! 2020"})
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, "playlists", "road trip 2020.m3u8"), path)

		gp, readErr := ReadGlobalPlaylist(path)
		require.NoError(t, readErr)
		assert.Equal(t, "Road Trip! 2020", gp.Name)
	})

	t.Run("truncates the sanitized filename to 40 characters", func(t *testing.T) {
		root := t.TempDir()
		longName := strings.Repeat("a", 60)
		path, _, err := Create(root, CreateOptions{Name: longName})
		require.NoError(t, err)
		gotStem := strings.TrimSuffix(filepath.Base(path), ".m3u8")
		assert.Len(t, gotStem, 40)
	})

	t.Run("a name that sanitizes to an empty string is an error", func(t *testing.T) {
		root := t.TempDir()
		_, _, err := Create(root, CreateOptions{Name: "!!!"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "sanitizes to an empty string")
	})

	t.Run("an existing destination is an error, never overwritten", func(t *testing.T) {
		root := t.TempDir()
		_, _, err := Create(root, CreateOptions{Name: "Road Trip"})
		require.NoError(t, err)

		_, _, err = Create(root, CreateOptions{Name: "Road Trip"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")

		gp, readErr := ReadGlobalPlaylist(filepath.Join(root, "playlists", "road trip.m3u8"))
		require.NoError(t, readErr)
		assert.Equal(t, "Road Trip", gp.Name, "the original file must be untouched")
	})

	t.Run("adds the new file's entry to an existing playlists/sums.md5", func(t *testing.T) {
		root := t.TempDir()
		playlistsDir := filepath.Join(root, "playlists")
		require.NoError(t, os.MkdirAll(playlistsDir, 0755))
		require.NoError(t, hasher.Hash(playlistsDir, nil))

		path, warning, err := Create(root, CreateOptions{Name: "Road Trip"})
		require.NoError(t, err)
		assert.Empty(t, warning)

		sums, existed, readErr := hasher.ReadSums(playlistsDir, hasher.SumsFilename)
		require.NoError(t, readErr)
		require.True(t, existed)
		assert.Contains(t, sums, filepath.Base(path))
	})

	t.Run("no playlists/sums.md5 at all: create still succeeds, no warning, none is created", func(t *testing.T) {
		root := t.TempDir()
		_, warning, err := Create(root, CreateOptions{Name: "Road Trip"})
		require.NoError(t, err)
		assert.Empty(t, warning)
		assert.NoFileExists(t, filepath.Join(root, "playlists", hasher.SumsFilename))
	})
}
