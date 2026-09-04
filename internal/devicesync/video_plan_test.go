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
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/hasher"
	"github.com/mfinelli/musicrename/internal/playlist"
	"github.com/mfinelli/musicrename/internal/target"
	"github.com/mfinelli/musicrename/internal/testutil"
)

// setupVideoFixture places a real video fixture at libraryRootRoot/videos/
// rel, writes its source sums.md5, and selects it for targetName which is
// everything VideoPlan needs to find one desired video.
func setupVideoFixture(t *testing.T, root, targetName string, width, height int) (rel, videoDir string) {
	t.Helper()

	videos := filepath.Join(root, "videos")
	rel = filepath.Join("b", "beyonce", "crazy in love", "crazy in love.mp4")
	videoDir = filepath.Dir(filepath.Join(videos, rel))
	require.NoError(t, os.MkdirAll(videoDir, 0755))

	src := testutil.MakeVideoFile(t, videoDir, "crazy in love.mp4", width, height, "libx264", "aac")
	srcHash, err := hasher.HashFile(src)
	require.NoError(t, err)
	require.NoError(t, hasher.WriteSums(videoDir, hasher.SumsFilename, map[string]string{
		"crazy in love.mp4": srcHash,
	}))
	require.NoError(t, playlist.WriteManifest(videos, targetName, []string{filepath.ToSlash(rel)}))

	return rel, videoDir
}

func TestVideoPlan(t *testing.T) {
	t.Run("errors on an unrecognized target", func(t *testing.T) {
		_, err := VideoPlan(t.TempDir(), t.TempDir(), "chromecast")
		assert.Error(t, err)
	})

	t.Run("errors on a target that doesn't support video", func(t *testing.T) {
		_, err := VideoPlan(t.TempDir(), t.TempDir(), "sdcard")
		assert.Error(t, err)
	})

	t.Run("full cycle: add, execute, then a clean skip on a second plan", func(t *testing.T) {
		root := t.TempDir()
		device := t.TempDir()
		setupVideoFixture(t, root, "ipod", 640, 360)

		plan1, err := VideoPlan(root, device, "ipod")
		require.NoError(t, err)
		require.Empty(t, plan1.Warnings)
		counts1 := CountChanges(plan1.Diff)
		require.Equal(t, 1, counts1.Add)
		require.Equal(t, 0, counts1.Skip)
		require.True(t, plan1.Capacity.Sufficient())

		result, err := Execute(context.Background(), root, device, "ipod", plan1.Diff, false, nil)
		require.NoError(t, err)
		require.Len(t, result.Created, 1)

		devicePath := filepath.Join(device, "videos", "b", "beyonce", "crazy in love", "crazy in love.mpg")
		assert.FileExists(t, devicePath)

		out := testutil.ProbeText(t, devicePath, "stream=codec_name", "nokey=1")
		assert.Contains(t, out, "mpeg2video")
		assert.Contains(t, out, "mp3")

		// Second plan, nothing changed on the source side at all: must
		// be a clean Skip, not Add/Regenerate which is the entire point
		// of reusing the {target}.src.md5 sidecar mechanism for video:
		// a transcoded file's on-device bytes never equal the source's
		// hash directly (completely different format), so without that
		// sidecar recording what source hash actually produced it,
		// this would regenerate (a full re-transcode) on every single
		// sync forever.
		plan2, err := VideoPlan(root, device, "ipod")
		require.NoError(t, err)
		counts2 := CountChanges(plan2.Diff)
		assert.Equal(t, 0, counts2.Add)
		assert.Equal(t, 0, counts2.Regenerate)
		assert.Equal(t, 1, counts2.Skip)

		// And the on-device sidecar is really what makes that possible:
		// confirm it actually exists and records the source's hash,
		// not just infer it from the Skip outcome above.
		deviceVideoDir := filepath.Dir(devicePath)
		srcSums, existed, err := hasher.ReadSums(deviceVideoDir, target.SrcSumsFilename("ipod"))
		require.NoError(t, err)
		require.True(t, existed)
		assert.NotEmpty(t, srcSums["crazy in love.mpg"])
	})

	t.Run("regenerates when the source video's content changes", func(t *testing.T) {
		root := t.TempDir()
		device := t.TempDir()
		_, videoDir := setupVideoFixture(t, root, "ipod", 640, 360)

		plan1, err := VideoPlan(root, device, "ipod")
		require.NoError(t, err)
		_, err = Execute(context.Background(), root, device, "ipod", plan1.Diff, false, nil)
		require.NoError(t, err)

		// Replace the source with different content and refresh its
		// sums.md5, simulating a re-fetch (without touching the
		// manifest at all).
		newSrc := testutil.MakeVideoFile(t, videoDir, "replacement.mp4", 320, 240, "libx264", "aac")
		require.NoError(t, os.Remove(filepath.Join(videoDir, "crazy in love.mp4")))
		require.NoError(t, os.Rename(newSrc, filepath.Join(videoDir, "crazy in love.mp4")))
		newHash, err := hasher.HashFile(filepath.Join(videoDir, "crazy in love.mp4"))
		require.NoError(t, err)
		require.NoError(t, hasher.WriteSums(videoDir, hasher.SumsFilename, map[string]string{
			"crazy in love.mp4": newHash,
		}))

		plan2, err := VideoPlan(root, device, "ipod")
		require.NoError(t, err)
		counts2 := CountChanges(plan2.Diff)
		assert.Equal(t, 1, counts2.Regenerate)
		assert.Equal(t, 0, counts2.Skip)
	})

	t.Run("deletes an on-device video no longer selected", func(t *testing.T) {
		root := t.TempDir()
		device := t.TempDir()
		videos := filepath.Join(root, "videos")
		setupVideoFixture(t, root, "ipod", 640, 360)

		plan1, err := VideoPlan(root, device, "ipod")
		require.NoError(t, err)
		_, err = Execute(context.Background(), root, device, "ipod", plan1.Diff, false, nil)
		require.NoError(t, err)

		// Deselect it entirely.
		require.NoError(t, playlist.WriteManifest(videos, "ipod", nil))

		plan2, err := VideoPlan(root, device, "ipod")
		require.NoError(t, err)
		counts2 := CountChanges(plan2.Diff)
		assert.Equal(t, 1, counts2.Delete)

		result, err := Execute(context.Background(), root, device, "ipod", plan2.Diff, false, nil)
		require.NoError(t, err)
		assert.Len(t, result.Deleted, 1)

		// The whole now-empty video directory (and its sums.md5/
		// {target}.src.md5, per Execute's existing empty-album cleanup)
		// should be gone, mirroring exactly how a deleted audio track's
		// empty album directory is cleaned up.
		assert.NoDirExists(t, filepath.Join(device, "videos", "b", "beyonce", "crazy in love"))
	})
}

func TestMergePlans(t *testing.T) {
	t.Run("nil video returns audio unchanged", func(t *testing.T) {
		audio := &PlanResult{
			Diff:     &DiffResult{Changes: []PlannedChange{{Entry: DesiredEntry{Root: "main", Rel: "a.flac"}}}},
			Capacity: &CapacityReport{NeededBytes: 100, FreedBytes: 10, AvailableBytes: 1000},
		}
		merged := MergePlans(audio, nil)
		assert.Same(t, audio, merged)
	})

	t.Run("concatenates changes and warnings from both plans", func(t *testing.T) {
		audio := &PlanResult{
			Diff: &DiffResult{
				Changes:  []PlannedChange{{Entry: DesiredEntry{Root: "main", Rel: "a.flac"}}},
				Warnings: []string{"audio diff warning"},
			},
			Capacity: &CapacityReport{},
			Warnings: []string{"audio plan warning"},
		}
		video := &PlanResult{
			Diff: &DiffResult{
				Changes:  []PlannedChange{{Entry: DesiredEntry{Root: "videos", Rel: "a.mp4"}}},
				Warnings: []string{"video diff warning"},
			},
			Capacity: &CapacityReport{},
			Warnings: []string{"video plan warning"},
		}

		merged := MergePlans(audio, video)
		require.Len(t, merged.Diff.Changes, 2)
		assert.Equal(t, "main", merged.Diff.Changes[0].Entry.Root)
		assert.Equal(t, "videos", merged.Diff.Changes[1].Entry.Root)
		assert.Equal(t, []string{"audio diff warning", "video diff warning"}, merged.Diff.Warnings)
		assert.Equal(t, []string{"audio plan warning", "video plan warning"}, merged.Warnings)
	})

	t.Run("NeededBytes and FreedBytes are summed, AvailableBytes is taken from audio only", func(t *testing.T) {
		audio := &PlanResult{
			Diff:     &DiffResult{},
			Capacity: &CapacityReport{NeededBytes: 100, FreedBytes: 10, AvailableBytes: 5000},
		}
		video := &PlanResult{
			Diff:     &DiffResult{},
			Capacity: &CapacityReport{NeededBytes: 300, FreedBytes: 20, AvailableBytes: 5000},
		}

		merged := MergePlans(audio, video)
		assert.Equal(t, int64(400), merged.Capacity.NeededBytes)
		assert.Equal(t, int64(30), merged.Capacity.FreedBytes)
		assert.Equal(t, int64(5000), merged.Capacity.AvailableBytes)
	})

	t.Run("merged Sufficient() reflects the combined totals correctly", func(t *testing.T) {
		audio := &PlanResult{
			Diff:     &DiffResult{},
			Capacity: &CapacityReport{NeededBytes: 600, FreedBytes: 0, AvailableBytes: 1000},
		}
		video := &PlanResult{
			Diff:     &DiffResult{},
			Capacity: &CapacityReport{NeededBytes: 600, FreedBytes: 0, AvailableBytes: 1000},
		}

		// Individually, either plan alone fits easily (600 <= 1000). Only
		// the combined total (1200) actually exceeds availability (the
		// exact kind of mistake summing each plan's Sufficient() result
		// independently, rather than merging first, would miss).
		merged := MergePlans(audio, video)
		assert.False(t, merged.Capacity.Sufficient())
	})
}
