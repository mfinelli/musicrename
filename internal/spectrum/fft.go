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

// fftPlan holds the precomputed tables for an in-place radix-2 FFT of one
// fixed size. Building the tables once and reusing them for every frame of a
// track is what keeps the analysis fast: a three-minute stereo track needs
// several thousand transforms of the same size.
type fftPlan struct {
	n   int
	rev []int     // bit-reversal permutation
	cos []float64 // cos(2πk/n) for k in [0, n/2)
	sin []float64 // sin(2πk/n) for k in [0, n/2)
}

// newFFTPlan builds a plan for transforms of n points. n must be a power of
// two (at least 2); anything else is a programming error and panics.
func newFFTPlan(n int) *fftPlan {
	if n < 2 || n&(n-1) != 0 {
		panic("spectrum: FFT size must be a power of two")
	}

	p := &fftPlan{
		n:   n,
		rev: make([]int, n),
		cos: make([]float64, n/2),
		sin: make([]float64, n/2),
	}

	bits := 0
	for 1<<bits < n {
		bits++
	}
	for i := range n {
		r := 0
		for b := range bits {
			if i&(1<<b) != 0 {
				r |= 1 << (bits - 1 - b)
			}
		}
		p.rev[i] = r
	}

	for k := range n / 2 {
		a := 2 * math.Pi * float64(k) / float64(n)
		p.cos[k] = math.Cos(a)
		p.sin[k] = math.Sin(a)
	}

	return p
}

// transform computes the forward discrete Fourier transform of (re, im) in
// place. Both slices must have length p.n.
func (p *fftPlan) transform(re, im []float64) {
	n := p.n

	for i, j := range p.rev {
		if i < j {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}

	for size := 2; size <= n; size <<= 1 {
		half := size >> 1
		step := n / size
		for start := 0; start < n; start += size {
			for k := range half {
				// Twiddle factor w = e^(-2πik/size) = c - i·s.
				c, s := p.cos[k*step], p.sin[k*step]
				a, b := start+k, start+k+half
				tr := re[b]*c + im[b]*s
				ti := im[b]*c - re[b]*s
				re[b], im[b] = re[a]-tr, im[a]-ti
				re[a], im[a] = re[a]+tr, im[a]+ti
			}
		}
	}
}

// hann returns the periodic Hann window of length n. The periodic form (the
// window that would continue seamlessly if repeated) is the right one for
// spectral analysis; the symmetric form is for filter design.
func hann(n int) []float64 {
	w := make([]float64, n)
	for i := range w {
		w[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n))
	}
	return w
}

// maxBinHz is the widest frequency bin (in Hz) the analysis tolerates. The
// window size is the smallest power of two, no smaller than minWindow, that
// keeps bins at or under this width.
const (
	maxBinHz  = 12.0
	minWindow = 4096
	maxWindow = 32768
)

// WindowSize returns the analysis window length, in samples, used for audio
// at sampleRate Hz. The window grows with the sample rate so the width of a
// frequency bin (sampleRate / window) stays roughly constant at about
// 11 Hz: 4096 samples at 44.1 and 48 kHz, 8192 at 88.2 and 96 kHz, 16384 at
// 176.4 and 192 kHz.
func WindowSize(sampleRate int) int {
	n := minWindow
	for float64(sampleRate)/float64(n) > maxBinHz && n < maxWindow {
		n <<= 1
	}
	return n
}
