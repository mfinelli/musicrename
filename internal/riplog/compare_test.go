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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/pcm"
	"github.com/mfinelli/musicrename/internal/testutil"
)

// fakeComparer is a Comparer that gives what a test tells it.
type fakeComparer struct {
	diff       pcm.Difference
	err        error
	shifted    pcm.Difference
	shiftedErr error

	calls        [][2][]string
	shiftedCalls []int64
}

func (f *fakeComparer) Diff(_ context.Context, a, b []string) (pcm.Difference, error) {
	f.calls = append(f.calls, [2][]string{a, b})
	return f.diff, f.err
}

func (f *fakeComparer) DiffShifted(_ context.Context, _, _ []string, shift int64) (pcm.Difference, error) {
	f.shiftedCalls = append(f.shiftedCalls, shift)
	return f.shifted, f.shiftedErr
}

// cdPair writes two CD-audio FLAC files that are only a header, and returns their
// paths.
func cdPair(t *testing.T) (a, b string) {
	t.Helper()
	dir := t.TempDir()
	a, b = filepath.Join(dir, "a.flac"), filepath.Join(dir, "b.flac")
	require.NoError(t, os.WriteFile(a, withMD5(cdFLAC(1000), testMD5), 0o644))
	require.NoError(t, os.WriteFile(b, withMD5(cdFLAC(1000), testMD5), 0o644))
	return a, b
}

// rng is a stretch of samples.
func rng(start, end int64) pcm.Range { return pcm.Range{Start: start, End: end} }

// differing is a Difference for audio of the given lengths, which differs in the
// given way.
func differing(a, b, n, first, last int64, runs ...pcm.Range) pcm.Difference {
	return pcm.Difference{SamplesA: a, SamplesB: b, Differing: n, First: first, Last: last, Runs: runs}
}

func TestCompareFilesFormat(t *testing.T) {
	good := withMD5(cdFLAC(1000), testMD5)
	write := func(t *testing.T, header []byte) (a, b string) {
		t.Helper()
		a, b = cdPair(t)
		require.NoError(t, os.WriteFile(b, header, 0o644))
		return a, b
	}

	t.Run("two CD-audio FLAC files", func(t *testing.T) {
		a, b := cdPair(t)
		dec := &fakeComparer{diff: pcm.Difference{SamplesA: 1000, SamplesB: 1000, First: -1, Last: -1}}

		r := CompareFiles(context.Background(), a, b, dec)

		assert.Equal(t, "both are 44.1 kHz, 16-bit, stereo FLAC", want(t, r, "compare-format", StatusPass).Detail)
		assert.Equal(t, []string{"compare-format", "compare-audio"}, ids(r))
		assert.Equal(t, [][2][]string{{{a}, {b}}}, dec.calls, "A and B, as they were given")
	})

	for name, tc := range map[string]struct {
		header []byte
		detail string
	}{
		"24-bit":    {flacHeader(44100, 2, 24, 1000), "B (b.flac): 44.1 kHz, 24-bit, stereo"},
		"48 kHz":    {flacHeader(48000, 2, 16, 1000), "B (b.flac): 48 kHz, 16-bit, stereo"},
		"mono":      {flacHeader(44100, 1, 16, 1000), "B (b.flac): 44.1 kHz, 16-bit, mono"},
		"not FLAC":  {[]byte("ID3 this is an mp3, or something"), "B (b.flac): not a FLAC file"},
		"empty":     {nil, "B (b.flac): not a FLAC file"},
		"too short": {good[:20], "B (b.flac): not a FLAC file"},
		"surround":  {flacHeader(44100, 6, 16, 1000), "B (b.flac): 44.1 kHz, 16-bit, 6 channels"},
	} {
		t.Run(name+" is refused, and nothing is decoded", func(t *testing.T) {
			a, b := write(t, tc.header)
			dec := &fakeComparer{}

			r := CompareFiles(context.Background(), a, b, dec)

			c := want(t, r, "compare-format", StatusError)
			assert.Contains(t, c.Detail, tc.detail)
			assert.Contains(t, c.Detail, "only CD audio, 44.1 kHz, 16-bit, stereo, in FLAC, can be compared exactly")
			assert.Empty(t, dec.calls)
			assert.Equal(t, []string{"compare-format"}, ids(r))
		})
	}

	t.Run("both are named when both are wrong", func(t *testing.T) {
		a, b := cdPair(t)
		require.NoError(t, os.WriteFile(a, flacHeader(48000, 2, 16, 1), 0o644))
		require.NoError(t, os.WriteFile(b, []byte("x"), 0o644))

		r := CompareFiles(context.Background(), a, b, &fakeComparer{})

		c := want(t, r, "compare-format", StatusError)
		assert.Contains(t, c.Detail, "A (a.flac): 48 kHz")
		assert.Contains(t, c.Detail, "; B (b.flac): not a FLAC file")
	})

	t.Run("a file that is not there", func(t *testing.T) {
		a, _ := cdPair(t)

		r := CompareFiles(context.Background(), a, filepath.Join(t.TempDir(), "gone.flac"), &fakeComparer{})

		assert.Contains(t, want(t, r, "compare-format", StatusError).Detail, "B (gone.flac): cannot read")
	})
}

func TestCompareFilesReports(t *testing.T) {
	a, b := cdPair(t)
	compare := func(t *testing.T, dec *fakeComparer) Check {
		t.Helper()
		r := CompareFiles(context.Background(), a, b, dec)
		return want(t, r, "compare-audio", StatusError)
	}
	identical := func(t *testing.T, d pcm.Difference) Check {
		t.Helper()
		return compare(t, &fakeComparer{diff: d})
	}
	const n = 1000000

	t.Run("the same audio", func(t *testing.T) {
		r := CompareFiles(context.Background(), a, b, &fakeComparer{diff: pcm.Difference{SamplesA: 12345, SamplesB: 12345, First: -1, Last: -1}})

		assert.Equal(t, "the same audio: 12345 samples", want(t, r, "compare-audio", StatusPass).Detail)
		assert.False(t, r.Failed())
	})

	t.Run("one sample", func(t *testing.T) {
		c := identical(t, differing(n, n, 1, 12345, 12345, pcm.Range{Start: 12345, End: 12346}))

		assert.Equal(t, "1 of 1000000 samples differ, from sample 12345 (00:00:20) to sample 12345 (00:00:20)", c.Detail)
	})

	t.Run("a few samples at the start", func(t *testing.T) {
		c := identical(t, differing(n, n, 6, 0, 5, pcm.Range{Start: 0, End: 6}))

		assert.Equal(t, "6 of 1000000 samples differ, from sample 0 (00:00:00) to sample 5 (00:00:00), all within the first 6 samples", c.Detail)
	})

	t.Run("a few samples at the end", func(t *testing.T) {
		c := identical(t, differing(n, n, 6, n-6, n-1, pcm.Range{Start: n - 6, End: n}))

		assert.Contains(t, c.Detail, ", all within the last 6 samples")
	})

	t.Run("a few at each end are two stretches", func(t *testing.T) {
		c := identical(t, differing(n, n, 12, 0, n-1, pcm.Range{Start: 0, End: 6}, pcm.Range{Start: n - 6, End: n}))

		assert.NotContains(t, c.Detail, "all within")
		assert.Contains(t, c.Detail, "; in the stretches 0 to 5, 999994 to 999999")
	})

	t.Run("what counts as the start", func(t *testing.T) {
		// Within 2% of what both have, and no more.
		const limit = n / 50
		assert.Contains(t, identical(t, differing(n, n, limit, 0, limit-1)).Detail, "all within the first 20000 samples")
		assert.NotContains(t, identical(t, differing(n, n, limit+1, 0, limit)).Detail, "all within")
	})

	t.Run("what counts as the end", func(t *testing.T) {
		const limit = n / 50
		assert.Contains(t, identical(t, differing(n, n, limit, n-limit, n-1)).Detail, "all within the last 20000 samples")
		assert.NotContains(t, identical(t, differing(n, n, limit+1, n-limit-1, n-1)).Detail, "all within")
	})

	t.Run("differences near the end that stop short of it are not at the end", func(t *testing.T) {
		// In the last 2%, but not touching the last sample, so not what a drive that
		// could not read the end of a disc gives.
		c := identical(t, differing(n, n, 50, n-100, n-51, rng(n-100, n-50)))

		assert.NotContains(t, c.Detail, "all within")
	})

	t.Run("differences near the start that begin after it are not at the start", func(t *testing.T) {
		c := identical(t, differing(n, n, 50, 50, 99, rng(50, 100)))

		assert.NotContains(t, c.Detail, "all within")
	})

	t.Run("differences all through are not said to be at an end", func(t *testing.T) {
		c := identical(t, differing(n, n, n, 0, n-1, pcm.Range{Start: 0, End: n}))

		assert.NotContains(t, c.Detail, "all within")
		assert.NotContains(t, c.Detail, "stretches", "one stretch is not listed")
	})

	t.Run("at the start of audio that is all of it", func(t *testing.T) {
		// First is 0 and Last the last sample: not "within the first" anything.
		c := identical(t, differing(100, 100, 100, 0, 99))

		assert.NotContains(t, c.Detail, "all within")
	})

	t.Run("a few stretches are named, and the rest counted", func(t *testing.T) {
		runs := []pcm.Range{rng(10, 11), rng(5000, 5010), rng(90000, 90001), rng(500000, 500002), rng(700000, 700001)}
		c := identical(t, differing(n, n, 15, 10, 700000, runs...))

		assert.Contains(t, c.Detail, "in the stretches 10 to 10, 5000 to 5009, 90000 to 90000, and 2 more")
	})

	t.Run("more stretches than were kept", func(t *testing.T) {
		d := differing(n, n, 99, 10, 900000, pcm.Range{Start: 10, End: 11}, pcm.Range{Start: 5000, End: 5001})
		d.RunsTruncated = true

		assert.Contains(t, identical(t, d).Detail, "in the stretches 10 to 10, 5000 to 5000, and others")
	})

	t.Run("one shorter than the other, the rest the same", func(t *testing.T) {
		c := identical(t, differing(n, n-3*588, 0, -1, -1))

		assert.Equal(t, "the audio they have in common is the same; B is 3 sectors shorter than A (998236 samples, where A has 1000000)", c.Detail)
	})

	t.Run("one longer, by a part of a sector", func(t *testing.T) {
		c := identical(t, differing(n, n+7, 0, -1, -1))

		assert.Equal(t, "the audio they have in common is the same; B is 7 samples longer than A (1000007 samples, where A has 1000000)", c.Detail)
	})

	t.Run("a difference, and a difference in length", func(t *testing.T) {
		c := identical(t, differing(n, n-1, 1, 5, 5))

		assert.Contains(t, c.Detail, "1 of 999999 samples differ, from sample 5 (00:00:00) to sample 5 (00:00:00); B is 1 sample shorter than A")
	})
}

func TestCompareFilesShifted(t *testing.T) {
	a, b := cdPair(t)
	const n = 1000000

	shifted := func(k int64, rest pcm.Difference, lenA, lenB int64) (*fakeComparer, Check) {
		d := pcm.Difference{SamplesA: lenA, SamplesB: lenB, Differing: lenA - 100, First: 0, Last: lenA - 1,
			Shift: &pcm.Shift{Samples: k, Matches: 2, Tried: 3}}
		dec := &fakeComparer{diff: d, shifted: rest}
		r := CompareFiles(context.Background(), a, b, dec)
		return dec, want(t, r, "compare-audio", StatusError)
	}
	same := pcm.Difference{First: -1, Last: -1}

	t.Run("later, and the same once it is allowed for", func(t *testing.T) {
		dec, c := shifted(6, same, n, n)

		assert.Equal(t, "B is A moved 6 samples later (found at 2 of 3 places looked at); the audio is the same once that is allowed for; "+
			"B has 6 at its start, and A has 6 at its end that the other has no counterpart for", c.Detail)
		assert.Equal(t, []int64{6}, dec.shiftedCalls, "compared again with that allowed for")
	})

	t.Run("earlier", func(t *testing.T) {
		dec, c := shifted(-667, same, n, n)

		assert.Equal(t, "B is A moved 667 samples earlier (found at 2 of 3 places looked at); the audio is the same once that is allowed for; "+
			"A has 667 at its start, and B has 667 at its end that the other has no counterpart for", c.Detail)
		assert.Equal(t, []int64{-667}, dec.shiftedCalls)
	})

	t.Run("by one sample", func(t *testing.T) {
		_, c := shifted(1, same, n, n)

		assert.Contains(t, c.Detail, "B is A moved 1 sample later")
	})

	t.Run("a pregap's worth, and tracks of different lengths", func(t *testing.T) {
		// B has 4704 samples before A begins, and ends where A does.
		_, c := shifted(4704, same, n, n+4704)

		assert.Contains(t, c.Detail, "B has 4704 at its start that the other has no counterpart for")
		assert.NotContains(t, c.Detail, "at its end")
	})

	t.Run("still different once it is allowed for", func(t *testing.T) {
		rest := differing(n, n, 3, 500, 900)
		_, c := shifted(6, rest, n, n)

		assert.Contains(t, c.Detail, "; even allowing for that, 3 samples differ, from sample 500 (00:00:00) to sample 900 (00:00:01)")
		assert.NotContains(t, c.Detail, "is the same once")
	})

	t.Run("the comparison with it allowed for could not be made", func(t *testing.T) {
		d := pcm.Difference{SamplesA: n, SamplesB: n, Differing: n, Shift: &pcm.Shift{Samples: 6, Matches: 1, Tried: 1}}
		dec := &fakeComparer{diff: d, shiftedErr: &pcm.DecodeError{Path: b, Message: "CRC error"}}

		r := CompareFiles(context.Background(), a, b, dec)

		c := want(t, r, "compare-audio", StatusError)
		assert.Contains(t, c.Detail, "B is A moved 6 samples later")
		assert.Contains(t, c.Detail, "what is left with that allowed for could not be compared: B (b.flac) does not decode cleanly: CRC error")
	})
}

func TestUnmatched(t *testing.T) {
	d := func(a, b int64) pcm.Difference { return pcm.Difference{SamplesA: a, SamplesB: b} }

	assert.Equal(t, "B has 6 at its start, and A has 6 at its end that the other has no counterpart for", unmatched(d(100, 100), 6))
	assert.Equal(t, "A has 6 at its start, and B has 6 at its end that the other has no counterpart for", unmatched(d(100, 100), -6))
	assert.Equal(t, "B has 6 at its start that the other has no counterpart for", unmatched(d(100, 106), 6), "B longer by the shift: all of A is matched")
	assert.Equal(t, "A has 6 at its start that the other has no counterpart for", unmatched(d(106, 100), -6))
	assert.Equal(t, "B has 6 at its start, and B has 10 at its end that the other has no counterpart for", unmatched(d(100, 116), 6))
	assert.Equal(t, "", unmatched(d(100, 100), 0), "everything has a counterpart")
	assert.Equal(t, "B has 5 at its start, and A has 100 at its end that the other has no counterpart for", unmatched(d(100, 5), 5), "a shift past the end of the shorter")
	assert.Equal(t, "", unmatched(d(0, 0), 0))
	assert.Equal(t, "B has 3 at its start, and A has 100 at its end that the other has no counterpart for", unmatched(d(100, 3), 5),
		"B has only 3, however far it is shifted")
	assert.Equal(t, "A has 3 at its start, and B has 100 at its end that the other has no counterpart for", unmatched(d(3, 100), -5))
}

func TestCompareFilesWhenItCannotCompare(t *testing.T) {
	a, b := cdPair(t)

	for name, tc := range map[string]struct {
		err    error
		detail string
	}{
		"no ffmpeg":         {pcm.ErrNoFFmpeg, "ffmpeg was not found, so the audio was not compared"},
		"A does not decode": {&pcm.DecodeError{Path: a, Message: "CRC error"}, "A (a.flac) does not decode cleanly: CRC error"},
		"B does not decode": {&pcm.DecodeError{Path: b, Message: "invalid residual"}, "B (b.flac) does not decode cleanly: invalid residual"},
		"something else":    {errors.New("context canceled"), "the audio could not be compared: context canceled"},
	} {
		t.Run(name+" is an error, since nothing was compared", func(t *testing.T) {
			r := CompareFiles(context.Background(), a, b, &fakeComparer{err: tc.err})

			assert.Equal(t, tc.detail, want(t, r, "compare-audio", StatusError).Detail)
			assert.True(t, r.Failed())
		})
	}

	t.Run("no decoder", func(t *testing.T) {
		r := CompareFiles(context.Background(), a, b, nil)

		assert.Contains(t, want(t, r, "compare-audio", StatusError).Detail, "nothing to decode with")
	})
}

func TestSampleAt(t *testing.T) {
	assert.Equal(t, "sample 0 (00:00:00)", sampleAt(0))
	assert.Equal(t, "sample 587 (00:00:00)", sampleAt(587), "within the first sector")
	assert.Equal(t, "sample 588 (00:00:01)", sampleAt(588))
	assert.Equal(t, "sample 2646000 (01:00:00)", sampleAt(2646000), "a minute in")
}

func delayedBy(pcm []byte, k int) []byte {
	return append(make([]byte, k*4), pcm[:len(pcm)-k*4]...)
}

func advancedBy(pcm []byte, k int) []byte {
	return append(append([]byte(nil), pcm[k*4:]...), make([]byte, k*4)...)
}

func TestCompareFilesWithFFmpeg(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	const n = 700000
	base := testAudio(n, 71)

	flac := func(t *testing.T, name string, data []byte, args ...string) string {
		t.Helper()
		return testutil.MakePCMFlac(t, dir, name, data, args...)
	}
	compare := func(t *testing.T, a, b string) *Result {
		t.Helper()
		return CompareFiles(ctx, a, b, pcm.Decoder{})
	}
	audio := func(t *testing.T, r *Result) Check {
		t.Helper()
		c, ok := r.Check("compare-audio")
		require.True(t, ok)
		return c
	}

	t.Run("the same audio, stored differently", func(t *testing.T) {
		a := flac(t, "same-a.flac", base, "-compression_level", "0")
		b := flac(t, "same-b.flac", base, "-compression_level", "8", "-metadata", "TITLE=x")

		c := audio(t, compare(t, a, b))

		assert.Equal(t, StatusPass, c.Status)
		assert.Equal(t, "the same audio: 700000 samples", c.Detail)
	})

	t.Run("one sample", func(t *testing.T) {
		changed := append([]byte(nil), base...)
		changed[12345*4] ^= 1

		c := audio(t, compare(t, flac(t, "one-a.flac", base), flac(t, "one-b.flac", changed)))

		assert.Equal(t, StatusError, c.Status)
		assert.Equal(t, "1 of 700000 samples differ, from sample 12345 (00:00:20) to sample 12345 (00:00:20)", c.Detail)
	})

	t.Run("the last few samples, as from a drive that could not read them", func(t *testing.T) {
		silenced := append([]byte(nil), base...)
		for i := n - 6; i < n; i++ {
			copy(silenced[i*4:], []byte{0, 0, 0, 0})
		}

		c := audio(t, compare(t, flac(t, "end-a.flac", base), flac(t, "end-b.flac", silenced)))

		assert.Equal(t, StatusError, c.Status)
		assert.Contains(t, c.Detail, "to sample 699999")
		assert.Contains(t, c.Detail, ", all within the last 6 samples")
	})

	t.Run("a drive with an offset 6 greater", func(t *testing.T) {
		c := audio(t, compare(t, flac(t, "d6-a.flac", base), flac(t, "d6-b.flac", delayedBy(base, 6))))

		assert.Equal(t, StatusError, c.Status)
		assert.Equal(t, "B is A moved 6 samples later (found at 2 of 2 places looked at); the audio is the same once that is allowed for; "+
			"B has 6 at its start, and A has 6 at its end that the other has no counterpart for", c.Detail)
	})

	t.Run("a drive with an offset 667 less", func(t *testing.T) {
		c := audio(t, compare(t, flac(t, "d667-a.flac", base), flac(t, "d667-b.flac", advancedBy(base, 667))))

		assert.Contains(t, c.Detail, "B is A moved 667 samples earlier")
		assert.Contains(t, c.Detail, "the audio is the same once that is allowed for")
	})

	t.Run("the same track cut with its pregap in another place", func(t *testing.T) {
		// A track as the workflow cuts it and as the cue sheet's INDEX 00 would: B
		// begins 4704 samples (8 sectors) before A, and ends as A does.
		long := testAudio(1000000, 72)
		a := flac(t, "pre-a.flac", long[100000*4:900000*4])
		b := flac(t, "pre-b.flac", long[(100000-4704)*4:(900000-4704)*4])

		c := audio(t, compare(t, a, b))

		assert.Contains(t, c.Detail, "B is A moved 4704 samples later")
		assert.Contains(t, c.Detail, "the audio is the same once that is allowed for")
	})

	t.Run("a stretch moved that is not the same everywhere", func(t *testing.T) {
		// Moved at the start and not at the end: no one shift, and the report is of
		// what differs.
		partly := append(delayedBy(base[:350000*4], 6), base[350000*4:]...)

		c := audio(t, compare(t, flac(t, "part-a.flac", base), flac(t, "part-b.flac", partly)))

		assert.Equal(t, StatusError, c.Status)
		assert.NotContains(t, c.Detail, "moved")
		assert.Contains(t, c.Detail, "samples differ")
	})

	t.Run("unrelated audio", func(t *testing.T) {
		c := audio(t, compare(t, flac(t, "un-a.flac", base), flac(t, "un-b.flac", testAudio(n, 73))))

		assert.Equal(t, StatusError, c.Status)
		assert.NotContains(t, c.Detail, "moved")
		assert.Regexp(t, `^69\d{4} of 700000 samples differ, from sample 0 `, c.Detail)
	})

	t.Run("one cut short", func(t *testing.T) {
		c := audio(t, compare(t, flac(t, "cut-a.flac", base), flac(t, "cut-b.flac", base[:(n-1176)*4])))

		assert.Equal(t, "the audio they have in common is the same; B is 2 sectors shorter than A (698824 samples, where A has 700000)", c.Detail)
	})

	t.Run("an MP3 of it, a 24-bit file, and a mono file are refused", func(t *testing.T) {
		a := flac(t, "fmt-a.flac", base)
		mp3 := filepath.Join(dir, "x.mp3")
		wide := filepath.Join(dir, "wide.flac")
		mono := filepath.Join(dir, "mono.flac")
		src := filepath.Join(dir, "src.pcm")
		require.NoError(t, os.WriteFile(src, base, 0o644))
		for _, args := range [][]string{
			{"-c:a", "libmp3lame", mp3},
			{"-c:a", "flac", "-sample_fmt", "s32", "-bits_per_raw_sample", "24", wide},
			{"-c:a", "flac", "-ac", "1", mono},
		} {
			full := append([]string{"-v", "error", "-y", "-f", "s16le", "-ar", "44100", "-ac", "2", "-i", src}, args...)
			require.NoError(t, exec.Command("ffmpeg", full...).Run())
		}

		for name, other := range map[string]string{"an MP3": mp3, "24-bit": wide, "mono": mono} {
			r := compare(t, a, other)

			assert.Equal(t, StatusError, want(t, r, "compare-format", StatusError).Status, name)
			_, decoded := r.Check("compare-audio")
			assert.False(t, decoded, name)
		}
	})

	t.Run("no ffmpeg", func(t *testing.T) {
		a := flac(t, "nf-a.flac", base[:1000*4])
		b := flac(t, "nf-b.flac", base[:1000*4])
		t.Setenv("PATH", t.TempDir())

		c := audio(t, compare(t, a, b))

		assert.Equal(t, StatusError, c.Status)
		assert.Equal(t, "ffmpeg was not found, so the audio was not compared", c.Detail)
	})

	t.Run("a file that does not decode", func(t *testing.T) {
		a := flac(t, "bad-a.flac", base[:50000*4])
		data, err := os.ReadFile(a)
		require.NoError(t, err)
		b := filepath.Join(dir, "bad-b.flac")
		data[len(data)/2] ^= 0x04
		require.NoError(t, os.WriteFile(b, data, 0o644))

		r := compare(t, a, b)

		c := audio(t, r)
		assert.Equal(t, StatusError, c.Status)
		assert.True(t, r.Failed())
		assert.NotEmpty(t, c.Detail)
	})
}

func TestCompareFilesDescribeSomething(t *testing.T) {
	// A smoke test of the whole: what is said about two files that differ in the one
	// way a drive's offset does.
	assert.NotEmpty(t, fmt.Sprint(describeDifference(pcm.Difference{SamplesA: 5, SamplesB: 5, Differing: 1, First: 2, Last: 2})))
}
