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
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/pcm"
	"github.com/mfinelli/musicrename/internal/testutil"
)

// setup is how a rip was made, as its log says.
type setup struct {
	drive    string
	offset   int
	overread bool
}

var (
	asus    = "ASUS    SDRW-08V1M-U"
	pioneer = "PIONEER BD-RW   BDR-209D"
)

// replaceOnce replaces what is in text, which must be there.
func replaceOnce(t *testing.T, text, old, new string) string {
	t.Helper()
	require.Contains(t, text, old)
	return strings.Replace(text, old, new, 1)
}

// logWith is the text of a log of the disc d, from a drive with the given settings.
func logWith(t *testing.T, d tinyDisc, s setup) string {
	t.Helper()
	text := withTOC(t, fixtureText(t, fixtureBlack), d.tr)
	text = replaceOnce(t, text, "Used drive  : ASUS    SDRW-08V1M-U   Adapter: 1  ID: 0", "Used drive  : "+s.drive+"   Adapter: 1  ID: 0")
	text = replaceOnce(t, text, "Read offset correction                      : 6", fmt.Sprintf("Read offset correction                      : %d", s.offset))
	over := "No"
	if s.overread {
		over = "Yes"
	}
	return replaceOnce(t, text, "Overread into Lead-In and Lead-Out          : No", "Overread into Lead-In and Lead-Out          : "+over)
}

// ripOf is a rip of the disc d with the given settings, with an image that is only
// a header, as there is no audio to compare in tests that use a fake.
func ripOf(t *testing.T, d tinyDisc, s setup) rip {
	t.Helper()
	return buildRip(t, "Back in Black", []byte(signed(logWith(t, d, s))), d.cue)
}

// withAudio gives the rip an image of real audio.
func withAudio(t *testing.T, r rip, audio []byte) rip {
	t.Helper()
	testutil.MakePCMFlac(t, r.dir, "Back in Black.flac", audio)
	return r
}

func TestCompareRips(t *testing.T) {
	ctx := context.Background()
	d := newTinyDisc(t, 0)
	n := int64(len(d.pcm) / 4)

	a6 := setup{asus, 6, false}        // cannot read the last 6 samples
	b667 := setup{pioneer, 667, false} // the last 667
	over := setup{pioneer, 667, true}  // all of it
	neg := setup{asus, -30, false}     // the first 30

	same := pcm.Difference{SamplesA: n, SamplesB: n, First: -1, Last: -1}
	// differ is a Difference of the images, with the runs it is told, each of which
	// is all silence on a side unless it says it is not.
	differ := func(silent int64, runs ...pcm.Range) pcm.Difference {
		df := pcm.Difference{SamplesA: n, SamplesB: n, First: runs[0].Start, Last: runs[len(runs)-1].End - 1, Runs: runs}
		for _, r := range runs {
			df.Differing += r.End - r.Start
		}
		df.Silent = df.Differing
		if silent >= 0 {
			df.Silent = silent
		}
		return df
	}
	compare := func(t *testing.T, a, b setup, df pcm.Difference) (*Result, *fakeComparer) {
		t.Helper()
		ra, rb := ripOf(t, d, a), ripOf(t, d, b)
		dec := &fakeComparer{diff: df, shifted: same}
		r, err := CompareRips(ctx, ra.logPath(), rb.logPath(), dec)
		require.NoError(t, err)
		return r, dec
	}
	audio := func(t *testing.T, r *Result, s Status) string {
		t.Helper()
		return want(t, r, "compare-audio", s).Detail
	}

	t.Run("the same audio", func(t *testing.T) {
		r, dec := compare(t, a6, over, same)

		assert.Equal(t, fmt.Sprintf("the same audio: %d samples", n), audio(t, r, StatusPass))
		assert.False(t, r.Failed())
		assert.Equal(t, []string{"checksum-a", "checksum-b", "compare-disc", "compare-rip-a", "compare-rip-b",
			"compare-images", "compare-format", "compare-audio"}, ids(r))
		assert.Len(t, dec.calls, 1)
		assert.Equal(t, "the same disc: 3 tracks, disc ID "+mustLog(t, logWith(t, d, a6)).DiscID(), want(t, r, "compare-disc", StatusPass).Detail)
	})

	t.Run("each rip is described", func(t *testing.T) {
		r, _ := compare(t, a6, over, same)

		assert.Equal(t, "asus sdrw-08v1m-u, offset +6, overread off: could not read its last 6 samples", want(t, r, "compare-rip-a", StatusInfo).Detail)
		assert.Contains(t, want(t, r, "compare-rip-b", StatusInfo).Detail, "offset +667, overread on: could read all of it")
	})

	t.Run("the end that one drive could not read", func(t *testing.T) {
		r, _ := compare(t, a6, over, differ(-1, rng(n-6, n)))

		assert.Equal(t, "the same audio except for 6 samples at the end, within what the drives could not read (A its last 6 samples, B nothing); "+
			"in every one of them one rip has silence, which is what EAC fills in for samples a drive could not read", audio(t, r, StatusInfo))
		assert.False(t, r.Failed())
		assert.False(t, r.Fails(true), "it is only a note")
	})

	t.Run("the end that two drives could not read, to different extents", func(t *testing.T) {
		r, _ := compare(t, a6, b667, differ(-1, rng(n-667, n-6)))

		assert.Contains(t, audio(t, r, StatusInfo), "the same audio except for 661 samples at the end, within what the drives could not read (A its last 6 samples, B its last 667 samples)")
	})

	t.Run("the whole of what the drive could not read", func(t *testing.T) {
		r, _ := compare(t, a6, b667, differ(-1, rng(n-667, n)))

		assert.Contains(t, audio(t, r, StatusInfo), "667 samples at the end")
	})

	t.Run("a sample past what the drive could not read", func(t *testing.T) {
		r, _ := compare(t, a6, b667, differ(-1, rng(n-668, n-6)))

		c := want(t, r, "compare-audio", StatusError)
		assert.Contains(t, c.Detail, "662 of 1764000 samples differ", "one more than the 661 the drives could not read")
		assert.Contains(t, c.Detail, "; that is not what the drives account for: A could not read its last 6 samples, and B its last 667 samples")
	})

	t.Run("the start that a drive with a negative offset could not read", func(t *testing.T) {
		r, _ := compare(t, neg, over, differ(-1, rng(0, 30)))

		assert.Contains(t, audio(t, r, StatusInfo), "30 samples at the start, within what the drives could not read (A its first 30 samples, B nothing)")

		r, _ = compare(t, neg, over, differ(-1, rng(0, 31)))
		want(t, r, "compare-audio", StatusError)
	})

	t.Run("the start of one and the end of the other", func(t *testing.T) {
		r, _ := compare(t, neg, a6, differ(-1, rng(0, 30), rng(n-6, n)))

		assert.Contains(t, audio(t, r, StatusInfo), "36 samples at the start and the end, within what the drives could not read (A its first 30 samples, B its last 6 samples)")
	})

	t.Run("what is not silence is not what EAC fills in", func(t *testing.T) {
		r, _ := compare(t, a6, over, differ(5, rng(n-6, n)))

		c := want(t, r, "compare-audio", StatusWarning)
		assert.Contains(t, c.Detail, "within what the drives could not read")
		assert.Contains(t, c.Detail, "; but in 1 of them both rips have sound, so they are not only what EAC fills in")
		assert.False(t, r.Failed(), "a warning does not fail the run")
	})

	t.Run("a difference in the middle", func(t *testing.T) {
		r, _ := compare(t, a6, over, differ(-1, rng(n/2, n/2+1)))

		c := want(t, r, "compare-audio", StatusError)
		assert.Contains(t, c.Detail, "1 of 1764000 samples differ")
		assert.Contains(t, c.Detail, "that is not what the drives account for: A could not read its last 6 samples, and B nothing")
		assert.NotContains(t, c.Detail, "overread", "no end is touched")
		assert.True(t, r.Failed())
	})

	t.Run("a drive that says it read past the end, and differs there", func(t *testing.T) {
		r, _ := compare(t, a6, over, differ(-1, rng(n-100, n)))

		c := want(t, r, "compare-audio", StatusError)
		assert.Contains(t, c.Detail, "; the log of B says overread was on, which does not show that its drive read past the ends of the disc")
	})

	t.Run("damage in the middle, with an end that a drive accounts for, has no word of overread", func(t *testing.T) {
		r, _ := compare(t, a6, over, differ(-1, rng(n/2, n/2+1), rng(n-6, n)))

		c := want(t, r, "compare-audio", StatusError)
		assert.Contains(t, c.Detail, "that is not what the drives account for")
		assert.NotContains(t, c.Detail, "overread was on")
	})

	t.Run("a difference at the end that no drive claims", func(t *testing.T) {
		r, _ := compare(t, a6, a6, differ(-1, rng(n-100, n)))

		c := want(t, r, "compare-audio", StatusError)
		assert.NotContains(t, c.Detail, "overread was on", "neither says it")
	})

	t.Run("images of different lengths are not what a drive accounts for", func(t *testing.T) {
		df := differ(-1, rng(n-6, n))
		df.SamplesB = n - 6
		r, _ := compare(t, a6, over, df)

		c := want(t, r, "compare-audio", StatusError)
		assert.Contains(t, c.Detail, "B is 6 samples shorter than A")
	})

	t.Run("more stretches than were kept cannot be shown to be within it", func(t *testing.T) {
		df := differ(-1, rng(n-6, n))
		df.RunsTruncated = true
		r, _ := compare(t, a6, over, df)

		want(t, r, "compare-audio", StatusError)
	})

	t.Run("one the other moved along", func(t *testing.T) {
		df := pcm.Difference{SamplesA: n, SamplesB: n, Differing: n - 30, First: 0, Last: n - 1,
			Shift: &pcm.Shift{Samples: 661, Matches: 2, Tried: 2}}
		r, dec := compare(t, a6, b667, df)

		c := want(t, r, "compare-audio", StatusError)
		assert.Contains(t, c.Detail, "B is A moved 661 samples later")
		assert.Contains(t, c.Detail, "; one of the offsets (A's is +6, B's is +667) is not its drive's, by as much as that; ")
		assert.Equal(t, []int64{661}, dec.shiftedCalls)
	})

	t.Run("not the same disc", func(t *testing.T) {
		other := newTinyDisc(t, 150)
		ra, rb := ripOf(t, d, a6), ripOf(t, other, a6)
		dec := &fakeComparer{diff: same}

		r, err := CompareRips(ctx, ra.logPath(), rb.logPath(), dec)

		require.NoError(t, err)
		c := want(t, r, "compare-disc", StatusError)
		assert.Contains(t, c.Detail, "not the same disc: track 1 is at sectors 0 to 999 in A (disc ID ")
		assert.Contains(t, c.Detail, ") and 150 to 1149 in B (disc ID ")
		assert.Empty(t, dec.calls, "audio of different discs is not compared")
		assert.Equal(t, []string{"checksum-a", "checksum-b", "compare-disc"}, ids(r))
		assert.True(t, r.Failed())
	})

	t.Run("a disc with more tracks", func(t *testing.T) {
		more := d
		more.tr = append(append([]TOCTrack(nil), d.tr...), TOCTrack{4, 3000, 3999})
		ra, rb := ripOf(t, d, a6), ripOf(t, more, a6)

		r, err := CompareRips(ctx, ra.logPath(), rb.logPath(), &fakeComparer{})

		require.NoError(t, err)
		assert.Contains(t, want(t, r, "compare-disc", StatusError).Detail, "not the same disc: A has 3 tracks (disc ID ")
		assert.Contains(t, want(t, r, "compare-disc", StatusError).Detail, ") and B has 4 (disc ID ")
	})

	t.Run("a disc with fewer tracks", func(t *testing.T) {
		fewer := d
		fewer.tr = d.tr[:2]
		ra, rb := ripOf(t, d, a6), ripOf(t, fewer, a6)

		r, err := CompareRips(ctx, ra.logPath(), rb.logPath(), &fakeComparer{})

		require.NoError(t, err)
		assert.Contains(t, want(t, r, "compare-disc", StatusError).Detail, "not the same disc: A has 3 tracks (disc ID ")
		assert.Contains(t, want(t, r, "compare-disc", StatusError).Detail, ") and B has 2 (disc ID ")
	})

	t.Run("the image of A is compared with that of B, in that order", func(t *testing.T) {
		ra, rb := ripOf(t, d, a6), ripOf(t, d, over)
		dec := &fakeComparer{diff: same}

		_, err := CompareRips(ctx, ra.logPath(), rb.logPath(), dec)

		require.NoError(t, err)
		assert.Equal(t, [][2][]string{{{ra.path(".flac")}, {rb.path(".flac")}}}, dec.calls)
	})

	t.Run("a log that has been changed since EAC wrote it", func(t *testing.T) {
		ra := ripOf(t, d, a6)
		text := replaceOnce(t, signed(logWith(t, d, over)), "PIONEER", "PIONEEZ")
		rb := buildRip(t, "Back in Black", []byte(text), d.cue)

		r, err := CompareRips(ctx, ra.logPath(), rb.logPath(), &fakeComparer{diff: same})

		require.NoError(t, err)
		assert.Equal(t, StatusPass, want(t, r, "checksum-a", StatusPass).Status)
		assert.Contains(t, want(t, r, "checksum-b", StatusError).Detail, "checksum")
		want(t, r, "compare-audio", StatusPass)
		assert.True(t, r.Failed(), "and it is carried on with, since the audio can still be compared")
	})

	t.Run("a log that is not a log", func(t *testing.T) {
		ra := ripOf(t, d, a6)
		rb := buildRip(t, "Back in Black", []byte(signed(logWith(t, d, a6))), d.cue)
		require.NoError(t, os.WriteFile(rb.logPath(), []byte("not an EAC log"), 0o644))

		_, err := CompareRips(ctx, ra.logPath(), rb.logPath(), &fakeComparer{})
		assert.ErrorContains(t, err, "log B (Back in Black.log):")

		_, err = CompareRips(ctx, rb.logPath(), ra.logPath(), &fakeComparer{})
		assert.ErrorContains(t, err, "log A (Back in Black.log):")
	})

	t.Run("a log that does not say how it was read", func(t *testing.T) {
		for name, tc := range map[string]struct{ old, new, detail string }{
			"no offset":                      {"Read offset correction                      : 6\n", "", "A: the log does not state its read offset"},
			"an offset that is not a number": {"Read offset correction                      : 6", "Read offset correction                      : six", `A: "six" is not a read offset`},
			"no overread":                    {"Overread into Lead-In and Lead-Out          : No\n", "", "A: the log does not state whether it read past the ends of the disc"},
			"overread neither":               {"Overread into Lead-In and Lead-Out          : No", "Overread into Lead-In and Lead-Out          : Maybe", `A: "Maybe" is neither Yes nor No`},
		} {
			text := replaceOnce(t, logWith(t, d, a6), tc.old, tc.new)
			ra := buildRip(t, "Back in Black", []byte(signed(text)), d.cue)
			rb := ripOf(t, d, a6)
			dec := &fakeComparer{}

			r, err := CompareRips(ctx, ra.logPath(), rb.logPath(), dec)

			require.NoError(t, err, name)
			assert.Contains(t, want(t, r, "compare-rips", StatusError).Detail, tc.detail+", so what its drive could not read is not known", name)
			assert.Empty(t, dec.calls, name)
		}
	})

	t.Run("a rip with no cue sheet, or with one that names no image", func(t *testing.T) {
		ra, rb := ripOf(t, d, a6), ripOf(t, d, a6)
		require.NoError(t, os.Remove(rb.path(".cue")))

		r, err := CompareRips(ctx, ra.logPath(), rb.logPath(), &fakeComparer{})
		require.NoError(t, err)
		assert.Equal(t, `B: no "Back in Black.cue" beside the log`, want(t, r, "compare-images", StatusError).Detail)

		require.NoError(t, os.WriteFile(ra.path(".cue"), []byte("TITLE \"x\"\r\n"), 0o644))
		require.NoError(t, os.WriteFile(rb.path(".cue"), []byte(d.cue), 0o644))
		require.NoError(t, os.Remove(rb.path(".flac")))
		r, err = CompareRips(ctx, ra.logPath(), rb.logPath(), &fakeComparer{})
		require.NoError(t, err)
		c := want(t, r, "compare-images", StatusError)
		assert.Contains(t, c.Detail, "A: ")
		assert.Contains(t, c.Detail, "; B: the cue sheet does not name one FLAC image that is beside the log")
	})

	t.Run("a cue sheet that cannot be read or is not one", func(t *testing.T) {
		ra, rb := ripOf(t, d, a6), ripOf(t, d, a6)
		require.NoError(t, os.WriteFile(ra.path(".cue"), []byte("FILE \"Back in Black.flac\" WAVE\r\n  INDEX 01 00:00:00\r\n"), 0o644))
		require.NoError(t, os.Remove(rb.path(".cue")))
		require.NoError(t, os.Mkdir(rb.path(".cue"), 0o755)) // there, and not a file

		r, err := CompareRips(ctx, ra.logPath(), rb.logPath(), &fakeComparer{})

		require.NoError(t, err)
		c := want(t, r, "compare-images", StatusError)
		assert.Contains(t, c.Detail, "A: Back in Black.cue: line 2: INDEX before any TRACK")
		assert.Contains(t, c.Detail, `; B: cannot read "Back in Black.cue"`)
	})

	t.Run("an image that is not CD audio", func(t *testing.T) {
		ra, rb := ripOf(t, d, a6), ripOf(t, d, a6)
		require.NoError(t, os.WriteFile(rb.path(".flac"), flacHeader(44100, 2, 24, uint64(n)), 0o644))
		dec := &fakeComparer{}

		r, err := CompareRips(ctx, ra.logPath(), rb.logPath(), dec)

		require.NoError(t, err)
		assert.Contains(t, want(t, r, "compare-format", StatusError).Detail, "B (Back in Black.flac): 44.1 kHz, 24-bit, stereo")
		assert.Empty(t, dec.calls)
	})

	t.Run("an image that does not decode", func(t *testing.T) {
		ra, rb := ripOf(t, d, a6), ripOf(t, d, a6)
		dec := &fakeComparer{err: &pcm.DecodeError{Path: rb.path(".flac"), Message: "CRC error"}}

		r, err := CompareRips(ctx, ra.logPath(), rb.logPath(), dec)

		require.NoError(t, err)
		assert.Equal(t, "B (Back in Black.flac) does not decode cleanly: CRC error", want(t, r, "compare-audio", StatusError).Detail)
	})

	t.Run("no decoder", func(t *testing.T) {
		ra, rb := ripOf(t, d, a6), ripOf(t, d, a6)

		r, err := CompareRips(ctx, ra.logPath(), rb.logPath(), nil)

		require.NoError(t, err)
		assert.Contains(t, want(t, r, "compare-audio", StatusError).Detail, "nothing to decode with")
	})
}

// mustLog parses text as a log.
func mustLog(t *testing.T, text string) *Log {
	t.Helper()
	return parse(t, text)
}

func TestCheckSameDiscWithNoTableOfContents(t *testing.T) {
	toc := []TOCTrack{{1, 0, 999}}

	for name, logs := range map[string][2]*Log{
		"A has none": {{}, {TOC: toc}},
		"B has none": {{TOC: toc}, {}},
		"neither":    {{}, {}},
	} {
		r := &Result{}

		ok := checkSameDisc(r, logs[0], logs[1])

		assert.False(t, ok, name)
		assert.Contains(t, want(t, r, "compare-disc", StatusError).Detail, "has no table of contents", name)
	}
}

func TestInterpretRipsWhereTheEndsMeet(t *testing.T) {
	// So little audio that the start and the end of what could not be read meet: a
	// difference at the first sample past the start is in the end's, and not the start's.
	const n = 100
	fa, fb := ripFacts{side: "A", offset: -30}, ripFacts{side: "B", offset: 80}
	d := pcm.Difference{SamplesA: n, SamplesB: n, Differing: 10, Silent: 10, First: 30, Last: 39, Runs: []pcm.Range{rng(30, 40)}}

	status, detail := interpretRips(context.Background(), &fakeComparer{}, "a", "b", d, nil, nil, fa, fb)

	assert.Equal(t, StatusInfo, status)
	assert.Contains(t, detail, "10 samples at the end, within")
	assert.NotContains(t, detail, "the start")
}

func TestRipFacts(t *testing.T) {
	for name, tc := range map[string]struct {
		f          ripFacts
		start, end int64
		cannot     string
	}{
		"a positive offset loses the end":   {ripFacts{offset: 6}, 0, 6, "its last 6 samples"},
		"a negative offset loses the start": {ripFacts{offset: -30}, 30, 0, "its first 30 samples"},
		"no offset loses nothing":           {ripFacts{offset: 0}, 0, 0, "nothing"},
		"overread loses nothing":            {ripFacts{offset: 667, overread: true}, 0, 0, "nothing"},
		"overread, negative":                {ripFacts{offset: -30, overread: true}, 0, 0, "nothing"},
	} {
		start, end := tc.f.unreadable()
		assert.Equal(t, tc.start, start, name)
		assert.Equal(t, tc.end, end, name)
		assert.Equal(t, tc.cannot, tc.f.cannotRead(), name)
	}

	assert.Equal(t, "offset +6, overread off", ripFacts{offset: 6}.settings())
	assert.Equal(t, "offset -30, overread on", ripFacts{offset: -30, overread: true}.settings())
	assert.Equal(t, "offset +0, overread off", ripFacts{}.settings())
	assert.Equal(t, "ASUS SDRW, offset +6, overread off: could not read its last 6 samples", ripFacts{drive: "ASUS SDRW", offset: 6}.describe())
	assert.Equal(t, "ASUS SDRW, offset +6, overread on: could read all of it", ripFacts{drive: "ASUS SDRW", offset: 6, overread: true}.describe())
	assert.Equal(t, "a drive that is not named, offset +0, overread off: could read all of it", ripFacts{}.describe())
}

func TestWithinEnds(t *testing.T) {
	const n = 1000
	d := func(runs ...pcm.Range) pcm.Difference {
		df := pcm.Difference{SamplesA: n, SamplesB: n, Runs: runs}
		for _, r := range runs {
			df.Differing += r.End - r.Start
		}
		return df
	}

	assert.True(t, withinEnds(d(rng(n-6, n)), 0, 6))
	assert.True(t, withinEnds(d(rng(0, 30)), 30, 0))
	assert.True(t, withinEnds(d(rng(0, 30), rng(n-6, n)), 30, 6))
	assert.True(t, withinEnds(d(rng(n-6, n-3)), 0, 6), "within it, not filling it")
	assert.False(t, withinEnds(d(rng(n-7, n)), 0, 6), "a sample before it")
	assert.False(t, withinEnds(d(rng(0, 31)), 30, 0), "a sample after it")
	assert.False(t, withinEnds(d(rng(500, 501)), 30, 6), "in the middle")
	assert.False(t, withinEnds(d(rng(n-6, n)), 0, 0), "nothing was unreadable")
	assert.False(t, withinEnds(d(), 30, 6), "nothing differs")
	assert.False(t, withinEnds(d(rng(n-6, n), rng(500, 501)), 0, 6), "one of two")

	short := d(rng(n-6, n))
	short.SamplesB = n - 6
	assert.False(t, withinEnds(short, 0, 6), "the lengths differ")

	trunc := d(rng(n-6, n))
	trunc.RunsTruncated = true
	assert.False(t, withinEnds(trunc, 0, 6), "there were more than are known")
}

func TestOverreadHint(t *testing.T) {
	const n = 1000
	on := ripFacts{side: "B", overread: true}
	off := ripFacts{side: "A"}
	d := func(runs ...pcm.Range) pcm.Difference { return pcm.Difference{SamplesA: n, SamplesB: n, Runs: runs} }
	const hint = "the log of B says overread was on, which does not show that its drive read past the ends of the disc"

	assert.Equal(t, hint, overreadHint(d(rng(n-100, n)), 0, 6, off, on), "the end, and more than a drive could not read")
	assert.Equal(t, hint, overreadHint(d(rng(0, 50)), 0, 0, off, on), "the start")
	assert.Equal(t, "the log of A and B says overread was on, which does not show that its drive read past the ends of the disc",
		overreadHint(d(rng(0, 50)), 0, 0, ripFacts{side: "A", overread: true}, on), "both")
	assert.Equal(t, "", overreadHint(d(rng(100, 200)), 0, 6, off, on), "in the middle")
	assert.Equal(t, "", overreadHint(d(rng(n-6, n)), 0, 6, off, on), "an end, but accounted for by a drive")
	assert.Equal(t, "", overreadHint(d(rng(0, 30)), 30, 0, off, on), "the start, but accounted for by a drive")
	assert.Equal(t, "", overreadHint(d(rng(n-100, n)), 0, 6, off, off), "neither says it")
	assert.Equal(t, "", overreadHint(d(rng(100, 200), rng(n-6, n)), 0, 6, off, on), "the middle is not accounted for, and the end is, so the end is not the matter")
	assert.Equal(t, hint, overreadHint(d(rng(100, 200), rng(n-100, n)), 0, 6, off, on), "one of several")
	assert.Equal(t, "", overreadHint(d(), 0, 6, off, on), "no differences")
	assert.Equal(t, hint, overreadHint(pcm.Difference{SamplesA: n, SamplesB: n - 10, Runs: []pcm.Range{rng(n-110, n-10)}}, 0, 0, off, on),
		"the end of the shorter, when the lengths differ")
}

func TestShiftReason(t *testing.T) {
	confirmed := mustLog(t, fixtureText(t, fixtureBlack))
	unconfirmed := mustLog(t, fixtureText(t, fixtureBlack))
	unconfirmed.AR = nil
	require.True(t, arConfirmsAll(confirmed))
	fa, fb := ripFacts{offset: 6}, ripFacts{offset: -30}

	lead := "one of the offsets (A's is +6, B's is -30) is not its drive's, by as much as that; "
	assert.Equal(t, lead+"AccurateRip confirmed every track of A and not of B, so B's is the one to doubt", shiftReason(confirmed, unconfirmed, fa, fb))
	assert.Equal(t, lead+"AccurateRip confirmed every track of B and not of A, so A's is the one to doubt", shiftReason(unconfirmed, confirmed, fa, fb))
	assert.Equal(t, lead+"AccurateRip confirmed every track of both, which the images being moved along does not agree with", shiftReason(confirmed, confirmed, fa, fb))
	assert.Equal(t, lead+"AccurateRip confirmed neither", shiftReason(unconfirmed, unconfirmed, fa, fb))
}

func TestParseOffsetAndYesNo(t *testing.T) {
	for raw, want := range map[string]int{"6": 6, "+6": 6, "-30": -30, " 667 ": 667, "0": 0} {
		got, err := parseOffset(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}
	_, err := parseOffset("six")
	assert.Error(t, err)

	for raw, want := range map[string]bool{"Yes": true, "yes": true, "No": false, "NO": false} {
		got, ok := parseYesNo(raw)
		assert.True(t, ok, raw)
		assert.Equal(t, want, got, raw)
	}
	_, ok := parseYesNo("Maybe")
	assert.False(t, ok)
	_, ok = parseYesNo("")
	assert.False(t, ok)
}

func TestCompareRipsWithFFmpeg(t *testing.T) {
	ctx := context.Background()
	d := newTinyDisc(t, 0)
	disc := d.pcm
	n := len(disc) / 4

	a6 := setup{asus, 6, false}
	b667 := setup{pioneer, 667, false}
	over := setup{pioneer, 667, true}
	neg := setup{asus, -30, false}

	zeroEnd := func(pcm []byte, k int) []byte {
		out := append([]byte(nil), pcm...)
		clear(out[len(out)-k*4:])
		return out
	}
	zeroStart := func(pcm []byte, k int) []byte {
		out := append([]byte(nil), pcm...)
		clear(out[:k*4])
		return out
	}
	compare := func(t *testing.T, sa setup, audioA []byte, sb setup, audioB []byte) *Result {
		t.Helper()
		ra := withAudio(t, ripOf(t, d, sa), audioA)
		rb := withAudio(t, ripOf(t, d, sb), audioB)
		r, err := CompareRips(ctx, ra.logPath(), rb.logPath(), pcm.Decoder{})
		require.NoError(t, err)
		return r
	}

	t.Run("the same audio", func(t *testing.T) {
		r := compare(t, over, disc, over, disc)

		assert.Equal(t, fmt.Sprintf("the same audio: %d samples", n), want(t, r, "compare-audio", StatusPass).Detail)
		assert.False(t, r.Failed())
	})

	t.Run("the last 6 samples of a drive with offset +6 and no overread", func(t *testing.T) {
		r := compare(t, a6, zeroEnd(disc, 6), over, disc)

		c := want(t, r, "compare-audio", StatusInfo)
		assert.Contains(t, c.Detail, "the same audio except for 6 samples at the end, within what the drives could not read (A its last 6 samples, B nothing)")
		assert.Contains(t, c.Detail, "in every one of them one rip has silence")
		assert.False(t, r.Failed())
	})

	t.Run("two drives that could not read the end, to different extents", func(t *testing.T) {
		r := compare(t, a6, zeroEnd(disc, 6), b667, zeroEnd(disc, 667))

		assert.Contains(t, want(t, r, "compare-audio", StatusInfo).Detail, "661 samples at the end")
	})

	t.Run("the start, with a negative offset", func(t *testing.T) {
		r := compare(t, neg, zeroStart(disc, 30), over, disc)

		assert.Contains(t, want(t, r, "compare-audio", StatusInfo).Detail, "30 samples at the start, within what the drives could not read (A its first 30 samples, B nothing)")
	})

	t.Run("a rip with sound where its log says silence was filled in", func(t *testing.T) {
		// A cannot have read its last 6 samples, yet it has sound there, and so does B,
		// differently.
		other := append([]byte(nil), disc...)
		other[(n-1)*4] ^= 0x7f
		r := compare(t, a6, other, over, disc)

		c := want(t, r, "compare-audio", StatusWarning)
		assert.Contains(t, c.Detail, "; but in 1 of them both rips have sound")
	})

	t.Run("a drive that is not the offset its log says", func(t *testing.T) {
		// B's log says +667, and its audio is 661 samples later than A's, which says +6:
		// they would line up if the offsets were right.
		r := compare(t, a6, disc, b667, append(make([]byte, 661*4), disc[:len(disc)-661*4]...))

		c := want(t, r, "compare-audio", StatusError)
		assert.Contains(t, c.Detail, "B is A moved 661 samples later")
		assert.Contains(t, c.Detail, "the audio is the same once that is allowed for")
		assert.Contains(t, c.Detail, "one of the offsets (A's is +6, B's is +667) is not its drive's, by as much as that")
		assert.True(t, r.Failed())
	})

	t.Run("a difference that is not at an end", func(t *testing.T) {
		damaged := append([]byte(nil), disc...)
		damaged[(n/2)*4] ^= 1
		r := compare(t, a6, disc, over, damaged)

		c := want(t, r, "compare-audio", StatusError)
		assert.Contains(t, c.Detail, fmt.Sprintf("1 of %d samples differ, from sample %d", n, n/2))
		assert.Contains(t, c.Detail, "that is not what the drives account for")
	})

	t.Run("an end that differs by more than the drive could not read", func(t *testing.T) {
		r := compare(t, a6, zeroEnd(disc, 100), over, disc)

		c := want(t, r, "compare-audio", StatusError)
		assert.Contains(t, c.Detail, "100 of")
		assert.Contains(t, c.Detail, "the log of B says overread was on")
	})

	t.Run("images of different lengths", func(t *testing.T) {
		r := compare(t, over, disc, over, disc[:(n-588)*4])

		c := want(t, r, "compare-audio", StatusError)
		assert.Contains(t, c.Detail, "B is 1 sector shorter than A")
	})

	t.Run("the audio of another disc", func(t *testing.T) {
		r := compare(t, over, disc, over, testAudio(n, 99))

		c := want(t, r, "compare-audio", StatusError)
		assert.NotContains(t, c.Detail, "within what the drives could not read")
	})
}
