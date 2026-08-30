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

func TestReadEntries(t *testing.T) {
	t.Run("returns empty, non-nil slice when the file does not exist", func(t *testing.T) {
		dir := t.TempDir()
		entries, err := ReadEntries(filepath.Join(dir, "roadtrip.m3u8"))
		require.NoError(t, err)
		assert.NotNil(t, entries)
		assert.Empty(t, entries)
	})

	t.Run("returns plain entries, skipping directives and blank lines", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "roadtrip.m3u8")
		require.NoError(t, os.WriteFile(path, []byte(
			"#PLAYLIST:Road Trip\n"+
				"#NAVIDROME-ID:abc123\n"+
				"\n"+
				"main/a/artist/2020 album/01 track.flac\n"+
				"christmas/b/other/2019 album/02 track.flac\n",
		), 0644))

		entries, err := ReadEntries(path)
		require.NoError(t, err)
		assert.Equal(t, []string{
			"main/a/artist/2020 album/01 track.flac",
			"christmas/b/other/2019 album/02 track.flac",
		}, entries)
	})

	t.Run("preserves order", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "list.m3u8")
		require.NoError(t, os.WriteFile(path, []byte("c\na\nb\n"), 0644))

		entries, err := ReadEntries(path)
		require.NoError(t, err)
		assert.Equal(t, []string{"c", "a", "b"}, entries)
	})
}

func TestReadNavidromeID(t *testing.T) {
	t.Run("returns not-ok when the file does not exist", func(t *testing.T) {
		dir := t.TempDir()
		id, ok, err := ReadNavidromeID(filepath.Join(dir, "missing.m3u8"))
		require.NoError(t, err)
		assert.False(t, ok)
		assert.Empty(t, id)
	})

	t.Run("returns not-ok when the file has no directive", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "list.m3u8")
		require.NoError(t, os.WriteFile(path, []byte("#PLAYLIST:Road Trip\ntrack.flac\n"), 0644))

		id, ok, err := ReadNavidromeID(path)
		require.NoError(t, err)
		assert.False(t, ok)
		assert.Empty(t, id)
	})

	t.Run("extracts the id, trimming surrounding whitespace", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "list.m3u8")
		require.NoError(t, os.WriteFile(path, []byte(
			"#PLAYLIST:Road Trip\n#NAVIDROME-ID: abc-123 \ntrack.flac\n",
		), 0644))

		id, ok, err := ReadNavidromeID(path)
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, "abc-123", id)
	})
}

func TestReadTargets(t *testing.T) {
	t.Run("returns not-ok when the file does not exist", func(t *testing.T) {
		dir := t.TempDir()
		names, ok, err := ReadTargets(filepath.Join(dir, "missing.m3u8"))
		require.NoError(t, err)
		assert.False(t, ok)
		assert.Nil(t, names)
	})

	t.Run("returns not-ok when the file has no directive: applies to every target", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "list.m3u8")
		require.NoError(t, os.WriteFile(path, []byte("#PLAYLIST:Road Trip\ntrack.flac\n"), 0644))

		names, ok, err := ReadTargets(path)
		require.NoError(t, err)
		assert.False(t, ok)
		assert.Nil(t, names)
	})

	t.Run("splits a comma-separated list, trimming whitespace", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "list.m3u8")
		require.NoError(t, os.WriteFile(path, []byte("#TARGETS: ipod, sdcard \ntrack.flac\n"), 0644))

		names, ok, err := ReadTargets(path)
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, []string{"ipod", "sdcard"}, names)
	})

	t.Run("single target", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "list.m3u8")
		require.NoError(t, os.WriteFile(path, []byte("#TARGETS:ipod\ntrack.flac\n"), 0644))

		names, ok, err := ReadTargets(path)
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, []string{"ipod"}, names)
	})

	t.Run("present but empty: explicit empty list, distinct from absent", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "list.m3u8")
		require.NoError(t, os.WriteFile(path, []byte("#TARGETS:\ntrack.flac\n"), 0644))

		names, ok, err := ReadTargets(path)
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Empty(t, names)
		assert.NotNil(t, names)
	})

	t.Run("unrecognized target names are returned as-is; validation is the caller's job", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "list.m3u8")
		require.NoError(t, os.WriteFile(path, []byte("#TARGETS:ipod,xbox\ntrack.flac\n"), 0644))

		names, ok, err := ReadTargets(path)
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, []string{"ipod", "xbox"}, names)
	})
}
