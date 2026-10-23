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
	"math/cmplx"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFFT(t *testing.T) {
	t.Run("matches a naive DFT", func(t *testing.T) {
		const n = 64
		rng := rand.New(rand.NewSource(1))
		re := make([]float64, n)
		im := make([]float64, n)
		in := make([]complex128, n)
		for i := range n {
			re[i], im[i] = rng.NormFloat64(), rng.NormFloat64()
			in[i] = complex(re[i], im[i])
		}

		newFFTPlan(n).transform(re, im)

		for k := range n {
			var want complex128
			for j := range n {
				want += in[j] * cmplx.Exp(complex(0, -2*math.Pi*float64(j*k)/n))
			}
			assert.InDelta(t, real(want), re[k], 1e-9, "re[%d]", k)
			assert.InDelta(t, imag(want), im[k], 1e-9, "im[%d]", k)
		}
	})

	t.Run("an impulse has a flat spectrum", func(t *testing.T) {
		const n = 128
		re := make([]float64, n)
		im := make([]float64, n)
		re[0] = 1

		newFFTPlan(n).transform(re, im)

		for k := range n {
			assert.InDelta(t, 1, re[k], 1e-12)
			assert.InDelta(t, 0, im[k], 1e-12)
		}
	})

	t.Run("a sine on a bin concentrates in that bin", func(t *testing.T) {
		const n, bin = 256, 17
		re := make([]float64, n)
		im := make([]float64, n)
		for i := range n {
			re[i] = math.Sin(2 * math.Pi * bin * float64(i) / n)
		}

		newFFTPlan(n).transform(re, im)

		for k := 0; k <= n/2; k++ {
			mag := math.Hypot(re[k], im[k])
			if k == bin {
				assert.InDelta(t, n/2, mag, 1e-9)
			} else {
				assert.InDelta(t, 0, mag, 1e-9, "bin %d", k)
			}
		}
	})

	t.Run("rejects sizes that are not a power of two", func(t *testing.T) {
		assert.Panics(t, func() { newFFTPlan(100) })
		assert.Panics(t, func() { newFFTPlan(1) })
		assert.Panics(t, func() { newFFTPlan(0) })
	})
}

func TestHann(t *testing.T) {
	const n = 1024
	w := hann(n)

	assert.Len(t, w, n)
	assert.InDelta(t, 0, w[0], 1e-12, "starts at zero")
	assert.InDelta(t, 1, w[n/2], 1e-12, "peaks at one in the middle")

	var sum float64
	for _, v := range w {
		sum += v
	}
	assert.InDelta(t, n/2, sum, 1e-9, "the periodic Hann window sums to n/2")
}

func TestWindowSize(t *testing.T) {
	tests := []struct {
		rate int
		want int
	}{
		{8000, 4096},
		{22050, 4096},
		{32000, 4096},
		{44100, 4096},
		{48000, 4096},
		{88200, 8192},
		{96000, 8192},
		{176400, 16384},
		{192000, 16384},
		{384000, 32768},
		{768000, 32768}, // capped
	}
	for _, tc := range tests {
		t.Run(rateName(tc.rate), func(t *testing.T) {
			got := WindowSize(tc.rate)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, 0, got&(got-1), "must be a power of two")
		})
	}
}

func rateName(r int) string {
	return map[bool]string{true: "hi-res", false: "cd-rate"}[r >= 88200] + "_" + itoa(r)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}
