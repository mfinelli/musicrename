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

	"github.com/mfinelli/musicrename/internal/hasher"
	"github.com/mfinelli/musicrename/internal/playlist"
)

func TestPlan(t *testing.T) {
	t.Run("errors on an unrecognized target", func(t *testing.T) {
		_, err := Plan(t.TempDir(), t.TempDir(), "chromecast")
		assert.Error(t, err)
	})

	t.Run("an empty library and empty device produce an empty, non-error plan", func(t *testing.T) {
		result, err := Plan(t.TempDir(), t.TempDir(), "ipod")
		require.NoError(t, err)
		assert.Empty(t, result.Diff.Changes)
		assert.Empty(t, result.Warnings)
		assert.True(t, result.Capacity.Sufficient())
	})

	t.Run("aggregates warnings from every planning stage", func(t *testing.T) {
		root := t.TempDir()
		device := t.TempDir()
		album := filepath.Join(root, "main", "a", "artist", "album")
		touch(t, filepath.Join(album, "01 track.flac"))
		require.NoError(t, playlist.WriteManifest(album, "ipod", []string{"01 track.flac"}))
		// No source sums.md5 at all but the entry also needs to already
		// be on-device, or Diff never even reaches the "is there a source
		// hash to compare against" check at all (a brand-new add doesn't
		// need one). Put a matching device-side file + sums.md5 in place
		// so Diff's warning path is the one actually exercised.
		deviceAlbum := filepath.Join(device, "main", "a", "artist", "album")
		touch(t, filepath.Join(deviceAlbum, "01 track.flac"))
		require.NoError(t, hasher.WriteSums(deviceAlbum, hasher.SumsFilename, map[string]string{
			"01 track.flac": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}))

		result, err := Plan(root, device, "ipod")
		require.NoError(t, err)
		require.NotEmpty(t, result.Warnings, "Diff's own warning must flow through Plan's aggregation")
	})

	t.Run("reflects a real end-to-end add scenario in Diff and Capacity together", func(t *testing.T) {
		root := t.TempDir()
		device := t.TempDir()
		album := filepath.Join(root, "main", "a", "artist", "album")
		touch(t, filepath.Join(album, "01 track.flac"))
		require.NoError(t, hasher.WriteSums(album, hasher.SumsFilename, map[string]string{
			"01 track.flac": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}))
		require.NoError(t, playlist.WriteManifest(album, "ipod", []string{"01 track.flac"}))

		result, err := Plan(root, device, "ipod")
		require.NoError(t, err)
		require.Len(t, result.Diff.Changes, 1)
		assert.Equal(t, ActionAdd, result.Diff.Changes[0].Action)
		assert.Greater(t, result.Capacity.NeededBytes, int64(0))
	})
}

func TestCountChanges(t *testing.T) {
	t.Run("tallies every action correctly", func(t *testing.T) {
		diff := &DiffResult{Changes: []PlannedChange{
			{Action: ActionAdd}, {Action: ActionAdd},
			{Action: ActionRegenerate},
			{Action: ActionDelete},
			{Action: ActionSkip}, {Action: ActionSkip}, {Action: ActionSkip},
		}}
		c := CountChanges(diff)
		assert.Equal(t, ChangeCounts{Add: 2, Regenerate: 1, Delete: 1, Skip: 3}, c)
		assert.Equal(t, 7, c.Total())
	})

	t.Run("empty diff produces a zeroed count", func(t *testing.T) {
		c := CountChanges(&DiffResult{})
		assert.Equal(t, ChangeCounts{}, c)
		assert.Equal(t, 0, c.Total())
	})
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{5, "5 B"},
		{999, "999 B"},
		{1000, "1.0 kB"},
		{1500, "1.5 kB"},
		{1000000, "1.0 MB"},
		{1500000, "1.5 MB"},
		{1000000000, "1.0 GB"},
		{1500000000, "1.5 GB"},
		{1099511627776, "1.1 TB"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, FormatBytes(tt.in), "FormatBytes(%d)", tt.in)
	}
}
