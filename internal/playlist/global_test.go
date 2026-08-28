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
)

func TestReadGlobalPlaylist(t *testing.T) {
	t.Run("missing file returns an empty, non-nil struct", func(t *testing.T) {
		dir := t.TempDir()
		gp, err := ReadGlobalPlaylist(filepath.Join(dir, "missing.m3u8"))
		require.NoError(t, err)
		assert.NotNil(t, gp)
		assert.Empty(t, gp.Name)
		assert.False(t, gp.HasNavidromeID)
		assert.False(t, gp.HasTargets)
		assert.Empty(t, gp.Entries)
	})

	t.Run("parses all directives and entries", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "roadtrip.m3u8")
		require.NoError(t, os.WriteFile(path, []byte(
			"#PLAYLIST:Road Trip\n"+
				"#NAVIDROME-ID:abc-123\n"+
				"#TARGETS:ipod,sdcard\n"+
				"\n"+
				"main/a/artist/2020 album/01 track.flac\n"+
				"christmas/b/other/2019 album/02 track.flac\n",
		), 0644))

		gp, err := ReadGlobalPlaylist(path)
		require.NoError(t, err)
		assert.Equal(t, "Road Trip", gp.Name)
		assert.Equal(t, "abc-123", gp.NavidromeID)
		assert.True(t, gp.HasNavidromeID)
		assert.Equal(t, []string{"ipod", "sdcard"}, gp.Targets)
		assert.True(t, gp.HasTargets)
		assert.Equal(t, []string{
			"main/a/artist/2020 album/01 track.flac",
			"christmas/b/other/2019 album/02 track.flac",
		}, gp.Entries)
	})

	t.Run("no directives at all: just entries", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "list.m3u8")
		require.NoError(t, os.WriteFile(path, []byte("track1.flac\ntrack2.flac\n"), 0644))

		gp, err := ReadGlobalPlaylist(path)
		require.NoError(t, err)
		assert.False(t, gp.HasNavidromeID)
		assert.False(t, gp.HasTargets)
		assert.Equal(t, []string{"track1.flac", "track2.flac"}, gp.Entries)
	})

	t.Run("present-but-empty #TARGETS: is distinct from absent", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "list.m3u8")
		require.NoError(t, os.WriteFile(path, []byte("#TARGETS:\ntrack.flac\n"), 0644))

		gp, err := ReadGlobalPlaylist(path)
		require.NoError(t, err)
		assert.True(t, gp.HasTargets)
		assert.Empty(t, gp.Targets)
		assert.NotNil(t, gp.Targets)
	})

	t.Run("unrecognized directive lines are skipped, not treated as entries", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "list.m3u8")
		require.NoError(t, os.WriteFile(path, []byte(
			"#SOME-FUTURE-DIRECTIVE:whatever\ntrack.flac\n",
		), 0644))

		gp, err := ReadGlobalPlaylist(path)
		require.NoError(t, err)
		assert.Equal(t, []string{"track.flac"}, gp.Entries)
	})
}

func TestWriteGlobalPlaylist(t *testing.T) {
	t.Run("writes directives in order, then entries", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "roadtrip.m3u8")

		err := WriteGlobalPlaylist(path, &GlobalPlaylist{
			Name:           "Road Trip",
			NavidromeID:    "abc-123",
			HasNavidromeID: true,
			Targets:        []string{"ipod", "sdcard"},
			HasTargets:     true,
			Entries:        []string{"main/a/track.flac", "christmas/b/track.flac"},
		})
		require.NoError(t, err)

		got, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t,
			"#PLAYLIST:Road Trip\n#NAVIDROME-ID:abc-123\n#TARGETS:ipod,sdcard\n"+
				"main/a/track.flac\nchristmas/b/track.flac\n",
			string(got),
		)
	})

	t.Run("omits directives that are not set", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "list.m3u8")

		require.NoError(t, WriteGlobalPlaylist(path, &GlobalPlaylist{
			Entries: []string{"track.flac"},
		}))

		got, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "track.flac\n", string(got))
	})

	t.Run("creates parent directories as needed", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "nested", "sub", "list.m3u8")

		require.NoError(t, WriteGlobalPlaylist(path, &GlobalPlaylist{Entries: []string{"track.flac"}}))
		assert.FileExists(t, path)
	})

	t.Run("round-trips through ReadGlobalPlaylist", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "list.m3u8")

		want := &GlobalPlaylist{
			Name:           "My List",
			NavidromeID:    "xyz",
			HasNavidromeID: true,
			HasTargets:     false,
			Entries:        []string{"a.flac", "b.flac"},
		}
		require.NoError(t, WriteGlobalPlaylist(path, want))

		got, err := ReadGlobalPlaylist(path)
		require.NoError(t, err)
		assert.Equal(t, want.Name, got.Name)
		assert.Equal(t, want.NavidromeID, got.NavidromeID)
		assert.Equal(t, want.HasNavidromeID, got.HasNavidromeID)
		assert.Equal(t, want.HasTargets, got.HasTargets)
		assert.Equal(t, want.Entries, got.Entries)
	})

	t.Run("overwrites existing content", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "list.m3u8")
		require.NoError(t, os.WriteFile(path, []byte("#PLAYLIST:Old\nold.flac\n"), 0644))

		require.NoError(t, WriteGlobalPlaylist(path, &GlobalPlaylist{
			Name: "New", Entries: []string{"new.flac"},
		}))

		got, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "#PLAYLIST:New\nnew.flac\n", string(got))
	})
}
