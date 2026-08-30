//go:build linux || darwin

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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckCapacity(t *testing.T) {
	t.Run("sums source file sizes for add and regenerate entries", func(t *testing.T) {
		root := t.TempDir()
		device := t.TempDir()

		addPath := filepath.Join(root, "main", "a", "artist", "album", "01 track.flac")
		regenPath := filepath.Join(root, "main", "b", "artist", "album", "02 track.flac")
		require.NoError(t, os.MkdirAll(filepath.Dir(addPath), 0755))
		require.NoError(t, os.MkdirAll(filepath.Dir(regenPath), 0755))
		require.NoError(t, os.WriteFile(addPath, make([]byte, 1000), 0644))
		require.NoError(t, os.WriteFile(regenPath, make([]byte, 2000), 0644))

		diff := &DiffResult{Changes: []PlannedChange{
			{Entry: DesiredEntry{Root: "main", Rel: "a/artist/album/01 track.flac"}, Action: ActionAdd},
			{Entry: DesiredEntry{Root: "main", Rel: "b/artist/album/02 track.flac"}, Action: ActionRegenerate},
			{Entry: DesiredEntry{Root: "main", Rel: "c/artist/album/03 track.flac"}, Action: ActionSkip},
		}}
		current := &CurrentStateResult{Entries: map[DesiredEntry]DeviceEntry{}}

		report, err := CheckCapacity(root, device, diff, current)
		require.NoError(t, err)
		assert.EqualValues(t, 3000, report.NeededBytes)
		assert.EqualValues(t, 0, report.FreedBytes)
		assert.Greater(t, report.AvailableBytes, int64(0))
	})

	t.Run("sums on-device sizes for delete entries, from CurrentState, not a fresh stat", func(t *testing.T) {
		root := t.TempDir()
		device := t.TempDir()

		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/01 track.flac"}
		diff := &DiffResult{Changes: []PlannedChange{
			{Entry: entry, Action: ActionDelete},
		}}
		current := &CurrentStateResult{Entries: map[DesiredEntry]DeviceEntry{
			entry: {Size: 5000},
		}}

		report, err := CheckCapacity(root, device, diff, current)
		require.NoError(t, err)
		assert.EqualValues(t, 0, report.NeededBytes)
		assert.EqualValues(t, 5000, report.FreedBytes)
	})

	t.Run("a source file that can no longer be stat'd is silently excluded, not an error", func(t *testing.T) {
		root := t.TempDir()
		device := t.TempDir()

		diff := &DiffResult{Changes: []PlannedChange{
			{Entry: DesiredEntry{Root: "main", Rel: "a/artist/album/missing.flac"}, Action: ActionAdd},
		}}
		current := &CurrentStateResult{Entries: map[DesiredEntry]DeviceEntry{}}

		report, err := CheckCapacity(root, device, diff, current)
		require.NoError(t, err)
		assert.EqualValues(t, 0, report.NeededBytes)
	})

	t.Run("errors when the device path itself cannot be statfs'd", func(t *testing.T) {
		root := t.TempDir()
		_, err := CheckCapacity(
			root, filepath.Join(root, "no", "such", "mount"),
			&DiffResult{}, &CurrentStateResult{Entries: map[DesiredEntry]DeviceEntry{}},
		)
		assert.Error(t, err)
	})

	t.Run("empty diff produces a zeroed, non-error report", func(t *testing.T) {
		device := t.TempDir()
		report, err := CheckCapacity(
			t.TempDir(), device, &DiffResult{}, &CurrentStateResult{Entries: map[DesiredEntry]DeviceEntry{}},
		)
		require.NoError(t, err)
		assert.EqualValues(t, 0, report.NeededBytes)
		assert.EqualValues(t, 0, report.FreedBytes)
		assert.Greater(t, report.AvailableBytes, int64(0))
	})
}

func TestCapacityReportSufficient(t *testing.T) {
	t.Run("enough available space alone is sufficient", func(t *testing.T) {
		r := CapacityReport{NeededBytes: 100, AvailableBytes: 200}
		assert.True(t, r.Sufficient())
	})

	t.Run("not enough available, but freed space makes up the difference", func(t *testing.T) {
		r := CapacityReport{NeededBytes: 100, AvailableBytes: 50, FreedBytes: 50}
		assert.True(t, r.Sufficient())
	})

	t.Run("neither available nor freed space is enough", func(t *testing.T) {
		r := CapacityReport{NeededBytes: 200, AvailableBytes: 50, FreedBytes: 50}
		assert.False(t, r.Sufficient())
	})

	t.Run("exactly enough is sufficient", func(t *testing.T) {
		r := CapacityReport{NeededBytes: 100, AvailableBytes: 60, FreedBytes: 40}
		assert.True(t, r.Sufficient())
	})

	t.Run("zero-value report needing nothing is trivially sufficient", func(t *testing.T) {
		assert.True(t, CapacityReport{}.Sufficient())
	})
}
