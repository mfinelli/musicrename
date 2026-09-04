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

package video

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.senan.xyz/taglib"

	"github.com/mfinelli/musicrename/internal/hasher"
	"github.com/mfinelli/musicrename/internal/testutil"
)

// writeNFOFixture writes a minimal musicvideo.nfo alongside videoPath.
func writeNFOFixture(t *testing.T, videoPath string, nfo NFO) {
	t.Helper()
	require.NoError(t, writeNFO(nfoPath(filepath.Dir(videoPath)), nfo))
}

func TestExtractAudio(t *testing.T) {
	ctx := context.Background()
	nfo := NFO{Title: "Crazy in Love", Artist: "Beyoncé"}

	t.Run("fresh extraction: remuxes, tags, and computes ReplayGain", func(t *testing.T) {
		dir := t.TempDir()
		videoPath := testutil.MakeVideoFileWithTone(t, dir, "video.mp4", "libx264", "aac")
		writeNFOFixture(t, videoPath, nfo)

		dst, err := ExtractAudio(ctx, videoPath, nfo, ExtractAudioOptions{})
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(dir, "video.m4a"), dst)
		assert.FileExists(t, dst)

		tags, err := taglib.ReadTags(dst)
		require.NoError(t, err)
		assert.Equal(t, []string{"Crazy in Love"}, tags[taglib.Title])
		assert.Equal(t, []string{"Beyoncé"}, tags[taglib.Artist])
		assert.NotEmpty(t, tags["REPLAYGAIN_TRACK_GAIN"])
	})

	t.Run("fresh extraction writes audio.src.md5 recording the video's hash", func(t *testing.T) {
		dir := t.TempDir()
		videoPath := testutil.MakeVideoFileWithTone(t, dir, "video.mp4", "libx264", "aac")
		writeNFOFixture(t, videoPath, nfo)

		_, err := ExtractAudio(ctx, videoPath, nfo, ExtractAudioOptions{})
		require.NoError(t, err)

		wantHash, err := hasher.HashFile(videoPath)
		require.NoError(t, err)

		sums, existed, err := hasher.ReadSums(dir, AudioSrcSumsFilename)
		require.NoError(t, err)
		require.True(t, existed)
		assert.Equal(t, wantHash, sums["video.mp4"])
	})

	t.Run("fresh extraction updates an existing sums.md5", func(t *testing.T) {
		dir := t.TempDir()
		videoPath := testutil.MakeVideoFileWithTone(t, dir, "video.mp4", "libx264", "aac")
		writeNFOFixture(t, videoPath, nfo)
		require.NoError(t, hasher.Hash(dir, nil))

		dst, err := ExtractAudio(ctx, videoPath, nfo, ExtractAudioOptions{})
		require.NoError(t, err)

		sums, existed, err := hasher.ReadSums(dir, hasher.SumsFilename)
		require.NoError(t, err)
		require.True(t, existed)
		assert.NotEmpty(t, sums[filepath.Base(dst)])
	})

	t.Run("fresh extraction leaves sums.md5 absent if it never existed", func(t *testing.T) {
		dir := t.TempDir()
		videoPath := testutil.MakeVideoFileWithTone(t, dir, "video.mp4", "libx264", "aac")
		writeNFOFixture(t, videoPath, nfo)

		_, err := ExtractAudio(ctx, videoPath, nfo, ExtractAudioOptions{})
		require.NoError(t, err)

		_, existed, err := hasher.ReadSums(dir, hasher.SumsFilename)
		require.NoError(t, err)
		assert.False(t, existed)
	})

	t.Run("fresh extraction errors if a derived audio file already exists", func(t *testing.T) {
		dir := t.TempDir()
		videoPath := testutil.MakeVideoFileWithTone(t, dir, "video.mp4", "libx264", "aac")
		writeNFOFixture(t, videoPath, nfo)
		_, err := ExtractAudio(ctx, videoPath, nfo, ExtractAudioOptions{})
		require.NoError(t, err)

		_, err = ExtractAudio(ctx, videoPath, nfo, ExtractAudioOptions{})
		assert.Error(t, err)
	})

	t.Run("errors when nfo is missing title or artist", func(t *testing.T) {
		dir := t.TempDir()
		videoPath := testutil.MakeVideoFileWithTone(t, dir, "video.mp4", "libx264", "aac")
		_, err := ExtractAudio(ctx, videoPath, NFO{Artist: "Beyoncé"}, ExtractAudioOptions{})
		assert.Error(t, err)
	})

	t.Run("errors when retag and force are both set", func(t *testing.T) {
		dir := t.TempDir()
		videoPath := testutil.MakeVideoFileWithTone(t, dir, "video.mp4", "libx264", "aac")
		_, err := ExtractAudio(ctx, videoPath, nfo, ExtractAudioOptions{Retag: true, Force: true})
		assert.Error(t, err)
	})

	t.Run("errors on an unrecognized audio codec", func(t *testing.T) {
		dir := t.TempDir()
		// pcm_s16le is a real, valid audio codec ffprobe will happily
		// report (just not one transcode.ExtensionForCodec knows).
		videoPath := testutil.MakeVideoFileWithTone(t, dir, "video.mp4", "libx264", "pcm_s16le")
		writeNFOFixture(t, videoPath, nfo)

		_, err := ExtractAudio(ctx, videoPath, nfo, ExtractAudioOptions{})
		assert.Error(t, err)
	})

	t.Run("retag: rewrites tags without touching audio.src.md5 or ReplayGain", func(t *testing.T) {
		dir := t.TempDir()
		videoPath := testutil.MakeVideoFileWithTone(t, dir, "video.mp4", "libx264", "aac")
		writeNFOFixture(t, videoPath, nfo)

		dst, err := ExtractAudio(ctx, videoPath, nfo, ExtractAudioOptions{})
		require.NoError(t, err)

		beforeSums, _, err := hasher.ReadSums(dir, AudioSrcSumsFilename)
		require.NoError(t, err)
		beforeTags, err := taglib.ReadTags(dst)
		require.NoError(t, err)
		beforeGain := beforeTags["REPLAYGAIN_TRACK_GAIN"]

		updated := NFO{Title: "Crazy in Love (Remastered)", Artist: "Beyoncé"}
		got, err := ExtractAudio(ctx, videoPath, updated, ExtractAudioOptions{Retag: true})
		require.NoError(t, err)
		assert.Equal(t, dst, got)

		afterTags, err := taglib.ReadTags(dst)
		require.NoError(t, err)
		assert.Equal(t, []string{"Crazy in Love (Remastered)"}, afterTags[taglib.Title])
		assert.Equal(t, beforeGain, afterTags["REPLAYGAIN_TRACK_GAIN"])

		afterSums, _, err := hasher.ReadSums(dir, AudioSrcSumsFilename)
		require.NoError(t, err)
		assert.Equal(t, beforeSums, afterSums)
	})

	t.Run("retag errors when no derived audio exists yet", func(t *testing.T) {
		dir := t.TempDir()
		videoPath := testutil.MakeVideoFileWithTone(t, dir, "video.mp4", "libx264", "aac")
		writeNFOFixture(t, videoPath, nfo)

		_, err := ExtractAudio(ctx, videoPath, nfo, ExtractAudioOptions{Retag: true})
		assert.Error(t, err)
	})

	t.Run("force: re-extracts in place when the codec is unchanged", func(t *testing.T) {
		dir := t.TempDir()
		videoPath := testutil.MakeVideoFileWithTone(t, dir, "video.mp4", "libx264", "aac")
		writeNFOFixture(t, videoPath, nfo)

		dst, err := ExtractAudio(ctx, videoPath, nfo, ExtractAudioOptions{})
		require.NoError(t, err)

		got, err := ExtractAudio(ctx, videoPath, nfo, ExtractAudioOptions{Force: true})
		require.NoError(t, err)
		assert.Equal(t, dst, got)

		matches, err := DerivedAudioFiles(videoPath)
		require.NoError(t, err)
		assert.Equal(t, []string{dst}, matches)
	})

	t.Run("force: cleans up a stale-extension file when the source codec changed", func(t *testing.T) {
		dir := t.TempDir()
		videoPath := testutil.MakeVideoFileWithTone(t, dir, "video.mp4", "libx264", "aac")
		writeNFOFixture(t, videoPath, nfo)

		oldDst, err := ExtractAudio(ctx, videoPath, nfo, ExtractAudioOptions{})
		require.NoError(t, err)
		require.Equal(t, filepath.Join(dir, "video.m4a"), oldDst)
		require.NoError(t, hasher.Hash(dir, nil)) // simulate sums.md5 existing with the old entry

		// Replace the source video with one whose audio codec differs which
		// simulates a re-fetch that landed a different source stream.
		newVideoPath := testutil.MakeVideoFileWithTone(t, dir, "video.webm", "libvpx", "libopus")
		require.NoError(t, os.Remove(videoPath))
		require.NoError(t, os.Rename(newVideoPath, videoPath))

		newDst, err := ExtractAudio(ctx, videoPath, nfo, ExtractAudioOptions{Force: true})
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(dir, "video.opus"), newDst)

		assert.NoFileExists(t, oldDst)
		matches, err := DerivedAudioFiles(videoPath)
		require.NoError(t, err)
		assert.Equal(t, []string{newDst}, matches)

		sums, _, err := hasher.ReadSums(dir, hasher.SumsFilename)
		require.NoError(t, err)
		_, oldStillPresent := sums["video.m4a"]
		assert.False(t, oldStillPresent)
		assert.NotEmpty(t, sums["video.opus"])
	})

	t.Run("more than one existing derived audio file is always a hard error", func(t *testing.T) {
		dir := t.TempDir()
		videoPath := testutil.MakeVideoFileWithTone(t, dir, "video.mp4", "libx264", "aac")
		writeNFOFixture(t, videoPath, nfo)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "video.m4a"), []byte("d"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "video.opus"), []byte("d"), 0o644))

		_, err := ExtractAudio(ctx, videoPath, nfo, ExtractAudioOptions{})
		assert.Error(t, err)

		_, err = ExtractAudio(ctx, videoPath, nfo, ExtractAudioOptions{Force: true})
		assert.Error(t, err, "even --force must not silently guess which stale file to keep")
	})
}
