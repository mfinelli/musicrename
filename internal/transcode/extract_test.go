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

package transcode

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/testutil"
)

// fakeProbeRunner records the args it was invoked with and returns
// canned stdout/error, rather than actually shelling out to ffprobe.
type fakeProbeRunner struct {
	called  bool
	gotArgs []string
	stdout  string
	err     error
}

func (f *fakeProbeRunner) Run(_ context.Context, args []string) (string, error) {
	f.called = true
	f.gotArgs = args
	return f.stdout, f.err
}

func TestExtensionForCodec(t *testing.T) {
	t.Run("known codecs resolve to the expected extension", func(t *testing.T) {
		cases := map[string]string{
			"aac":    ".m4a",
			"alac":   ".m4a",
			"opus":   ".opus",
			"vorbis": ".ogg",
			"mp3":    ".mp3",
			"flac":   ".flac",
		}
		for codec, want := range cases {
			ext, ok := ExtensionForCodec(codec)
			assert.True(t, ok, "expected %q to be recognized", codec)
			assert.Equal(t, want, ext)
		}
	})

	t.Run("an unrecognized codec returns false, not a guess", func(t *testing.T) {
		_, ok := ExtensionForCodec("pcm_s16le")
		assert.False(t, ok)
	})
}

func TestDerivedAudioExtensions(t *testing.T) {
	t.Run("deduplicates codecs sharing an extension", func(t *testing.T) {
		exts := DerivedAudioExtensions()
		// aac and alac both map to .m4a; it should appear once.
		count := 0
		for _, e := range exts {
			if e == ".m4a" {
				count++
			}
		}
		assert.Equal(t, 1, count)
	})

	t.Run("every mapped extension is present exactly once", func(t *testing.T) {
		exts := DerivedAudioExtensions()
		want := []string{".flac", ".m4a", ".mp3", ".ogg", ".opus"}
		assert.ElementsMatch(t, want, exts)
	})
}

func TestIsDerivedAudioExt(t *testing.T) {
	t.Run("recognizes a known extension", func(t *testing.T) {
		assert.True(t, IsDerivedAudioExt(".m4a"))
		assert.True(t, IsDerivedAudioExt(".opus"))
	})

	t.Run("rejects an unrecognized extension", func(t *testing.T) {
		assert.False(t, IsDerivedAudioExt(".wav"))
		assert.False(t, IsDerivedAudioExt(".mp4"))
	})
}

func TestProbeAudioCodec(t *testing.T) {
	t.Run("builds the expected ffprobe arguments", func(t *testing.T) {
		r := &fakeProbeRunner{stdout: "aac\n"}
		codec, err := probeAudioCodec(context.Background(), r, "src.mp4")
		require.NoError(t, err)
		require.True(t, r.called)

		assert.Equal(t, "aac", codec)
		assert.Equal(t, []string{
			"-v", "error",
			"-select_streams", "a:0",
			"-show_entries", "stream=codec_name",
			"-of", "default=noprint_wrappers=1:nokey=1",
			"src.mp4",
		}, r.gotArgs)
	})

	t.Run("trims whitespace from ffprobe's output", func(t *testing.T) {
		r := &fakeProbeRunner{stdout: "opus\n"}
		codec, err := probeAudioCodec(context.Background(), r, "src.webm")
		require.NoError(t, err)
		assert.Equal(t, "opus", codec)
	})

	t.Run("empty stdout (no audio stream) is an error, not a zero value", func(t *testing.T) {
		r := &fakeProbeRunner{stdout: ""}
		_, err := probeAudioCodec(context.Background(), r, "silent.mp4")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no audio stream")
	})

	t.Run("whitespace-only stdout is treated the same as empty", func(t *testing.T) {
		r := &fakeProbeRunner{stdout: "  \n"}
		_, err := probeAudioCodec(context.Background(), r, "silent.mp4")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no audio stream")
	})

	t.Run("propagates a runner failure", func(t *testing.T) {
		r := &fakeProbeRunner{err: errors.New("boom")}
		_, err := probeAudioCodec(context.Background(), r, "src.mp4")
		assert.Error(t, err)
	})
}

// probeStreamTypes returns the codec_type ("audio", "video", ...) of every
// stream in path via a direct real ffprobe call. A small test-only
// verification helper (confirming -vn actually dropped the video stream,
// not just that RemuxAudio didn't error) and not something extract.go itself
// needs, so it stays here rather than becoming new production API surface.
func probeStreamTypes(t *testing.T, path string) []string {
	t.Helper()

	out := testutil.ProbeText(t, path, "stream=codec_type", "nokey=1")

	var types []string
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if line != "" {
			types = append(types, line)
		}
	}
	return types
}

// TestProbeAudioCodecReal exercises ProbeAudioCodec against real ffprobe
// output rather than a fake runner's canned response (the fake-runner tests
// above confirm the argument-building/error-handling logic; these confirm
// the actual assumptions that logic depends on).
func TestProbeAudioCodecReal(t *testing.T) {
	t.Run("reports aac for an mp4 with an AAC audio stream", func(t *testing.T) {
		dir := t.TempDir()
		path := testutil.MakeVideoFile(t, dir, "src.mp4", 320, 240, "libx264", "aac")

		codec, err := ProbeAudioCodec(context.Background(), path)
		require.NoError(t, err)
		assert.Equal(t, "aac", codec)
	})

	t.Run("reports opus for a webm with an Opus audio stream", func(t *testing.T) {
		dir := t.TempDir()
		path := testutil.MakeVideoFile(t, dir, "src.webm", 320, 240, "libvpx", "libopus")

		codec, err := ProbeAudioCodec(context.Background(), path)
		require.NoError(t, err)
		assert.Equal(t, "opus", codec)
	})

	t.Run("errors on a video with no audio stream at all", func(t *testing.T) {
		dir := t.TempDir()
		path := testutil.MakeVideoFile(t, dir, "silent.mp4", 320, 240, "libx264", "")

		_, err := ProbeAudioCodec(context.Background(), path)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no audio stream")
	})
}

// TestRemuxAudioReal exercises RemuxAudio against real ffmpeg, confirming
// the output is genuinely audio-only (the video stream was actually
// dropped, not just that the call didn't error) and that the codec really
// did pass through unchanged (a true remux, not a silent re-encode).
func TestRemuxAudioReal(t *testing.T) {
	t.Run("extracts an AAC stream into a playable, audio-only .m4a", func(t *testing.T) {
		dir := t.TempDir()
		src := testutil.MakeVideoFile(t, dir, "src.mp4", 320, 240, "libx264", "aac")
		dst := filepath.Join(dir, "src.m4a")

		require.NoError(t, RemuxAudio(context.Background(), src, dst))

		streams := probeStreamTypes(t, dst)
		require.Len(t, streams, 1)
		assert.Equal(t, "audio", streams[0])

		codec, err := ProbeAudioCodec(context.Background(), dst)
		require.NoError(t, err)
		assert.Equal(t, "aac", codec)
	})

	t.Run("extracts an Opus stream into a playable, audio-only .opus", func(t *testing.T) {
		dir := t.TempDir()
		src := testutil.MakeVideoFile(t, dir, "src.webm", 320, 240, "libvpx", "libopus")
		dst := filepath.Join(dir, "src.opus")

		require.NoError(t, RemuxAudio(context.Background(), src, dst))

		streams := probeStreamTypes(t, dst)
		require.Len(t, streams, 1)
		assert.Equal(t, "audio", streams[0])

		codec, err := ProbeAudioCodec(context.Background(), dst)
		require.NoError(t, err)
		assert.Equal(t, "opus", codec)
	})
}

func TestRemuxAudio(t *testing.T) {
	t.Run("builds the expected ffmpeg arguments", func(t *testing.T) {
		r := &fakeRunner{}
		err := remuxAudio(context.Background(), r, "video.mp4", "video.m4a")
		require.NoError(t, err)
		require.True(t, r.called)

		assert.Equal(t, []string{
			"-y",
			"-i", "video.mp4",
			"-map_metadata", "-1",
			"-vn",
			"-c:a", "copy",
			"video.m4a",
		}, r.gotArgs)
	})

	t.Run("propagates a runner failure", func(t *testing.T) {
		r := &fakeRunner{err: errors.New("boom")}
		err := remuxAudio(context.Background(), r, "video.mp4", "video.m4a")
		assert.Error(t, err)
	})
}
