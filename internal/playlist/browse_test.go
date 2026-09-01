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

func TestListSubdirectories(t *testing.T) {
	t.Run("lists direct subdirectories only, not files", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "artist-a"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "artist-b"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "notadir.flac"), []byte("x"), 0644))

		names, err := ListSubdirectories(dir)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"artist-a", "artist-b"}, names)
	})

	t.Run("excludes dotfiles/dot-directories", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "artist-a"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".hidden"), 0755))

		names, err := ListSubdirectories(dir)
		require.NoError(t, err)
		assert.Equal(t, []string{"artist-a"}, names)
	})

	t.Run("does not recurse into subdirectories", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "artist-a", "album-1"), 0755))

		names, err := ListSubdirectories(dir)
		require.NoError(t, err)
		assert.Equal(t, []string{"artist-a"}, names)
	})

	t.Run("an empty directory returns no names, no error", func(t *testing.T) {
		dir := t.TempDir()
		names, err := ListSubdirectories(dir)
		require.NoError(t, err)
		assert.Empty(t, names)
	})

	t.Run("errors on a nonexistent directory", func(t *testing.T) {
		dir := t.TempDir()
		_, err := ListSubdirectories(filepath.Join(dir, "nope"))
		assert.Error(t, err)
	})
}

func TestFilterNames(t *testing.T) {
	names := []string{"The Beatles", "Pink Floyd", "ABBA", "the rolling stones"}

	t.Run("empty query returns the exact same slice, unchanged", func(t *testing.T) {
		got := FilterNames(names, "")
		assert.Equal(t, names, got)
	})

	t.Run("case-insensitive substring match", func(t *testing.T) {
		got := FilterNames(names, "the")
		assert.Equal(t, []string{"The Beatles", "the rolling stones"}, got)
	})

	t.Run("matches anywhere in the name, not just a prefix", func(t *testing.T) {
		got := FilterNames(names, "floyd")
		assert.Equal(t, []string{"Pink Floyd"}, got)
	})

	t.Run("no matches returns an empty, non-nil slice", func(t *testing.T) {
		got := FilterNames(names, "zzz")
		assert.NotNil(t, got)
		assert.Empty(t, got)
	})

	t.Run("preserves original relative order", func(t *testing.T) {
		got := FilterNames(names, "a")
		assert.Equal(t, []string{"The Beatles", "ABBA"}, got)
	})
}
