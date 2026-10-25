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

// Difference is how two stretches of audio, A and B, differ. Samples are counted
// from zero, and compared where both have one: audio that one has beyond the
// end of the other is a difference in length and not in the samples.
type Difference struct {
	// SamplesA and SamplesB are how many samples each has.
	SamplesA, SamplesB int64
	// Differing is how many samples differ, among those both have.
	Differing int64
	// First and Last are the first and last of them, or -1 if none differ.
	First, Last int64
	// Runs are the stretches they fall in, in order, with those no more than
	// runGap samples apart taken as one. Only the first maxRuns are kept, and
	// RunsTruncated says whether there were more.
	Runs          []Range
	RunsTruncated bool
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
)

// Diff decodes the files of each side, in order, as one stretch of audio, and
// compares the two, in step, without holding either: what is held is a chunk of
// each, however long they are. It stops at the first file that does not decode
// cleanly, on either side, with the *DecodeError for it, or ErrNoFFmpeg if there
// is no ffmpeg, or the context's error if it was cancelled.
//
// What it finds is only whether the samples are the same. Where one stretch is
// the other shifted, as when a wrong drive offset moves everything, nearly every
// sample differs, which is for the caller to recognize.
func (Decoder) Diff(ctx context.Context, a, b []string) (Difference, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ra, rb := stream(ctx, a), stream(ctx, b)
	// Closing the readers lets a decode that is still writing know it is not
	// wanted, whichever way this returns.
	defer ra.Close()
	defer rb.Close()

	d := Difference{First: -1, Last: -1}
	bufA, bufB := make([]byte, diffChunk), make([]byte, diffChunk)
	var totalA, totalB, base int64 // bytes read, and how many have been compared
	doneA, doneB := false, false

	for !doneA || !doneB {
		na, endA, err := readChunk(ra, bufA, doneA)
		if err != nil {
			return d, err
		}
		nb, endB, err := readChunk(rb, bufB, doneB)
		if err != nil {
			return d, err
		}
		totalA += int64(na)
		totalB += int64(nb)
		doneA, doneB = doneA || endA, doneB || endB

		// Only what both have is compared. Once one is done, all that is
		// left of the other is how long it is.
		common := min(na, nb)
		common -= common % bytesPerSample
		if !bytes.Equal(bufA[:common], bufB[:common]) {
			d.note(bufA[:common], bufB[:common], base/bytesPerSample)
		}
		base += int64(common)
	}

	d.SamplesA, d.SamplesB = totalA/bytesPerSample, totalB/bytesPerSample
	return d, nil
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
