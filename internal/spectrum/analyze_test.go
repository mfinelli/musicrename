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
	"bytes"
	"encoding/binary"
	"math"
	"math/rand"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shaped returns n samples of noise whose spectrum is multiplied by gain(hz),
// scaled to a peak of 0.9. It filters white noise in the frequency domain,
// which gives exact control over where the content ends.
func shaped(rate, n int, seed int64, gain func(hz float64) float64) []float64 {
	rng := rand.New(rand.NewSource(seed))
	re := make([]float64, n)
	im := make([]float64, n)
	for i := range re {
		re[i] = rng.NormFloat64()
	}

	plan := newFFTPlan(n)
	plan.transform(re, im)

	for k := 0; k <= n/2; k++ {
		g := gain(float64(k) * float64(rate) / float64(n))
		re[k] *= g
		im[k] *= g
		if k > 0 && k < n/2 {
			re[n-k] *= g
			im[n-k] *= g
		}
	}

	// Inverse transform via conjugation: x = conj(FFT(conj(X))) / n.
	for i := range im {
		im[i] = -im[i]
	}
	plan.transform(re, im)

	var peak float64
	for _, v := range re {
		peak = math.Max(peak, math.Abs(v))
	}
	for i := range re {
		re[i] *= 0.9 / peak
	}
	return re
}

// music is a gain function with a spectrum shaped roughly like recorded music:
// flat to 500 Hz, then falling at about 10 dB per octave. Content stops dead at
// cutoff Hz (or runs to the top of the band when cutoff is zero).
func music(cutoff float64) func(float64) float64 {
	return func(hz float64) float64 {
		switch {
		case hz < 30:
			return 0
		case cutoff > 0 && hz > cutoff:
			return 0
		case hz > 500:
			return math.Pow(hz/500, -1.7)
		default:
			return 1
		}
	}
}

// quantize rounds samples to a signed integer grid of the given bit depth, as
// a real file of that depth would store them.
func quantize(x []float64, bits int) []float64 {
	scale := math.Ldexp(1, bits-1)
	out := make([]float64, len(x))
	for i, v := range x {
		out[i] = math.Round(v*scale) / scale
	}
	return out
}

// pcm interleaves channels into the 32-bit float stream AnalyzeStream reads.
func pcm(chans ...[]float64) []byte {
	n := len(chans[0])
	out := make([]byte, 0, n*len(chans)*4)
	for i := range n {
		for _, ch := range chans {
			out = binary.LittleEndian.AppendUint32(out, math.Float32bits(float32(ch[i])))
		}
	}
	return out
}

const synthSamples = 1 << 18 // ~6 s at 44.1 kHz

// musicKey identifies a signal made by musicSignal.
type musicKey struct {
	rate, n int
	seed    int64
	cutoff  float64
}

var musicCache = map[musicKey][]float64{}

// musicSignal returns shaped(rate, n, seed, music(cutoff)), generating each
// distinct signal only once per test run: building them is by far the slowest
// part of these tests. Callers must not modify the returned slice (quantize
// returns a copy).
func musicSignal(rate, n int, seed int64, cutoff float64) []float64 {
	k := musicKey{rate, n, seed, cutoff}
	if x, ok := musicCache[k]; ok {
		return x
	}
	x := shaped(rate, n, seed, music(cutoff))
	musicCache[k] = x
	return x
}

func stereo44(bits int) StreamInfo {
	return StreamInfo{
		Codec: "flac", SampleRate: 44100, Channels: 2,
		ChannelLayout: "stereo", BitsPerSample: bits,
	}
}

// analyzeSynth runs channels through AnalyzeStream with default thresholds.
func analyzeSynth(t *testing.T, info StreamInfo, chans ...[]float64) *Result {
	t.Helper()
	res, err := AnalyzeStream(bytes.NewReader(pcm(chans...)), info, DefaultThresholds())
	require.NoError(t, err)
	return res
}

func TestAnalyzeStream(t *testing.T) {
	t.Run("a genuine full-band file is ok", func(t *testing.T) {
		l := quantize(musicSignal(44100, synthSamples, 1, 0), 16)
		r := quantize(musicSignal(44100, synthSamples, 2, 0), 16)

		res := analyzeSynth(t, stereo44(16), l, r)

		assert.Equal(t, VerdictOK, res.Verdict)
		assert.Nil(t, res.Cliff)
		assert.Empty(t, res.Findings)
		assert.Equal(t, 22050.0, res.BandwidthHz)
		assert.Equal(t, 16, res.EffectiveBits)
		assert.Equal(t, 4096, res.Window)
		assert.Len(t, res.Channels, 2)
	})

	t.Run("a lowpass at 16 kHz is a likely transcode", func(t *testing.T) {
		l := quantize(musicSignal(44100, synthSamples, 1, 16000), 16)
		r := quantize(musicSignal(44100, synthSamples, 2, 16000), 16)

		res := analyzeSynth(t, stereo44(16), l, r)

		assert.Equal(t, VerdictLikelyFake, res.Verdict)
		require.NotNil(t, res.Cliff)
		assert.InDelta(t, 16000, res.Cliff.CutoffHz, 500)
		assert.Greater(t, res.Cliff.DropDB, 40.0)
		assert.InDelta(t, res.Cliff.CutoffHz, res.BandwidthHz, 1e-9)
		require.Len(t, res.Findings, 1)
		assert.Equal(t, KindCutoff, res.Findings[0].Kind)
	})

	t.Run("a lowpass at 20.3 kHz is only suspect", func(t *testing.T) {
		l := quantize(musicSignal(44100, synthSamples, 1, 20300), 16)
		r := quantize(musicSignal(44100, synthSamples, 2, 20300), 16)

		res := analyzeSynth(t, stereo44(16), l, r)

		assert.Equal(t, VerdictSuspect, res.Verdict)
		require.NotNil(t, res.Cliff)
		assert.InDelta(t, 20300, res.Cliff.CutoffHz, 500)
	})

	t.Run("an anti-alias roll-off near the top of the band is ok", func(t *testing.T) {
		l := quantize(musicSignal(44100, synthSamples, 1, 21500), 16)
		r := quantize(musicSignal(44100, synthSamples, 2, 21500), 16)

		res := analyzeSynth(t, stereo44(16), l, r)

		assert.Equal(t, VerdictOK, res.Verdict)
	})

	t.Run("genuine hi-res is ok", func(t *testing.T) {
		info := StreamInfo{Codec: "flac", SampleRate: 96000, Channels: 2, ChannelLayout: "stereo", BitsPerSample: 24}
		l := quantize(musicSignal(96000, synthSamples, 1, 0), 24)
		r := quantize(musicSignal(96000, synthSamples, 2, 0), 24)

		res := analyzeSynth(t, info, l, r)

		assert.Equal(t, VerdictOK, res.Verdict)
		assert.Equal(t, 8192, res.Window)
		assert.Equal(t, 24, res.EffectiveBits)
	})

	t.Run("hi-res limited to CD bandwidth is flagged as upsampled", func(t *testing.T) {
		info := StreamInfo{Codec: "flac", SampleRate: 96000, Channels: 2, ChannelLayout: "stereo", BitsPerSample: 24}
		l := quantize(musicSignal(96000, synthSamples, 1, 22050), 24)
		r := quantize(musicSignal(96000, synthSamples, 2, 22050), 24)

		res := analyzeSynth(t, info, l, r)

		assert.Equal(t, VerdictLikelyFake, res.Verdict)
		require.Len(t, res.Findings, 1)
		assert.Equal(t, KindUpsampled, res.Findings[0].Kind)
		assert.InDelta(t, 22050, res.Cliff.CutoffHz, 700)
	})

	t.Run("16-bit audio in a 24-bit container is flagged", func(t *testing.T) {
		l := quantize(musicSignal(44100, synthSamples, 1, 0), 16)
		r := quantize(musicSignal(44100, synthSamples, 2, 0), 16)

		res := analyzeSynth(t, stereo44(24), l, r)

		assert.Equal(t, VerdictSuspect, res.Verdict)
		assert.Equal(t, 16, res.EffectiveBits)
		require.Len(t, res.Findings, 1)
		assert.Equal(t, KindBitDepth, res.Findings[0].Kind)
	})

	t.Run("real 24-bit audio is not flagged", func(t *testing.T) {
		l := quantize(musicSignal(44100, synthSamples, 1, 0), 24)
		r := quantize(musicSignal(44100, synthSamples, 2, 0), 24)

		res := analyzeSynth(t, stereo44(24), l, r)

		assert.Equal(t, VerdictOK, res.Verdict)
		assert.Equal(t, 24, res.EffectiveBits)
	})

	t.Run("a transcode and padded bits are both reported", func(t *testing.T) {
		l := quantize(musicSignal(44100, synthSamples, 1, 16000), 16)
		r := quantize(musicSignal(44100, synthSamples, 2, 16000), 16)

		res := analyzeSynth(t, stereo44(24), l, r)

		assert.Equal(t, VerdictLikelyFake, res.Verdict, "the worst finding wins")
		assert.Len(t, res.Findings, 2)
	})

	t.Run("mono", func(t *testing.T) {
		info := StreamInfo{Codec: "flac", SampleRate: 44100, Channels: 1, ChannelLayout: "mono", BitsPerSample: 16}
		m := quantize(musicSignal(44100, synthSamples, 1, 16000), 16)

		res := analyzeSynth(t, info, m)

		assert.Equal(t, VerdictLikelyFake, res.Verdict)
		assert.Len(t, res.Channels, 1)
	})

	t.Run("one full-band channel shows the file is not band-limited", func(t *testing.T) {
		l := quantize(musicSignal(44100, synthSamples, 1, 16000), 16)
		r := quantize(musicSignal(44100, synthSamples, 2, 0), 16)

		res := analyzeSynth(t, stereo44(16), l, r)

		assert.Equal(t, VerdictOK, res.Verdict)
		assert.Equal(t, 1, res.Best)
		assert.NotNil(t, res.Channels[0].Cliff)
		assert.Nil(t, res.Channels[1].Cliff)
	})

	t.Run("a silent channel is skipped, not mistaken for full band", func(t *testing.T) {
		l := make([]float64, synthSamples)
		r := quantize(musicSignal(44100, synthSamples, 2, 16000), 16)

		res := analyzeSynth(t, stereo44(16), l, r)

		assert.Equal(t, SkipQuiet, res.Channels[0].Skipped)
		assert.Equal(t, 1, res.Best)
		assert.Equal(t, VerdictLikelyFake, res.Verdict)
	})

	t.Run("digital silence is inconclusive", func(t *testing.T) {
		z := make([]float64, synthSamples)

		res := analyzeSynth(t, stereo44(16), z, z)

		assert.Equal(t, VerdictInconclusive, res.Verdict)
		assert.Equal(t, -1, res.Best)
		assert.Nil(t, res.Cliff)
		assert.Empty(t, res.Findings)
	})

	t.Run("a track too short to judge is inconclusive", func(t *testing.T) {
		l := quantize(musicSignal(44100, 1<<14, 1, 16000), 16) // ~0.4 s
		r := quantize(musicSignal(44100, 1<<14, 2, 16000), 16)

		res := analyzeSynth(t, stereo44(16), l, r)

		assert.Equal(t, VerdictInconclusive, res.Verdict)
	})

	t.Run("trailing partial sample data is ignored", func(t *testing.T) {
		l := quantize(musicSignal(44100, synthSamples, 1, 0), 16)
		r := quantize(musicSignal(44100, synthSamples, 2, 0), 16)
		data := append(pcm(l, r), 1, 2, 3)

		res, err := AnalyzeStream(bytes.NewReader(data), stereo44(16), DefaultThresholds())

		require.NoError(t, err)
		assert.Equal(t, VerdictOK, res.Verdict)
	})
}

func TestAnalyzeStreamMultichannel(t *testing.T) {
	const n = 1 << 17 // ~3 s at 44.1 kHz; six channels of it

	info := StreamInfo{
		Codec: "flac", SampleRate: 44100, Channels: 6,
		ChannelLayout: "5.1", BitsPerSample: 16,
	}

	lfeGain := func(hz float64) float64 {
		if hz < 30 || hz > 120 {
			return 0
		}
		return 1
	}

	// A 5.1 transcode: five full-range channels lowpassed at 16 kHz and an
	// LFE channel that only ever holds bass.
	chans := make([][]float64, 6)
	for c := range chans {
		if c == 3 {
			chans[c] = quantize(shaped(44100, n, int64(10+c), lfeGain), 16)
			continue
		}
		chans[c] = quantize(musicSignal(44100, n, int64(10+c), 16000), 16)
	}

	t.Run("the LFE channel does not mask the transcode", func(t *testing.T) {
		res := analyzeSynth(t, info, chans...)

		assert.Equal(t, SkipLFE, res.Channels[3].Skipped)
		assert.NotEqual(t, 3, res.Best)
		assert.Equal(t, VerdictLikelyFake, res.Verdict)
		require.NotNil(t, res.Cliff)
		assert.InDelta(t, 16000, res.Cliff.CutoffHz, 500)
	})

	t.Run("without the layout the LFE channel would hide it", func(t *testing.T) {
		// This is the failure the LFE rule exists to prevent: a bass-only
		// channel has no cliff above the search floor, so it looks like
		// full bandwidth and wins "best channel".
		unknown := info
		unknown.ChannelLayout = ""

		res := analyzeSynth(t, unknown, chans...)

		assert.Equal(t, VerdictOK, res.Verdict)
		assert.Equal(t, 3, res.Best)
	})
}

func TestAnalyzeStreamErrors(t *testing.T) {
	th := DefaultThresholds()

	t.Run("invalid stream properties", func(t *testing.T) {
		_, err := AnalyzeStream(strings.NewReader(""), StreamInfo{SampleRate: 0, Channels: 2}, th)
		assert.ErrorContains(t, err, "invalid stream")

		_, err = AnalyzeStream(strings.NewReader(""), StreamInfo{SampleRate: 44100, Channels: 0}, th)
		assert.ErrorContains(t, err, "invalid stream")
	})

	t.Run("a failing reader", func(t *testing.T) {
		_, err := AnalyzeStream(iotest.ErrReader(assert.AnError), stereo44(16), th)
		assert.ErrorContains(t, err, "reading decoded audio")
	})

	t.Run("an empty stream is inconclusive, not an error", func(t *testing.T) {
		res, err := AnalyzeStream(strings.NewReader(""), stereo44(16), th)
		require.NoError(t, err)
		assert.Equal(t, VerdictInconclusive, res.Verdict)
	})
}

// BenchmarkAnalyzeStream measures the analysis of a full-length track: three
// and a half minutes of stereo 44.1 kHz audio.
func BenchmarkAnalyzeStream(b *testing.B) {
	const frames = 44100 * 210
	rng := rand.New(rand.NewSource(1))
	l := make([]float64, frames)
	r := make([]float64, frames)
	for i := range l {
		l[i] = 0.2 * rng.NormFloat64()
		r[i] = 0.2 * rng.NormFloat64()
	}
	data := pcm(l, r)
	info := stereo44(16)
	th := DefaultThresholds()

	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for range b.N {
		if _, err := AnalyzeStream(bytes.NewReader(data), info, th); err != nil {
			b.Fatal(err)
		}
	}
}
