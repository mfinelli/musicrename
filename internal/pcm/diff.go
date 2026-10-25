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
	"io"
)

// Range is a stretch of samples, from Start up to but not including End.
type Range struct {
	Start, End int64
}

// Shift is how far one stretch of audio is from being the other moved along: what
// a drive with another read offset gives, or a split with a pregap kept in another
// place.
type Shift struct {
	// Samples is how far B lags A: sample i of A is sample i+Samples of B. It is
	// negative if B leads.
	Samples int64
	// Matches is how many of the places looked at found it, out of Tried, which
	// is how many there was a distinctive stretch of A to look for.
	Matches, Tried int
}

// Difference is how two stretches of audio, A and B, differ. Samples are counted
// from zero, and compared where both have one: audio that one has beyond the
// end of the other is a difference in length and not in the samples.
type Difference struct {
	// SamplesA and SamplesB are how many samples each has.
	SamplesA, SamplesB int64
	// Differing is how many samples differ, among those both have.
	Differing int64
	// Silent is how many of them are silence, every value zero, in one of the two:
	// what a rip has where its drive could not read, which Exact Audio Copy fills
	// in. If it is the same as Differing, every difference is a sample of silence
	// against one of sound.
	Silent int64
	// First and Last are the first and last of them, or -1 if none differ.
	First, Last int64
	// Runs are the stretches they fall in, in order, with those no more than
	// runGap samples apart taken as one. Only the first maxRuns are kept, and
	// RunsTruncated says whether there were more.
	Runs          []Range
	RunsTruncated bool
	// Shift is set if B is A moved along, by the same amount everywhere that
	// was looked, which is what it looks like when nearly every sample differs.
	// It is nil if the audio is not shifted, or if nothing could be told.
	Shift *Shift
}

// Identical reports whether the two are the same audio: the same length, with
// every sample equal.
func (d Difference) Identical() bool {
	return d.SamplesA == d.SamplesB && d.Differing == 0
}

const (
	// diffChunk is how much of each side is read at a time. A whole number of
	// samples.
	diffChunk = 1 << 16
	// maxRuns is how many runs of differing samples a Difference lists.
	maxRuns = 16
	// runGap is how far apart differing samples may be and still be one run, so
	// that noise (a whole stretch of audio that differs) is a few runs and not
	// thousands.
	runGap = 1000

	// A shift is looked for by finding a stretch of A, windowSamples long, in B
	// within maxShift samples either way of where it was in A. Drive offsets
	// are a few thousand samples at most; a pregap moved from one track to the
	// next can be seconds, so this allows about three.
	windowSamples = 2048
	maxShift      = 1 << 17
	// A window of fewer different samples than this is not looked for: a
	// stretch of silence, or of one repeated sound, is found everywhere.
	minDistinct = windowSamples / 4
)

// captureCentres are where in A, in samples, windows are taken from to look
// for a shift. They are spread out, since one place may be silent, and start
// far enough in for maxShift before it to be there.
var captureCentres = []int64{200000, 500000, 1000000, 2000000, 4000000, 8000000, 16000000, 32000000, 64000000}

// Diff decodes the files of each side, in order, as one stretch of audio, and
// compares the two, in step, without holding either: what is held is a chunk of
// each, and the few windows used to look for a shift, however long they are. It
// stops at the first file that does not decode cleanly, on either side, with the
// *DecodeError for it, or ErrNoFFmpeg if there is no ffmpeg, or the context's
// error if it was cancelled.
//
// Where nearly every sample differs because one is the other moved along, as when
// a drive's offset is wrong, the Difference says by how much (see Shift), and
// DiffShifted then compares them with that allowed for.
func (Decoder) Diff(ctx context.Context, a, b []string) (Difference, error) {
	return diff(ctx, a, b, 0, 0, true)
}

// DiffShifted is Diff for B that is A moved along by shift samples (see Shift):
// sample i of A is compared with sample i+shift of B. Positions in what it
// returns are those in A, and the lengths are of the whole of each. It does not
// look for a shift.
func (Decoder) DiffShifted(ctx context.Context, a, b []string, shift int64) (Difference, error) {
	var skipA, skipB int64
	if shift > 0 {
		skipB = shift
	} else {
		skipA = -shift
	}
	return diff(ctx, a, b, skipA, skipB, false)
}

// diff compares A and B after skipping the first skipA samples of A and skipB of
// B. With look set, it also takes windows to look for a shift, which only makes
// sense with nothing skipped.
func diff(ctx context.Context, a, b []string, skipA, skipB int64, look bool) (Difference, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ra, rb := stream(ctx, a), stream(ctx, b)
	// Closing the readers lets a decode that is still writing know it is not
	// wanted, whichever way this returns.
	defer ra.Close()
	defer rb.Close()

	d := Difference{First: -1, Last: -1}
	var captures []*capture
	if look {
		for _, c := range captureCentres {
			captures = append(captures, newCapture(c))
		}
	}

	var totalA, totalB, base int64 // bytes read, and how many have been compared
	doneA, doneB := false, false

	n, ended, err := skipBytes(ra, skipA*bytesPerSample)
	if err != nil {
		return d, err
	}
	totalA, doneA = n, ended
	n, ended, err = skipBytes(rb, skipB*bytesPerSample)
	if err != nil {
		return d, err
	}
	totalB, doneB = n, ended

	bufA, bufB := make([]byte, diffChunk), make([]byte, diffChunk)
	for !doneA || !doneB {
		at := totalA // where this chunk begins, when nothing is skipped
		na, endA, err := readChunk(ra, bufA, doneA)
		if err != nil {
			return d, err
		}
		nb, endB, err := readChunk(rb, bufB, doneB)
		if err != nil {
			return d, err
		}
		for _, c := range captures {
			c.add(bufA[:na], bufB[:nb], at)
		}
		totalA += int64(na)
		totalB += int64(nb)
		doneA, doneB = doneA || endA, doneB || endB

		// Only what both have is compared. Once one is done, all that is
		// left of the other is how long it is.
		common := min(na, nb)
		common -= common % bytesPerSample
		if !bytes.Equal(bufA[:common], bufB[:common]) {
			d.note(bufA[:common], bufB[:common], skipA+base/bytesPerSample)
		}
		base += int64(common)
	}

	d.SamplesA, d.SamplesB = totalA/bytesPerSample, totalB/bytesPerSample
	if d.Differing > 0 {
		d.Shift = detectShift(captures)
	}
	return d, nil
}

// skipBytes reads and discards n bytes of a stream, and reports how many there
// were, and whether it ended before that.
func skipBytes(r io.Reader, n int64) (read int64, ended bool, err error) {
	read, err = io.CopyN(io.Discard, r, n)
	switch {
	case err == nil:
		return read, false, nil
	case errors.Is(err, io.EOF):
		return read, true, nil
	}
	return read, false, err
}

// note records the samples of a and b, which start at sample index start, that
// differ.
func (d *Difference) note(a, b []byte, start int64) {
	for i := 0; i < len(a); i += bytesPerSample {
		if bytes.Equal(a[i:i+bytesPerSample], b[i:i+bytesPerSample]) {
			continue
		}

		at := start + int64(i/bytesPerSample)
		d.Differing++
		if silent(a[i:i+bytesPerSample]) || silent(b[i:i+bytesPerSample]) {
			d.Silent++
		}
		if d.First < 0 {
			d.First = at
		}
		d.Last = at

		if n := len(d.Runs); n > 0 && at-d.Runs[n-1].End <= runGap && !d.RunsTruncated {
			d.Runs[n-1].End = at + 1
		} else if n < maxRuns {
			d.Runs = append(d.Runs, Range{at, at + 1})
		} else {
			d.RunsTruncated = true
		}
	}
}

// silent reports whether a sample is all zero.
func silent(sample []byte) bool {
	for _, b := range sample {
		if b != 0 {
			return false
		}
	}
	return true
}

// capture is a window of A and the part of B around the same place, taken as
// both are read, to look for a shift.
type capture struct {
	centre int64  // the window's first sample in A
	bLo    int64  // the first sample of region
	window []byte // A from centre, windowSamples long
	region []byte // B from bLo, windowSamples+2*maxShift long, or as far as it goes
}

func newCapture(centre int64) *capture {
	return &capture{
		centre: centre,
		bLo:    centre - maxShift,
		window: make([]byte, 0, windowSamples*bytesPerSample),
		region: make([]byte, 0, (windowSamples+2*maxShift)*bytesPerSample),
	}
}

// add takes what is wanted from the next chunk of each side, which begin at byte
// position at. The chunks come in order, so each buffer is filled from its start.
func (c *capture) add(a, b []byte, at int64) {
	appendOverlap(&c.window, c.centre*bytesPerSample, a, at)
	appendOverlap(&c.region, c.bLo*bytesPerSample, b, at)
}

// appendOverlap appends to dst, which holds what there is so far of the bytes of
// the stream that begin at lo and that has room for as many as it is to hold, the
// part of chunk, which begins at position at, that comes next.
func appendOverlap(dst *[]byte, lo int64, chunk []byte, at int64) {
	start := max(lo, at)
	end := min(lo+int64(cap(*dst)), at+int64(len(chunk)))
	if start >= end || int64(len(*dst)) != start-lo {
		return
	}
	*dst = append(*dst, chunk[start-at:end-at]...)
}

// detectShift looks for A's windows in B's regions. It reports a shift if every
// window it could find agrees on one, which is not zero, and nil otherwise.
func detectShift(captures []*capture) *Shift {
	var found []int64
	tried := 0
	for _, c := range captures {
		if len(c.window) < cap(c.window) || distinctSamples(c.window) < minDistinct {
			continue
		}
		tried++
		if i, ok := findAligned(c.region, c.window); ok {
			found = append(found, c.bLo+int64(i/bytesPerSample)-c.centre)
		}
	}

	if len(found) == 0 || found[0] == 0 {
		return nil
	}
	for _, k := range found {
		if k != found[0] {
			return nil
		}
	}
	return &Shift{Samples: found[0], Matches: len(found), Tried: tried}
}

// distinctSamples counts the different samples in b.
func distinctSamples(b []byte) int {
	seen := map[[bytesPerSample]byte]struct{}{}
	for i := 0; i+bytesPerSample <= len(b); i += bytesPerSample {
		seen[[bytesPerSample]byte(b[i:i+bytesPerSample])] = struct{}{}
	}
	return len(seen)
}

// findAligned finds window in region, on a sample boundary, if it is there once
// and only once, and returns its byte position. More than once, a window that is
// not distinctive enough to say where, is not found.
func findAligned(region, window []byte) (at int, ok bool) {
	found := -1
	for from := 0; from <= len(region)-len(window); {
		i := bytes.Index(region[from:], window)
		if i < 0 {
			break
		}
		i += from
		if i%bytesPerSample == 0 {
			if found >= 0 {
				return 0, false
			}
			found = i
		}
		from = i + 1
	}
	return found, found >= 0
}

// stream decodes the files, in order, as one stretch of audio, which is read from
// the pipe it returns. A file that does not decode ends the stream with that
// error.
func stream(ctx context.Context, paths []string) *io.PipeReader {
	pr, pw := io.Pipe()
	go func() {
		for _, path := range paths {
			if err := decode(ctx, path, pw); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		pw.Close()
	}()
	return pr
}

// readChunk reads up to len(buf) bytes of a stream, which is a full buffer unless
// the stream ends in it. It reports whether it has, and nothing is read of a
// stream already done. An error is one the stream ended with.
func readChunk(r io.Reader, buf []byte, done bool) (n int, ended bool, err error) {
	if done {
		return 0, true, nil
	}
	n, err = io.ReadFull(r, buf)
	switch {
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return n, true, nil
	case err != nil:
		return n, false, err
	}
	return n, false, nil
}
