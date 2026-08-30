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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/hasher"
)

// writeSums writes a hand-built sums.md5 (or {target}.src.md5, same
// format) file at dir/filename from a map of name -> hash, using the
// binary ("*") separator throughout which is sufficient for these tests,
// which only care about the (name, hash) pairs hasher.ReadSums extracts.
func writeSums(t *testing.T, dir, filename string, entries map[string]string) {
	t.Helper()
	var sb strings.Builder
	for name, hash := range entries {
		sb.WriteString(hash)
		sb.WriteString(" *")
		sb.WriteString(name)
		sb.WriteString("\n")
	}
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, filename), []byte(sb.String()), 0644))
}

func TestCurrentState(t *testing.T) {
	t.Run("errors on an unrecognized target", func(t *testing.T) {
		_, err := CurrentState(t.TempDir(), "chromecast")
		assert.Error(t, err)
	})

	t.Run("a device path that does not exist yet is not an error", func(t *testing.T) {
		result, err := CurrentState(filepath.Join(t.TempDir(), "missing"), "ipod")
		require.NoError(t, err)
		assert.Empty(t, result.Entries)
		assert.Empty(t, result.Warnings)
	})

	t.Run("an empty device (no roots synced yet) is not an error", func(t *testing.T) {
		device := t.TempDir()
		result, err := CurrentState(device, "ipod")
		require.NoError(t, err)
		assert.Empty(t, result.Entries)
	})

	t.Run("reads a passthrough album's sums.md5 into the result, keyed by (root, rel)", func(t *testing.T) {
		device := t.TempDir()
		album := filepath.Join(device, "main", "b", "beyonce", "2003 album")
		writeSums(t, album, hasher.SumsFilename, map[string]string{
			"01 track.flac": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		})

		result, err := CurrentState(device, "ipod")
		require.NoError(t, err)

		entry := DesiredEntry{Root: "main", Rel: "b/beyonce/2003 album/01 track.flac"}
		require.Contains(t, result.Entries, entry)
		got := result.Entries[entry]
		assert.Equal(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", got.Hash)
		assert.False(t, got.HasSrcHash, "a passthrough file must have no sidecar entry")
	})

	t.Run("populates Size from the actual on-device file, not the sums.md5 entry", func(t *testing.T) {
		device := t.TempDir()
		album := filepath.Join(device, "main", "a", "artist", "album")
		require.NoError(t, os.MkdirAll(album, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(album, "01 track.flac"), []byte("0123456789"), 0644))
		writeSums(t, album, hasher.SumsFilename, map[string]string{
			"01 track.flac": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		})

		result, err := CurrentState(device, "ipod")
		require.NoError(t, err)

		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/01 track.flac"}
		require.Contains(t, result.Entries, entry)
		assert.EqualValues(t, 10, result.Entries[entry].Size)
	})

	t.Run("a file listed in sums.md5 but missing from disk gets a zero Size, not an error", func(t *testing.T) {
		device := t.TempDir()
		album := filepath.Join(device, "main", "a", "artist", "album")
		writeSums(t, album, hasher.SumsFilename, map[string]string{
			"01 track.flac": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		})

		result, err := CurrentState(device, "ipod")
		require.NoError(t, err)

		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/01 track.flac"}
		require.Contains(t, result.Entries, entry)
		assert.EqualValues(t, 0, result.Entries[entry].Size)
	})

	t.Run("a derived file's sidecar src hash is attached alongside its device hash", func(t *testing.T) {
		device := t.TempDir()
		album := filepath.Join(device, "main", "a", "artist", "album")
		writeSums(t, album, hasher.SumsFilename, map[string]string{
			"01 track.mp3": "deadbeefdeadbeefdeadbeefdeadbeef",
		})
		writeSums(t, album, "sdcard.src.md5", map[string]string{
			"01 track.mp3": "cafebabecafebabecafebabecafebabe",
		})

		result, err := CurrentState(device, "sdcard")
		require.NoError(t, err)

		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/01 track.mp3"}
		got := result.Entries[entry]
		assert.Equal(t, "deadbeefdeadbeefdeadbeefdeadbeef", got.Hash)
		assert.True(t, got.HasSrcHash)
		assert.Equal(t, "cafebabecafebabecafebabecafebabe", got.SrcHash)
	})

	t.Run("a src.md5 for a different target does not attach to this target's entries", func(t *testing.T) {
		device := t.TempDir()
		album := filepath.Join(device, "main", "a", "artist", "album")
		writeSums(t, album, hasher.SumsFilename, map[string]string{
			"01 track.mp3": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		})
		writeSums(t, album, "sdcard.src.md5", map[string]string{
			"01 track.mp3": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		})

		result, err := CurrentState(device, "ipod")
		require.NoError(t, err)

		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/01 track.mp3"}
		got := result.Entries[entry]
		assert.False(t, got.HasSrcHash, "ipod's discovery must not pick up sdcard's sidecar")
	})

	t.Run("multiple albums across multiple roots are all discovered", func(t *testing.T) {
		device := t.TempDir()
		writeSums(t, filepath.Join(device, "main", "a", "artist", "album"), hasher.SumsFilename,
			map[string]string{"01 track.flac": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
		writeSums(t, filepath.Join(device, "christmas", "m", "mariah", "album"), hasher.SumsFilename,
			map[string]string{"01 track.flac": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"})

		result, err := CurrentState(device, "ipod")
		require.NoError(t, err)
		assert.Len(t, result.Entries, 2)
		assert.Contains(t, result.Entries, DesiredEntry{Root: "main", Rel: "a/artist/album/01 track.flac"})
		assert.Contains(t, result.Entries, DesiredEntry{Root: "christmas", Rel: "m/mariah/album/01 track.flac"})
	})

	t.Run("an album directly under the root (no intermediate letter/artist dirs) resolves correctly", func(t *testing.T) {
		device := t.TempDir()
		writeSums(t, filepath.Join(device, "main"), hasher.SumsFilename,
			map[string]string{"01 track.flac": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})

		result, err := CurrentState(device, "ipod")
		require.NoError(t, err)
		assert.Contains(t, result.Entries, DesiredEntry{Root: "main", Rel: "01 track.flac"})
	})

	t.Run("a root with no sums.md5 anywhere under it contributes nothing", func(t *testing.T) {
		device := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(device, "main", "some", "dir"), 0755))

		result, err := CurrentState(device, "ipod")
		require.NoError(t, err)
		assert.Empty(t, result.Entries)
		assert.Empty(t, result.Warnings)
	})

	t.Run("populates AlbumArtHash for an embedding target, from the src.md5 bookkeeping entry", func(t *testing.T) {
		device := t.TempDir()
		album := filepath.Join(device, "main", "a", "artist", "album")
		artHash := strings.Repeat("c", 32)
		writeSums(t, album, hasher.SumsFilename, map[string]string{
			"01 track.mp3": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		})
		writeSums(t, album, "sdcard.src.md5", map[string]string{
			"01 track.mp3": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			"folder.jpg":   artHash, // bookkeeping-only entry
		})

		result, err := CurrentState(device, "sdcard")
		require.NoError(t, err)

		key := AlbumKey{Root: "main", Dir: "a/artist/album"}
		require.Contains(t, result.AlbumArtHash, key)
		assert.Equal(t, artHash, result.AlbumArtHash[key])
	})

	t.Run("an album with no artwork bookkeeping entry has no AlbumArtHash entry", func(t *testing.T) {
		device := t.TempDir()
		album := filepath.Join(device, "main", "a", "artist", "album")
		writeSums(t, album, hasher.SumsFilename, map[string]string{
			"01 track.mp3": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		})
		writeSums(t, album, "sdcard.src.md5", map[string]string{
			"01 track.mp3": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		})

		result, err := CurrentState(device, "sdcard")
		require.NoError(t, err)

		key := AlbumKey{Root: "main", Dir: "a/artist/album"}
		assert.NotContains(t, result.AlbumArtHash, key)
	})

	t.Run("never populates AlbumArtHash for a non-embedding target, even if its own src.md5 has an artwork entry", func(t *testing.T) {
		device := t.TempDir()
		album := filepath.Join(device, "main", "a", "artist", "album")
		artHash := strings.Repeat("c", 32)
		writeSums(t, album, hasher.SumsFilename, map[string]string{
			"01 track.flac": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"folder.jpg":    strings.Repeat("d", 32),
		})
		// ipod's sidecar, populated as it would be if ipod's artwork
		// were genuinely resized (not a byte-identical passthrough).
		writeSums(t, album, "ipod.src.md5", map[string]string{
			"folder.jpg": artHash,
		})

		result, err := CurrentState(device, "ipod")
		require.NoError(t, err)

		key := AlbumKey{Root: "main", Dir: "a/artist/album"}
		assert.NotContains(t, result.AlbumArtHash, key,
			"ipod's artwork drift is tracked via its own ordinary DesiredEntry, not AlbumArtHash")

		// The artwork's entry, however, must still come through the
		// normal per-file mechanism exactly like any other derived file.
		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/folder.jpg"}
		require.Contains(t, result.Entries, entry)
		assert.True(t, result.Entries[entry].HasSrcHash)
		assert.Equal(t, artHash, result.Entries[entry].SrcHash)
	})
}
