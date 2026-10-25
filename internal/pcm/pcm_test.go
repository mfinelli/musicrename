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

package pcm

import (
	"context"
	"crypto/md5"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/testutil"
)

// audio returns n sample frames of stereo 16-bit audio, as the bytes of the
// samples, made from the seed. It is pseudo-random, so it is the same every time
// and is not mistaken for another stretch of it.
func audio(n int, seed int64) []byte {
	rng := rand.New(rand.NewSource(seed))
	out := make([]byte, 0, n*bytesPerSample)
	var left int
	for range n {
		left = (left*29+rng.Intn(1400)-700)%24000 - 12000
		right := left * 3 / 4
		out = binary.LittleEndian.AppendUint16(out, uint16(int16(left)))
		out = binary.LittleEndian.AppendUint16(out, uint16(int16(right)))
	}
	return out
}

// want is the fingerprint of pcm, worked out here and not by the code under
// test.
func want(pcm []byte) Fingerprint {
	return Fingerprint{Bytes: int64(len(pcm)), CRC32: crc32.ChecksumIEEE(pcm), MD5: md5.Sum(pcm)}
}

func TestFingerprint(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	t.Run("audio comes back as it went in", func(t *testing.T) {
		pcm := audio(44100*2, 1)
		path := testutil.MakePCMFlac(t, dir, "one.flac", pcm)

		got, err := Decoder{}.Fingerprint(ctx, path)

		require.NoError(t, err)
		assert.Equal(t, want(pcm), got)
	})

	t.Run("the same audio fingerprints the same however it is stored", func(t *testing.T) {
		pcm := audio(44100*2, 2)
		fast := testutil.MakePCMFlac(t, dir, "fast.flac", pcm, "-compression_level", "0")
		small := testutil.MakePCMFlac(t, dir, "small.flac", pcm, "-compression_level", "8",
			"-metadata", "ARTIST=Someone", "-metadata", "TITLE=Something")

		a, err := Decoder{}.Fingerprint(ctx, fast)
		require.NoError(t, err)
		b, err := Decoder{}.Fingerprint(ctx, small)
		require.NoError(t, err)

		info1, _ := os.Stat(fast)
		info2, _ := os.Stat(small)
		require.NotEqual(t, info1.Size(), info2.Size(), "the files should differ")
		assert.Equal(t, a, b)
		assert.Equal(t, want(pcm), a)
	})

	t.Run("different audio fingerprints differently", func(t *testing.T) {
		a := testutil.MakePCMFlac(t, dir, "a.flac", audio(44100, 3))
		b := testutil.MakePCMFlac(t, dir, "b.flac", audio(44100, 4))

		fa, err := Decoder{}.Fingerprint(ctx, a)
		require.NoError(t, err)
		fb, err := Decoder{}.Fingerprint(ctx, b)
		require.NoError(t, err)

		assert.Equal(t, fa.Bytes, fb.Bytes)
		assert.NotEqual(t, fa.CRC32, fb.CRC32)
		assert.NotEqual(t, fa.MD5, fb.MD5)
	})

	t.Run("a single sample changed", func(t *testing.T) {
		pcm := audio(44100, 5)
		changed := append([]byte(nil), pcm...)
		changed[len(changed)/2] ^= 0x01

		a := testutil.MakePCMFlac(t, dir, "orig.flac", pcm)
		b := testutil.MakePCMFlac(t, dir, "chg.flac", changed)
		fa, err := Decoder{}.Fingerprint(ctx, a)
		require.NoError(t, err)
		fb, err := Decoder{}.Fingerprint(ctx, b)
		require.NoError(t, err)

		assert.NotEqual(t, fa.CRC32, fb.CRC32)
		assert.NotEqual(t, fa.MD5, fb.MD5)
	})

	t.Run("several files are one stretch of audio, in order", func(t *testing.T) {
		// The tracks of an album fingerprint as the image they were cut from.
		p1, p2, p3 := audio(30000, 6), audio(41000, 7), audio(1234, 8)
		whole := append(append(append([]byte(nil), p1...), p2...), p3...)
		f1 := testutil.MakePCMFlac(t, dir, "t1.flac", p1)
		f2 := testutil.MakePCMFlac(t, dir, "t2.flac", p2, "-metadata", "TITLE=two")
		f3 := testutil.MakePCMFlac(t, dir, "t3.flac", p3, "-compression_level", "1")

		got, err := Decoder{}.Fingerprint(ctx, f1, f2, f3)
		require.NoError(t, err)
		assert.Equal(t, want(whole), got)

		swapped, err := Decoder{}.Fingerprint(ctx, f2, f1, f3)
		require.NoError(t, err)
		assert.Equal(t, got.Bytes, swapped.Bytes, "the same audio")
		assert.NotEqual(t, got.CRC32, swapped.CRC32, "in another order")
	})

	t.Run("no files", func(t *testing.T) {
		got, err := Decoder{}.Fingerprint(ctx)

		require.NoError(t, err)
		assert.Equal(t, want(nil), got)
	})

	t.Run("a file whose name looks like a URL", func(t *testing.T) {
		pcm := audio(8000, 9)
		path := testutil.MakePCMFlac(t, dir, "http:odd name.flac", pcm)

		got, err := Decoder{}.Fingerprint(ctx, path)

		require.NoError(t, err)
		assert.Equal(t, want(pcm), got)
	})

	t.Run("a file with embedded art and tags is only its audio", func(t *testing.T) {
		pcm := audio(8000, 10)
		path := testutil.MakePCMFlac(t, dir, "tagged.flac", pcm,
			"-metadata", "ALBUM=An Album", "-metadata", "DATE=1980")

		got, err := Decoder{}.Fingerprint(ctx, path)

		require.NoError(t, err)
		assert.Equal(t, want(pcm), got)
	})
}

func TestFingerprintAccessors(t *testing.T) {
	f := Fingerprint{Bytes: 4 * 111505380, CRC32: 0x9FB18E2F}

	assert.Equal(t, int64(111505380), f.Samples())
	assert.Equal(t, "9FB18E2F", f.CRC())

	assert.Equal(t, "0000ABCD", Fingerprint{CRC32: 0xABCD}.CRC(), "leading zeros are kept")
	assert.Equal(t, "00000000", Fingerprint{}.CRC())

	empty := want(nil)
	assert.Equal(t, "d41d8cd98f00b204e9800998ecf8427e", empty.MD5Hex())
	assert.Equal(t, int64(0), empty.Samples())
	assert.Equal(t, int64(2), Fingerprint{Bytes: 11}.Samples(), "a part of a sample is not counted")
}

func TestFingerprintBadAudio(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	pcm := audio(44100*3, 11)
	good := testutil.MakePCMFlac(t, dir, "good.flac", pcm)
	clean := want(pcm)

	// notClean checks that decoding did not give the clean audio: either it
	// returned an error, or the fingerprint is different. Which of the two a given
	// ffmpeg gives for a given kind of damage is its business: it may name the
	// damage, or exit successfully with the wrong samples and say nothing, as it
	// does unless asked to check frames, and not every version or build is the
	// same about it. That damaged audio is never taken for good is ours, and
	// holds either way. What is logged says which it was, for a failure to be
	// understood.
	notClean := func(t *testing.T, path string) {
		t.Helper()
		got, err := Decoder{}.Fingerprint(ctx, path)
		if err != nil {
			var de *DecodeError
			assert.ErrorAs(t, err, &de)
			assert.Equal(t, path, de.Path)
			assert.NotEmpty(t, de.Message)
			t.Logf("ffmpeg named the damage: %q", de.Message)
			return
		}
		t.Logf("ffmpeg did not name the damage, and the fingerprint is what shows it (%s)", ffmpegVersion())
		assert.NotEqual(t, clean, got, "damaged audio decoded as if it were good")
	}

	damage := func(t *testing.T, name string, f func([]byte) []byte) string {
		t.Helper()
		data, err := os.ReadFile(good)
		require.NoError(t, err)
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, f(data), 0o644))
		return path
	}

	t.Run("a bit flipped in the middle is never taken for good audio", func(t *testing.T) {
		// ffmpeg exits successfully with the full length of wrong audio, and is
		// asked to name the damage, which the versions tried (4.2 to 7.x) do. A
		// build that does not still gives a different fingerprint, so this holds
		// either way.
		notClean(t, damage(t, "flip.flac", func(b []byte) []byte {
			b[len(b)/2] ^= 0x04
			return b
		}))
	})

	t.Run("a stretch of the audio overwritten", func(t *testing.T) {
		notClean(t, damage(t, "burst.flac", func(b []byte) []byte {
			for i := len(b) / 2; i < len(b)/2+256; i++ {
				b[i] = 0xA5
			}
			return b
		}))
	})

	t.Run("flipped bits in several places", func(t *testing.T) {
		for i, frac := range []int{4, 3, 2} {
			notClean(t, damage(t, "flips"+string(rune('a'+i))+".flac", func(b []byte) []byte {
				b[len(b)/frac] ^= 0x80
				return b
			}))
		}
	})

	t.Run("cut short", func(t *testing.T) {
		notClean(t, damage(t, "short.flac", func(b []byte) []byte { return b[:len(b)*2/3] }))
	})

	t.Run("a file that is not audio", func(t *testing.T) {
		path := filepath.Join(dir, "text.flac")
		require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("not audio at all\n", 100)), 0o644))

		_, err := Decoder{}.Fingerprint(ctx, path)

		var de *DecodeError
		require.ErrorAs(t, err, &de)
		assert.Equal(t, path, de.Path)
		assert.NotEmpty(t, de.Message)
	})

	t.Run("a file that is not there", func(t *testing.T) {
		path := filepath.Join(dir, "missing.flac")

		_, err := Decoder{}.Fingerprint(ctx, path)

		var de *DecodeError
		require.ErrorAs(t, err, &de)
		assert.Equal(t, path, de.Path)
		assert.NotErrorIs(t, err, ErrNoFFmpeg)
	})

	t.Run("an empty file", func(t *testing.T) {
		path := filepath.Join(dir, "empty.flac")
		require.NoError(t, os.WriteFile(path, nil, 0o644))

		_, err := Decoder{}.Fingerprint(ctx, path)

		var de *DecodeError
		assert.ErrorAs(t, err, &de)
	})

	t.Run("it stops at the first file that does not decode, and says which", func(t *testing.T) {
		bad := filepath.Join(dir, "bad.flac")
		require.NoError(t, os.WriteFile(bad, []byte("garbage"), 0o644))

		fp, err := Decoder{}.Fingerprint(ctx, good, bad, good)

		var de *DecodeError
		require.ErrorAs(t, err, &de)
		assert.Equal(t, bad, de.Path)
		assert.Equal(t, clean, fp, "the fingerprint covers the audio before the problem")
	})

	t.Run("the error says what and where", func(t *testing.T) {
		err := &DecodeError{Path: "/rips/album.flac", Message: "CRC error at PTS 0"}

		assert.Equal(t, "/rips/album.flac does not decode cleanly: CRC error at PTS 0", err.Error())
	})
}

func TestFingerprintEnvironment(t *testing.T) {
	dir := t.TempDir()
	path := testutil.MakePCMFlac(t, dir, "a.flac", audio(8000, 12))

	t.Run("no ffmpeg", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())

		_, err := Decoder{}.Fingerprint(context.Background(), path)

		assert.ErrorIs(t, err, ErrNoFFmpeg)
		var de *DecodeError
		assert.False(t, errors.As(err, &de), "that is not the audio's fault")
	})

	t.Run("a context that is already cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := Decoder{}.Fingerprint(ctx, path)

		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("a context that runs out", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 1)
		defer cancel()
		<-ctx.Done()

		_, err := Decoder{}.Fingerprint(ctx, path)

		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("ffmpeg exists", func(t *testing.T) {
		// The other tests rest on it: a missing one shows here, not as a
		// string of failures that look like bugs. Which one is logged, since how
		// ffmpeg treats damage is what differs between them.
		_, err := exec.LookPath("ffmpeg")

		require.NoError(t, err)
		t.Logf("ffmpeg: %s", ffmpegVersion())
	})
}

func TestMessage(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   string
	}{
		{"nothing", "", ""},
		{"only whitespace", " \n\n  \n", ""},
		{"a plain line", "Invalid data found when processing input\n", "Invalid data found when processing input"},
		{"a prefix naming the component", "[flac @ 0x55d85d2f3340] CRC error at PTS 441000\n", "CRC error at PTS 441000"},
		{"a position that is not known", "[flac @ 0x55d85d2f3340] CRC error at PTS -9223372036854775808\n", "CRC error"},
		{"the unknown position only at the end", "PTS -9223372036854775808 at PTS 5\n", "PTS -9223372036854775808 at PTS 5"},
		{"the first of several", "[flac @ 0x1] invalid residual\n[flac @ 0x1] decode_frame() failed\n[aist#0:0/flac @ 0x2] Error submitting packet\n", "invalid residual"},
		{"leading blank lines", "\n\n[flac @ 0x1] something\n", "something"},
		{"a prefix alone", "[flac @ 0x1]\nnext line\n", "next line"},
		{"brackets that are not a prefix", "file [1].flac: No such file or directory\n", "file [1].flac: No such file or directory"},
		{"Windows line endings", "[x @ 0x1] bad\r\nmore\r\n", "bad"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, message(tc.stderr))
		})
	}
}

func TestHeadBuffer(t *testing.T) {
	t.Run("keeps what fits", func(t *testing.T) {
		h := &headBuffer{limit: 10}

		n, err := h.Write([]byte("abc"))
		require.NoError(t, err)
		assert.Equal(t, 3, n)
		h.Write([]byte("def"))

		assert.Equal(t, "abcdef", h.String())
	})

	t.Run("keeps the first part of what does not", func(t *testing.T) {
		h := &headBuffer{limit: 5}

		n, _ := h.Write([]byte("0123456789"))
		m, _ := h.Write([]byte("more"))

		assert.Equal(t, "01234", h.String())
		assert.Equal(t, 10, n, "it says all was written, so the command is not stopped")
		assert.Equal(t, 4, m)
	})

	t.Run("a write exactly to the limit, then another", func(t *testing.T) {
		h := &headBuffer{limit: 3}

		h.Write([]byte("abc"))
		h.Write([]byte("d"))

		assert.Equal(t, "abc", h.String())
	})
}

func TestCountWriter(t *testing.T) {
	var sink strings.Builder
	c := &countWriter{w: &sink}

	c.Write([]byte("hello "))
	c.Write([]byte("world"))

	assert.Equal(t, int64(11), c.n)
	assert.Equal(t, "hello world", sink.String())
}

// fakeFFmpeg puts an ffmpeg that is the given shell script first on PATH, and
// returns the file it records its arguments in, one per line. The script can use
// $ARGS to write that file.
func fakeFFmpeg(t *testing.T, script string) (argsFile string) {
	t.Helper()

	bin := t.TempDir()
	argsFile = filepath.Join(t.TempDir(), "args")
	body := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ARGS\"\n" + script + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(bin, "ffmpeg"), []byte(body), 0o755))

	t.Setenv("PATH", bin)
	t.Setenv("ARGS", argsFile)
	return argsFile
}

func TestFingerprintRunsFFmpegAsIntended(t *testing.T) {
	// What ffmpeg is asked, with nothing real behind it, so each choice is pinned
	// down whatever the installed version would do about it.
	args := fakeFFmpeg(t, "printf 'abcd'")

	_, err := Decoder{}.Fingerprint(context.Background(), "/rips/The Album/a:b.flac")

	require.NoError(t, err)
	got, err := os.ReadFile(args)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"-v", "error", // report errors, and nothing else
		"-nostdin",
		"-err_detect", "crccheck", // name a damaged frame
		"-i", "file:/rips/The Album/a:b.flac", // as a file, whatever the name looks like
		"-map", "0:a:0", // the first audio stream, not a picture
		"-f", "s16le", // raw samples, with no rate, channels or format changed
		"-",
	}, strings.Split(strings.TrimSuffix(string(got), "\n"), "\n"))
}

func TestFingerprintReadsFFmpegsVerdict(t *testing.T) {
	ctx := context.Background()

	t.Run("a message with a successful exit is a problem", func(t *testing.T) {
		// ffmpeg can exit 0 after a damaged frame, having said so.
		fakeFFmpeg(t, "printf 'abcd'; echo '[flac @ 0x55d85d2f3340] CRC error at PTS 0' >&2")

		fp, err := Decoder{}.Fingerprint(ctx, "x.flac")

		var de *DecodeError
		require.ErrorAs(t, err, &de)
		assert.Equal(t, "CRC error at PTS 0", de.Message)
		assert.Equal(t, "x.flac", de.Path)
		assert.Equal(t, int64(4), fp.Bytes, "the audio it did give is in the fingerprint")
	})

	t.Run("a failed exit with nothing said", func(t *testing.T) {
		fakeFFmpeg(t, "exit 3")

		_, err := Decoder{}.Fingerprint(ctx, "x.flac")

		var de *DecodeError
		require.ErrorAs(t, err, &de)
		assert.Contains(t, de.Message, "exit status 3")
	})

	t.Run("a failed exit with a message says the message", func(t *testing.T) {
		fakeFFmpeg(t, "echo 'x.flac: Invalid data found when processing input' >&2; exit 1")

		_, err := Decoder{}.Fingerprint(ctx, "x.flac")

		var de *DecodeError
		require.ErrorAs(t, err, &de)
		assert.Equal(t, "x.flac: Invalid data found when processing input", de.Message)
	})

	t.Run("a clean exit with no audio is no audio, not an error", func(t *testing.T) {
		// What that means is for the caller to judge, against what it expects.
		fakeFFmpeg(t, "exit 0")

		fp, err := Decoder{}.Fingerprint(ctx, "x.flac")

		require.NoError(t, err)
		assert.Equal(t, want(nil), fp)
	})

	t.Run("a great deal said is kept in check", func(t *testing.T) {
		fakeFFmpeg(t, "i=0; while [ $i -lt 20000 ]; do echo \"[flac @ 0x1] problem number $i with the frame\" >&2; i=$((i+1)); done")

		_, err := Decoder{}.Fingerprint(ctx, "x.flac")

		var de *DecodeError
		require.ErrorAs(t, err, &de)
		assert.Equal(t, "problem number 0 with the frame", de.Message)
	})
}

// ffmpegVersion returns the first line of what ffmpeg says of itself, for
// logging.
func ffmpegVersion() string {
	out, err := exec.Command("ffmpeg", "-version").Output()
	if err != nil {
		return "unknown: " + err.Error()
	}
	line, _, _ := strings.Cut(string(out), "\n")
	return line
}
