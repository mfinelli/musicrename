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

func touchAddEntriesFile(t *testing.T, root, relPath string) {
	t.Helper()
	full := filepath.Join(root, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
	require.NoError(t, os.WriteFile(full, []byte("audio"), 0644))
}

func TestAddEntries(t *testing.T) {
	t.Run("errors when the file does not exist", func(t *testing.T) {
		root := t.TempDir()
		_, _, err := AddEntries(root, filepath.Join(root, "playlists", "missing.m3u8"), []string{"track.flac"})
		assert.Error(t, err)
	})

	t.Run("appends resolving entries in order, preserving existing ones", func(t *testing.T) {
		root := t.TempDir()
		touchAddEntriesFile(t, root, "main/a/artist/album/02 second.flac")
		touchAddEntriesFile(t, root, "main/a/artist/album/03 third.flac")

		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, WriteGlobalPlaylist(path, &GlobalPlaylist{
			Name:    "Road Trip",
			Entries: []string{"main/a/artist/album/01 first.flac"},
		}))

		added, warnings, err := AddEntries(root, path, []string{
			"main/a/artist/album/02 second.flac",
			"main/a/artist/album/03 third.flac",
		})
		require.NoError(t, err)
		assert.Empty(t, warnings)
		assert.Equal(t, []string{
			"main/a/artist/album/02 second.flac",
			"main/a/artist/album/03 third.flac",
		}, added)

		gp, readErr := ReadGlobalPlaylist(path)
		require.NoError(t, readErr)
		assert.Equal(t, []string{
			"main/a/artist/album/01 first.flac",
			"main/a/artist/album/02 second.flac",
			"main/a/artist/album/03 third.flac",
		}, gp.Entries)
	})

	t.Run("preserves every other directive", func(t *testing.T) {
		root := t.TempDir()
		touchAddEntriesFile(t, root, "main/a/artist/album/01 track.flac")

		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, WriteGlobalPlaylist(path, &GlobalPlaylist{
			Name: "Road Trip", NavidromeID: "id-1", HasNavidromeID: true,
			HasTargets: true, Targets: []string{"ipod"},
		}))

		_, _, err := AddEntries(root, path, []string{"main/a/artist/album/01 track.flac"})
		require.NoError(t, err)

		gp, readErr := ReadGlobalPlaylist(path)
		require.NoError(t, readErr)
		assert.Equal(t, "Road Trip", gp.Name)
		assert.Equal(t, "id-1", gp.NavidromeID)
		assert.Equal(t, []string{"ipod"}, gp.Targets)
	})

	t.Run("a non-resolving path is skipped with a warning, others still processed", func(t *testing.T) {
		root := t.TempDir()
		touchAddEntriesFile(t, root, "main/a/artist/album/01 real.flac")

		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, WriteGlobalPlaylist(path, &GlobalPlaylist{Name: "Road Trip"}))

		added, warnings, err := AddEntries(root, path, []string{
			"main/a/artist/album/does-not-exist.flac",
			"main/a/artist/album/01 real.flac",
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"main/a/artist/album/01 real.flac"}, added)
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "does-not-exist.flac")
		assert.Contains(t, warnings[0], "skipped")

		gp, readErr := ReadGlobalPlaylist(path)
		require.NoError(t, readErr)
		assert.Equal(t, []string{"main/a/artist/album/01 real.flac"}, gp.Entries)
	})

	t.Run("every path invalid: no write happens at all", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, WriteGlobalPlaylist(path, &GlobalPlaylist{Name: "Road Trip"}))
		before, statErr := os.ReadFile(path)
		require.NoError(t, statErr)

		added, warnings, err := AddEntries(root, path, []string{"nope.flac"})
		require.NoError(t, err)
		assert.Empty(t, added)
		require.Len(t, warnings, 1)

		after, statErr := os.ReadFile(path)
		require.NoError(t, statErr)
		assert.Equal(t, before, after, "the file must not have been rewritten")
	})

	t.Run("refreshes the entry in an existing playlists/sums.md5", func(t *testing.T) {
		root := t.TempDir()
		playlistsDir := filepath.Join(root, "playlists")
		touchAddEntriesFile(t, root, "main/a/artist/album/01 track.flac")

		path := filepath.Join(playlistsDir, "roadtrip.m3u8")
		require.NoError(t, WriteGlobalPlaylist(path, &GlobalPlaylist{Name: "Road Trip"}))
		require.NoError(t, hasher.Hash(playlistsDir, nil))
		before, _, err := hasher.ReadSums(playlistsDir, hasher.SumsFilename)
		require.NoError(t, err)
		oldHash := before["roadtrip.m3u8"]
		require.NotEmpty(t, oldHash)

		_, warnings, err := AddEntries(root, path, []string{"main/a/artist/album/01 track.flac"})
		require.NoError(t, err)
		assert.Empty(t, warnings)

		after, _, err := hasher.ReadSums(playlistsDir, hasher.SumsFilename)
		require.NoError(t, err)
		assert.NotEqual(t, oldHash, after["roadtrip.m3u8"])
	})

	t.Run("no playlists/sums.md5 at all: add still succeeds, no warning", func(t *testing.T) {
		root := t.TempDir()
		touchAddEntriesFile(t, root, "main/a/artist/album/01 track.flac")

		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, WriteGlobalPlaylist(path, &GlobalPlaylist{Name: "Road Trip"}))

		_, warnings, err := AddEntries(root, path, []string{"main/a/artist/album/01 track.flac"})
		require.NoError(t, err)
		assert.Empty(t, warnings)
	})
}
