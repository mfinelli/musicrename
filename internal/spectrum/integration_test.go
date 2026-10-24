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

package spectrum

import (
	"context"
	"image"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/testutil"
)

// These tests run the whole pipeline (ffprobe, ffmpeg, analysis) against real
// files, including ones deliberately made to be fake. They require ffmpeg and
// ffprobe on PATH, with the libmp3lame encoder, like the rest of the suite.

func TestAnalyzeRealFiles(t *testing.T) {
	ctx := context.Background()
	th := DefaultThresholds()
	dir := t.TempDir()

	genuine := testutil.MakeNoiseFlac(t, dir, "genuine.flac", 44100, 12)

	t.Run("a genuine lossless file is ok", func(t *testing.T) {
		res, err := Analyze(ctx, genuine, th)

		require.NoError(t, err)
		assert.Equal(t, VerdictOK, res.Verdict)
		assert.Nil(t, res.Cliff)
		assert.Equal(t, StreamInfo{
			Codec: "flac", SampleRate: 44100, Channels: 2,
			ChannelLayout: "stereo", BitsPerSample: 16,
		}, res.Info)
		assert.Equal(t, 22050.0, res.BandwidthHz)
	})

	t.Run("a 128k MP3 stored as FLAC is a likely transcode", func(t *testing.T) {
		path := testutil.MakeLossyTranscodeFlac(t, genuine, dir, "from-mp3.flac", ".mp3",
			"-c:a", "libmp3lame", "-b:a", "128k")

		res, err := Analyze(ctx, path, th)

		require.NoError(t, err)
		assert.Equal(t, VerdictLikelyFake, res.Verdict)
		require.NotNil(t, res.Cliff)
		assert.InDelta(t, 16500, res.Cliff.CutoffHz, 1000, "LAME's 128k lowpass is around 16-17 kHz")
	})

	t.Run("a 128k AAC stored as FLAC is a likely transcode", func(t *testing.T) {
		path := testutil.MakeLossyTranscodeFlac(t, genuine, dir, "from-aac.flac", ".m4a",
			"-c:a", "aac", "-b:a", "128k")

		res, err := Analyze(ctx, path, th)

		require.NoError(t, err)
		assert.Equal(t, VerdictLikelyFake, res.Verdict)
		require.NotNil(t, res.Cliff)
		assert.InDelta(t, 17500, res.Cliff.CutoffHz, 1500)
	})

	t.Run("a CD-rate file upsampled to 96 kHz is flagged", func(t *testing.T) {
		path := testutil.MakeHiResFlac(t, genuine, dir, "upsampled.flac", 96000)

		res, err := Analyze(ctx, path, th)

		require.NoError(t, err)
		assert.Equal(t, 96000, res.Info.SampleRate)
		assert.Equal(t, 24, res.Info.BitsPerSample)
		assert.Equal(t, VerdictLikelyFake, res.Verdict)
		require.NotEmpty(t, res.Findings)
		assert.Equal(t, KindUpsampled, res.Findings[0].Kind)
	})

	t.Run("a 16-bit file padded into 24 bits is flagged", func(t *testing.T) {
		path := testutil.MakeHiResFlac(t, genuine, dir, "padded.flac", 0)

		res, err := Analyze(ctx, path, th)

		require.NoError(t, err)
		assert.Equal(t, 24, res.Info.BitsPerSample)
		assert.Equal(t, 16, res.EffectiveBits)
		assert.Equal(t, VerdictSuspect, res.Verdict)
		require.Len(t, res.Findings, 1)
		assert.Equal(t, KindBitDepth, res.Findings[0].Kind)
	})

	t.Run("a lossy file is refused", func(t *testing.T) {
		mp3 := testutil.MakeAudioFile(t, dir, "lossy.mp3", nil)

		res, err := Analyze(ctx, mp3, th)

		assert.Nil(t, res)
		var nle *NotLosslessError
		require.ErrorAs(t, err, &nle)
		assert.Equal(t, "mp3", nle.Codec)
	})

	t.Run("a missing file is an error", func(t *testing.T) {
		_, err := Analyze(ctx, dir+"/does-not-exist.flac", th)
		assert.Error(t, err)
	})
}

func TestAnalyzeStopsWhenCancelled(t *testing.T) {
	dir := t.TempDir()
	path := testutil.MakeNoiseFlac(t, dir, "x.flac", 44100, 2)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Analyze(ctx, path, DefaultThresholds())

	assert.Error(t, err)
}

func TestWriteAnnotatedSpectrogramRealFiles(t *testing.T) {
	ctx := context.Background()
	th := DefaultThresholds()
	dir := t.TempDir()

	genuine := testutil.MakeNoiseFlac(t, dir, "genuine.flac", 44100, 8)

	render := func(t *testing.T, src string) (*Result, *image.RGBA) {
		t.Helper()
		res, err := Analyze(ctx, src, th)
		require.NoError(t, err)

		dst := filepath.Join(dir, filepath.Base(src)+".png")
		require.NoError(t, WriteAnnotatedSpectrogram(ctx, src, dst, res, th, AnnotateOptions{}))
		return res, decodePNG(t, dst)
	}

	t.Run("the cutoff marker sits on the edge of the content in a transcode", func(t *testing.T) {
		src := testutil.MakeLossyTranscodeFlac(t, genuine, dir, "from-mp3.flac", ".mp3",
			"-c:a", "libmp3lame", "-b:a", "128k")

		res, img := render(t, src)

		require.NotNil(t, res.Cliff)
		y := freqY(res.Cliff.CutoffHz, 22050)
		x := marginL + plotW/2
		assert.Equal(t, colCutoff, img.RGBAAt(x, y))
		// Loud just below the marker and quiet just above it: the line is
		// where the picture's content actually ends, not merely where the
		// code says it is.
		below, above := luma(img.RGBAAt(x, y+10)), luma(img.RGBAAt(x, y-10))
		assert.Greater(t, below, above+40)
	})

	t.Run("a genuine file has no marker", func(t *testing.T) {
		_, img := render(t, genuine)

		assert.Equal(t, 0, count(img, plotRect, colCutoff))
	})

	t.Run("an upsampled file shows the CD bandwidth marker", func(t *testing.T) {
		src := testutil.MakeHiResFlac(t, genuine, dir, "upsampled.flac", 96000)

		res, img := render(t, src)

		assert.Equal(t, 96000, res.Info.SampleRate)
		assert.Greater(t, count(img, plotRect, colCDBand), 300)
		assert.Greater(t, count(img, plotRect, colCutoff), 2*plotW-1)
	})
}
