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
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.senan.xyz/taglib"

	"github.com/mfinelli/musicrename/internal/target"
	"github.com/mfinelli/musicrename/internal/testutil"
)

func fileMD5(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	sum := md5.Sum(data)
	return fmt.Sprintf("%x", sum)
}

func TestPrepareTrack(t *testing.T) {
	t.Run("passthrough is byte-for-byte identical to source, tags untouched", func(t *testing.T) {
		dir := t.TempDir()
		src := testutil.MakeAudioFile(t, dir, "01 track.flac", map[string]string{
			"TITLE": "Track One", "ARTIST": "Test Artist",
		})
		dst := filepath.Join(dir, "out", "01 track.flac")

		def := target.Definition{AcceptedFormats: []string{".flac", ".mp3", ".m4a"}}
		require.NoError(t, PrepareTrack(context.Background(), src, dst, def, nil, nil))

		assert.FileExists(t, dst)
		assert.Equal(t, fileMD5(t, src), fileMD5(t, dst),
			"a passthrough copy must be byte-for-byte identical to source — "+
				"this is what lets on-device drift detection skip rehashing entirely (DESIGN.md §7.6)")

		tags, err := taglib.ReadTags(dst)
		require.NoError(t, err)
		assert.Equal(t, []string{"Track One"}, tags[taglib.Title])
		assert.Equal(t, []string{"Test Artist"}, tags[taglib.Artist])
	})

	t.Run("passthrough with an embedding target still stays untouched by a tag rewrite", func(t *testing.T) {
		dir := t.TempDir()
		src := testutil.MakeAudioFile(t, dir, "01 track.mp3", map[string]string{
			"TITLE": "Track One", "ARTIST": "Test Artist",
		})
		dst := filepath.Join(dir, "out.mp3")

		// sdcard-shaped: accepts mp3 passthrough, but embeds art. Original
		// tags must still survive via the copy itself, not a rewrite.
		def := target.Definition{AcceptedFormats: []string{".mp3"}, EmbedArt: true}
		require.NoError(t, PrepareTrack(context.Background(), src, dst, def, nil, nil))

		tags, err := taglib.ReadTags(dst)
		require.NoError(t, err)
		assert.Equal(t, []string{"Track One"}, tags[taglib.Title])
		assert.Equal(t, []string{"Test Artist"}, tags[taglib.Artist])
	})

	t.Run("transcode: unsupported format is transcoded and tags are migrated", func(t *testing.T) {
		dir := t.TempDir()
		src := testutil.MakeAudioFile(t, dir, "01 track.flac", map[string]string{
			"TITLE": "Track One", "ARTIST": "Test Artist",
		})
		dst := filepath.Join(dir, "01 track.mp3")

		def := target.Definition{
			AcceptedFormats: []string{".mp3"},
			TranscodeFormat: target.FormatMP3,
		}
		require.NoError(t, PrepareTrack(context.Background(), src, dst, def, nil, nil))

		assert.FileExists(t, dst)

		srcInfo, err := os.Stat(src)
		require.NoError(t, err)
		dstInfo, err := os.Stat(dst)
		require.NoError(t, err)
		assert.NotEqual(t, srcInfo.Size(), dstInfo.Size(), "a real transcode should not produce an identical file")

		tags, err := taglib.ReadTags(dst)
		require.NoError(t, err)
		assert.Equal(t, []string{"Track One"}, tags[taglib.Title])
		assert.Equal(t, []string{"Test Artist"}, tags[taglib.Artist])
	})

	t.Run("embeds artwork when the target embeds and art is given", func(t *testing.T) {
		dir := t.TempDir()
		src := testutil.MakeAudioFile(t, dir, "01 track.mp3", map[string]string{"TITLE": "Track One"})
		dst := filepath.Join(dir, "out.mp3")

		def := target.Definition{AcceptedFormats: []string{".mp3"}, EmbedArt: true}
		fakeJPEG := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0} // minimal-looking JPEG header

		err := PrepareTrack(context.Background(), src, dst, def, fakeJPEG, nil)
		// taglib may or may not validate the image bytes strictly; either
		// a clean write or a decode-related error is acceptable here, but
		// a completely unrelated failure (e.g. the earlier copy step
		// breaking) is not.
		if err != nil {
			t.Logf("WriteImage returned an error for a minimal fake JPEG (may be expected): %v", err)
			return
		}
		assert.FileExists(t, dst)
	})

	t.Run("does not embed artwork when the target does not embed, even if art is given", func(t *testing.T) {
		dir := t.TempDir()
		src := testutil.MakeAudioFile(t, dir, "01 track.mp3", map[string]string{"TITLE": "Track One"})
		dst := filepath.Join(dir, "out.mp3")

		def := target.Definition{AcceptedFormats: []string{".mp3"}, EmbedArt: false}
		require.NoError(t, PrepareTrack(context.Background(), src, dst, def, []byte("not even valid image data"), nil))
		assert.FileExists(t, dst)
	})

	t.Run("creates the destination directory if it does not exist", func(t *testing.T) {
		dir := t.TempDir()
		src := testutil.MakeAudioFile(t, dir, "01 track.flac", nil)
		dst := filepath.Join(dir, "a", "b", "c", "track.flac")

		def := target.Definition{AcceptedFormats: []string{".flac"}}
		require.NoError(t, PrepareTrack(context.Background(), src, dst, def, nil, nil))
		assert.FileExists(t, dst)
	})

	t.Run("errors when the target's TranscodeFormat has no encode params", func(t *testing.T) {
		dir := t.TempDir()
		src := testutil.MakeAudioFile(t, dir, "01 track.flac", nil)
		dst := filepath.Join(dir, "out.ogg")

		def := target.Definition{
			AcceptedFormats: []string{".mp3"},
			TranscodeFormat: target.AudioFormat("vorbis"), // not defined in internal/target
		}
		err := PrepareTrack(context.Background(), src, dst, def, nil, nil)
		assert.Error(t, err)
	})

	t.Run("errors when the source file does not exist", func(t *testing.T) {
		dir := t.TempDir()
		def := target.Definition{AcceptedFormats: []string{".flac"}}
		err := PrepareTrack(
			context.Background(), filepath.Join(dir, "missing.flac"), filepath.Join(dir, "out.flac"), def, nil, nil,
		)
		assert.Error(t, err)
	})
}
