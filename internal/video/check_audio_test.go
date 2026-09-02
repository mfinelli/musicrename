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
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.senan.xyz/taglib"

	"github.com/mfinelli/musicrename/internal/hasher"
)

func TestDerivedAudioTagDriftMessage(t *testing.T) {
	nfo := NFO{Artist: "Beyoncé", Title: "Crazy in Love"}

	t.Run("matching title and artist: no drift", func(t *testing.T) {
		dir := t.TempDir()
		path := makeAudioFile(t, dir, "track.m4a")
		require.NoError(t, WriteDerivedAudioTags(path, nfo))

		msg, err := derivedAudioTagDriftMessage(path, nfo)
		require.NoError(t, err)
		assert.Empty(t, msg)
	})

	t.Run("matching title, artist, album, and year: no drift", func(t *testing.T) {
		dir := t.TempDir()
		full := NFO{Artist: "Beyoncé", Title: "Crazy in Love", Album: "Dangerously in Love", Year: "2003"}
		path := makeAudioFile(t, dir, "track.m4a")
		require.NoError(t, WriteDerivedAudioTags(path, full))

		msg, err := derivedAudioTagDriftMessage(path, full)
		require.NoError(t, err)
		assert.Empty(t, msg)
	})

	t.Run("a foreign tag (ReplayGain) never counts as drift", func(t *testing.T) {
		dir := t.TempDir()
		path := makeAudioFile(t, dir, "track.m4a")
		require.NoError(t, WriteDerivedAudioTags(path, nfo))
		require.NoError(t, taglib.WriteTags(path, map[string][]string{
			"REPLAYGAIN_TRACK_GAIN": {"3.75 dB"},
		}, taglib.WriteOption(0)))

		msg, err := derivedAudioTagDriftMessage(path, nfo)
		require.NoError(t, err)
		assert.Empty(t, msg)
	})

	t.Run("a different title is drift", func(t *testing.T) {
		dir := t.TempDir()
		path := makeAudioFile(t, dir, "track.m4a")
		require.NoError(t, WriteDerivedAudioTags(path, nfo))

		msg, err := derivedAudioTagDriftMessage(path, NFO{Artist: "Beyoncé", Title: "Halo"})
		require.NoError(t, err)
		assert.NotEmpty(t, msg)
	})

	t.Run("a different artist is drift", func(t *testing.T) {
		dir := t.TempDir()
		path := makeAudioFile(t, dir, "track.m4a")
		require.NoError(t, WriteDerivedAudioTags(path, nfo))

		msg, err := derivedAudioTagDriftMessage(path, NFO{Artist: "Someone Else", Title: "Crazy in Love"})
		require.NoError(t, err)
		assert.NotEmpty(t, msg)
	})

	t.Run("nfo gaining an album the file doesn't have yet is drift", func(t *testing.T) {
		dir := t.TempDir()
		path := makeAudioFile(t, dir, "track.m4a")
		require.NoError(t, WriteDerivedAudioTags(path, nfo)) // no album written

		msg, err := derivedAudioTagDriftMessage(path, NFO{Artist: "Beyoncé", Title: "Crazy in Love", Album: "Dangerously in Love"})
		require.NoError(t, err)
		assert.NotEmpty(t, msg)
	})

	t.Run("a stale album the nfo no longer has is drift", func(t *testing.T) {
		dir := t.TempDir()
		path := makeAudioFile(t, dir, "track.m4a")
		require.NoError(t, WriteDerivedAudioTags(path, NFO{
			Artist: "Beyoncé", Title: "Crazy in Love", Album: "Dangerously in Love",
		}))

		// nfo dropped Album since the file was last tagged.
		msg, err := derivedAudioTagDriftMessage(path, nfo)
		require.NoError(t, err)
		assert.NotEmpty(t, msg)
	})

	t.Run("errors on a nonexistent file", func(t *testing.T) {
		_, err := derivedAudioTagDriftMessage("/nonexistent/track.m4a", nfo)
		assert.Error(t, err)
	})
}

func TestDerivedAudioContentDriftMessage(t *testing.T) {
	videoPath := "/library/video.mp4" // never read; only its Base() is used

	hashA := fmt.Sprintf("%032x", 1)
	hashB := fmt.Sprintf("%032x", 2)

	t.Run("no audio.src.md5 at all", func(t *testing.T) {
		dir := t.TempDir()

		msg, err := derivedAudioContentDriftMessage(dir, videoPath)
		require.NoError(t, err)
		assert.Equal(t, "derived audio exists but no audio.src.md5 sidecar was found", msg)
	})

	t.Run("audio.src.md5 exists but has no entry for the video's current filename", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, hasher.WriteSums(dir, AudioSrcSumsFilename, map[string]string{
			"some-other-file.mp4": hashA,
		}))

		msg, err := derivedAudioContentDriftMessage(dir, videoPath)
		require.NoError(t, err)
		assert.Contains(t, msg, "can't verify")
	})

	t.Run("audio.src.md5 has an entry but sums.md5 doesn't exist", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, hasher.WriteSums(dir, AudioSrcSumsFilename, map[string]string{
			"video.mp4": hashA,
		}))

		msg, err := derivedAudioContentDriftMessage(dir, videoPath)
		require.NoError(t, err)
		assert.Contains(t, msg, "sums.md5 is missing")
	})

	t.Run("sums.md5 exists but has no entry for the video", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, hasher.WriteSums(dir, AudioSrcSumsFilename, map[string]string{
			"video.mp4": hashA,
		}))
		require.NoError(t, hasher.WriteSums(dir, hasher.SumsFilename, map[string]string{
			"unrelated.txt": hashB,
		}))

		msg, err := derivedAudioContentDriftMessage(dir, videoPath)
		require.NoError(t, err)
		assert.Contains(t, msg, "sums.md5 has no entry for the video")
	})

	t.Run("matching hashes: no drift", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, hasher.WriteSums(dir, AudioSrcSumsFilename, map[string]string{
			"video.mp4": hashA,
		}))
		require.NoError(t, hasher.WriteSums(dir, hasher.SumsFilename, map[string]string{
			"video.mp4": hashA,
		}))

		msg, err := derivedAudioContentDriftMessage(dir, videoPath)
		require.NoError(t, err)
		assert.Empty(t, msg)
	})

	t.Run("mismatched hashes: content drift", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, hasher.WriteSums(dir, AudioSrcSumsFilename, map[string]string{
			"video.mp4": hashA,
		}))
		require.NoError(t, hasher.WriteSums(dir, hasher.SumsFilename, map[string]string{
			"video.mp4": hashB,
		}))

		msg, err := derivedAudioContentDriftMessage(dir, videoPath)
		require.NoError(t, err)
		assert.Contains(t, msg, "derived audio may be stale")
	})

	t.Run("videoPath's directory component is ignored; only dir is consulted", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, hasher.WriteSums(dir, AudioSrcSumsFilename, map[string]string{
			"video.mp4": hashA,
		}))
		require.NoError(t, hasher.WriteSums(dir, hasher.SumsFilename, map[string]string{
			"video.mp4": hashA,
		}))

		msg, err := derivedAudioContentDriftMessage(dir, filepath.Join("some", "other", "path", "video.mp4"))
		require.NoError(t, err)
		assert.Empty(t, msg)
	})
}
