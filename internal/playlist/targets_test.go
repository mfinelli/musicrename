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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/hasher"
)

func TestSetTargets(t *testing.T) {
	t.Run("errors when the file does not exist", func(t *testing.T) {
		root := t.TempDir()
		_, err := SetTargets(root, filepath.Join(root, "playlists", "missing.m3u8"), []string{"ipod"})
		assert.Error(t, err)
	})

	t.Run("sets an explicit #TARGETS: directive", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, WriteGlobalPlaylist(path, &GlobalPlaylist{Name: "Road Trip"}))

		warning, err := SetTargets(root, path, []string{"sdcard", "ipod"})
		require.NoError(t, err)
		assert.Empty(t, warning)

		gp, readErr := ReadGlobalPlaylist(path)
		require.NoError(t, readErr)
		assert.True(t, gp.HasTargets)
		assert.ElementsMatch(t, []string{"ipod", "sdcard"}, gp.Targets)
	})

	t.Run("nil targets removes the directive entirely", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, WriteGlobalPlaylist(path, &GlobalPlaylist{
			Name: "Road Trip", HasTargets: true, Targets: []string{"ipod"},
		}))

		warning, err := SetTargets(root, path, nil)
		require.NoError(t, err)
		assert.Empty(t, warning)

		gp, readErr := ReadGlobalPlaylist(path)
		require.NoError(t, readErr)
		assert.False(t, gp.HasTargets)
	})

	t.Run("an empty but non-nil targets writes an explicit empty #TARGETS:", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, WriteGlobalPlaylist(path, &GlobalPlaylist{
			Name: "Road Trip", HasTargets: true, Targets: []string{"ipod"},
		}))

		warning, err := SetTargets(root, path, []string{})
		require.NoError(t, err)
		assert.Empty(t, warning)

		got, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		assert.Contains(t, string(got), "#TARGETS:\n")
	})

	t.Run("preserves every other directive and all entries", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, WriteGlobalPlaylist(path, &GlobalPlaylist{
			Name: "Road Trip", NavidromeID: "id-1", HasNavidromeID: true,
			Entries: []string{"main/a/artist/album/01 track.flac"},
		}))

		warning, err := SetTargets(root, path, []string{"ipod"})
		require.NoError(t, err)
		assert.Empty(t, warning)

		gp, readErr := ReadGlobalPlaylist(path)
		require.NoError(t, readErr)
		assert.Equal(t, "Road Trip", gp.Name)
		assert.Equal(t, "id-1", gp.NavidromeID)
		assert.True(t, gp.HasNavidromeID)
		assert.Equal(t, []string{"main/a/artist/album/01 track.flac"}, gp.Entries)
	})

	t.Run("refreshes the entry in an existing playlists/sums.md5", func(t *testing.T) {
		root := t.TempDir()
		playlistsDir := filepath.Join(root, "playlists")
		path := filepath.Join(playlistsDir, "roadtrip.m3u8")
		require.NoError(t, WriteGlobalPlaylist(path, &GlobalPlaylist{Name: "Road Trip"}))
		require.NoError(t, hasher.Hash(playlistsDir, nil))
		before, _, err := hasher.ReadSums(playlistsDir, hasher.SumsFilename)
		require.NoError(t, err)
		oldHash := before["roadtrip.m3u8"]
		require.NotEmpty(t, oldHash)

		warning, err := SetTargets(root, path, []string{"ipod"})
		require.NoError(t, err)
		assert.Empty(t, warning)

		after, _, err := hasher.ReadSums(playlistsDir, hasher.SumsFilename)
		require.NoError(t, err)
		assert.NotEqual(t, oldHash, after["roadtrip.m3u8"])
	})

	t.Run("no playlists/sums.md5 at all: update still succeeds, no warning", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, WriteGlobalPlaylist(path, &GlobalPlaylist{Name: "Road Trip"}))

		warning, err := SetTargets(root, path, []string{"ipod"})
		require.NoError(t, err)
		assert.Empty(t, warning)
	})

	t.Run("subdirectory playlist updates the correctly-relative entry", func(t *testing.T) {
		root := t.TempDir()
		playlistsDir := filepath.Join(root, "playlists")
		path := filepath.Join(playlistsDir, "ipod", "roadtrip.m3u8")
		require.NoError(t, WriteGlobalPlaylist(path, &GlobalPlaylist{Name: "Road Trip"}))
		require.NoError(t, hasher.Hash(playlistsDir, nil))

		warning, err := SetTargets(root, path, []string{"ipod"})
		require.NoError(t, err)
		assert.Empty(t, warning)

		sums, _, err := hasher.ReadSums(playlistsDir, hasher.SumsFilename)
		require.NoError(t, err)
		assert.Contains(t, sums, filepath.Join("ipod", "roadtrip.m3u8"))
	})
}
