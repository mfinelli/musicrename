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

func TestManifestFilename(t *testing.T) {
	assert.Equal(t, "ipod.m3u8", ManifestFilename("ipod"))
	assert.Equal(t, "sdcard.m3u8", ManifestFilename("sdcard"))
}

func TestReadManifest(t *testing.T) {
	t.Run("returns empty, non-nil slice when the manifest does not exist", func(t *testing.T) {
		dir := t.TempDir()
		names, err := ReadManifest(dir, "ipod")
		require.NoError(t, err)
		assert.NotNil(t, names)
		assert.Empty(t, names)
	})

	t.Run("returns each non-blank line", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "ipod.m3u8"),
			[]byte("01 track one.flac\n02 track two.flac\n"),
			0644,
		))

		names, err := ReadManifest(dir, "ipod")
		require.NoError(t, err)
		assert.Equal(t, []string{"01 track one.flac", "02 track two.flac"}, names)
	})

	t.Run("skips blank lines and comment lines", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "ipod.m3u8"),
			[]byte("01 track one.flac\n\n# a comment\n02 track two.flac\n"),
			0644,
		))

		names, err := ReadManifest(dir, "ipod")
		require.NoError(t, err)
		assert.Equal(t, []string{"01 track one.flac", "02 track two.flac"}, names)
	})

	t.Run("different targets read independent files", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "ipod.m3u8"), []byte("01 track.flac\n"), 0644,
		))

		ipodNames, err := ReadManifest(dir, "ipod")
		require.NoError(t, err)
		assert.Equal(t, []string{"01 track.flac"}, ipodNames)

		sdcardNames, err := ReadManifest(dir, "sdcard")
		require.NoError(t, err)
		assert.Empty(t, sdcardNames)
	})
}

func TestWriteManifest(t *testing.T) {
	t.Run("writes one filename per line in the given order", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, WriteManifest(dir, "ipod", []string{"02 b.flac", "01 a.flac"}))

		got, err := os.ReadFile(filepath.Join(dir, "ipod.m3u8"))
		require.NoError(t, err)
		assert.Equal(t, "02 b.flac\n01 a.flac\n", string(got))
	})

	t.Run("deletes an existing manifest when given an empty selection", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, WriteManifest(dir, "ipod", []string{"01 track.flac"}))

		require.NoError(t, WriteManifest(dir, "ipod", []string{}))

		_, err := os.Stat(filepath.Join(dir, "ipod.m3u8"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("empty selection with no existing manifest is a no-op, not an error", func(t *testing.T) {
		dir := t.TempDir()
		assert.NoError(t, WriteManifest(dir, "ipod", []string{}))
	})

	t.Run("round-trips through ReadManifest", func(t *testing.T) {
		dir := t.TempDir()
		want := []string{"01 track one.flac", "02 track two.flac", "03 track three.flac"}
		require.NoError(t, WriteManifest(dir, "sdcard", want))

		got, err := ReadManifest(dir, "sdcard")
		require.NoError(t, err)
		assert.Equal(t, want, got)
	})
}
