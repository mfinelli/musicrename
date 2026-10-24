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
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWelch(t *testing.T) {
	const sr, n = 44100, 4096

	sine := func(amp, freq float64, samples int) []float64 {
		x := make([]float64, samples)
		for i := range x {
			x[i] = amp * math.Sin(2*math.Pi*freq*float64(i)/sr)
		}
		return x
	}
	db := func(p float64) float64 { return 10 * math.Log10(p) }

	t.Run("a full-scale sine reads as 0 dB", func(t *testing.T) {
		// A frequency exactly on a bin centre, so no energy straddles bins.
		freq := 100 * float64(sr) / n
		w := newWelch(n, 1, -75)
		w.feed(0, sine(1, freq, n*20))

		p, frames := w.average(0)
		assert.Greater(t, frames, 0)
		assert.InDelta(t, 0, db(p[100]), 0.01)
	})

	t.Run("level follows amplitude", func(t *testing.T) {
		freq := 100 * float64(sr) / n
		w := newWelch(n, 1, -75)
		w.feed(0, sine(0.5, freq, n*20))

		p, _ := w.average(0)
		assert.InDelta(t, -6.02, db(p[100]), 0.01)
	})

	t.Run("result does not depend on how input is chunked", func(t *testing.T) {
		rng := rand.New(rand.NewSource(2))
		x := make([]float64, n*30+123)
		for i := range x {
			x[i] = 0.3 * rng.NormFloat64()
		}

		whole := newWelch(n, 1, -75)
		whole.feed(0, x)
		pw, fw := whole.average(0)

		chunked := newWelch(n, 1, -75)
		for i := 0; i < len(x); {
			step := 1 + rng.Intn(5000)
			if i+step > len(x) {
				step = len(x) - i
			}
			chunked.feed(0, x[i:i+step])
			i += step
		}
		pc, fc := chunked.average(0)

		assert.Equal(t, fw, fc)
		for k := range pw {
			assert.InDelta(t, pw[k], pc[k], 1e-15)
		}
	})

	t.Run("frames overlap by half", func(t *testing.T) {
		w := newWelch(n, 1, -200)
		x := make([]float64, n*4)
		for i := range x {
			x[i] = 0.1
		}
		w.feed(0, x)
		_, frames := w.average(0)
		// Frame starts at 0, n/2, ..., 3n: seven complete frames.
		assert.Equal(t, 7, frames)
	})

	t.Run("silent frames are skipped", func(t *testing.T) {
		w := newWelch(n, 1, -75)
		w.feed(0, make([]float64, n*10)) // digital silence
		p, frames := w.average(0)
		assert.Equal(t, 0, frames)
		assert.Equal(t, 0.0, p[10])
		assert.Greater(t, w.ch[0].silent, 0)
	})

	t.Run("channels are kept separate", func(t *testing.T) {
		freqA := 100 * float64(sr) / n
		freqB := 300 * float64(sr) / n
		w := newWelch(n, 2, -75)
		w.feed(0, sine(1, freqA, n*10))
		w.feed(1, sine(1, freqB, n*10))

		p0, _ := w.average(0)
		p1, _ := w.average(1)
		assert.Greater(t, p0[100], 0.1)
		assert.Less(t, p0[300], 1e-6)
		assert.Greater(t, p1[300], 0.1)
		assert.Less(t, p1[100], 1e-6)
	})
}
