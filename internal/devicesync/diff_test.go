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
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeHash returns a guaranteed-32-character hex string derived from n.
func fakeHash(n int) string {
	return fmt.Sprintf("%032x", n)
}

func TestDiff(t *testing.T) {
	t.Run("errors on an unrecognized target", func(t *testing.T) {
		_, err := Diff(t.TempDir(), "chromecast", &DesiredStateResult{}, &CurrentStateResult{})
		assert.Error(t, err)
	})

	t.Run("desired but not on device: add", func(t *testing.T) {
		root := t.TempDir()
		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/01 track.flac"}
		touch(t, filepath.Join(root, "main", "a", "artist", "album", "01 track.flac"))

		desired := &DesiredStateResult{Entries: []DesiredEntry{entry}}
		current := &CurrentStateResult{Entries: map[DesiredEntry]DeviceEntry{}}

		result, err := Diff(root, "ipod", desired, current)
		require.NoError(t, err)
		require.Len(t, result.Changes, 1)
		assert.Equal(t, ActionAdd, result.Changes[0].Action)
	})

	t.Run("on device but not desired: delete", func(t *testing.T) {
		root := t.TempDir()
		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/01 track.flac"}

		desired := &DesiredStateResult{}
		current := &CurrentStateResult{Entries: map[DesiredEntry]DeviceEntry{
			entry: {Hash: fakeHash(1)},
		}}

		result, err := Diff(root, "ipod", desired, current)
		require.NoError(t, err)
		require.Len(t, result.Changes, 1)
		assert.Equal(t, ActionDelete, result.Changes[0].Action)
	})

	t.Run("device hash matches source directly: skip (ordinary passthrough)", func(t *testing.T) {
		root := t.TempDir()
		album := filepath.Join(root, "main", "a", "artist", "album")
		touch(t, filepath.Join(album, "01 track.flac"))
		writeSums(t, album, "sums.md5", map[string]string{"01 track.flac": fakeHash(1)})

		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/01 track.flac"}
		desired := &DesiredStateResult{Entries: []DesiredEntry{entry}}
		current := &CurrentStateResult{Entries: map[DesiredEntry]DeviceEntry{
			entry: {Hash: fakeHash(1)},
		}}

		result, err := Diff(root, "ipod", desired, current)
		require.NoError(t, err)
		require.Len(t, result.Changes, 1)
		assert.Equal(t, ActionSkip, result.Changes[0].Action)
		assert.Empty(t, result.Warnings)
	})

	t.Run("device hash matches source directly even with no sidecar: skip (artwork that happened to pass through)", func(t *testing.T) {
		root := t.TempDir()
		album := filepath.Join(root, "main", "a", "artist", "album")
		touch(t, filepath.Join(album, "folder.jpg"))
		writeSums(t, album, "sums.md5", map[string]string{"folder.jpg": fakeHash(1)})

		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/folder.jpg"}
		desired := &DesiredStateResult{Entries: []DesiredEntry{entry}}
		current := &CurrentStateResult{Entries: map[DesiredEntry]DeviceEntry{
			// No sidecar at all since this artwork was a byte-identical
			// passthrough (already a small JPEG), exactly like an audio
			// passthrough file, and must be skippable the same way.
			entry: {Hash: fakeHash(1), HasSrcHash: false},
		}}

		result, err := Diff(root, "ipod", desired, current)
		require.NoError(t, err)
		require.Len(t, result.Changes, 1)
		assert.Equal(t, ActionSkip, result.Changes[0].Action,
			"a direct hash match must be enough to skip, even without a sidecar")
	})

	t.Run("device hash differs from source but the sidecar's recorded source hash matches: skip", func(t *testing.T) {
		root := t.TempDir()
		album := filepath.Join(root, "main", "a", "artist", "album")
		touch(t, filepath.Join(album, "01 track.flac"))
		writeSums(t, album, "sums.md5", map[string]string{"01 track.flac": fakeHash(1)})

		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/01 track.flac"}
		desired := &DesiredStateResult{Entries: []DesiredEntry{entry}}
		current := &CurrentStateResult{Entries: map[DesiredEntry]DeviceEntry{
			// Device hash (the transcoded/resized bytes) never matches
			// source's raw hash, only the sidecar comparison can confirm
			// this is unchanged.
			entry: {Hash: fakeHash(99), SrcHash: fakeHash(1), HasSrcHash: true},
		}}

		result, err := Diff(root, "sdcard", desired, current)
		require.NoError(t, err)
		require.Len(t, result.Changes, 1)
		assert.Equal(t, ActionSkip, result.Changes[0].Action)
	})

	t.Run("neither the device hash nor the sidecar match source: regenerate, no special warning", func(t *testing.T) {
		root := t.TempDir()
		album := filepath.Join(root, "main", "a", "artist", "album")
		touch(t, filepath.Join(album, "01 track.flac"))
		writeSums(t, album, "sums.md5", map[string]string{"01 track.flac": fakeHash(2)}) // source changed

		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/01 track.flac"}
		desired := &DesiredStateResult{Entries: []DesiredEntry{entry}}
		current := &CurrentStateResult{Entries: map[DesiredEntry]DeviceEntry{
			entry: {Hash: fakeHash(99), SrcHash: fakeHash(1), HasSrcHash: true}, // recorded for the OLD source
		}}

		result, err := Diff(root, "sdcard", desired, current)
		require.NoError(t, err)
		require.Len(t, result.Changes, 1)
		assert.Equal(t, ActionRegenerate, result.Changes[0].Action)
		assert.Empty(t, result.Warnings, "a real detected change needs no special warning")
	})

	t.Run("no sidecar and no direct hash match: regenerate, no special warning", func(t *testing.T) {
		root := t.TempDir()
		album := filepath.Join(root, "main", "a", "artist", "album")
		touch(t, filepath.Join(album, "01 track.flac"))
		writeSums(t, album, "sums.md5", map[string]string{"01 track.flac": fakeHash(1)})

		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/01 track.flac"}
		desired := &DesiredStateResult{Entries: []DesiredEntry{entry}}
		current := &CurrentStateResult{Entries: map[DesiredEntry]DeviceEntry{
			entry: {Hash: fakeHash(99), HasSrcHash: false}, // neither check can succeed
		}}

		result, err := Diff(root, "sdcard", desired, current)
		require.NoError(t, err)
		require.Len(t, result.Changes, 1)
		assert.Equal(t, ActionRegenerate, result.Changes[0].Action)
		assert.Empty(t, result.Warnings, "a missing device-side sidecar is normal, not a source-side gap")
	})

	t.Run("no source sums.md5 at all: regenerate with a warning", func(t *testing.T) {
		root := t.TempDir()
		album := filepath.Join(root, "main", "a", "artist", "album")
		touch(t, filepath.Join(album, "01 track.flac")) // no sums.md5 written

		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/01 track.flac"}
		desired := &DesiredStateResult{Entries: []DesiredEntry{entry}}
		current := &CurrentStateResult{Entries: map[DesiredEntry]DeviceEntry{
			entry: {Hash: fakeHash(1)},
		}}

		result, err := Diff(root, "ipod", desired, current)
		require.NoError(t, err)
		require.Len(t, result.Changes, 1)
		assert.Equal(t, ActionRegenerate, result.Changes[0].Action)
		require.Len(t, result.Warnings, 1)
		assert.Contains(t, result.Warnings[0], "no sums.md5 recorded")
		assert.Contains(t, result.Warnings[0], "musicrename sums")
	})

	t.Run("source sums.md5 exists but lacks this file: regenerate with a warning", func(t *testing.T) {
		root := t.TempDir()
		album := filepath.Join(root, "main", "a", "artist", "album")
		touch(t, filepath.Join(album, "01 track.flac"))
		touch(t, filepath.Join(album, "02 track.flac"))
		// sums.md5 only covers the OTHER track, not the one being checked
		writeSums(t, album, "sums.md5", map[string]string{"02 track.flac": fakeHash(2)})

		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/01 track.flac"}
		desired := &DesiredStateResult{Entries: []DesiredEntry{entry}}
		current := &CurrentStateResult{Entries: map[DesiredEntry]DeviceEntry{
			entry: {Hash: fakeHash(1)},
		}}

		result, err := Diff(root, "ipod", desired, current)
		require.NoError(t, err)
		require.Len(t, result.Changes, 1)
		assert.Equal(t, ActionRegenerate, result.Changes[0].Action)
		require.Len(t, result.Warnings, 1)
		assert.Contains(t, result.Warnings[0], "no sums.md5 recorded")
	})

	t.Run("results are sorted by (root, relative path)", func(t *testing.T) {
		root := t.TempDir()
		entryZ := DesiredEntry{Root: "main", Rel: "z/track.flac"}
		entryA := DesiredEntry{Root: "main", Rel: "a/track.flac"}
		entryChristmas := DesiredEntry{Root: "christmas", Rel: "a/track.flac"}

		desired := &DesiredStateResult{Entries: []DesiredEntry{entryZ, entryA, entryChristmas}}
		current := &CurrentStateResult{Entries: map[DesiredEntry]DeviceEntry{}}

		result, err := Diff(root, "ipod", desired, current)
		require.NoError(t, err)
		require.Len(t, result.Changes, 3)
		assert.Equal(t, "christmas", result.Changes[0].Entry.Root)
		assert.Equal(t, "main", result.Changes[1].Entry.Root)
		assert.Equal(t, "a/track.flac", result.Changes[1].Entry.Rel)
		assert.Equal(t, "main", result.Changes[2].Entry.Root)
		assert.Equal(t, "z/track.flac", result.Changes[2].Entry.Rel)
	})

	t.Run("empty desired and current produce an empty, non-error result", func(t *testing.T) {
		result, err := Diff(
			t.TempDir(), "ipod", &DesiredStateResult{}, &CurrentStateResult{Entries: map[DesiredEntry]DeviceEntry{}},
		)
		require.NoError(t, err)
		assert.Empty(t, result.Changes)
		assert.Empty(t, result.Warnings)
	})
}
