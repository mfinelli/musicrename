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
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/target"
	"github.com/mfinelli/musicrename/internal/testutil"
)

func testVideoSettings() target.VideoTranscodeSettings {
	return target.VideoTranscodeSettings{
		VideoBitrateKbps: 400,
		AudioBitrateKbps: 128,
		Fullscreen:       target.VideoScale{Width: 320, Height: 240},
		Widescreen:       target.VideoScale{Width: 320, Height: 176},
	}
}

func TestParseRatioString(t *testing.T) {
	t.Run("parses a clean W:H ratio", func(t *testing.T) {
		ratio, ok := parseRatioString("16:9")
		require.True(t, ok)
		assert.InDelta(t, 16.0/9.0, ratio, 0.0001)
	})

	t.Run("rejects N/A", func(t *testing.T) {
		_, ok := parseRatioString("N/A")
		assert.False(t, ok)
	})

	t.Run("rejects a zero denominator (division by zero)", func(t *testing.T) {
		_, ok := parseRatioString("0:0")
		assert.False(t, ok)
	})

	t.Run("rejects a zero numerator (meaningless as a real aspect ratio)", func(t *testing.T) {
		_, ok := parseRatioString("0:1")
		assert.False(t, ok)
	})

	t.Run("rejects a string with no colon", func(t *testing.T) {
		_, ok := parseRatioString("garbage")
		assert.False(t, ok)
	})
}

func TestChooseVideoScale(t *testing.T) {
	settings := testVideoSettings()

	t.Run("exactly 4:3 picks fullscreen", func(t *testing.T) {
		scale, err := ChooseVideoScale(4.0/3.0, settings)
		require.NoError(t, err)
		assert.Equal(t, settings.Fullscreen, scale)
	})

	t.Run("exactly 16:9 picks widescreen", func(t *testing.T) {
		scale, err := ChooseVideoScale(16.0/9.0, settings)
		require.NoError(t, err)
		assert.Equal(t, settings.Widescreen, scale)
	})

	t.Run("just below the midpoint picks fullscreen", func(t *testing.T) {
		scale, err := ChooseVideoScale(aspectRatioThreshold-0.01, settings)
		require.NoError(t, err)
		assert.Equal(t, settings.Fullscreen, scale)
	})

	t.Run("just above the midpoint picks widescreen", func(t *testing.T) {
		scale, err := ChooseVideoScale(aspectRatioThreshold+0.01, settings)
		require.NoError(t, err)
		assert.Equal(t, settings.Widescreen, scale)
	})

	t.Run("a slightly odd near-16:9 ratio still picks widescreen", func(t *testing.T) {
		scale, err := ChooseVideoScale(500.0/281.0, settings) // ~1.779
		require.NoError(t, err)
		assert.Equal(t, settings.Widescreen, scale)
	})

	t.Run("portrait/vertical video is refused, not force-fit", func(t *testing.T) {
		_, err := ChooseVideoScale(9.0/16.0, settings) // ~0.5625
		assert.Error(t, err)
	})

	t.Run("absurdly ultra-wide video is refused, not force-fit", func(t *testing.T) {
		_, err := ChooseVideoScale(3.5, settings)
		assert.Error(t, err)
	})

	t.Run("exactly at the min/max boundary is accepted, not refused", func(t *testing.T) {
		_, err := ChooseVideoScale(aspectRatioMin, settings)
		assert.NoError(t, err)
		_, err = ChooseVideoScale(aspectRatioMax, settings)
		assert.NoError(t, err)
	})
}

func TestProbeAspectRatio(t *testing.T) {
	t.Run("uses display_aspect_ratio when present", func(t *testing.T) {
		r := &fakeProbeRunner{stdout: "width=640\nheight=360\ndisplay_aspect_ratio=16:9\n"}
		ratio, err := probeAspectRatio(context.Background(), r, "src.mp4")
		require.NoError(t, err)
		assert.InDelta(t, 16.0/9.0, ratio, 0.0001)
	})

	t.Run("falls back to width/height when display_aspect_ratio is N/A", func(t *testing.T) {
		r := &fakeProbeRunner{stdout: "width=500\nheight=333\ndisplay_aspect_ratio=N/A\n"}
		ratio, err := probeAspectRatio(context.Background(), r, "src.mp4")
		require.NoError(t, err)
		assert.InDelta(t, 500.0/333.0, ratio, 0.0001)
	})

	t.Run("errors when nothing usable is present", func(t *testing.T) {
		r := &fakeProbeRunner{stdout: ""}
		_, err := probeAspectRatio(context.Background(), r, "src.mp4")
		assert.Error(t, err)
	})

	t.Run("propagates a runner failure", func(t *testing.T) {
		r := &fakeProbeRunner{err: errors.New("boom")}
		_, err := probeAspectRatio(context.Background(), r, "src.mp4")
		assert.Error(t, err)
	})
}

func TestTranscodeVideo(t *testing.T) {
	t.Run("builds the expected ffmpeg arguments for a widescreen source", func(t *testing.T) {
		pr := &fakeProbeRunner{stdout: "width=640\nheight=360\ndisplay_aspect_ratio=16:9\n"}
		r := &fakeRunner{}
		err := transcodeVideo(context.Background(), pr, r, "src.mp4", "dst.mpg", testVideoSettings())
		require.NoError(t, err)
		require.True(t, r.called)

		assert.Equal(t, []string{
			"-y",
			"-i", "src.mp4",
			"-vcodec", "mpeg2video",
			"-vf", "scale=320:176",
			"-b:v", "400k",
			"-acodec", "libmp3lame",
			"-b:a", "128k",
			"-ar", "44100",
			"-f", "mpeg",
			"dst.mpg",
		}, r.gotArgs)
	})

	t.Run("builds the expected ffmpeg arguments for a fullscreen source", func(t *testing.T) {
		pr := &fakeProbeRunner{stdout: "width=320\nheight=240\ndisplay_aspect_ratio=4:3\n"}
		r := &fakeRunner{}
		err := transcodeVideo(context.Background(), pr, r, "src.mp4", "dst.mpg", testVideoSettings())
		require.NoError(t, err)
		assert.Contains(t, r.gotArgs, "scale=320:240")
	})

	t.Run("never calls ffmpeg when the aspect ratio is refused", func(t *testing.T) {
		pr := &fakeProbeRunner{stdout: "width=200\nheight=400\ndisplay_aspect_ratio=1:2\n"} // portrait
		r := &fakeRunner{}
		err := transcodeVideo(context.Background(), pr, r, "src.mp4", "dst.mpg", testVideoSettings())
		assert.Error(t, err)
		assert.False(t, r.called, "must not run ffmpeg on a source it already refused")
	})

	t.Run("propagates a probe failure without calling ffmpeg", func(t *testing.T) {
		pr := &fakeProbeRunner{err: errors.New("boom")}
		r := &fakeRunner{}
		err := transcodeVideo(context.Background(), pr, r, "src.mp4", "dst.mpg", testVideoSettings())
		assert.Error(t, err)
		assert.False(t, r.called)
	})

	t.Run("propagates an ffmpeg failure", func(t *testing.T) {
		pr := &fakeProbeRunner{stdout: "width=640\nheight=360\ndisplay_aspect_ratio=16:9\n"}
		r := &fakeRunner{err: errors.New("boom")}
		err := transcodeVideo(context.Background(), pr, r, "src.mp4", "dst.mpg", testVideoSettings())
		assert.Error(t, err)
	})
}

// makeTestVideo generates a short synthetic video at the given pixel
// dimensions via ffmpeg's lavfi testsrc, for exercising TranscodeVideo
// against real ffmpeg/ffprobe rather than only a fake runner.
func makeTestVideo(t *testing.T, dir, name string, width, height int) string {
	t.Helper()

	path := filepath.Join(dir, name)
	out, err := exec.Command(
		"ffmpeg", "-y",
		"-f", "lavfi", "-i", fmt.Sprintf("testsrc=duration=1:size=%dx%d:rate=5", width, height),
		"-f", "lavfi", "-i", "anullsrc=r=44100:cl=stereo",
		"-c:v", "libx264", "-c:a", "aac", "-shortest", "-t", "1",
		path,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("makeTestVideo: ffmpeg failed: %v\n%s", err, out)
	}
	return path
}

func TestTranscodeVideoReal(t *testing.T) {
	t.Run("produces a real MPEG-2/MPEG-PS file from a widescreen source", func(t *testing.T) {
		dir := t.TempDir()
		src := makeTestVideo(t, dir, "src.mp4", 640, 360)
		dst := filepath.Join(dir, "dst.mpg")

		err := TranscodeVideo(context.Background(), src, dst, testVideoSettings())
		require.NoError(t, err)

		outStr := testutil.ProbeText(t, dst, "stream=codec_name,codec_type,width,height")
		assert.Contains(t, outStr, "codec_name=mpeg2video")
		assert.Contains(t, outStr, "codec_name=mp3")
		assert.Contains(t, outStr, "width=320")
		assert.Contains(t, outStr, "height=176")
	})

	t.Run("produces a real MPEG-2/MPEG-PS file from a fullscreen source", func(t *testing.T) {
		dir := t.TempDir()
		src := makeTestVideo(t, dir, "src.mp4", 320, 240)
		dst := filepath.Join(dir, "dst.mpg")

		err := TranscodeVideo(context.Background(), src, dst, testVideoSettings())
		require.NoError(t, err)

		outStr := testutil.ProbeText(t, dst, "stream=width,height")
		assert.Contains(t, outStr, "width=320")
		assert.Contains(t, outStr, "height=240")
	})

	t.Run("refuses a portrait source without ever calling ffmpeg on it", func(t *testing.T) {
		dir := t.TempDir()
		src := makeTestVideo(t, dir, "src.mp4", 360, 640)
		dst := filepath.Join(dir, "dst.mpg")

		err := TranscodeVideo(context.Background(), src, dst, testVideoSettings())
		assert.Error(t, err)
		assert.NoFileExists(t, dst)
	})
}
