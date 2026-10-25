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
	"bytes"
	"context"
	"crypto/md5"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/testutil"
)

// failingReader gives some bytes and then fails.
type failingReader struct {
	data []byte
	err  error
}

func (f *failingReader) Read(p []byte) (int, error) {
	if len(f.data) == 0 {
		return 0, f.err
	}
	n := copy(p, f.data)
	f.data = f.data[n:]
	return n, nil
}

// sameAudio asserts that two stretches of audio are the same, saying how they
// differ if not, and never printing the audio.
func sameAudio(t *testing.T, want, got []byte) {
	t.Helper()
	require.Equal(t, len(want), len(got), "the length in bytes")
	assert.True(t, bytes.Equal(want, got), "the samples differ")
}

func TestEncode(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	encode := func(t *testing.T, name string, data []byte, tags ...Tag) string {
		t.Helper()
		path := filepath.Join(dir, name)
		require.NoError(t, Decoder{}.Encode(ctx, bytes.NewReader(data), path, tags))
		return path
	}

	t.Run("what is encoded decodes to the same samples", func(t *testing.T) {
		for _, n := range []int{1, 2, 100, 588, 4096, 4097, 100000, 1000003} {
			data := audio(n, int64(n))
			path := encode(t, "exact.flac", data)

			fp, err := Decoder{}.Fingerprint(ctx, path)

			require.NoError(t, err, "%d samples", n)
			assert.Equal(t, int64(n), fp.Samples(), "%d samples", n)
			assert.Equal(t, crc32.ChecksumIEEE(data), fp.CRC32, "%d samples", n)
			assert.Equal(t, md5.Sum(data), fp.MD5, "%d samples", n)
		}
	})

	t.Run("silence and full-scale samples", func(t *testing.T) {
		for name, data := range map[string][]byte{
			"silence":    make([]byte, 40000),
			"full scale": bytes.Repeat([]byte{0xff, 0x7f, 0x00, 0x80}, 10000),
		} {
			path := encode(t, "extreme.flac", data)

			fp, err := Decoder{}.Fingerprint(ctx, path)

			require.NoError(t, err, name)
			assert.Equal(t, crc32.ChecksumIEEE(data), fp.CRC32, name)
		}
	})

	t.Run("the tags given, byte for byte, in order, and nothing else", func(t *testing.T) {
		title := "Ünï Cödé – \"Ørjan\" ♥ 日本語 & Beyoncé's ¿Qué?"
		path := encode(t, "tags.flac", audio(2000, 1),
			Tag{"TITLE", title},
			Tag{"ARTIST", "AC/DC"},
			Tag{"TRACKNUMBER", "3"},
			Tag{"NOTE", "a = sign, and a \\ backslash"},
			Tag{"LONG", strings.Repeat("x", 300)},
			Tag{"MUSICBRAINZ_ALBUMID", "0a4a8b6e-0000-4000-8000-000000000000"},
		)

		vendor, comments := testutil.FLACComments(t, path)

		assert.Equal(t, []string{
			"TITLE=" + title,
			"ARTIST=AC/DC",
			"TRACKNUMBER=3",
			"NOTE=a = sign, and a \\ backslash",
			"LONG=" + strings.Repeat("x", 300),
			"MUSICBRAINZ_ALBUMID=0a4a8b6e-0000-4000-8000-000000000000",
		}, comments)
		assert.NotContains(t, vendor, "Lavf", "no version of ffmpeg is said")
	})

	t.Run("no tags is no comments, and no mention of ffmpeg", func(t *testing.T) {
		path := encode(t, "none.flac", audio(2000, 2))

		vendor, comments := testutil.FLACComments(t, path)

		assert.Empty(t, comments, "not even an encoder tag")
		assert.NotContains(t, vendor, "Lavf")
	})

	t.Run("a tag with nothing in it is left out", func(t *testing.T) {
		path := encode(t, "empty.flac", audio(2000, 3), Tag{"TITLE", ""}, Tag{"ARTIST", "x"})

		_, comments := testutil.FLACComments(t, path)

		assert.Equal(t, []string{"ARTIST=x"}, comments)
	})

	t.Run("a file that is there is replaced", func(t *testing.T) {
		path := encode(t, "again.flac", audio(5000, 4), Tag{"TITLE", "first"})
		again := audio(3000, 5)
		require.NoError(t, Decoder{}.Encode(ctx, bytes.NewReader(again), path, []Tag{{"TITLE", "second"}}))

		fp, err := Decoder{}.Fingerprint(ctx, path)

		require.NoError(t, err)
		assert.Equal(t, int64(3000), fp.Samples())
		_, comments := testutil.FLACComments(t, path)
		assert.Equal(t, []string{"TITLE=second"}, comments)
	})

	t.Run("a name that is not a flac name, as a file not finished has", func(t *testing.T) {
		path := encode(t, ".01 Title.flac.partial", audio(3000, 6))

		fp, err := Decoder{}.Fingerprint(ctx, path)

		require.NoError(t, err)
		assert.Equal(t, int64(3000), fp.Samples())
	})

	t.Run("a name with a colon in it is not a URL", func(t *testing.T) {
		path := encode(t, "a:b.flac", audio(3000, 7))

		_, err := os.Stat(path)

		assert.NoError(t, err)
	})
}

func TestStreamAndEncode(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	t.Run("a stream read to the end is the audio of the file", func(t *testing.T) {
		data := audio(70000, 11)
		src := testutil.MakePCMFlac(t, dir, "src.flac", data)

		rc := Decoder{}.Stream(ctx, src)
		got, err := io.ReadAll(rc)
		require.NoError(t, rc.Close())

		require.NoError(t, err)
		sameAudio(t, data, got)
	})

	t.Run("pieces of a stream, encoded, are the audio of the file", func(t *testing.T) {
		// What a split does: one decode, cut by exact counts, each piece a file.
		data := audio(70000, 12)
		src := testutil.MakePCMFlac(t, dir, "album.flac", data)
		cuts := []int{1, 39999, 25000, 5000} // samples, which are 70000 together

		rc := Decoder{}.Stream(ctx, src)
		defer rc.Close()
		var files []string
		for i, n := range cuts {
			path := filepath.Join(dir, "piece"+string(rune('a'+i))+".flac")
			require.NoError(t, Decoder{}.Encode(ctx, io.LimitReader(rc, int64(n)*4), path, []Tag{{"TRACKNUMBER", "1"}}))
			files = append(files, path)
		}
		rest, err := io.ReadAll(rc)
		require.NoError(t, err)
		assert.Empty(t, rest, "nothing is left")

		var joined []byte
		for _, f := range files {
			rc := Decoder{}.Stream(ctx, f)
			b, err := io.ReadAll(rc)
			rc.Close()
			require.NoError(t, err)
			joined = append(joined, b...)
		}
		sameAudio(t, data, joined)
	})

	t.Run("a stream closed early stops", func(t *testing.T) {
		src := testutil.MakePCMFlac(t, dir, "big.flac", audio(300000, 13))
		rc := Decoder{}.Stream(ctx, src)
		_, err := io.ReadFull(rc, make([]byte, 1000))
		require.NoError(t, err)

		done := make(chan struct{})
		go func() {
			rc.Close()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(20 * time.Second):
			t.Fatal("Close did not return")
		}
		_, err = rc.Read(make([]byte, 10))
		assert.Error(t, err)
	})

	t.Run("a file that does not decode ends its stream with the error, and Encode gives it", func(t *testing.T) {
		bad := filepath.Join(dir, "bad.flac")
		require.NoError(t, os.WriteFile(bad, []byte("garbage"), 0o644))

		rc := Decoder{}.Stream(ctx, bad)
		defer rc.Close()
		err := Decoder{}.Encode(ctx, rc, filepath.Join(dir, "out.flac"), nil)

		var de *DecodeError
		require.ErrorAs(t, err, &de)
		assert.Equal(t, bad, de.Path)
	})

	t.Run("no ffmpeg", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())

		rc := Decoder{}.Stream(ctx, "x.flac")
		_, readErr := io.ReadAll(rc)
		rc.Close()
		encErr := Decoder{}.Encode(ctx, bytes.NewReader(audio(100, 1)), filepath.Join(dir, "none.flac"), nil)

		assert.ErrorIs(t, readErr, ErrNoFFmpeg)
		assert.ErrorIs(t, encErr, ErrNoFFmpeg)
	})
}

func TestEncodeFailures(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	t.Run("a reader that fails midway, with the error it failed with", func(t *testing.T) {
		boom := errors.New("the disk is on fire")

		err := Decoder{}.Encode(ctx, &failingReader{data: audio(5000, 1), err: boom}, filepath.Join(dir, "x.flac"), nil)

		assert.ErrorIs(t, err, boom)
	})

	t.Run("a file that cannot be written", func(t *testing.T) {
		path := filepath.Join(dir, "no", "such", "directory", "x.flac")

		err := Decoder{}.Encode(ctx, bytes.NewReader(audio(5000, 2)), path, nil)

		var ee *EncodeError
		require.ErrorAs(t, err, &ee)
		assert.Equal(t, path, ee.Path)
		assert.NotEmpty(t, ee.Message)
		assert.Contains(t, ee.Error(), "could not write "+path)
	})

	t.Run("an ffmpeg that fails and says nothing", func(t *testing.T) {
		fakeFFmpeg(t, "exit 1")

		err := Decoder{}.Encode(ctx, bytes.NewReader(audio(5000, 4)), filepath.Join(dir, "q.flac"), nil)

		var ee *EncodeError
		require.ErrorAs(t, err, &ee)
		assert.Equal(t, "exit status 1", ee.Message)
	})

	t.Run("a context that is cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		cancel()

		err := Decoder{}.Encode(ctx, bytes.NewReader(audio(5000, 3)), filepath.Join(dir, "c.flac"), nil)

		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("nothing to encode", func(t *testing.T) {
		path := filepath.Join(dir, "empty.flac")

		err := Decoder{}.Encode(ctx, bytes.NewReader(nil), path, nil)

		// What ffmpeg makes of no audio is for it to say; it is not a file to use, and
		// whoever gave it nothing knows.
		if err == nil {
			fp, ferr := Decoder{}.Fingerprint(ctx, path)
			require.NoError(t, ferr)
			assert.Equal(t, int64(0), fp.Samples())
		}
	})
}
