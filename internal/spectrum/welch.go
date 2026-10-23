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

package spectrum

import "math"

// welch accumulates a long-term average power spectrum for each channel of a
// stream using Welch's method: the audio is cut into overlapping frames, each
// frame is multiplied by a Hann window and Fourier transformed, and the
// resulting power spectra are averaged. Averaging many frames washes out the
// moment-to-moment detail of the music and leaves the stable, structural
// features (such as the lowpass a lossy encoder applies) standing out.
//
// Frames overlap by 50%. With a Hann window that is the standard choice: every
// sample is weighted by the two frames that contain it so no part of the audio
// is effectively ignored.
//
// A welch is not safe for concurrent use.
type welch struct {
	n          int // window length in samples
	hop        int // samples between frame starts
	plan       *fftPlan
	win        []float64
	norm       float64 // scales |X|² so a full-scale sine reads as 1.0 (0 dB)
	silenceRMS float64 // frames with a lower RMS are ignored
	re, im     []float64
	ch         []*chanState
}

type chanState struct {
	pending []float64 // samples not yet consumed by a complete frame
	power   []float64 // running sum of per-frame power, n/2+1 bins
	frames  int       // frames included in power
	silent  int       // frames skipped for being too quiet
}

// newWelch prepares an accumulator for the given window length and channel
// count. Frames whose RMS level is below silenceDB (in dB relative to full
// scale) are skipped.
func newWelch(n, channels int, silenceDB float64) *welch {
	win := hann(n)
	var sum float64
	for _, v := range win {
		sum += v
	}

	w := &welch{
		n:          n,
		hop:        n / 2,
		plan:       newFFTPlan(n),
		win:        win,
		norm:       (sum / 2) * (sum / 2),
		silenceRMS: math.Pow(10, silenceDB/20),
		re:         make([]float64, n),
		im:         make([]float64, n),
		ch:         make([]*chanState, channels),
	}
	for i := range w.ch {
		w.ch[i] = &chanState{power: make([]float64, n/2+1)}
	}
	return w
}

// feed appends samples (scaled so full scale is ±1.0) for one channel and
// processes every complete frame that results.
func (w *welch) feed(ch int, x []float64) {
	st := w.ch[ch]
	st.pending = append(st.pending, x...)

	off := 0
	for len(st.pending)-off >= w.n {
		w.frame(st, st.pending[off:off+w.n])
		off += w.hop
	}

	rem := copy(st.pending, st.pending[off:])
	st.pending = st.pending[:rem]
}

func (w *welch) frame(st *chanState, x []float64) {
	var ss float64
	for _, v := range x {
		ss += v * v
	}
	if math.Sqrt(ss/float64(w.n)) < w.silenceRMS {
		st.silent++
		return
	}

	for i, v := range x {
		w.re[i] = v * w.win[i]
		w.im[i] = 0
	}
	w.plan.transform(w.re, w.im)

	for k := 0; k <= w.n/2; k++ {
		st.power[k] += (w.re[k]*w.re[k] + w.im[k]*w.im[k]) / w.norm
	}
	st.frames++
}

// average returns the mean power spectrum of channel ch (n/2+1 bins, linear
// power) and the number of frames it was averaged over. With no frames the
// spectrum is all zeros.
func (w *welch) average(ch int) (power []float64, frames int) {
	st := w.ch[ch]
	out := make([]float64, len(st.power))
	if st.frames == 0 {
		return out, 0
	}
	for k, v := range st.power {
		out[k] = v / float64(st.frames)
	}
	return out, st.frames
}
