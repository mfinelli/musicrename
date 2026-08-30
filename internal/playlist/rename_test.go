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
)

func writePlaylistFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}

func TestPlanRenames(t *testing.T) {
	t.Run("no playlists/ directory produces no ops or skips", func(t *testing.T) {
		root := t.TempDir()
		ops, skipped, err := PlanRenames(root)
		require.NoError(t, err)
		assert.Empty(t, ops)
		assert.Empty(t, skipped)
	})

	t.Run("sanitizes name and renames the file", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "old-name.m3u8")
		writePlaylistFile(t, path, "#PLAYLIST:Road Trip! 2020\nmain/a/artist/album/01 t.flac\n")

		ops, skipped, err := PlanRenames(root)
		require.NoError(t, err)
		require.Empty(t, skipped)
		require.Len(t, ops, 1)
		assert.Equal(t, path, ops[0].OldPath)
		assert.Equal(t, filepath.Join(root, "playlists", "road trip 2020.m3u8"), ops[0].NewPath)
	})

	t.Run("truncates the sanitized name to 40 characters", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "long.m3u8")
		longName := strings.Repeat("a", 60)
		writePlaylistFile(t, path, "#PLAYLIST:"+longName+"\n")

		ops, _, err := PlanRenames(root)
		require.NoError(t, err)
		require.Len(t, ops, 1)
		gotStem := strings.TrimSuffix(filepath.Base(ops[0].NewPath), ".m3u8")
		assert.Len(t, gotStem, 40)
		assert.Equal(t, strings.Repeat("a", 40), gotStem)
	})

	t.Run("already correctly named file produces no op", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "road trip.m3u8")
		writePlaylistFile(t, path, "#PLAYLIST:Road Trip\n")

		ops, skipped, err := PlanRenames(root)
		require.NoError(t, err)
		assert.Empty(t, ops)
		assert.Empty(t, skipped)
	})

	t.Run("target-specific subdirectory files are renamed in place", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "ipod", "old.m3u8")
		writePlaylistFile(t, path, "#PLAYLIST:Workout Mix\n")

		ops, _, err := PlanRenames(root)
		require.NoError(t, err)
		require.Len(t, ops, 1)
		assert.Equal(t, filepath.Join(root, "playlists", "ipod", "workout mix.m3u8"), ops[0].NewPath)
	})

	t.Run("missing #PLAYLIST directive is skipped, not an error", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "no-directive.m3u8")
		writePlaylistFile(t, path, "main/a/artist/album/01 t.flac\n")

		ops, skipped, err := PlanRenames(root)
		require.NoError(t, err)
		assert.Empty(t, ops)
		require.Len(t, skipped, 1)
		assert.Equal(t, path, skipped[0].Path)
		assert.Contains(t, skipped[0].Message, "no #PLAYLIST: directive")
	})

	t.Run("name that sanitizes to empty string is skipped, not an error", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "symbols.m3u8")
		writePlaylistFile(t, path, "#PLAYLIST:!!!\n")

		ops, skipped, err := PlanRenames(root)
		require.NoError(t, err)
		assert.Empty(t, ops)
		require.Len(t, skipped, 1)
		assert.Equal(t, path, skipped[0].Path)
		assert.Contains(t, skipped[0].Message, "sanitizes to an empty string")
	})

	t.Run("two files sanitizing to the same name is a collision error", func(t *testing.T) {
		root := t.TempDir()
		writePlaylistFile(t, filepath.Join(root, "playlists", "a.m3u8"), "#PLAYLIST:Road Trip\n")
		writePlaylistFile(t, filepath.Join(root, "playlists", "b.m3u8"), "#PLAYLIST:Road Trip!\n")

		ops, skipped, err := PlanRenames(root)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "collision detected")
		assert.Nil(t, ops)
		assert.Nil(t, skipped)
	})

	t.Run("results are sorted deterministically", func(t *testing.T) {
		root := t.TempDir()
		writePlaylistFile(t, filepath.Join(root, "playlists", "zzz.m3u8"), "#PLAYLIST:Zebra\n")
		writePlaylistFile(t, filepath.Join(root, "playlists", "aaa.m3u8"), "#PLAYLIST:Apple\n")
		writePlaylistFile(t, filepath.Join(root, "playlists", "no-directive-z.m3u8"), "\n")
		writePlaylistFile(t, filepath.Join(root, "playlists", "no-directive-a.m3u8"), "\n")

		ops, skipped, err := PlanRenames(root)
		require.NoError(t, err)
		require.Len(t, ops, 2)
		assert.True(t, ops[0].OldPath < ops[1].OldPath)
		require.Len(t, skipped, 2)
		assert.True(t, skipped[0].Path < skipped[1].Path)
	})
}

func TestExecuteRenames(t *testing.T) {
	t.Run("performs the rename", func(t *testing.T) {
		root := t.TempDir()
		oldPath := filepath.Join(root, "old.m3u8")
		newPath := filepath.Join(root, "new.m3u8")
		writePlaylistFile(t, oldPath, "#PLAYLIST:New\n")

		warnings, err := ExecuteRenames([]RenameOp{{OldPath: oldPath, NewPath: newPath}})
		require.NoError(t, err)
		assert.Empty(t, warnings)
		assert.NoFileExists(t, oldPath)
		assert.FileExists(t, newPath)
	})

	t.Run("race condition at destination is skipped with a warning, not an error", func(t *testing.T) {
		root := t.TempDir()
		oldPath := filepath.Join(root, "old.m3u8")
		newPath := filepath.Join(root, "new.m3u8")
		writePlaylistFile(t, oldPath, "#PLAYLIST:New\n")
		writePlaylistFile(t, newPath, "#PLAYLIST:Already here\n")

		warnings, err := ExecuteRenames([]RenameOp{{OldPath: oldPath, NewPath: newPath}})
		require.NoError(t, err)
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "race condition")
		// The source is left untouched since the rename was skipped.
		assert.FileExists(t, oldPath)
	})

	t.Run("a later op still runs after an earlier race-condition skip", func(t *testing.T) {
		root := t.TempDir()
		skippedOld := filepath.Join(root, "skipped-old.m3u8")
		skippedNew := filepath.Join(root, "skipped-new.m3u8")
		okOld := filepath.Join(root, "ok-old.m3u8")
		okNew := filepath.Join(root, "ok-new.m3u8")
		writePlaylistFile(t, skippedOld, "#PLAYLIST:Skipped\n")
		writePlaylistFile(t, skippedNew, "#PLAYLIST:Already here\n")
		writePlaylistFile(t, okOld, "#PLAYLIST:OK\n")

		warnings, err := ExecuteRenames([]RenameOp{
			{OldPath: skippedOld, NewPath: skippedNew},
			{OldPath: okOld, NewPath: okNew},
		})
		require.NoError(t, err)
		require.Len(t, warnings, 1)
		assert.FileExists(t, skippedOld)
		assert.NoFileExists(t, okOld)
		assert.FileExists(t, okNew)
	})
}
