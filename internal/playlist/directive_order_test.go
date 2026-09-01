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

func TestCheckDirectiveOrder(t *testing.T) {
	t.Run("missing file: ok, no error", func(t *testing.T) {
		dir := t.TempDir()
		ok, got, want, err := CheckDirectiveOrder(filepath.Join(dir, "missing.m3u8"))
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Nil(t, got)
		assert.Nil(t, want)
	})

	t.Run("no directives at all: ok", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "list.m3u8")
		require.NoError(t, os.WriteFile(path, []byte("track.flac\n"), 0644))

		ok, _, _, err := CheckDirectiveOrder(path)
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("a single directive alone is always ok", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "list.m3u8")
		require.NoError(t, os.WriteFile(path, []byte("#TARGETS:ipod\ntrack.flac\n"), 0644))

		ok, _, _, err := CheckDirectiveOrder(path)
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("all four, canonical order: ok", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "list.m3u8")
		require.NoError(t, os.WriteFile(path, []byte(
			"#PLAYLIST:Road Trip\n#NAVIDROME-ID:id-1\n#TARGETS:ipod\n#SORT:artist\ntrack.flac\n",
		), 0644))

		ok, got, want, err := CheckDirectiveOrder(path)
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Nil(t, got)
		assert.Nil(t, want)
	})

	t.Run("a subset present in canonical relative order: ok", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "list.m3u8")
		// No #NAVIDROME-ID: at all -- #PLAYLIST: then #TARGETS: then
		// #SORT: is still the correct relative order for what's present.
		require.NoError(t, os.WriteFile(path, []byte(
			"#PLAYLIST:Road Trip\n#TARGETS:ipod\n#SORT:artist\ntrack.flac\n",
		), 0644))

		ok, _, _, err := CheckDirectiveOrder(path)
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("out of order: not ok, reports both actual and expected", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "list.m3u8")
		require.NoError(t, os.WriteFile(path, []byte(
			"#TARGETS:ipod\n#PLAYLIST:Road Trip\ntrack.flac\n",
		), 0644))

		ok, got, want, err := CheckDirectiveOrder(path)
		require.NoError(t, err)
		assert.False(t, ok)
		assert.Equal(t, []string{targetsPrefix, playlistNamePrefix}, got)
		assert.Equal(t, []string{playlistNamePrefix, targetsPrefix}, want)
	})

	t.Run("#SORT: before #TARGETS: is out of order", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "list.m3u8")
		require.NoError(t, os.WriteFile(path, []byte(
			"#SORT:artist\n#TARGETS:ipod\ntrack.flac\n",
		), 0644))

		ok, got, want, err := CheckDirectiveOrder(path)
		require.NoError(t, err)
		assert.False(t, ok)
		assert.Equal(t, []string{sortPrefix, targetsPrefix}, got)
		assert.Equal(t, []string{targetsPrefix, sortPrefix}, want)
	})

	t.Run("#NAVIDROME-ID: before #PLAYLIST: is out of order", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "list.m3u8")
		require.NoError(t, os.WriteFile(path, []byte(
			"#NAVIDROME-ID:id-1\n#PLAYLIST:Road Trip\ntrack.flac\n",
		), 0644))

		ok, _, _, err := CheckDirectiveOrder(path)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("a duplicate directive's position reflects only its first occurrence", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "list.m3u8")
		// #PLAYLIST: appears twice, but always first -- order is still
		// canonical; DuplicateDirectives is the one that flags the
		// repetition itself, not this function.
		require.NoError(t, os.WriteFile(path, []byte(
			"#PLAYLIST:First\n#TARGETS:ipod\n#PLAYLIST:Second\ntrack.flac\n",
		), 0644))

		ok, _, _, err := CheckDirectiveOrder(path)
		require.NoError(t, err)
		assert.True(t, ok)
	})
}
