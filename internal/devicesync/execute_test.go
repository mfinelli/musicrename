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

	t.Run("skip and delete actions are ignored entirely", func(t *testing.T) {
		root := t.TempDir()
		device := t.TempDir()

		diff := &DiffResult{Changes: []PlannedChange{
			{Entry: DesiredEntry{Root: "main", Rel: "a/artist/album/01 track.flac"}, Action: ActionSkip},
			{Entry: DesiredEntry{Root: "main", Rel: "a/artist/album/02 track.flac"}, Action: ActionDelete},
		}}

		result, err := Execute(context.Background(), root, device, "ipod", diff, false)
		require.NoError(t, err)
		assert.Empty(t, result.Created)
		assert.Empty(t, result.Updated)
		assert.Empty(t, result.Warnings)
	})
}
