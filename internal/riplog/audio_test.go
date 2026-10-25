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
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/pcm"
	"github.com/mfinelli/musicrename/internal/testutil"
)

// The real Copy CRC in Back in Black's log, which a good rip's image must decode
// to, and what was confirmed by decoding the author's own image of that disc.
const blackCopyCRC = "9FB18E2F"

func TestCheckFileAudio(t *testing.T) {
	t.Run("an image that decodes to what the log says", func(t *testing.T) {
		rip := blackRip(t)
		audio := rip.honest(t)

		r := rip.checkOpts(t, Options{Audio: audio})

		c := want(t, r, "audio", StatusPass)
		assert.Equal(t, "CRC 9FB18E2F matches the log's Copy CRC, and the audio matches the MD5 in the FLAC's header", c.Detail)
		assert.Equal(t, [][]string{{rip.path(".flac")}}, audio.calls, "the image, and nothing else, decoded once")
	})

	t.Run("a FLAC that stores no MD5", func(t *testing.T) {
		rip := blackRip(t)
		l, err := ParseLog(fixture(t, fixtureBlack))
		require.NoError(t, err)
		// The right length, with no MD5.
		require.NoError(t, os.WriteFile(rip.path(".flac"), cdFLAC(uint64(expectedImageSamples(l, 0))), 0o644))
		audio := rip.honest(t)
		// Decoding always gives an MD5, whatever the header stores.
		audio.fp.MD5 = testMD5

		c := want(t, rip.checkOpts(t, Options{Audio: audio}), "audio", StatusPass)

		assert.Equal(t, "CRC 9FB18E2F matches the log's Copy CRC (the FLAC stores no MD5 to compare with)", c.Detail)
	})

	t.Run("a CRC that is not the log's", func(t *testing.T) {
		rip := blackRip(t)
		audio := rip.honest(t)
		audio.fp.CRC32 = 0x12345678

		r := rip.checkOpts(t, Options{Audio: audio})

		c := want(t, r, "audio", StatusError)
		assert.Contains(t, c.Detail, "the audio's CRC is 12345678, not the log's Copy CRC 9FB18E2F")
		assert.Contains(t, c.Detail, "not the audio EAC ripped")
		assert.NotContains(t, c.Detail, "damaged", "the file itself is consistent")
		assert.True(t, r.Failed())
	})

	t.Run("the CRC is compared whatever its case", func(t *testing.T) {
		text := strings.ReplaceAll(fixtureText(t, fixtureBlack), blackCopyCRC, strings.ToLower(blackCopyCRC))
		rip := buildRip(t, "Back in Black", []byte(signed(text)), cueText(t, fixtureBlackCue))

		want(t, rip.checkOpts(t, Options{Audio: rip.honest(t)}), "audio", StatusPass)
	})

	t.Run("a CRC with leading zeros", func(t *testing.T) {
		// The log writes eight digits, and so must what is compared with it.
		text := strings.ReplaceAll(fixtureText(t, fixtureBlack), blackCopyCRC, "0000ABCD")
		rip := buildRip(t, "Back in Black", []byte(signed(text)), cueText(t, fixtureBlackCue))
		audio := rip.honest(t)
		require.Equal(t, uint32(0xABCD), audio.fp.CRC32)

		want(t, rip.checkOpts(t, Options{Audio: audio}), "audio", StatusPass)
	})

	t.Run("a MD5 that is not the header's is damage", func(t *testing.T) {
		rip := blackRip(t)
		audio := rip.honest(t)
		audio.fp.MD5[0] ^= 0xff

		c := want(t, rip.checkOpts(t, Options{Audio: audio}), "audio", StatusError)

		assert.Equal(t, "the FLAC is damaged: its audio does not match the MD5 in its header", c.Detail,
			"and with the CRC right, that is all that is wrong")
	})

	t.Run("a length that is not the header's is damage", func(t *testing.T) {
		rip := blackRip(t)
		audio := rip.honest(t)
		audio.fp.Bytes -= 4 * 588

		c := want(t, rip.checkOpts(t, Options{Audio: audio}), "audio", StatusError)

		assert.Contains(t, c.Detail, "the FLAC is damaged: it decodes to 111504792 samples, not the 111505380 in its header")
	})

	t.Run("a length that is not recorded is not compared", func(t *testing.T) {
		rip := blackRip(t)
		require.NoError(t, os.WriteFile(rip.path(".flac"), withMD5(cdFLAC(0), testMD5), 0o644))
		audio := rip.honest(t)
		audio.fp.Bytes = 12345 * 4

		want(t, rip.checkOpts(t, Options{Audio: audio}), "audio", StatusPass)
	})

	t.Run("damage, and a CRC that is not the log's, are both said", func(t *testing.T) {
		rip := blackRip(t)
		audio := rip.honest(t)
		audio.fp.MD5[3] ^= 1
		audio.fp.Bytes -= 4
		audio.fp.CRC32 ^= 1

		c := want(t, rip.checkOpts(t, Options{Audio: audio}), "audio", StatusError)

		assert.Contains(t, c.Detail, "the FLAC is damaged: it decodes to 111505379 samples, not the 111505380 in its header, and its audio does not match the MD5 in its header; the audio's CRC is")
	})

	t.Run("audio that does not decode cleanly", func(t *testing.T) {
		rip := blackRip(t)
		audio := rip.honest(t)
		audio.err = &pcm.DecodeError{Path: rip.path(".flac"), Message: "CRC error at PTS 0"}

		r := rip.checkOpts(t, Options{Audio: audio})

		c := want(t, r, "audio", StatusError)
		assert.Equal(t, "the FLAC does not decode cleanly: CRC error at PTS 0", c.Detail)
		assert.True(t, r.Failed())
	})

	t.Run("no ffmpeg is not the rip's fault", func(t *testing.T) {
		rip := blackRip(t)
		audio := rip.honest(t)
		audio.err = pcm.ErrNoFFmpeg

		r := rip.checkOpts(t, Options{Audio: audio})

		c := want(t, r, "audio", StatusWarning)
		assert.Contains(t, c.Detail, "ffmpeg was not found")
		assert.False(t, r.Failed())
	})

	t.Run("a decode that could not be made", func(t *testing.T) {
		rip := blackRip(t)
		audio := rip.honest(t)
		audio.err = errors.New("context canceled")

		r := rip.checkOpts(t, Options{Audio: audio})

		c := want(t, r, "audio", StatusWarning)
		assert.Contains(t, c.Detail, "could not decode the image")
		assert.Contains(t, c.Detail, "context canceled")
		assert.False(t, r.Failed())
	})

	t.Run("with no decoder the image is not decoded", func(t *testing.T) {
		r := blackRip(t).checkOpts(t, Options{})

		c := want(t, r, "audio", StatusInfo)
		assert.Contains(t, c.Detail, "was not decoded")
	})
}

func TestCheckFileAudioNotComparable(t *testing.T) {
	// Each of these is a log whose CRC is not the one this reproduces, or is not
	// there, and the audio is not decoded at all.
	tests := []struct {
		name   string
		edit   func(string) string
		detail string
	}{
		{
			"null samples left out of the CRC",
			func(s string) string {
				return strings.Replace(s, "Null samples used in CRC calculations       : Yes", "Null samples used in CRC calculations       : No", 1)
			},
			"leaves out null samples",
		},
		{
			"the setting missing from the log",
			func(s string) string {
				return withoutLine(t, s, "Null samples used in CRC calculations")
			},
			"leaves out null samples",
		},
		{
			"no Copy CRC",
			func(s string) string { return withoutLine(t, s, "Copy CRC") },
			"no Copy CRC",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			text := tc.edit(fixtureText(t, fixtureBlack))
			require.NotEqual(t, fixtureText(t, fixtureBlack), text)
			rip := buildRip(t, "Back in Black", []byte(signed(text)), cueText(t, fixtureBlackCue))
			audio := &fakeAudio{}

			r := rip.checkOpts(t, Options{Audio: audio})

			c := want(t, r, "audio", StatusInfo)
			assert.Contains(t, c.Detail, tc.detail)
			assert.Empty(t, audio.calls, "nothing to compare with, so no decoding")
		})
	}
}

func TestCheckFileAudioOnlyForAGoodImage(t *testing.T) {
	// The image has to be there, and be CD audio, for its audio to be decoded.
	t.Run("an image that is not there", func(t *testing.T) {
		rip := blackRip(t)
		require.NoError(t, os.Remove(rip.path(".flac")))
		audio := &fakeAudio{}

		r := rip.checkOpts(t, Options{Audio: audio})

		_, found := r.Check("audio")
		assert.False(t, found)
		assert.Empty(t, audio.calls)
	})

	t.Run("a file that is not FLAC", func(t *testing.T) {
		rip := blackRip(t)
		require.NoError(t, os.WriteFile(rip.path(".flac"), []byte("not flac"), 0o644))
		audio := &fakeAudio{}

		r := rip.checkOpts(t, Options{Audio: audio})

		_, found := r.Check("audio")
		assert.False(t, found)
		assert.Empty(t, audio.calls)
	})

	t.Run("an image that is not CD audio", func(t *testing.T) {
		// It would be converted to 16 bits, and what was hashed would be that.
		for name, header := range map[string][]byte{
			"24-bit": flacHeader(44100, 2, 24, 1000),
			"48 kHz": flacHeader(48000, 2, 16, 1000),
			"mono":   flacHeader(44100, 1, 16, 1000),
		} {
			rip := blackRip(t)
			require.NoError(t, os.WriteFile(rip.path(".flac"), header, 0o644))
			audio := &fakeAudio{}

			r := rip.checkOpts(t, Options{Audio: audio})

			want(t, r, "image-format", StatusError)
			_, found := r.Check("audio")
			assert.False(t, found, name)
			assert.Empty(t, audio.calls, name)
		}
	})

	t.Run("no cue sheet", func(t *testing.T) {
		rip := blackRip(t)
		require.NoError(t, os.Remove(rip.path(".cue")))
		audio := &fakeAudio{}

		rip.checkOpts(t, Options{Audio: audio})

		assert.Empty(t, audio.calls)
	})

	t.Run("an image named differently from the one the log ripped is still the one decoded", func(t *testing.T) {
		// The cue sheet says which file is the image; the log's name for it is a
		// check on that, and a warning.
		text := strings.ReplaceAll(fixtureText(t, fixtureBlack), "Back in Black.wav", "Some Other Album.wav")
		require.NotEqual(t, fixtureText(t, fixtureBlack), text)
		rip := buildRip(t, "Back in Black", []byte(signed(text)), cueText(t, fixtureBlackCue))
		audio := rip.honest(t)

		r := rip.checkOpts(t, Options{Audio: audio})

		want(t, r, "cue-image", StatusWarning)
		want(t, r, "audio", StatusPass)
		assert.Equal(t, [][]string{{rip.path(".flac")}}, audio.calls)
	})

	t.Run("a length that is wrong does not stop the audio being checked", func(t *testing.T) {
		// The two checks say different things, and each is made.
		rip := blackRip(t)
		require.NoError(t, os.WriteFile(rip.path(".flac"), withMD5(cdFLAC(5000), testMD5), 0o644))

		r := rip.checkOpts(t, Options{Audio: rip.honest(t)})

		want(t, r, "image-length", StatusError)
		want(t, r, "audio", StatusPass)
	})
}

func TestCheckFileAudioEveryRealLog(t *testing.T) {
	// The CRC compared is each real log's own Copy CRC.
	for name, tc := range map[string]struct {
		rip func(*testing.T) rip
		crc string
	}{
		"Back in Black":        {blackRip, "9FB18E2F"},
		"B'day":                {bdayRip, "B1A3B305"},
		"Central Park":         {centralRip, "0F3D7554"},
		"Transistor (EAC 1.5)": {transistorRip, "1EB5B797"},
	} {
		t.Run(name, func(t *testing.T) {
			c := want(t, tc.rip(t).check(t), "audio", StatusPass)

			assert.True(t, strings.HasPrefix(c.Detail, "CRC "+tc.crc+" matches"), c.Detail)
		})
	}
}

// testAudio returns n sample frames of CD audio as bytes, the same each time.
func testAudio(n int, seed int64) []byte {
	rng := rand.New(rand.NewSource(seed))
	out := make([]byte, 0, n*4)
	var v int
	for range n {
		v = (v*29+rng.Intn(1400)-700)%24000 - 12000
		out = binary.LittleEndian.AppendUint16(out, uint16(int16(v)))
		out = binary.LittleEndian.AppendUint16(out, uint16(int16(v*3/4)))
	}
	return out
}

// realAudioRip builds Back in Black's rip with a real FLAC image of audio made
// here, whose CRC is the log's Copy CRC (and Test CRC): the log is edited to
// say so and signed again. The image is far shorter than the disc, which the
// length check says, and which has nothing to do with the audio check. It
// returns the audio.
func realAudioRip(t *testing.T) (rip, []byte) {
	t.Helper()

	pcmData := testAudio(44100*2, 21)
	crc := fmt.Sprintf("%08X", crc32.ChecksumIEEE(pcmData))
	text := strings.ReplaceAll(fixtureText(t, fixtureBlack), blackCopyCRC, crc)
	require.NotEqual(t, fixtureText(t, fixtureBlack), text)

	rip := buildRip(t, "Back in Black", []byte(signed(text)), cueText(t, fixtureBlackCue))
	testutil.MakePCMFlac(t, rip.dir, "Back in Black.flac", pcmData)
	return rip, pcmData
}

// TestCheckFileAudioWithFFmpeg is the check run through the real decoder, on a
// FLAC made from audio whose CRC is known. The rest is checked against fakes,
// which is only as good as their idea of what ffmpeg does.
func TestCheckFileAudioWithFFmpeg(t *testing.T) {
	opts := Options{Audio: pcm.Decoder{}}

	t.Run("an image that is the audio the log's CRC is of", func(t *testing.T) {
		rip, _ := realAudioRip(t)

		c := want(t, rip.checkOpts(t, opts), "audio", StatusPass)

		assert.Contains(t, c.Detail, "matches the log's Copy CRC, and the audio matches the MD5 in the FLAC's header")
	})

	t.Run("a log whose CRC is of other audio", func(t *testing.T) {
		rip, _ := realAudioRip(t)
		l, err := ParseLog(mustRead(t, rip.logPath()))
		require.NoError(t, err)
		other := fmt.Sprintf("%08X", crc32.ChecksumIEEE(testAudio(44100*2, 22)))
		text := strings.ReplaceAll(string(mustRead(t, rip.logPath())), l.Range.CopyCRC, other)
		require.NoError(t, os.WriteFile(rip.logPath(), []byte(signed(text)), 0o644))

		c := want(t, rip.checkOpts(t, opts), "audio", StatusError)

		assert.Contains(t, c.Detail, "not the log's Copy CRC "+other)
		assert.NotContains(t, c.Detail, "damaged")
	})

	t.Run("a bit flipped in the image", func(t *testing.T) {
		rip, _ := realAudioRip(t)
		data := mustRead(t, rip.path(".flac"))
		data[len(data)/2] ^= 0x04
		require.NoError(t, os.WriteFile(rip.path(".flac"), data, 0o644))

		r := rip.checkOpts(t, opts)

		// ffmpeg may name the damage, or exit successfully with the wrong audio and
		// say nothing, which is what some builds do; the header's MD5 and the log's
		// CRC then show it. What must hold is that it is found, one way or the
		// other.
		c := want(t, r, "audio", StatusError)
		assert.Regexp(t, `does not decode cleanly|the FLAC is damaged|not the log's Copy CRC`, c.Detail)
		assert.True(t, r.Failed())
	})

	t.Run("damage that ffmpeg does not name is still found", func(t *testing.T) {
		// An ffmpeg that exits successfully with the wrong audio and says nothing,
		// which is what it does about a damaged frame unless asked, and which some
		// builds do even when asked. Its output has the wrong length, the wrong
		// MD5 and the wrong CRC, and each is compared.
		rip, _ := realAudioRip(t)
		const output = "not the audio at all" // 20 bytes: 5 samples
		bin := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(bin, "ffmpeg"),
			[]byte("#!/bin/sh\nprintf '"+output+"'\n"), 0o755))
		t.Setenv("PATH", bin)

		r := rip.checkOpts(t, opts)

		c := want(t, r, "audio", StatusError)
		assert.Contains(t, c.Detail, "the FLAC is damaged")
		assert.Contains(t, c.Detail, fmt.Sprintf("decodes to %d samples, not the 88200 in its header", len(output)/4))
		assert.Contains(t, c.Detail, "does not match the MD5 in its header")
		assert.Contains(t, c.Detail, "not the log's Copy CRC")
		assert.True(t, r.Failed())
	})

	t.Run("an MD5 in the header that is not the audio's", func(t *testing.T) {
		rip, _ := realAudioRip(t)
		data := mustRead(t, rip.path(".flac"))
		data[8+18] ^= 0xff
		require.NoError(t, os.WriteFile(rip.path(".flac"), data, 0o644))

		c := want(t, rip.checkOpts(t, opts), "audio", StatusError)

		assert.Equal(t, "the FLAC is damaged: its audio does not match the MD5 in its header", c.Detail)
	})

	t.Run("a length in the header that is not the audio's", func(t *testing.T) {
		rip, _ := realAudioRip(t)
		si, err := readStreamInfo(rip.path(".flac"))
		require.NoError(t, err)
		// Change only the 36 bits of the length, which sit in the low nibble of one
		// byte and the four after it, so the rest of the header is the real one.
		data := mustRead(t, rip.path(".flac"))
		n := si.TotalSamples + 1000
		p := data[8+10+3 : 8+10+8]
		p[0] = p[0]&0xF0 | byte(n>>32)
		p[1], p[2], p[3], p[4] = byte(n>>24), byte(n>>16), byte(n>>8), byte(n)
		require.NoError(t, os.WriteFile(rip.path(".flac"), data, 0o644))
		got, err := readStreamInfo(rip.path(".flac"))
		require.NoError(t, err)
		require.Equal(t, n, got.TotalSamples, "the header says what the test means it to")

		c := want(t, rip.checkOpts(t, opts), "audio", StatusError)

		assert.Contains(t, c.Detail, fmt.Sprintf("it decodes to %d samples, not the %d in its header", si.TotalSamples, n))
	})

	t.Run("no ffmpeg", func(t *testing.T) {
		rip, _ := realAudioRip(t)
		t.Setenv("PATH", t.TempDir())

		r := rip.checkOpts(t, opts)

		c := want(t, r, "audio", StatusWarning)
		assert.Contains(t, c.Detail, "ffmpeg was not found")
		// The one error is the image being far shorter than the disc, which has
		// nothing to do with this.
		assert.Equal(t, 1, r.Count(StatusError))
		want(t, r, "image-length", StatusError)
	})

	t.Run("a FLAC cut short", func(t *testing.T) {
		rip, _ := realAudioRip(t)
		data := mustRead(t, rip.path(".flac"))
		require.NoError(t, os.WriteFile(rip.path(".flac"), data[:len(data)*2/3], 0o644))

		r := rip.checkOpts(t, opts)

		c := want(t, r, "audio", StatusError)
		assert.NotEmpty(t, c.Detail)
		assert.True(t, r.Failed())
	})

	t.Run("the image's tags and compression do not matter", func(t *testing.T) {
		rip, pcmData := realAudioRip(t)
		testutil.MakePCMFlac(t, rip.dir, "Back in Black.flac", pcmData,
			"-compression_level", "8", "-metadata", "ARTIST=AC/DC", "-metadata", "TITLE=Back in Black")

		want(t, rip.checkOpts(t, opts), "audio", StatusPass)
	})
}
