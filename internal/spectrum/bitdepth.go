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
	"math/bits"
)

// maxTrackedBits is the widest sample size the bit-depth check supports. The
// decoded samples are carried as 32-bit floats, which represent every integer
// of up to 24 bits exactly but not wider ones.
const (
	minTrackedBits = 8
	maxTrackedBits = 24
)

// bitTracker works out how many bits of a stream's declared bit depth are
// actually used. A 24-bit file made by padding 16-bit audio has eight low
// bits that are zero in every sample.
type bitTracker struct {
	declared int
	scale    float64
	used     uint64 // OR of every sample's magnitude, as integers
	broken   bool   // saw a sample that was not integer-valued
}

// newBitTracker returns a tracker for a stream declared to be `declared` bits
// per sample. For an unknown or unsupported depth the tracker is inert and
// effective always reports unknown.
func newBitTracker(declared int) *bitTracker {
	t := &bitTracker{declared: declared}
	if declared < minTrackedBits || declared > maxTrackedBits {
		t.broken = true
		return t
	}
	t.scale = math.Ldexp(1, declared-1)
	return t
}

// observe records one sample (full scale is ±1.0).
func (t *bitTracker) observe(v float64) {
	if t.broken {
		return
	}
	x := v * t.scale
	r := math.Round(x)
	if math.Abs(x-r) > 0.01 {
		// A lossless integer stream always scales to whole numbers; if
		// this one does not, the declared depth cannot be trusted.
		t.broken = true
		return
	}
	t.used |= uint64(math.Abs(r))
}

// effective returns the number of bits the samples actually use, or 0 if it
// cannot be determined (unknown depth, non-integer samples, or silence).
func (t *bitTracker) effective() int {
	if t.broken || t.used == 0 {
		return 0
	}
	return t.declared - bits.TrailingZeros64(t.used)
}
