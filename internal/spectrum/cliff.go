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

// powerFloor keeps log10 finite for bins with exactly zero power.
const powerFloor = 1e-20

// Curve is a long-term average spectrum condensed into fixed-width bands.
type Curve struct {
	// BandHz is the width of each band.
	BandHz float64
	// DB holds the mean level of each band in dB relative to a full-scale
	// sine wave (0 dB is a sine at full scale; a typical 16-bit rounding
	// noise floor is around -129 dB per bin at the default window size).
	// Band i covers [i*BandHz, (i+1)*BandHz).
	DB []float64
}

// Freq returns the centre frequency of band i in Hz.
func (c Curve) Freq(i int) float64 {
	return (float64(i) + 0.5) * c.BandHz
}

// newCurve condenses an average power spectrum (n/2+1 linear-power bins from
// a window of n samples at sampleRate) into bands bandHz wide. Levels are
// averaged in dB within a band, which keeps a single loud bin from dominating
// its neighbors.
func newCurve(power []float64, sampleRate, window int, bandHz float64) Curve {
	binHz := float64(sampleRate) / float64(window)
	per := int(math.Round(bandHz / binHz))
	if per < 1 {
		per = 1
	}

	// The last bin sits exactly at Nyquist; drop any partial trailing band.
	usable := len(power) - 1
	nb := usable / per

	db := make([]float64, nb)
	for i := range nb {
		var sum float64
		for _, p := range power[i*per : (i+1)*per] {
			sum += 10 * math.Log10(p+powerFloor)
		}
		db[i] = sum / float64(per)
	}

	return Curve{BandHz: binHz * float64(per), DB: db}
}

// Cliff describes a sharp drop in the spectrum: the point above which the
// recording has no (or almost no) content.
type Cliff struct {
	// CutoffHz is where the curve crosses halfway (in dB) between the level
	// just below the edge and the level just above it.
	CutoffHz float64
	// DropDB is the difference between those two levels at the steepest
	// point of the edge.
	DropDB float64
	// BelowDB and AboveDB are the mean levels on either side of the edge.
	BelowDB, AboveDB float64
}

// findCliff looks for the lowest-frequency sharp edge in c: the place where
// the content first ends. It compares, at each candidate boundary, the mean
// level of the CompareWindowHz below with the mean level of the
// CompareWindowHz above. The first run of boundaries where that difference
// reaches MinDropDB is one edge; the steepest boundary in the run locates it.
//
// The lowest edge is preferred over the biggest one because what matters is
// where the content first stops. A resampler or encoder can leave several
// smaller steps in the noise floor above the real edge, and the largest of
// those is not the one that matters.
func findCliff(c Curve, th Thresholds) (Cliff, bool) {
	w := int(math.Round(th.CompareWindowHz / c.BandHz))
	if w < 2 {
		w = 2
	}
	nb := len(c.DB)
	if nb < 2*w {
		return Cliff{}, false
	}

	// drop(j) compares bands [j-w, j) with bands [j, j+w).
	drop := func(j int) float64 {
		var left, right float64
		for _, v := range c.DB[j-w : j] {
			left += v
		}
		for _, v := range c.DB[j : j+w] {
			right += v
		}
		return (left - right) / float64(w)
	}

	first := w
	if from := int(math.Ceil(th.SearchFromHz / c.BandHz)); from > first {
		first = from
	}

	start := -1
	for j := first; j+w <= nb; j++ {
		if drop(j) >= th.MinDropDB {
			start = j
			break
		}
	}
	if start < 0 {
		return Cliff{}, false
	}

	peak, peakDrop := start, drop(start)
	for j := start + 1; j+w <= nb; j++ {
		d := drop(j)
		if d < th.MinDropDB {
			break
		}
		if d > peakDrop {
			peak, peakDrop = j, d
		}
	}

	var below, above float64
	for _, v := range c.DB[peak-w : peak] {
		below += v
	}
	for _, v := range c.DB[peak : peak+w] {
		above += v
	}
	below /= float64(w)
	above /= float64(w)

	mid := (below + above) / 2
	k := peak - w
	for k < nb && c.DB[k] >= mid {
		k++
	}

	return Cliff{
		CutoffHz: c.Freq(k),
		DropDB:   peakDrop,
		BelowDB:  below,
		AboveDB:  above,
	}, true
}
