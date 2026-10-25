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

package riplog

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/pcm"
	"github.com/mfinelli/musicrename/internal/testutil"
)

// writeFile writes data to a file in a new temporary directory and returns its
// path.
func writeFile(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "x.flac")
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

func md5Of(t *testing.T, s string) (sum [16]byte) {
	t.Helper()
	copy(sum[:], unhex(t, s))
	return sum
}

func TestReadStreamInfoRealHeaders(t *testing.T) {
	// The first 42 bytes of files made by ffmpeg's FLAC encoder, whose
	// properties ffprobe independently reported, and whose MD5 is that of the
	// decoded audio, which was checked by decoding it. Unlike the headers the
	// other tests build, these were not packed by this package's own code.
	tests := []struct {
		name string
		hex  string
		want streamInfo
	}{
		{
			"CD audio, 12 s",
			"664c614300000022120012000016b1001af60ac442f00008133049ad6c370bb36713806d6f43e9050882",
			streamInfo{SampleRate: 44100, Channels: 2, BitsPerSample: 16, TotalSamples: 529200, MD5: md5Of(t, "49ad6c370bb36713806d6f43e9050882")},
		},
		{
			"24-bit 96 kHz, 5 s",
			"664c61430000002220002000002f62004fda177003700007530074a8b458e0f5696c5d30c5bb0ba36534",
			streamInfo{SampleRate: 96000, Channels: 2, BitsPerSample: 24, TotalSamples: 480000, MD5: md5Of(t, "74a8b458e0f5696c5d30c5bb0ba36534")},
		},
		{
			"mono 48 kHz, 3 s",
			"664c6143000000221200120000071b001c2d0bb800f00002328037b8c1768ca3c4cabdd42cc3517a8637",
			streamInfo{SampleRate: 48000, Channels: 1, BitsPerSample: 16, TotalSamples: 144000, MD5: md5Of(t, "37b8c1768ca3c4cabdd42cc3517a8637")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := hex.DecodeString(tc.hex)
			require.NoError(t, err)

			got, err := readStreamInfo(writeFile(t, raw))

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestReadStreamInfoFields(t *testing.T) {
	t.Run("every field at its limits", func(t *testing.T) {
		// The fields share bytes, so a mistake in one shows in its neighbours.
		tests := []streamInfo{
			{SampleRate: 1, Channels: 1, BitsPerSample: 4, TotalSamples: 1},
			{SampleRate: 1048575, Channels: 8, BitsPerSample: 32, TotalSamples: 1<<36 - 1},
			{SampleRate: 44100, Channels: 2, BitsPerSample: 16, TotalSamples: 111505380},
			{SampleRate: 192000, Channels: 6, BitsPerSample: 24, TotalSamples: 1 << 35},
			{SampleRate: 96000, Channels: 2, BitsPerSample: 17, TotalSamples: 0xFFFFFFFF},
			{SampleRate: 0xFFFFF, Channels: 1, BitsPerSample: 5, TotalSamples: 1 << 32},
		}
		for _, want := range tests {
			got, err := readStreamInfo(writeFile(t, flacHeader(want.SampleRate, want.Channels, want.BitsPerSample, want.TotalSamples)))

			require.NoError(t, err)
			assert.Equal(t, want, got)
		}
	})

	t.Run("an unknown length is zero", func(t *testing.T) {
		got, err := readStreamInfo(writeFile(t, cdFLAC(0)))

		require.NoError(t, err)
		assert.Equal(t, uint64(0), got.TotalSamples)
	})

	t.Run("what follows the header is ignored", func(t *testing.T) {
		data := append(cdFLAC(1234), []byte("any amount of audio and further metadata blocks")...)

		got, err := readStreamInfo(writeFile(t, data))

		require.NoError(t, err)
		assert.Equal(t, uint64(1234), got.TotalSamples)
	})

	t.Run("the last-block flag does not matter", func(t *testing.T) {
		data := cdFLAC(99)
		data[4] = 0x00 // STREAMINFO, with more blocks to follow

		got, err := readStreamInfo(writeFile(t, data))

		require.NoError(t, err)
		assert.Equal(t, uint64(99), got.TotalSamples)
	})
}

func TestReadStreamInfoNotFLAC(t *testing.T) {
	tests := []struct {
		name   string
		data   []byte
		reason string
	}{
		{"empty", nil, "too short"},
		{"just the signature", []byte("fLaC"), "too short"},
		{"one byte short of a header", cdFLAC(1)[:41], "too short"},
		{"a WAV file", append([]byte("RIFF....WAVEfmt "), make([]byte, 40)...), "FLAC signature"},
		{"plain text", []byte("this is not audio at all, however long it is, really not"), "FLAC signature"},
		{"a first block that is not STREAMINFO", func() []byte { b := cdFLAC(1); b[4] = 0x84; return b }(), "not STREAMINFO"},
		{"a STREAMINFO of the wrong size", func() []byte { b := cdFLAC(1); b[7] = 20; return b }(), "20 bytes, not 34"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := readStreamInfo(writeFile(t, tc.data))

			assert.ErrorIs(t, err, errNotFLAC)
			assert.ErrorContains(t, err, tc.reason)
		})
	}
}

func TestReadStreamInfoCannotRead(t *testing.T) {
	dir := t.TempDir()

	t.Run("a file that is not there", func(t *testing.T) {
		_, err := readStreamInfo(filepath.Join(dir, "missing.flac"))

		assert.Error(t, err)
		assert.NotErrorIs(t, err, errNotFLAC)
	})

	t.Run("a directory, which can be opened but not read", func(t *testing.T) {
		_, err := readStreamInfo(dir)

		assert.Error(t, err)
		assert.NotErrorIs(t, err, errNotFLAC)
	})
}

// TestReadStreamInfoRealEncoder encodes audio with a real FLAC encoder and reads
// it back, for properties known from how it was made: the exact length of a
// generated signal, and the formats asked for.
func TestReadStreamInfoRealEncoder(t *testing.T) {
	dir := t.TempDir()

	t.Run("CD audio", func(t *testing.T) {
		path := testutil.MakeNoiseFlac(t, dir, "cd.flac", 44100, 12)

		got, err := readStreamInfo(path)

		require.NoError(t, err)
		// The MD5 is of the audio, so it is checked against a decode of it.
		fp, err := pcm.Decoder{}.Fingerprint(context.Background(), path)
		require.NoError(t, err)
		assert.True(t, got.HasMD5())
		assert.Equal(t, fp.MD5, got.MD5)
		got.MD5 = [16]byte{}
		assert.Equal(t, streamInfo{SampleRate: 44100, Channels: 2, BitsPerSample: 16, TotalSamples: 44100 * 12}, got)
	})

	t.Run("24-bit audio at another rate", func(t *testing.T) {
		src := testutil.MakeNoiseFlac(t, dir, "src.flac", 44100, 2)
		path := testutil.MakeHiResFlac(t, src, dir, "hires.flac", 96000)

		got, err := readStreamInfo(path)

		require.NoError(t, err)
		assert.Equal(t, 96000, got.SampleRate)
		assert.Equal(t, 24, got.BitsPerSample)
		assert.Equal(t, 2, got.Channels)
	})
}

// TestExpectedImageSamples checks the rule for an image's length against the
// numbers metaflac reported for real FLAC images: the last sector of the disc,
// plus one, times 588.
func TestExpectedImageSamples(t *testing.T) {
	tests := []struct {
		fixture  string
		metaflac int64
	}{
		{fixtureBlack, 111505380},
		{fixtureBday, 192729348},
	}
	for _, tc := range tests {
		t.Run(tc.fixture, func(t *testing.T) {
			l, err := ParseLog(fixture(t, tc.fixture))
			require.NoError(t, err)

			assert.Equal(t, tc.metaflac, expectedImageSamples(l, 0))
		})
	}

	t.Run("an image that leaves out the start of the disc is shorter", func(t *testing.T) {
		l := &Log{TOC: []TOCTrack{{1, 3000, 9999}}}

		assert.Equal(t, int64(10000-3000)*588, expectedImageSamples(l, 3000))
		assert.Equal(t, int64(10000)*588, expectedImageSamples(l, 0))
	})
}

func TestStreamInfoMD5(t *testing.T) {
	t.Run("a header that stores one", func(t *testing.T) {
		data := cdFLAC(1000)
		copy(data[26:], unhex(t, "0123456789abcdef0123456789abcdef"))

		got, err := readStreamInfo(writeFile(t, data))

		require.NoError(t, err)
		assert.Equal(t, md5Of(t, "0123456789abcdef0123456789abcdef"), got.MD5)
		assert.True(t, got.HasMD5())
		assert.Equal(t, uint64(1000), got.TotalSamples, "and the length before it is not disturbed")
	})

	t.Run("a header that does not", func(t *testing.T) {
		got, err := readStreamInfo(writeFile(t, cdFLAC(1000)))

		require.NoError(t, err)
		assert.Equal(t, [16]byte{}, got.MD5)
		assert.False(t, got.HasMD5())
	})

	t.Run("the last byte counts", func(t *testing.T) {
		data := cdFLAC(1)
		data[41] = 1

		got, err := readStreamInfo(writeFile(t, data))

		require.NoError(t, err)
		assert.True(t, got.HasMD5())
	})
}
