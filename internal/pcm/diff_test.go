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
	"errors"
	"fmt"
	"io"
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

// delayed is pcm moved later by k samples, with silence in front and the end cut:
// what a drive with an offset k greater gives. advanced is it moved earlier.
func delayed(pcm []byte, k int) []byte {
	out := make([]byte, k*bytesPerSample, len(pcm))
	return append(out, pcm[:len(pcm)-k*bytesPerSample]...)
}

func advanced(pcm []byte, k int) []byte {
	return append(append([]byte(nil), pcm[k*bytesPerSample:]...), make([]byte, k*bytesPerSample)...)
}

func TestDiffShift(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	const n = 700000 // 16 seconds: two of the places a shift is looked for are inside it
	base := audio(n, 61)

	diff := func(t *testing.T, name string, a, b []byte) Difference {
		t.Helper()
		fa := testutil.MakePCMFlac(t, dir, name+"-a.flac", a)
		fb := testutil.MakePCMFlac(t, dir, name+"-b.flac", b)
		d, err := Decoder{}.Diff(ctx, []string{fa}, []string{fb})
		require.NoError(t, err)
		return d
	}

	t.Run("audio moved later, by what drives differ by and what a pregap is", func(t *testing.T) {
		for _, k := range []int{1, 6, 667, 2500, 100000} {
			d := diff(t, fmt.Sprintf("later%d", k), base, delayed(base, k))

			require.NotNil(t, d.Shift, "shift %d", k)
			assert.Equal(t, int64(k), d.Shift.Samples, "shift %d", k)
			assert.Equal(t, 2, d.Shift.Matches, "both places agree")
			assert.Equal(t, 2, d.Shift.Tried)
			assert.False(t, d.Identical())
		}
	})

	t.Run("audio moved earlier", func(t *testing.T) {
		for _, k := range []int{1, 6, 667, -0 + 2500, 100000} {
			d := diff(t, fmt.Sprintf("earlier%d", k), base, advanced(base, k))

			require.NotNil(t, d.Shift, "shift %d", k)
			assert.Equal(t, int64(-k), d.Shift.Samples, "shift -%d", k)
		}
	})

	t.Run("a shift past what is looked for is not found", func(t *testing.T) {
		d := diff(t, "toofar", base, delayed(base, maxShift+1000))

		assert.Nil(t, d.Shift)
		assert.Greater(t, d.Differing, int64(n/2), "though nearly every sample differs")
	})

	t.Run("audio that is not shifted has no shift", func(t *testing.T) {
		d := diff(t, "same", base, base)
		assert.Nil(t, d.Shift)
		assert.True(t, d.Identical())

		d = diff(t, "local", base, changed(base, 300000, 450000))
		assert.Nil(t, d.Shift, "a few samples are wrong, and the rest is where it was")
		assert.Equal(t, int64(2), d.Differing)
	})

	t.Run("unrelated audio has no shift", func(t *testing.T) {
		d := diff(t, "unrelated", base, audio(n, 62))

		assert.Nil(t, d.Shift)
		assert.Greater(t, d.Differing, int64(n-1000))
	})

	t.Run("a shift that is only in part of it is not one shift", func(t *testing.T) {
		// Moved by 6 for the first part and not at all for the rest, as when a stretch
		// is inserted: the two places looked at disagree.
		partly := append(delayed(base[:350000*bytesPerSample], 6), base[350000*bytesPerSample:]...)
		d := diff(t, "partly", base, partly)

		assert.Nil(t, d.Shift)
		assert.Greater(t, d.Differing, int64(0))
	})

	t.Run("audio that is silent where it is looked at is skipped for another place", func(t *testing.T) {
		// Silent for the first 400000 samples and then music: only the later of
		// the places looked at has anything to find.
		quiet := append(make([]byte, 400000*bytesPerSample), base[:300000*bytesPerSample]...)
		d := diff(t, "quiet", quiet, delayed(quiet, 6))

		require.NotNil(t, d.Shift)
		assert.Equal(t, int64(6), d.Shift.Samples)
		assert.Equal(t, 1, d.Shift.Matches)
		assert.Equal(t, 1, d.Shift.Tried)
	})

	t.Run("all silence has nothing to find", func(t *testing.T) {
		silence := make([]byte, n*bytesPerSample)
		d := diff(t, "silence", silence, changed(silence, 100))

		assert.Nil(t, d.Shift)
		assert.Equal(t, int64(1), d.Differing)
	})

	t.Run("a sound that repeats exactly cannot be placed", func(t *testing.T) {
		// A pattern of 4096 samples over and over: a window of it is found at every
		// repeat, so where it was is not told by finding it.
		pattern := audio(4096, 63)
		var rep []byte
		for len(rep) < n*bytesPerSample {
			rep = append(rep, pattern...)
		}
		rep = rep[:n*bytesPerSample]
		d := diff(t, "repeat", rep, delayed(rep, 100))

		assert.Nil(t, d.Shift)
	})

	t.Run("audio too short to have places to look", func(t *testing.T) {
		short := audio(150000, 64)
		d := diff(t, "short", short, delayed(short, 6))

		assert.Nil(t, d.Shift)
		assert.Greater(t, d.Differing, int64(100000))
	})
}

func TestDiffShifted(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	base := audio(300000, 65)
	flac := func(t *testing.T, name string, pcm []byte) string {
		t.Helper()
		return testutil.MakePCMFlac(t, dir, name, pcm)
	}
	a := flac(t, "a.flac", base)

	t.Run("audio moved later, compared with that allowed for", func(t *testing.T) {
		b := flac(t, "later.flac", delayed(base, 667))

		d, err := Decoder{}.DiffShifted(ctx, []string{a}, []string{b}, 667)

		require.NoError(t, err)
		assert.Equal(t, int64(0), d.Differing)
		assert.Equal(t, int64(300000), d.SamplesA, "the lengths are of the whole of each")
		assert.Equal(t, int64(300000), d.SamplesB)
		assert.Nil(t, d.Shift, "it does not look for one")
	})

	t.Run("audio moved earlier", func(t *testing.T) {
		b := flac(t, "earlier.flac", advanced(base, 667))

		d, err := Decoder{}.DiffShifted(ctx, []string{a}, []string{b}, -667)

		require.NoError(t, err)
		assert.Equal(t, int64(0), d.Differing)
		assert.Equal(t, int64(300000), d.SamplesA, "the whole of each, though 667 of A were skipped")
		assert.Equal(t, int64(300000), d.SamplesB)
	})

	t.Run("it does not look for a shift, even when it is wrong about it", func(t *testing.T) {
		b := flac(t, "wrong.flac", delayed(base, 667))

		d, err := Decoder{}.DiffShifted(ctx, []string{a}, []string{b}, 100)

		require.NoError(t, err)
		assert.Greater(t, d.Differing, int64(250000), "it is not the shift it was told")
		assert.Nil(t, d.Shift)
	})

	t.Run("a shift of nothing is Diff", func(t *testing.T) {
		b := flac(t, "same.flac", changed(base, 1234))

		shifted, err := Decoder{}.DiffShifted(ctx, []string{a}, []string{b}, 0)
		require.NoError(t, err)
		plain, err := Decoder{}.Diff(ctx, []string{a}, []string{b})
		require.NoError(t, err)

		assert.Equal(t, plain.Differing, shifted.Differing)
		assert.Equal(t, plain.First, shifted.First)
	})

	t.Run("what still differs is placed by A's samples", func(t *testing.T) {
		// B moved later by 100, with the sample that is A's 5000th changed.
		b := flac(t, "later-changed.flac", changed(delayed(base, 100), 5100))

		later, err := Decoder{}.DiffShifted(ctx, []string{a}, []string{b}, 100)
		require.NoError(t, err)

		assert.Equal(t, int64(1), later.Differing)
		assert.Equal(t, int64(5000), later.First)

		// And moved earlier by 100, with A's 5000th, B's 4900th, changed.
		c := flac(t, "earlier-changed.flac", changed(advanced(base, 100), 4900))
		earlier, err := Decoder{}.DiffShifted(ctx, []string{a}, []string{c}, -100)
		require.NoError(t, err)

		assert.Equal(t, int64(1), earlier.Differing)
		assert.Equal(t, int64(5000), earlier.First)
	})

	t.Run("a shift past the end of one is nothing to compare", func(t *testing.T) {
		short := flac(t, "short.flac", base[:1000*bytesPerSample])

		d, err := Decoder{}.DiffShifted(ctx, []string{a}, []string{short}, 5000)

		require.NoError(t, err)
		assert.Equal(t, int64(0), d.Differing)
		assert.Equal(t, int64(1000), d.SamplesB)
		assert.Equal(t, int64(300000), d.SamplesA)

		d, err = Decoder{}.DiffShifted(ctx, []string{short}, []string{a}, -5000)
		require.NoError(t, err)
		assert.Equal(t, int64(0), d.Differing)
	})

	t.Run("a file that does not decode", func(t *testing.T) {
		bad := filepath.Join(dir, "bad.flac")
		require.NoError(t, os.WriteFile(bad, []byte("garbage"), 0o644))

		for _, shift := range []int64{0, 100, -100} {
			_, err := Decoder{}.DiffShifted(ctx, []string{a}, []string{bad}, shift)

			var de *DecodeError
			assert.ErrorAs(t, err, &de, "bad on the second side, shift %d", shift)

			_, err = Decoder{}.DiffShifted(ctx, []string{bad}, []string{a}, shift)
			assert.ErrorAs(t, err, &de, "bad on the first side, shift %d", shift)
		}
	})

	t.Run("a context that is cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		cancel()

		_, err := Decoder{}.DiffShifted(ctx, []string{a}, []string{a}, 100)

		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestAppendOverlap(t *testing.T) {
	// A buffer for the 8 bytes from position 10 on, fed chunks in order.
	fed := func(chunks ...[]byte) []byte {
		dst := make([]byte, 0, 8)
		at := int64(0)
		for _, c := range chunks {
			appendOverlap(&dst, 10, c, at)
			at += int64(len(c))
		}
		return dst
	}
	bytesFrom := func(from, to int) []byte {
		var b []byte
		for i := from; i < to; i++ {
			b = append(b, byte(i))
		}
		return b
	}

	assert.Equal(t, bytesFrom(10, 18), fed(bytesFrom(0, 30)), "all in one chunk")
	assert.Equal(t, bytesFrom(10, 18), fed(bytesFrom(0, 12), bytesFrom(12, 15), bytesFrom(15, 40)), "across several")
	assert.Equal(t, bytesFrom(10, 14), fed(bytesFrom(0, 14)), "as far as there is")
	assert.Equal(t, []byte{}, fed(bytesFrom(0, 10)), "ends before it begins")
	assert.Equal(t, []byte{}, fed(bytesFrom(20, 30)[:0], bytesFrom(40, 50)), "begins after it ends")
	assert.Equal(t, bytesFrom(10, 18), fed(bytesFrom(0, 18), bytesFrom(18, 30)), "a chunk that ends where it ends")

	t.Run("a chunk that does not follow on is not added", func(t *testing.T) {
		dst := make([]byte, 0, 8)
		appendOverlap(&dst, 10, bytesFrom(0, 12), 0)   // positions 10 and 11
		appendOverlap(&dst, 10, bytesFrom(16, 20), 16) // a gap: 12 to 15 were not given

		assert.Equal(t, bytesFrom(10, 12), dst)
	})
}

func TestFindAligned(t *testing.T) {
	win := []byte{1, 2, 3, 4, 5, 6, 7, 8}

	t.Run("found once, on a sample", func(t *testing.T) {
		region := append(append(make([]byte, 12), win...), make([]byte, 8)...)

		at, ok := findAligned(region, win)

		assert.True(t, ok)
		assert.Equal(t, 12, at)
	})

	t.Run("not there", func(t *testing.T) {
		_, ok := findAligned(make([]byte, 100), win)
		assert.False(t, ok)
	})

	t.Run("there twice", func(t *testing.T) {
		region := append(append(append(make([]byte, 8), win...), make([]byte, 4)...), win...)

		_, ok := findAligned(region, win)

		assert.False(t, ok, "so it does not say where")
	})

	t.Run("a match across samples is not one", func(t *testing.T) {
		region := append(append(make([]byte, 6), win...), make([]byte, 6)...) // starts 2 bytes into a sample

		_, ok := findAligned(region, win)

		assert.False(t, ok)
	})

	t.Run("one on a sample, and another that is not, is one", func(t *testing.T) {
		region := append(append(append(make([]byte, 8), win...), make([]byte, 6)...), win...)

		at, ok := findAligned(region, win)

		assert.True(t, ok)
		assert.Equal(t, 8, at)
	})

	t.Run("a region shorter than the window", func(t *testing.T) {
		_, ok := findAligned(win[:4], win)
		assert.False(t, ok)
	})
}

func TestDetectShift(t *testing.T) {
	// Captures made by hand: a window of A, and a region of B that has it k samples
	// on from where it was.
	win := audio(windowSamples, 66)
	mk := func(centre int64, k int64) *capture {
		c := newCapture(centre)
		c.window = append(c.window, win...)
		pad := make([]byte, (maxShift+k)*bytesPerSample)
		c.region = append(append(c.region, pad...), win...)
		return c
	}

	assert.Nil(t, detectShift(nil), "nothing looked at")

	s := detectShift([]*capture{mk(200000, 6), mk(500000, 6)})
	require.NotNil(t, s)
	assert.Equal(t, Shift{Samples: 6, Matches: 2, Tried: 2}, *s)

	assert.Nil(t, detectShift([]*capture{mk(200000, 6), mk(500000, 7)}), "they disagree")
	assert.Nil(t, detectShift([]*capture{mk(200000, 0)}), "found where it was")
	assert.Nil(t, detectShift([]*capture{mk(200000, 0), mk(500000, 6)}), "one where it was and one not")

	t.Run("a place that is not found does not count against one that is", func(t *testing.T) {
		unmatched := newCapture(500000)
		unmatched.window = append(unmatched.window, win...)
		unmatched.region = append(unmatched.region, make([]byte, 1000*bytesPerSample)...)

		s := detectShift([]*capture{mk(200000, 6), unmatched})

		require.NotNil(t, s)
		assert.Equal(t, Shift{Samples: 6, Matches: 1, Tried: 2}, *s)
	})

	t.Run("a window that is not all there is not used", func(t *testing.T) {
		partial := mk(200000, 6)
		partial.window = partial.window[:100]

		assert.Nil(t, detectShift([]*capture{partial}))
	})

	t.Run("a window that is only partly there is not used, however distinctive", func(t *testing.T) {
		partial := mk(200000, 6)
		partial.window = partial.window[:1000*bytesPerSample] // 1000 different samples, but not all it should be

		assert.Nil(t, detectShift([]*capture{partial}))
	})

	t.Run("a window that is silence is not used", func(t *testing.T) {
		quiet := newCapture(200000)
		quiet.window = append(quiet.window, make([]byte, windowSamples*bytesPerSample)...)
		quiet.region = append(quiet.region, make([]byte, 5000*bytesPerSample)...)

		assert.Nil(t, detectShift([]*capture{quiet}))
	})
}

func TestDistinctSamples(t *testing.T) {
	assert.Equal(t, 0, distinctSamples(nil))
	assert.Equal(t, 1, distinctSamples(make([]byte, 40)))
	assert.Equal(t, 2, distinctSamples([]byte{1, 0, 0, 0, 2, 0, 0, 0, 1, 0, 0, 0}))
	assert.Equal(t, 1, distinctSamples([]byte{1, 0, 0, 0, 1, 0, 0}), "a part of a sample at the end is not counted")
}

func TestSkipBytes(t *testing.T) {
	t.Run("what is skipped", func(t *testing.T) {
		n, ended, err := skipBytes(bytes.NewReader(make([]byte, 100)), 40)
		require.NoError(t, err)
		assert.Equal(t, int64(40), n)
		assert.False(t, ended)
	})
	t.Run("a stream that is shorter", func(t *testing.T) {
		n, ended, err := skipBytes(bytes.NewReader(make([]byte, 10)), 40)
		require.NoError(t, err)
		assert.Equal(t, int64(10), n)
		assert.True(t, ended)
	})
	t.Run("nothing", func(t *testing.T) {
		n, ended, err := skipBytes(bytes.NewReader(make([]byte, 10)), 0)
		require.NoError(t, err)
		assert.Equal(t, int64(0), n)
		assert.False(t, ended)
	})
	t.Run("a stream that fails", func(t *testing.T) {
		pr, pw := io.Pipe()
		pw.CloseWithError(errors.New("broken"))

		_, _, err := skipBytes(pr, 5)

		assert.ErrorContains(t, err, "broken")
	})
}
