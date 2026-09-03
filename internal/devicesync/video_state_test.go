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

package devicesync

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/playlist"
)

func TestVideoDesiredState(t *testing.T) {
	t.Run("errors on an unrecognized target", func(t *testing.T) {
		root := t.TempDir()
		_, err := VideoDesiredState(root, "chromecast")
		assert.Error(t, err)
	})

	t.Run("errors on a target that doesn't support video", func(t *testing.T) {
		root := t.TempDir()
		_, err := VideoDesiredState(root, "sdcard")
		assert.Error(t, err)
	})

	t.Run("no manifest yet is an empty result, not an error", func(t *testing.T) {
		root := t.TempDir()
		result, err := VideoDesiredState(root, "ipod")
		require.NoError(t, err)
		assert.Empty(t, result.Entries)
		assert.Empty(t, result.Warnings)
	})

	t.Run("includes a selected, resolvable video with Root always \"videos\"", func(t *testing.T) {
		root := t.TempDir()
		rel := filepath.Join("b", "beyonce", "crazy in love", "crazy in love.mp4")
		touch(t, filepath.Join(root, rel))
		require.NoError(t, playlist.WriteManifest(root, "ipod", []string{filepath.ToSlash(rel)}))

		result, err := VideoDesiredState(root, "ipod")
		require.NoError(t, err)
		require.Len(t, result.Entries, 1)
		assert.Equal(t, "videos", result.Entries[0].Root)
		assert.Equal(t, filepath.ToSlash(rel), result.Entries[0].Rel)
		assert.Empty(t, result.Warnings)
	})

	t.Run("a stale manifest entry is skipped with a warning, not included", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, playlist.WriteManifest(root, "ipod", []string{"b/beyonce/gone/gone.mp4"}))

		result, err := VideoDesiredState(root, "ipod")
		require.NoError(t, err)
		assert.Empty(t, result.Entries)
		require.Len(t, result.Warnings, 1)
		assert.Contains(t, result.Warnings[0], "gone.mp4")
	})

	t.Run("a manifest for a different target is not included", func(t *testing.T) {
		root := t.TempDir()
		rel := filepath.Join("b", "beyonce", "crazy in love", "crazy in love.mp4")
		touch(t, filepath.Join(root, rel))
		require.NoError(t, playlist.WriteManifest(root, "sdcard", []string{filepath.ToSlash(rel)}))

		result, err := VideoDesiredState(root, "ipod")
		require.NoError(t, err)
		assert.Empty(t, result.Entries)
	})

	t.Run("entries are sorted by Rel", func(t *testing.T) {
		root := t.TempDir()
		relB := filepath.Join("b", "beyonce", "crazy in love", "crazy in love.mp4")
		relA := filepath.Join("a", "artist", "aardvark", "aardvark.mp4")
		touch(t, filepath.Join(root, relB))
		touch(t, filepath.Join(root, relA))
		require.NoError(t, playlist.WriteManifest(root, "ipod", []string{
			filepath.ToSlash(relB), filepath.ToSlash(relA),
		}))

		result, err := VideoDesiredState(root, "ipod")
		require.NoError(t, err)
		require.Len(t, result.Entries, 2)
		assert.Equal(t, filepath.ToSlash(relA), result.Entries[0].Rel)
		assert.Equal(t, filepath.ToSlash(relB), result.Entries[1].Rel)
	})
}
