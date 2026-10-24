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

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// curveOf builds a Curve of 100 Hz bands up to nyquist from a level function.
func curveOf(nyquist float64, level func(hz float64) float64) Curve {
	const band = 100.0
	nb := int(nyquist / band)
	c := Curve{BandHz: band, DB: make([]float64, nb)}
	for i := range c.DB {
		c.DB[i] = level(c.Freq(i))
	}
	return c
}

// edge is a level function: flat at hi until from, ramping linearly to lo by
// to, flat at lo after.
func edge(hi, lo, from, to float64) func(float64) float64 {
	return func(f float64) float64 {
		switch {
		case f <= from:
			return hi
		case f >= to:
			return lo
		default:
			return hi + (lo-hi)*(f-from)/(to-from)
		}
	}
}

func TestFindCliff(t *testing.T) {
	th := DefaultThresholds()

	t.Run("a lossy-style lowpass", func(t *testing.T) {
		c := curveOf(22050, edge(-60, -127, 16000, 17000))

		got, ok := findCliff(c, th)

		require.True(t, ok)
		assert.InDelta(t, 16500, got.CutoffHz, 500)
		assert.Greater(t, got.DropDB, 50.0)
		// The levels are 1 kHz means adjacent to the steepest point, so on a
		// ramp they include part of the ramp itself.
		assert.Greater(t, got.BelowDB, -72.0)
		assert.Less(t, got.AboveDB, -115.0)
	})

	t.Run("a very sharp edge", func(t *testing.T) {
		c := curveOf(22050, edge(-80, -129, 15990, 16010))

		got, ok := findCliff(c, th)

		require.True(t, ok)
		assert.InDelta(t, 16000, got.CutoffHz, 300)
	})

	t.Run("a gradual resampler-style edge several kHz wide", func(t *testing.T) {
		c := curveOf(48000, edge(-85, -170, 22000, 26000))

		got, ok := findCliff(c, th)

		require.True(t, ok)
		assert.Greater(t, got.CutoffHz, 22000.0)
		assert.Less(t, got.CutoffHz, 26500.0)
	})

	t.Run("a smooth natural roll-off is not a cliff", func(t *testing.T) {
		// 10 dB per octave from 1 kHz: steep by music standards but
		// nowhere near an encoder's lowpass.
		c := curveOf(22050, func(f float64) float64 {
			return -40 - 10*math.Log2(math.Max(f, 1000)/1000)
		})

		_, ok := findCliff(c, th)

		assert.False(t, ok)
	})

	t.Run("a small step is not a cliff", func(t *testing.T) {
		c := curveOf(22050, edge(-60, -72, 15000, 15200))

		_, ok := findCliff(c, th)

		assert.False(t, ok)
	})

	t.Run("flat spectrum has no cliff", func(t *testing.T) {
		c := curveOf(22050, func(float64) float64 { return -70 })

		_, ok := findCliff(c, th)

		assert.False(t, ok)
	})

	t.Run("edges below the search floor are ignored", func(t *testing.T) {
		c := curveOf(22050, edge(-40, -110, 2500, 2700))

		_, ok := findCliff(c, th)

		assert.False(t, ok)
	})

	t.Run("the lowest edge wins over a bigger one above it", func(t *testing.T) {
		c := curveOf(48000, func(f float64) float64 {
			switch {
			case f < 18000:
				return -60
			case f < 20000:
				return -100 // first, smaller edge: content ends here
			default:
				return -170 // second, bigger step inside the noise floor
			}
		})

		got, ok := findCliff(c, th)

		require.True(t, ok)
		assert.InDelta(t, 18000, got.CutoffHz, 500)
	})

	t.Run("a curve too short to compare is not a cliff", func(t *testing.T) {
		c := Curve{BandHz: 100, DB: []float64{-60, -60, -120}}

		_, ok := findCliff(c, th)

		assert.False(t, ok)
	})
}

func TestNewCurve(t *testing.T) {
	t.Run("bands average the bins inside them", func(t *testing.T) {
		// 4096-point window at 44.1 kHz: ~10.77 Hz bins, 9 bins per
		// 100 Hz band (~96.9 Hz).
		const sr, window = 44100, 4096
		power := make([]float64, window/2+1)
		for k := range power {
			power[k] = 1e-6 // -60 dB everywhere
		}

		c := newCurve(power, sr, window, 100)

		assert.InDelta(t, 96.9, c.BandHz, 0.1)
		assert.Equal(t, (window/2)/9, len(c.DB))
		for _, v := range c.DB {
			assert.InDelta(t, -60, v, 1e-9)
		}
	})

	t.Run("zero power stays finite", func(t *testing.T) {
		power := make([]float64, 2049)

		c := newCurve(power, 44100, 4096, 100)

		for _, v := range c.DB {
			assert.False(t, math.IsInf(v, 0) || math.IsNaN(v))
		}
	})

	t.Run("band centres", func(t *testing.T) {
		c := Curve{BandHz: 100, DB: make([]float64, 3)}
		assert.Equal(t, 50.0, c.Freq(0))
		assert.Equal(t, 250.0, c.Freq(2))
	})
}
