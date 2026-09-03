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
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/hasher"
)

// makeArtworkFile writes a small solid-color JPEG at path.
func makeArtworkFile(t *testing.T, path string, size int) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{R: 255, A: 255}}, image.Point{}, draw.Src)
	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()
	require.NoError(t, jpeg.Encode(f, img, nil))
}

func TestExecute(t *testing.T) {
	t.Run("errors on an unrecognized target", func(t *testing.T) {
		_, err := Execute(context.Background(), t.TempDir(), t.TempDir(), "chromecast", &DiffResult{}, false)
		assert.Error(t, err)
	})

	t.Run("add: passthrough audio is copied, sums.md5 updated, no sidecar", func(t *testing.T) {
		root := t.TempDir()
		device := t.TempDir()
		album := filepath.Join(root, "main", "a", "artist", "album")
		require.NoError(t, os.MkdirAll(album, 0755))
		src := makeAudioFile(t, album, "01 track.flac", map[string]string{"TITLE": "Track"})
		hash, err := hasher.HashFile(src)
		require.NoError(t, err)
		require.NoError(t, hasher.WriteSums(album, hasher.SumsFilename, map[string]string{"01 track.flac": hash}))

		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/01 track.flac"}
		diff := &DiffResult{Changes: []PlannedChange{{Entry: entry, Action: ActionAdd}}}

		result, err := Execute(context.Background(), root, device, "ipod", diff, false)
		require.NoError(t, err)
		require.Len(t, result.Created, 1)
		assert.Empty(t, result.Warnings)

		destPath := filepath.Join(device, "main", "a", "artist", "album", "01 track.flac")
		assert.FileExists(t, destPath)

		deviceAlbum := filepath.Join(device, "main", "a", "artist", "album")
		sums, existed, err := hasher.ReadSums(deviceAlbum, hasher.SumsFilename)
		require.NoError(t, err)
		require.True(t, existed)
		assert.Equal(t, hash, sums["01 track.flac"])

		_, srcExisted, err := hasher.ReadSums(deviceAlbum, "ipod.src.md5")
		require.NoError(t, err)
		assert.False(t, srcExisted, "a passthrough file must not get a sidecar entry")
	})

	t.Run("add: unsupported format is transcoded, sidecar records the source hash", func(t *testing.T) {
		root := t.TempDir()
		device := t.TempDir()
		album := filepath.Join(root, "main", "a", "artist", "album")
		require.NoError(t, os.MkdirAll(album, 0755))
		src := makeAudioFile(t, album, "01 track.flac", map[string]string{"TITLE": "Track"})
		srcHash, err := hasher.HashFile(src)
		require.NoError(t, err)
		require.NoError(t, hasher.WriteSums(album, hasher.SumsFilename, map[string]string{"01 track.flac": srcHash}))

		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/01 track.flac"}
		diff := &DiffResult{Changes: []PlannedChange{{Entry: entry, Action: ActionAdd}}}

		result, err := Execute(context.Background(), root, device, "sdcard", diff, false)
		require.NoError(t, err)
		require.Len(t, result.Created, 1)
		assert.Empty(t, result.Warnings)

		destPath := filepath.Join(device, "main", "a", "artist", "album", "01 track.mp3")
		assert.FileExists(t, destPath)

		destHash, err := hasher.HashFile(destPath)
		require.NoError(t, err)
		assert.NotEqual(t, srcHash, destHash, "a real transcode must not be byte-identical")

		deviceAlbum := filepath.Join(device, "main", "a", "artist", "album")
		sums, _, err := hasher.ReadSums(deviceAlbum, hasher.SumsFilename)
		require.NoError(t, err)
		assert.Equal(t, destHash, sums["01 track.mp3"])

		srcSums, existed, err := hasher.ReadSums(deviceAlbum, "sdcard.src.md5")
		require.NoError(t, err)
		require.True(t, existed)
		assert.Equal(t, srcHash, srcSums["01 track.mp3"])
	})

	t.Run("regenerate: existing device file is overwritten to match the new source", func(t *testing.T) {
		root := t.TempDir()
		device := t.TempDir()
		album := filepath.Join(root, "main", "a", "artist", "album")
		require.NoError(t, os.MkdirAll(album, 0755))
		src := makeAudioFile(t, album, "01 track.flac", map[string]string{"TITLE": "New"})
		newHash, err := hasher.HashFile(src)
		require.NoError(t, err)
		require.NoError(t, hasher.WriteSums(album, hasher.SumsFilename, map[string]string{"01 track.flac": newHash}))

		deviceAlbum := filepath.Join(device, "main", "a", "artist", "album")
		require.NoError(t, os.MkdirAll(deviceAlbum, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(deviceAlbum, "01 track.flac"), []byte("stale"), 0644))
		require.NoError(t, hasher.WriteSums(deviceAlbum, hasher.SumsFilename, map[string]string{
			"01 track.flac": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}))

		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/01 track.flac"}
		diff := &DiffResult{Changes: []PlannedChange{{Entry: entry, Action: ActionRegenerate}}}

		result, err := Execute(context.Background(), root, device, "ipod", diff, false)
		require.NoError(t, err)
		require.Len(t, result.Updated, 1)
		assert.Empty(t, result.Created)

		got, err := os.ReadFile(filepath.Join(deviceAlbum, "01 track.flac"))
		require.NoError(t, err)
		assert.NotEqual(t, "stale", string(got))

		sums, _, err := hasher.ReadSums(deviceAlbum, hasher.SumsFilename)
		require.NoError(t, err)
		assert.Equal(t, newHash, sums["01 track.flac"])
	})

	t.Run("add: external artwork is resized and recorded like any other file", func(t *testing.T) {
		root := t.TempDir()
		device := t.TempDir()
		album := filepath.Join(root, "main", "a", "artist", "album")
		artPath := filepath.Join(album, "folder.jpg")
		makeArtworkFile(t, artPath, 1000)
		artHash, err := hasher.HashFile(artPath)
		require.NoError(t, err)
		require.NoError(t, hasher.WriteSums(album, hasher.SumsFilename, map[string]string{"folder.jpg": artHash}))

		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/folder.jpg"}
		diff := &DiffResult{Changes: []PlannedChange{{Entry: entry, Action: ActionAdd}}}

		result, err := Execute(context.Background(), root, device, "ipod", diff, false)
		require.NoError(t, err)
		require.Len(t, result.Created, 1)
		assert.Empty(t, result.Warnings)

		destPath := filepath.Join(device, "main", "a", "artist", "album", "folder.jpg")
		assert.FileExists(t, destPath)

		destHash, err := hasher.HashFile(destPath)
		require.NoError(t, err)
		assert.NotEqual(t, artHash, destHash, "a 1000px source must actually be resized down to ipod's 400px limit")

		deviceAlbum := filepath.Join(device, "main", "a", "artist", "album")
		sums, _, err := hasher.ReadSums(deviceAlbum, hasher.SumsFilename)
		require.NoError(t, err)
		assert.Equal(t, destHash, sums["folder.jpg"])
	})

	t.Run("embed target: audio gets art embedded, album-level bookkeeping entry is written", func(t *testing.T) {
		root := t.TempDir()
		device := t.TempDir()
		album := filepath.Join(root, "main", "a", "artist", "album")
		require.NoError(t, os.MkdirAll(album, 0755))
		src := makeAudioFile(t, album, "01 track.mp3", map[string]string{"TITLE": "Track"})
		artPath := filepath.Join(album, "folder.jpg")
		makeArtworkFile(t, artPath, 200)

		srcHash, err := hasher.HashFile(src)
		require.NoError(t, err)
		artHash, err := hasher.HashFile(artPath)
		require.NoError(t, err)
		require.NoError(t, hasher.WriteSums(album, hasher.SumsFilename, map[string]string{
			"01 track.mp3": srcHash,
			"folder.jpg":   artHash,
		}))

		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/01 track.mp3"}
		diff := &DiffResult{Changes: []PlannedChange{{Entry: entry, Action: ActionAdd}}}

		result, err := Execute(context.Background(), root, device, "sdcard", diff, false)
		require.NoError(t, err)
		require.Len(t, result.Created, 1)
		assert.Empty(t, result.Warnings)

		deviceAlbum := filepath.Join(device, "main", "a", "artist", "album")
		srcSums, existed, err := hasher.ReadSums(deviceAlbum, "sdcard.src.md5")
		require.NoError(t, err)
		require.True(t, existed)
		assert.Equal(t, srcHash, srcSums["01 track.mp3"])
		require.Contains(t, srcSums, "folder.jpg", "the album-level artwork bookkeeping entry must be written")
		assert.Equal(t, artHash, srcSums["folder.jpg"])

		// No external artwork file is copied to an embedding target.
		_, err = os.Stat(filepath.Join(deviceAlbum, "folder.jpg"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("dry run: nothing is written, but Created/Updated are still reported", func(t *testing.T) {
		root := t.TempDir()
		device := t.TempDir()
		album := filepath.Join(root, "main", "a", "artist", "album")
		require.NoError(t, os.MkdirAll(album, 0755))
		src := makeAudioFile(t, album, "01 track.flac", nil)
		hash, err := hasher.HashFile(src)
		require.NoError(t, err)
		require.NoError(t, hasher.WriteSums(album, hasher.SumsFilename, map[string]string{"01 track.flac": hash}))

		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/01 track.flac"}
		diff := &DiffResult{Changes: []PlannedChange{{Entry: entry, Action: ActionAdd}}}

		result, err := Execute(context.Background(), root, device, "ipod", diff, true)
		require.NoError(t, err)
		require.Len(t, result.Created, 1)

		destPath := filepath.Join(device, "main", "a", "artist", "album", "01 track.flac")
		_, statErr := os.Stat(destPath)
		assert.True(t, os.IsNotExist(statErr), "dry run must not write the audio file")

		_, statErr = os.Stat(filepath.Join(device, "main", "a", "artist", "album", hasher.SumsFilename))
		assert.True(t, os.IsNotExist(statErr), "dry run must not write sums.md5 either")
	})

	t.Run("multiple albums are handled independently, no cross-contamination", func(t *testing.T) {
		root := t.TempDir()
		device := t.TempDir()

		albumA := filepath.Join(root, "main", "a", "artist", "album")
		require.NoError(t, os.MkdirAll(albumA, 0755))
		srcA := makeAudioFile(t, albumA, "01 track.flac", nil)
		hashA, err := hasher.HashFile(srcA)
		require.NoError(t, err)
		require.NoError(t, hasher.WriteSums(albumA, hasher.SumsFilename, map[string]string{"01 track.flac": hashA}))

		albumB := filepath.Join(root, "main", "b", "artist", "album")
		require.NoError(t, os.MkdirAll(albumB, 0755))
		srcB := makeAudioFile(t, albumB, "01 track.flac", nil)
		hashB, err := hasher.HashFile(srcB)
		require.NoError(t, err)
		require.NoError(t, hasher.WriteSums(albumB, hasher.SumsFilename, map[string]string{"01 track.flac": hashB}))

		entryA := DesiredEntry{Root: "main", Rel: "a/artist/album/01 track.flac"}
		entryB := DesiredEntry{Root: "main", Rel: "b/artist/album/01 track.flac"}
		diff := &DiffResult{Changes: []PlannedChange{
			{Entry: entryA, Action: ActionAdd},
			{Entry: entryB, Action: ActionAdd},
		}}

		result, err := Execute(context.Background(), root, device, "ipod", diff, false)
		require.NoError(t, err)
		assert.Len(t, result.Created, 2)

		deviceAlbumA := filepath.Join(device, "main", "a", "artist", "album")
		sumsA, _, err := hasher.ReadSums(deviceAlbumA, hasher.SumsFilename)
		require.NoError(t, err)
		assert.Len(t, sumsA, 1, "album A's sums.md5 must contain only its own file")

		deviceAlbumB := filepath.Join(device, "main", "b", "artist", "album")
		sumsB, _, err := hasher.ReadSums(deviceAlbumB, hasher.SumsFilename)
		require.NoError(t, err)
		assert.Len(t, sumsB, 1, "album B's sums.md5 must contain only its own file")
	})

	t.Run("a failing entry produces a warning but does not abort the rest of the album", func(t *testing.T) {
		root := t.TempDir()
		device := t.TempDir()
		album := filepath.Join(root, "main", "a", "artist", "album")
		require.NoError(t, os.MkdirAll(album, 0755))
		src := makeAudioFile(t, album, "02 track.flac", nil)
		hash, err := hasher.HashFile(src)
		require.NoError(t, err)
		require.NoError(t, hasher.WriteSums(album, hasher.SumsFilename, map[string]string{
			"01 missing.flac": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"02 track.flac":   hash,
		}))

		missingEntry := DesiredEntry{Root: "main", Rel: "a/artist/album/01 missing.flac"}
		goodEntry := DesiredEntry{Root: "main", Rel: "a/artist/album/02 track.flac"}
		diff := &DiffResult{Changes: []PlannedChange{
			{Entry: missingEntry, Action: ActionAdd},
			{Entry: goodEntry, Action: ActionAdd},
		}}

		result, err := Execute(context.Background(), root, device, "ipod", diff, false)
		require.NoError(t, err)
		require.Len(t, result.Warnings, 1)
		require.Len(t, result.Created, 1)
		assert.Equal(t, goodEntry, result.Created[0])

		assert.FileExists(t, filepath.Join(device, "main", "a", "artist", "album", "02 track.flac"))
	})

	t.Run("skip actions are ignored entirely", func(t *testing.T) {
		root := t.TempDir()
		device := t.TempDir()

		diff := &DiffResult{Changes: []PlannedChange{
			{Entry: DesiredEntry{Root: "main", Rel: "a/artist/album/01 track.flac"}, Action: ActionSkip},
		}}

		result, err := Execute(context.Background(), root, device, "ipod", diff, false)
		require.NoError(t, err)
		assert.Empty(t, result.Created)
		assert.Empty(t, result.Updated)
		assert.Empty(t, result.Deleted)
		assert.Empty(t, result.Warnings)
	})

	t.Run("delete: removes the file, leaves the album directory when other files remain", func(t *testing.T) {
		root := t.TempDir()
		device := t.TempDir()
		deviceAlbum := filepath.Join(device, "main", "a", "artist", "album")
		require.NoError(t, os.MkdirAll(deviceAlbum, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(deviceAlbum, "01 gone.flac"), []byte("x"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(deviceAlbum, "02 stays.flac"), []byte("y"), 0644))
		require.NoError(t, hasher.WriteSums(deviceAlbum, hasher.SumsFilename, map[string]string{
			"01 gone.flac":  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"02 stays.flac": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		}))

		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/01 gone.flac"}
		diff := &DiffResult{Changes: []PlannedChange{{Entry: entry, Action: ActionDelete}}}

		result, err := Execute(context.Background(), root, device, "ipod", diff, false)
		require.NoError(t, err)
		require.Len(t, result.Deleted, 1)
		assert.Equal(t, entry, result.Deleted[0])

		_, statErr := os.Stat(filepath.Join(deviceAlbum, "01 gone.flac"))
		assert.True(t, os.IsNotExist(statErr))
		assert.FileExists(t, filepath.Join(deviceAlbum, "02 stays.flac"), "the other file must be untouched")
		assert.DirExists(t, deviceAlbum, "the album directory must remain since it still has content")

		sums, _, err := hasher.ReadSums(deviceAlbum, hasher.SumsFilename)
		require.NoError(t, err)
		assert.NotContains(t, sums, "01 gone.flac")
		assert.Contains(t, sums, "02 stays.flac")
	})

	t.Run("delete: the last file in an album removes the whole album directory, including sums.md5", func(t *testing.T) {
		root := t.TempDir()
		device := t.TempDir()
		deviceAlbum := filepath.Join(device, "main", "a", "artist", "album")
		require.NoError(t, os.MkdirAll(deviceAlbum, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(deviceAlbum, "01 gone.flac"), []byte("x"), 0644))
		require.NoError(t, hasher.WriteSums(deviceAlbum, hasher.SumsFilename, map[string]string{
			"01 gone.flac": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}))

		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/01 gone.flac"}
		diff := &DiffResult{Changes: []PlannedChange{{Entry: entry, Action: ActionDelete}}}

		result, err := Execute(context.Background(), root, device, "ipod", diff, false)
		require.NoError(t, err)
		require.Len(t, result.Deleted, 1)

		_, statErr := os.Stat(deviceAlbum)
		assert.True(t, os.IsNotExist(statErr), "the whole album directory must be removed, not left with an empty sums.md5")
	})

	t.Run("delete: empty parent directories are cleaned up, bubbling upward, but the root-level device directory is never removed", func(t *testing.T) {
		root := t.TempDir()
		device := t.TempDir()
		// main/a/artist/album -- "a" and "artist" have no other children.
		deviceAlbum := filepath.Join(device, "main", "a", "artist", "album")
		require.NoError(t, os.MkdirAll(deviceAlbum, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(deviceAlbum, "01 gone.flac"), []byte("x"), 0644))
		require.NoError(t, hasher.WriteSums(deviceAlbum, hasher.SumsFilename, map[string]string{
			"01 gone.flac": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}))

		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/01 gone.flac"}
		diff := &DiffResult{Changes: []PlannedChange{{Entry: entry, Action: ActionDelete}}}

		_, err := Execute(context.Background(), root, device, "ipod", diff, false)
		require.NoError(t, err)

		_, err = os.Stat(filepath.Join(device, "main", "a", "artist"))
		assert.True(t, os.IsNotExist(err), "the now-empty 'artist' directory must be cleaned up")
		_, err = os.Stat(filepath.Join(device, "main", "a"))
		assert.True(t, os.IsNotExist(err), "the now-empty 'a' directory must be cleaned up too, bubbling upward")

		assert.DirExists(t, filepath.Join(device, "main"), "the root-level device directory must never be removed")
	})

	t.Run("delete: a sibling album under the same letter directory prevents that directory from being removed", func(t *testing.T) {
		root := t.TempDir()
		device := t.TempDir()
		goneAlbum := filepath.Join(device, "main", "a", "artist-gone", "album")
		staysAlbum := filepath.Join(device, "main", "a", "artist-stays", "album")
		require.NoError(t, os.MkdirAll(goneAlbum, 0755))
		require.NoError(t, os.MkdirAll(staysAlbum, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(goneAlbum, "01 gone.flac"), []byte("x"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(staysAlbum, "01 stays.flac"), []byte("y"), 0644))
		require.NoError(t, hasher.WriteSums(goneAlbum, hasher.SumsFilename, map[string]string{
			"01 gone.flac": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}))
		require.NoError(t, hasher.WriteSums(staysAlbum, hasher.SumsFilename, map[string]string{
			"01 stays.flac": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		}))

		entry := DesiredEntry{Root: "main", Rel: "a/artist-gone/album/01 gone.flac"}
		diff := &DiffResult{Changes: []PlannedChange{{Entry: entry, Action: ActionDelete}}}

		_, err := Execute(context.Background(), root, device, "ipod", diff, false)
		require.NoError(t, err)

		_, err = os.Stat(filepath.Join(device, "main", "a", "artist-gone"))
		assert.True(t, os.IsNotExist(err))
		assert.DirExists(t, filepath.Join(device, "main", "a"), "'a' must remain: it still has artist-stays as a child")
		assert.DirExists(t, staysAlbum)
	})

	t.Run("delete: removes a stale src.md5 sidecar entirely once no derived entries remain", func(t *testing.T) {
		root := t.TempDir()
		device := t.TempDir()
		deviceAlbum := filepath.Join(device, "main", "a", "artist", "album")
		require.NoError(t, os.MkdirAll(deviceAlbum, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(deviceAlbum, "01 gone.mp3"), []byte("x"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(deviceAlbum, "02 stays.flac"), []byte("y"), 0644))
		require.NoError(t, hasher.WriteSums(deviceAlbum, hasher.SumsFilename, map[string]string{
			"01 gone.mp3":   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"02 stays.flac": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		}))
		// 01 was a derived (transcoded) file; 02 is passthrough (no entry).
		require.NoError(t, hasher.WriteSums(deviceAlbum, "sdcard.src.md5", map[string]string{
			"01 gone.mp3": strings.Repeat("c", 32),
		}))

		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/01 gone.mp3"}
		diff := &DiffResult{Changes: []PlannedChange{{Entry: entry, Action: ActionDelete}}}

		result, err := Execute(context.Background(), root, device, "sdcard", diff, false)
		require.NoError(t, err)
		require.Len(t, result.Deleted, 1)

		_, srcExisted, err := hasher.ReadSums(deviceAlbum, "sdcard.src.md5")
		require.NoError(t, err)
		assert.False(t, srcExisted, "the sidecar must be removed entirely once no derived entries remain")

		assert.FileExists(t, filepath.Join(deviceAlbum, "02 stays.flac"))
	})

	t.Run("delete: dry run reports what would be deleted without touching anything", func(t *testing.T) {
		root := t.TempDir()
		device := t.TempDir()
		deviceAlbum := filepath.Join(device, "main", "a", "artist", "album")
		require.NoError(t, os.MkdirAll(deviceAlbum, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(deviceAlbum, "01 track.flac"), []byte("x"), 0644))
		require.NoError(t, hasher.WriteSums(deviceAlbum, hasher.SumsFilename, map[string]string{
			"01 track.flac": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}))

		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/01 track.flac"}
		diff := &DiffResult{Changes: []PlannedChange{{Entry: entry, Action: ActionDelete}}}

		result, err := Execute(context.Background(), root, device, "ipod", diff, true)
		require.NoError(t, err)
		require.Len(t, result.Deleted, 1)

		assert.FileExists(t, filepath.Join(deviceAlbum, "01 track.flac"), "dry run must not actually remove the file")
	})

	t.Run("delete: a file already gone (e.g. removed by hand) is not a warning", func(t *testing.T) {
		root := t.TempDir()
		device := t.TempDir()
		deviceAlbum := filepath.Join(device, "main", "a", "artist", "album")
		require.NoError(t, os.MkdirAll(deviceAlbum, 0755))
		require.NoError(t, hasher.WriteSums(deviceAlbum, hasher.SumsFilename, map[string]string{
			"01 already-gone.flac": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}))
		// Note: the file itself was never actually created on disk.

		entry := DesiredEntry{Root: "main", Rel: "a/artist/album/01 already-gone.flac"}
		diff := &DiffResult{Changes: []PlannedChange{{Entry: entry, Action: ActionDelete}}}

		result, err := Execute(context.Background(), root, device, "ipod", diff, false)
		require.NoError(t, err)
		assert.Empty(t, result.Warnings)
		require.Len(t, result.Deleted, 1)
	})

	t.Run("add and delete in the same album are both applied correctly", func(t *testing.T) {
		root := t.TempDir()
		device := t.TempDir()
		sourceAlbum := filepath.Join(root, "main", "a", "artist", "album")
		require.NoError(t, os.MkdirAll(sourceAlbum, 0755))
		newSrc := makeAudioFile(t, sourceAlbum, "02 new.flac", nil)
		newHash, err := hasher.HashFile(newSrc)
		require.NoError(t, err)
		require.NoError(t, hasher.WriteSums(sourceAlbum, hasher.SumsFilename, map[string]string{
			"02 new.flac": newHash,
		}))

		deviceAlbum := filepath.Join(device, "main", "a", "artist", "album")
		require.NoError(t, os.MkdirAll(deviceAlbum, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(deviceAlbum, "01 old.flac"), []byte("stale"), 0644))
		require.NoError(t, hasher.WriteSums(deviceAlbum, hasher.SumsFilename, map[string]string{
			"01 old.flac": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}))

		diff := &DiffResult{Changes: []PlannedChange{
			{Entry: DesiredEntry{Root: "main", Rel: "a/artist/album/01 old.flac"}, Action: ActionDelete},
			{Entry: DesiredEntry{Root: "main", Rel: "a/artist/album/02 new.flac"}, Action: ActionAdd},
		}}

		result, err := Execute(context.Background(), root, device, "ipod", diff, false)
		require.NoError(t, err)
		require.Len(t, result.Deleted, 1)
		require.Len(t, result.Created, 1)

		_, statErr := os.Stat(filepath.Join(deviceAlbum, "01 old.flac"))
		assert.True(t, os.IsNotExist(statErr))
		assert.FileExists(t, filepath.Join(deviceAlbum, "02 new.flac"))

		sums, _, err := hasher.ReadSums(deviceAlbum, hasher.SumsFilename)
		require.NoError(t, err)
		assert.Len(t, sums, 1, "the final sums.md5 must reflect only the surviving file")
		assert.Contains(t, sums, "02 new.flac")
	})

	t.Run("add: a video is transcoded via PrepareVideo, not copied through", func(t *testing.T) {
		// Isolated, hand-built-diff test of executeAlbum's video branch
		// specifically (separate from TestVideoPlan's higher-level,
		// full-pipeline coverage, so a failure here points precisely at
		// the branch selection itself rather than needing to be
		// disentangled from VideoDesiredState/Diff too).
		root := t.TempDir()
		device := t.TempDir()
		videoDir := filepath.Join(root, "videos", "b", "beyonce", "crazy in love")
		require.NoError(t, os.MkdirAll(videoDir, 0755))
		src := makeVideoFile(t, videoDir, "crazy in love.mp4", 640, 360)
		srcHash, err := hasher.HashFile(src)
		require.NoError(t, err)
		require.NoError(t, hasher.WriteSums(videoDir, hasher.SumsFilename, map[string]string{
			"crazy in love.mp4": srcHash,
		}))

		entry := DesiredEntry{Root: "videos", Rel: "b/beyonce/crazy in love/crazy in love.mp4"}
		diff := &DiffResult{Changes: []PlannedChange{{Entry: entry, Action: ActionAdd}}}

		result, err := Execute(context.Background(), root, device, "ipod", diff, false)
		require.NoError(t, err)
		require.Len(t, result.Created, 1)
		assert.Empty(t, result.Warnings)

		deviceVideo := filepath.Join(device, "videos", "b", "beyonce", "crazy in love", "crazy in love.mpg")
		assert.FileExists(t, deviceVideo)
		assert.NoFileExists(t, filepath.Join(device, "videos", "b", "beyonce", "crazy in love", "crazy in love.mp4"),
			"the source container must not have been copied through unchanged")

		deviceSums, _, err := hasher.ReadSums(filepath.Dir(deviceVideo), hasher.SumsFilename)
		require.NoError(t, err)
		assert.NotEmpty(t, deviceSums["crazy in love.mpg"])
	})
}

func TestCleanupEmptyParents(t *testing.T) {
	t.Run("removes an empty directory and its empty parent, stopping at stopAt", func(t *testing.T) {
		root := t.TempDir()
		nested := filepath.Join(root, "main", "a", "artist")
		require.NoError(t, os.MkdirAll(nested, 0755))

		require.NoError(t, cleanupEmptyParents(nested, filepath.Join(root, "main")))

		_, err := os.Stat(nested)
		assert.True(t, os.IsNotExist(err))
		_, err = os.Stat(filepath.Join(root, "main", "a"))
		assert.True(t, os.IsNotExist(err))
		assert.DirExists(t, filepath.Join(root, "main"))
	})

	t.Run("stops as soon as a directory is non-empty", func(t *testing.T) {
		root := t.TempDir()
		nested := filepath.Join(root, "main", "a", "artist")
		require.NoError(t, os.MkdirAll(nested, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "main", "a", "keep.txt"), []byte("x"), 0644))

		require.NoError(t, cleanupEmptyParents(nested, filepath.Join(root, "main")))

		_, err := os.Stat(nested)
		assert.True(t, os.IsNotExist(err), "the empty leaf directory is still removed")
		assert.DirExists(t, filepath.Join(root, "main", "a"), "but its non-empty parent must survive")
	})

	t.Run("a nonexistent starting directory is not an error", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, cleanupEmptyParents(filepath.Join(root, "does", "not", "exist"), root))
	})

	t.Run("stopAt itself is never removed, even if empty", func(t *testing.T) {
		root := t.TempDir()
		stopAt := filepath.Join(root, "main")
		require.NoError(t, os.MkdirAll(stopAt, 0755))

		require.NoError(t, cleanupEmptyParents(stopAt, stopAt))
		assert.DirExists(t, stopAt)
	})
}
