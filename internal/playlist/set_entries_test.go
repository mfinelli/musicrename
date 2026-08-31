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
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/hasher"
)

func TestSetEntries(t *testing.T) {
	t.Run("errors when the file does not exist", func(t *testing.T) {
		root := t.TempDir()
		_, err := SetEntries(root, filepath.Join(root, "playlists", "missing.m3u8"), []string{"track.flac"})
		assert.Error(t, err)
	})

	t.Run("replaces the entry list", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, WriteGlobalPlaylist(path, &GlobalPlaylist{
			Name: "Road Trip", Entries: []string{"a.flac", "b.flac", "c.flac"},
		}))

		warning, err := SetEntries(root, path, []string{"a.flac", "c.flac"})
		require.NoError(t, err)
		assert.Empty(t, warning)

		gp, readErr := ReadGlobalPlaylist(path)
		require.NoError(t, readErr)
		assert.Equal(t, []string{"a.flac", "c.flac"}, gp.Entries)
	})

	t.Run("an empty entries slice clears the entry list entirely", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, WriteGlobalPlaylist(path, &GlobalPlaylist{
			Name: "Road Trip", Entries: []string{"a.flac"},
		}))

		warning, err := SetEntries(root, path, []string{})
		require.NoError(t, err)
		assert.Empty(t, warning)

		gp, readErr := ReadGlobalPlaylist(path)
		require.NoError(t, readErr)
		assert.Empty(t, gp.Entries)
	})

	t.Run("performs no existence validation against any library root", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, WriteGlobalPlaylist(path, &GlobalPlaylist{Name: "Road Trip"}))

		// "nowhere/nope.flac" does not exist anywhere under root; SetEntries
		// must accept it anyway -- validation is the caller's job.
		warning, err := SetEntries(root, path, []string{"nowhere/nope.flac"})
		require.NoError(t, err)
		assert.Empty(t, warning)

		gp, readErr := ReadGlobalPlaylist(path)
		require.NoError(t, readErr)
		assert.Equal(t, []string{"nowhere/nope.flac"}, gp.Entries)
	})

	t.Run("preserves every other directive", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, WriteGlobalPlaylist(path, &GlobalPlaylist{
			Name: "Road Trip", NavidromeID: "id-1", HasNavidromeID: true,
			HasTargets: true, Targets: []string{"ipod"},
			Entries: []string{"a.flac"},
		}))

		warning, err := SetEntries(root, path, []string{"b.flac"})
		require.NoError(t, err)
		assert.Empty(t, warning)

		gp, readErr := ReadGlobalPlaylist(path)
		require.NoError(t, readErr)
		assert.Equal(t, "Road Trip", gp.Name)
		assert.Equal(t, "id-1", gp.NavidromeID)
		assert.True(t, gp.HasNavidromeID)
		assert.Equal(t, []string{"ipod"}, gp.Targets)
	})

	t.Run("refreshes the entry in an existing playlists/sums.md5", func(t *testing.T) {
		root := t.TempDir()
		playlistsDir := filepath.Join(root, "playlists")
		path := filepath.Join(playlistsDir, "roadtrip.m3u8")
		require.NoError(t, WriteGlobalPlaylist(path, &GlobalPlaylist{
			Name: "Road Trip", Entries: []string{"a.flac", "b.flac"},
		}))
		require.NoError(t, hasher.Hash(playlistsDir, nil))
		before, _, err := hasher.ReadSums(playlistsDir, hasher.SumsFilename)
		require.NoError(t, err)
		oldHash := before["roadtrip.m3u8"]
		require.NotEmpty(t, oldHash)

		warning, err := SetEntries(root, path, []string{"a.flac"})
		require.NoError(t, err)
		assert.Empty(t, warning)

		after, _, err := hasher.ReadSums(playlistsDir, hasher.SumsFilename)
		require.NoError(t, err)
		assert.NotEqual(t, oldHash, after["roadtrip.m3u8"])
	})

	t.Run("no playlists/sums.md5 at all: update still succeeds, no warning", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, WriteGlobalPlaylist(path, &GlobalPlaylist{
			Name: "Road Trip", Entries: []string{"a.flac"},
		}))

		warning, err := SetEntries(root, path, []string{})
		require.NoError(t, err)
		assert.Empty(t, warning)
	})
}
