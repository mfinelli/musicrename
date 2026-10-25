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
	"path/filepath"
	"strings"

	"github.com/mfinelli/musicrename/internal/pcm"
)

// Comparer compares the audio of files. pcm.Decoder, which decodes with ffmpeg,
// is one; tests use a fake.
type Comparer interface {
	// Diff compares two sets of files, each as one stretch of audio, and finds
	// whether one is the other moved along. See pcm.Decoder.Diff.
	Diff(ctx context.Context, a, b []string) (pcm.Difference, error)
	// DiffShifted compares them with a shift allowed for. See
	// pcm.Decoder.DiffShifted.
	DiffShifted(ctx context.Context, a, b []string, shift int64) (pcm.Difference, error)
}

// CompareFiles compares the audio of two FLAC files, which can be tracks or
// images: whether it is the same, and if not, where it differs, or whether one is
// the other moved along. Nothing is assumed about why, since two files say
// nothing of how they were made. The compared are called A and B in what it
// reports.
//
// Both must be CD audio, as the decoder gives exactly what was stored only for
// that, and a comparison of anything else would be of a conversion. Everything
// that stops the comparison, ffmpeg being missing among it, is an error, since
// the audio was not compared.
func CompareFiles(ctx context.Context, a, b string, dec Comparer) *Result {
	r := &Result{}
	if !checkCompareFormat(r, a, b) {
		return r
	}
	if dec == nil {
		r.add("compare-audio", "Audio", StatusError, "there is nothing to decode with, so the audio was not compared")
		return r
	}

	d, err := dec.Diff(ctx, []string{a}, []string{b})
	if msg := compareError(err, b); msg != "" {
		r.add("compare-audio", "Audio", StatusError, "%s", msg)
		return r
	}

	switch {
	case d.Identical():
		r.add("compare-audio", "Audio", StatusPass, "the same audio: %d samples", d.SamplesA)
	case d.Shift != nil:
		r.add("compare-audio", "Audio", StatusError, "%s", describeShift(ctx, dec, a, b, d))
	default:
		r.add("compare-audio", "Audio", StatusError, "%s", describeDifference(d))
	}
	return r
}

// compareError says why a comparison could not be made, or "" if it was. A file
// that does not decode is named as B if it is b, and A otherwise.
func compareError(err error, b string) string {
	var decodeErr *pcm.DecodeError
	switch {
	case err == nil:
		return ""
	case errors.Is(err, pcm.ErrNoFFmpeg):
		return "ffmpeg was not found, so the audio was not compared"
	case errors.As(err, &decodeErr):
		side := "A"
		if decodeErr.Path == b {
			side = "B"
		}
		return fmt.Sprintf("%s (%s) does not decode cleanly: %s", side, filepath.Base(decodeErr.Path), decodeErr.Message)
	}
	return fmt.Sprintf("the audio could not be compared: %v", err)
}

// checkCompareFormat confirms both files are CD-audio FLAC files.
func checkCompareFormat(r *Result, a, b string) bool {
	const id, name = "compare-format", "Format"

	var problems []string
	for _, f := range []struct{ side, path string }{{"A", a}, {"B", b}} {
		si, err := readStreamInfo(f.path)
		switch {
		case errors.Is(err, errNotFLAC):
			problems = append(problems, fmt.Sprintf("%s (%s): %v", f.side, filepath.Base(f.path), err))
		case err != nil:
			problems = append(problems, fmt.Sprintf("%s (%s): cannot read: %v", f.side, filepath.Base(f.path), err))
		case si.SampleRate != cdSampleRate || si.Channels != cdChannels || si.BitsPerSample != cdBits:
			problems = append(problems, fmt.Sprintf("%s (%s): %s, %d-bit, %s", f.side, filepath.Base(f.path),
				rateName(si.SampleRate), si.BitsPerSample, channelsName(si.Channels)))
		}
	}

	if len(problems) > 0 {
		r.add(id, name, StatusError, "%s: only CD audio, %s, %d-bit, %s, in FLAC, can be compared exactly",
			strings.Join(problems, "; "), rateName(cdSampleRate), cdBits, channelsName(cdChannels))
		return false
	}
	r.add(id, name, StatusPass, "both are %s, %d-bit, %s FLAC", rateName(cdSampleRate), cdBits, channelsName(cdChannels))
	return true
}

// sampleAt writes a position as a sample number and the time it is at.
func sampleAt(n int64) string {
	return fmt.Sprintf("sample %d (%s)", n, msf(int(n/samplesPerSector)))
}

// describeDifference says how two stretches of audio that are not one the other
// moved along differ: how many samples, where they are, whether they are only
// at the start or the end, and how the lengths compare.
func describeDifference(d pcm.Difference) string {
	common := min(d.SamplesA, d.SamplesB)
	var parts []string

	if d.Differing > 0 {
		text := fmt.Sprintf("%d of %d samples differ, from %s to %s", d.Differing, common, sampleAt(d.First), sampleAt(d.Last))
		// A few samples at an end are what a drive that cannot read the ends of
		// a disc gives, which is worth saying without knowing why they are.
		switch {
		case d.First == 0 && d.Last+1 <= common/50:
			text += fmt.Sprintf(", all within the first %d samples", d.Last+1)
		case d.Last == common-1 && common-d.First <= common/50 && d.First > 0:
			text += fmt.Sprintf(", all within the last %d samples", common-d.First)
		}
		if len(d.Runs) > 1 || d.RunsTruncated {
			text += "; " + describeRuns(d)
		}
		parts = append(parts, text)
	} else {
		parts = append(parts, "the audio they have in common is the same")
	}

	if d.SamplesA != d.SamplesB {
		how, diff := "longer", d.SamplesB-d.SamplesA
		if diff < 0 {
			how, diff = "shorter", -diff
		}
		parts = append(parts, fmt.Sprintf("B is %s %s than A (%d samples, where A has %d)", describeSize(diff), how, d.SamplesB, d.SamplesA))
	}
	return strings.Join(parts, "; ")
}

// describeRuns names the first few stretches the differing samples fall in.
func describeRuns(d pcm.Difference) string {
	var shown []string
	for _, run := range d.Runs[:min(len(d.Runs), maxListed)] {
		shown = append(shown, fmt.Sprintf("%d to %d", run.Start, run.End-1))
	}
	text := "in the stretches " + strings.Join(shown, ", ")
	if len(d.Runs) > maxListed || d.RunsTruncated {
		more := "and others"
		if !d.RunsTruncated {
			more = fmt.Sprintf("and %d more", len(d.Runs)-maxListed)
		}
		text += ", " + more
	}
	return text
}

// describeShift says that B is A moved along, and what is left once that is
// allowed for: which is checked, with the comparison made again with the shift.
func describeShift(ctx context.Context, dec Comparer, a, b string, d pcm.Difference) string {
	k := d.Shift.Samples
	how, n := "later", k
	if k < 0 {
		how, n = "earlier", -k
	}
	text := fmt.Sprintf("B is A moved %d %s %s (found at %d of %d places looked at)",
		n, plural(n, "sample"), how, d.Shift.Matches, d.Shift.Tried)

	rest, err := dec.DiffShifted(ctx, []string{a}, []string{b}, k)
	switch {
	case err != nil:
		return text + fmt.Sprintf("; what is left with that allowed for could not be compared: %v", compareError(err, b))
	case rest.Differing == 0:
		text += "; the audio is the same once that is allowed for"
	default:
		text += fmt.Sprintf("; even allowing for that, %d samples differ, from %s to %s",
			rest.Differing, sampleAt(rest.First), sampleAt(rest.Last))
	}
	if extra := unmatched(d, k); extra != "" {
		text += "; " + extra
	}
	return text
}

// unmatched says how many samples at the start and end of each have none to
// compare with in the other, when B is A moved along by k. It is "" if every
// sample has one.
func unmatched(d pcm.Difference, k int64) string {
	// What is skipped at the front is no more than there is.
	skipA, skipB := min(max(-k, 0), d.SamplesA), min(max(k, 0), d.SamplesB)
	overlap := min(d.SamplesA-skipA, d.SamplesB-skipB) // not negative, as skipped is no more than there is

	var parts []string
	for _, p := range []struct {
		n    int64
		what string
	}{
		{skipA, "A has %d at its start"},
		{skipB, "B has %d at its start"},
		{d.SamplesA - skipA - overlap, "A has %d at its end"},
		{d.SamplesB - skipB - overlap, "B has %d at its end"},
	} {
		if p.n > 0 {
			parts = append(parts, fmt.Sprintf(p.what, p.n))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", and ") + " that the other has no counterpart for"
}
