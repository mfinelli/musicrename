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
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeProbe struct {
	out  string
	err  error
	args []string
}

func (f *fakeProbe) Run(_ context.Context, args []string) (string, error) {
	f.args = args
	return f.out, f.err
}

// fakeSource serves canned bytes and records whether it was opened and closed.
type fakeSource struct {
	data     []byte
	closeErr error
	opened   bool
	closed   bool
}

func (f *fakeSource) Open(context.Context, string) (io.ReadCloser, error) {
	f.opened = true
	return &fakeReadCloser{Reader: strings.NewReader(string(f.data)), src: f}, nil
}

type fakeReadCloser struct {
	io.Reader
	src *fakeSource
}

func (r *fakeReadCloser) Close() error {
	r.src.closed = true
	return r.src.closeErr
}

const flacProbe = `{"streams":[{"codec_name":"flac","sample_rate":"44100","channels":2,` +
	`"channel_layout":"stereo","bits_per_raw_sample":"16"}]}`

func TestProbe(t *testing.T) {
	ctx := context.Background()

	t.Run("parses a stream", func(t *testing.T) {
		fp := &fakeProbe{out: flacProbe}

		info, err := probe(ctx, fp, "x.flac")

		require.NoError(t, err)
		assert.Equal(t, StreamInfo{
			Codec: "flac", SampleRate: 44100, Channels: 2,
			ChannelLayout: "stereo", BitsPerSample: 16,
		}, info)
		assert.Contains(t, fp.args, "x.flac")
		assert.Contains(t, fp.args, "a:0")
	})

	t.Run("unknown bit depth", func(t *testing.T) {
		fp := &fakeProbe{out: `{"streams":[{"codec_name":"alac","sample_rate":"96000",` +
			`"channels":2,"channel_layout":"stereo","bits_per_raw_sample":"N/A"}]}`}

		info, err := probe(ctx, fp, "x.m4a")

		require.NoError(t, err)
		assert.Equal(t, 0, info.BitsPerSample)
		assert.Equal(t, 96000, info.SampleRate)
	})

	t.Run("no audio stream", func(t *testing.T) {
		_, err := probe(ctx, &fakeProbe{out: `{"streams":[]}`}, "x.flac")
		assert.ErrorContains(t, err, "no audio stream")
	})

	t.Run("garbage output", func(t *testing.T) {
		_, err := probe(ctx, &fakeProbe{out: "not json"}, "x.flac")
		assert.ErrorContains(t, err, "parsing ffprobe output")
	})

	t.Run("no usable sample rate", func(t *testing.T) {
		_, err := probe(ctx, &fakeProbe{out: `{"streams":[{"codec_name":"flac","channels":2}]}`}, "x.flac")
		assert.ErrorContains(t, err, "sample rate")
	})

	t.Run("no channels", func(t *testing.T) {
		_, err := probe(ctx, &fakeProbe{out: `{"streams":[{"codec_name":"flac","sample_rate":"44100"}]}`}, "x.flac")
		assert.ErrorContains(t, err, "channels")
	})

	t.Run("ffprobe failure", func(t *testing.T) {
		_, err := probe(ctx, &fakeProbe{err: errors.New("boom")}, "x.flac")
		assert.ErrorContains(t, err, "ffprobe: boom")
	})
}

func TestLFEIndex(t *testing.T) {
	tests := map[string]int{
		"":             -1,
		"mono":         -1,
		"stereo":       -1,
		"2.1":          2,
		"3.0":          -1,
		"3.1":          3,
		"4.0":          -1,
		"quad":         -1,
		"5.0":          -1,
		"5.0(side)":    -1,
		"5.1":          3,
		"5.1(side)":    3,
		"6.1":          3,
		"7.1":          3,
		"7.1(wide)":    3,
		"7.1.2":        3,
		"hexagonal":    -1,
		"unknown-ish":  -1,
		"5":            -1,
		"stereo(down)": -1,
	}
	for layout, want := range tests {
		t.Run(layout, func(t *testing.T) {
			assert.Equal(t, want, lfeIndex(layout))
		})
	}
}

func TestIsLosslessCodec(t *testing.T) {
	for _, c := range []string{"flac", "alac", "wavpack", "ape", "tta", "truehd", "pcm_s16le", "pcm_s24le", "pcm_f32le"} {
		assert.True(t, IsLosslessCodec(c), c)
	}
	for _, c := range []string{"mp3", "aac", "vorbis", "opus", "ac3", "eac3", "dts", "wmav2", "mp2", ""} {
		assert.False(t, IsLosslessCodec(c), c)
	}
}

func TestAnalyzeRefusesLossyCodecs(t *testing.T) {
	fp := &fakeProbe{out: `{"streams":[{"codec_name":"mp3","sample_rate":"44100","channels":2}]}`}
	src := &fakeSource{}

	res, err := analyze(context.Background(), fp, src, "x.mp3", DefaultThresholds())

	assert.Nil(t, res)
	var nle *NotLosslessError
	require.ErrorAs(t, err, &nle)
	assert.Equal(t, "mp3", nle.Codec)
	assert.ErrorContains(t, err, "not lossless")
	assert.False(t, src.opened, "must not decode a file it is going to refuse")
}

func TestAnalyzeClosesTheSource(t *testing.T) {
	ctx := context.Background()
	fp := &fakeProbe{out: flacProbe}

	t.Run("always", func(t *testing.T) {
		src := &fakeSource{}
		_, err := analyze(ctx, fp, src, "x.flac", DefaultThresholds())
		require.NoError(t, err)
		assert.True(t, src.closed)
	})

	t.Run("a decode failure reported on close is returned", func(t *testing.T) {
		src := &fakeSource{closeErr: errors.New("ffmpeg: exit status 1: corrupt")}
		_, err := analyze(ctx, fp, src, "x.flac", DefaultThresholds())
		assert.ErrorContains(t, err, "corrupt")
	})

	t.Run("a probe failure never opens the source", func(t *testing.T) {
		src := &fakeSource{}
		_, err := analyze(ctx, &fakeProbe{err: errors.New("nope")}, src, "x.flac", DefaultThresholds())
		assert.Error(t, err)
		assert.False(t, src.opened)
	})
}
