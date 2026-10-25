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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/testutil"
)

// changed returns pcm with the sample at index i made different.
func changed(pcm []byte, i ...int) []byte {
	out := append([]byte(nil), pcm...)
	for _, n := range i {
		out[n*bytesPerSample] ^= 0x01
	}
	return out
}

func TestDiff(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	flac := func(t *testing.T, name string, pcm []byte, args ...string) string {
		t.Helper()
		return testutil.MakePCMFlac(t, dir, name, pcm, args...)
	}
	diff := func(t *testing.T, a, b []string) Difference {
		t.Helper()
		d, err := Decoder{}.Diff(ctx, a, b)
		require.NoError(t, err)
		return d
	}

	base := audio(50000, 51) // 200 KB: three chunks and a part

	t.Run("the same audio, stored differently", func(t *testing.T) {
		a := flac(t, "a.flac", base, "-compression_level", "0")
		b := flac(t, "b.flac", base, "-compression_level", "8", "-metadata", "TITLE=x")

		d := diff(t, []string{a}, []string{b})

		assert.True(t, d.Identical())
		assert.Equal(t, Difference{SamplesA: 50000, SamplesB: 50000, First: -1, Last: -1}, d)
	})

	t.Run("one sample", func(t *testing.T) {
		a := flac(t, "one-a.flac", base)
		b := flac(t, "one-b.flac", changed(base, 12345))

		d := diff(t, []string{a}, []string{b})

		assert.False(t, d.Identical())
		assert.Equal(t, int64(1), d.Differing)
		assert.Equal(t, int64(12345), d.First)
		assert.Equal(t, int64(12345), d.Last)
		assert.Equal(t, []Range{{12345, 12346}}, d.Runs)
		assert.Equal(t, int64(50000), d.SamplesA)
		assert.Equal(t, int64(50000), d.SamplesB)
	})

	t.Run("the first and the last sample", func(t *testing.T) {
		a := flac(t, "ends-a.flac", base)
		b := flac(t, "ends-b.flac", changed(base, 0, 49999))

		d := diff(t, []string{a}, []string{b})

		assert.Equal(t, int64(2), d.Differing)
		assert.Equal(t, int64(0), d.First)
		assert.Equal(t, int64(49999), d.Last)
		assert.Equal(t, []Range{{0, 1}, {49999, 50000}}, d.Runs)
	})

	t.Run("either side of a boundary between the chunks that are compared", func(t *testing.T) {
		// A chunk is 16384 samples. The samples either side of the boundary are
		// in different reads, and still one run.
		const boundary = diffChunk / bytesPerSample
		a := flac(t, "chunk-a.flac", base)
		b := flac(t, "chunk-b.flac", changed(base, boundary-1, boundary))

		d := diff(t, []string{a}, []string{b})

		assert.Equal(t, int64(2), d.Differing)
		assert.Equal(t, int64(boundary-1), d.First)
		assert.Equal(t, int64(boundary), d.Last)
		assert.Equal(t, []Range{{boundary - 1, boundary + 1}}, d.Runs)
	})

	t.Run("a difference in each of several chunks, at the first and last sample of one", func(t *testing.T) {
		const chunk = diffChunk / bytesPerSample
		a := flac(t, "many-a.flac", base)
		b := flac(t, "many-b.flac", changed(base, 0, chunk-1, chunk, 2*chunk-1, 2*chunk, 3*chunk))

		d := diff(t, []string{a}, []string{b})

		assert.Equal(t, int64(6), d.Differing)
		assert.Equal(t, int64(0), d.First)
		assert.Equal(t, int64(3*chunk), d.Last)
	})

	t.Run("samples near one another are one run, and far apart are two", func(t *testing.T) {
		a := flac(t, "runs-a.flac", base)
		// The first two have exactly runGap samples between them, which is one run;
		// the second two have one more, which is two.
		b := flac(t, "runs-b.flac", changed(base, 1000, 1000+runGap+1, 20000, 20000+runGap+2))

		d := diff(t, []string{a}, []string{b})

		assert.Equal(t, int64(4), d.Differing)
		assert.Equal(t, []Range{{1000, 1000 + runGap + 2}, {20000, 20001}, {20000 + runGap + 2, 20000 + runGap + 3}}, d.Runs)
		assert.False(t, d.RunsTruncated)
	})

	t.Run("a stretch of audio that differs throughout is a few runs", func(t *testing.T) {
		a := flac(t, "noise-a.flac", base)
		b := flac(t, "noise-b.flac", audio(50000, 52))

		d := diff(t, []string{a}, []string{b})

		assert.Greater(t, d.Differing, int64(49000), "nearly every sample")
		assert.Equal(t, []Range{{d.First, d.Last + 1}}, d.Runs, "all of it, as one run")
	})

	t.Run("more runs than are kept", func(t *testing.T) {
		var at []int
		for i := range maxRuns + 4 {
			at = append(at, 100+i*(runGap+10))
		}
		a := flac(t, "trunc-a.flac", base)
		b := flac(t, "trunc-b.flac", changed(base, at...))

		d := diff(t, []string{a}, []string{b})

		assert.Equal(t, int64(maxRuns+4), d.Differing, "all are counted")
		assert.Len(t, d.Runs, maxRuns)
		assert.True(t, d.RunsTruncated)
		assert.Equal(t, int64(at[len(at)-1]), d.Last)
	})

	t.Run("one side shorter, and the rest the same", func(t *testing.T) {
		a := flac(t, "len-a.flac", base)
		b := flac(t, "len-b.flac", base[:40000*bytesPerSample])

		d := diff(t, []string{a}, []string{b})

		assert.False(t, d.Identical())
		assert.Equal(t, int64(0), d.Differing, "what both have is the same")
		assert.Equal(t, int64(50000), d.SamplesA)
		assert.Equal(t, int64(40000), d.SamplesB)
		assert.Equal(t, int64(-1), d.First)
	})

	t.Run("the other side shorter", func(t *testing.T) {
		a := flac(t, "len2-a.flac", base[:30000*bytesPerSample])
		b := flac(t, "len2-b.flac", base)

		d := diff(t, []string{a}, []string{b})

		assert.Equal(t, int64(30000), d.SamplesA)
		assert.Equal(t, int64(50000), d.SamplesB)
		assert.Equal(t, int64(0), d.Differing)
	})

	t.Run("a difference, and a difference in length", func(t *testing.T) {
		a := flac(t, "both-a.flac", base)
		b := flac(t, "both-b.flac", changed(base[:45000*bytesPerSample], 7))

		d := diff(t, []string{a}, []string{b})

		assert.Equal(t, int64(1), d.Differing)
		assert.Equal(t, int64(7), d.First)
		assert.Equal(t, int64(50000), d.SamplesA)
		assert.Equal(t, int64(45000), d.SamplesB)
	})

	t.Run("a side that is many files is one stretch of audio", func(t *testing.T) {
		// An image against the tracks it was split into.
		image := flac(t, "image.flac", base)
		t1 := flac(t, "t1.flac", base[:10000*bytesPerSample])
		t2 := flac(t, "t2.flac", base[10000*bytesPerSample:35000*bytesPerSample])
		t3 := flac(t, "t3.flac", base[35000*bytesPerSample:])

		d := diff(t, []string{image}, []string{t1, t2, t3})

		assert.True(t, d.Identical(), "%+v", d)

		swapped := diff(t, []string{image}, []string{t2, t1, t3})
		assert.Equal(t, int64(50000), swapped.SamplesB)
		assert.Greater(t, swapped.Differing, int64(0))
		assert.Equal(t, int64(0), swapped.First, "the first sample is already another track's")
	})

	t.Run("a boundary cut a sample late is not seen, since the audio is the same", func(t *testing.T) {
		// Which is why the lengths of a split's tracks are checked as well.
		image := flac(t, "shift-image.flac", base)
		t1 := flac(t, "s1.flac", base[:10001*bytesPerSample])
		t2 := flac(t, "s2.flac", base[10001*bytesPerSample:])

		assert.True(t, diff(t, []string{image}, []string{t1, t2}).Identical())
	})

	t.Run("nothing on either side", func(t *testing.T) {
		d := diff(t, nil, nil)

		assert.True(t, d.Identical())
		assert.Equal(t, Difference{First: -1, Last: -1}, d)
	})

	t.Run("nothing on one side", func(t *testing.T) {
		a := flac(t, "some.flac", base)

		d := diff(t, []string{a}, nil)

		assert.False(t, d.Identical())
		assert.Equal(t, int64(50000), d.SamplesA)
		assert.Equal(t, int64(0), d.SamplesB)
		assert.Equal(t, int64(0), d.Differing)
	})

	t.Run("a file with a part of a sample at its end", func(t *testing.T) {
		// A stream whose length is not a whole number of samples has only the
		// whole ones counted and compared.
		bin := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(bin, "ffmpeg"), []byte("#!/bin/sh\nprintf 'abcdefg'\n"), 0o755))
		t.Setenv("PATH", bin)

		d, err := Decoder{}.Diff(ctx, []string{"x"}, []string{"y"})

		require.NoError(t, err)
		assert.True(t, d.Identical())
		assert.Equal(t, int64(1), d.SamplesA)
	})
}

func TestDiffIgnoresAPartOfASampleAtTheEnd(t *testing.T) {
	// 7 bytes is one sample and 3 bytes over, which are not a sample and are not
	// compared, so streams that differ only in them are the same.
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "ffmpeg"), []byte(`#!/bin/sh
case "$*" in
  *other*) printf 'abcdefh' ;;
  *) printf 'abcdefg' ;;
esac
`), 0o755))
	t.Setenv("PATH", bin)

	d, err := Decoder{}.Diff(context.Background(), []string{"one"}, []string{"other"})

	require.NoError(t, err)
	assert.True(t, d.Identical(), "%+v", d)
	assert.Equal(t, int64(1), d.SamplesA)
	assert.Equal(t, int64(1), d.SamplesB)
}

func TestDiffAccessors(t *testing.T) {
	assert.True(t, Difference{SamplesA: 5, SamplesB: 5}.Identical())
	assert.False(t, Difference{SamplesA: 5, SamplesB: 6}.Identical())
	assert.False(t, Difference{SamplesA: 5, SamplesB: 5, Differing: 1}.Identical())
}

func TestDiffErrors(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	good := testutil.MakePCMFlac(t, dir, "good.flac", audio(20000, 53))
	bad := filepath.Join(dir, "bad.flac")
	require.NoError(t, os.WriteFile(bad, []byte("garbage"), 0o644))

	t.Run("a file that does not decode, on either side, is named", func(t *testing.T) {
		for name, sides := range map[string][2][]string{
			"the first":                {{bad}, {good}},
			"the second":               {{good}, {bad}},
			"in the middle of several": {{good, good}, {good, bad, good}},
		} {
			_, err := Decoder{}.Diff(ctx, sides[0], sides[1])

			var de *DecodeError
			require.ErrorAs(t, err, &de, name)
			assert.Equal(t, bad, de.Path, name)
		}
	})

	t.Run("no ffmpeg", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())

		_, err := Decoder{}.Diff(ctx, []string{good}, []string{good})

		assert.ErrorIs(t, err, ErrNoFFmpeg)
	})

	t.Run("a context that is cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		cancel()

		_, err := Decoder{}.Diff(ctx, []string{good}, []string{good})

		assert.ErrorIs(t, err, context.Canceled)
	})

	// An error on one side must not leave the other side's ffmpeg writing to a
	// pipe nobody reads. These use an ffmpeg that fails at once for a name with
	// "bad" in it and otherwise writes 50 MB.
	t.Run("a side that fails does not wait for one that is still going", func(t *testing.T) {
		fakeFFmpeg(t, `PATH=/usr/bin:/bin:$PATH
case "$*" in
  *bad*) echo "bad file" >&2; exit 1 ;;
  *) head -c 50000000 /dev/zero ;;
esac`)

		for name, sides := range map[string][2][]string{
			"the first fails":  {{"bad.flac"}, {"good.flac"}},
			"the second fails": {{"good.flac"}, {"bad.flac"}},
		} {
			done := make(chan error, 1)
			go func() {
				_, err := Decoder{}.Diff(ctx, sides[0], sides[1])
				done <- err
			}()

			select {
			case err := <-done:
				var de *DecodeError
				assert.ErrorAs(t, err, &de, name)
				assert.Equal(t, "bad file", de.Message, name)
			case <-time.After(20 * time.Second):
				t.Fatalf("%s: Diff did not return", name)
			}
		}
	})

	t.Run("a diff that ends early, on a difference, does not leave ffmpeg running", func(t *testing.T) {
		// Both sides are 50 MB of the same thing; it is compared to the end.
		fakeFFmpeg(t, "PATH=/usr/bin:/bin:$PATH; head -c 50000000 /dev/zero")

		done := make(chan Difference, 1)
		go func() {
			d, _ := Decoder{}.Diff(ctx, []string{"a"}, []string{"b"})
			done <- d
		}()

		select {
		case d := <-done:
			assert.True(t, d.Identical())
			assert.Equal(t, int64(50000000/bytesPerSample), d.SamplesA)
		case <-time.After(30 * time.Second):
			t.Fatal("Diff did not return")
		}
	})
}
