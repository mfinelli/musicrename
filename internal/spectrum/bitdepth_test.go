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
)

func TestBitTracker(t *testing.T) {
	// feed passes integer sample values (as they would appear in a file of
	// the given depth) to a tracker, scaled to the ±1.0 range.
	feed := func(declared int, vals ...int) *bitTracker {
		bt := newBitTracker(declared)
		for _, v := range vals {
			bt.observe(float64(v) / math.Ldexp(1, declared-1))
		}
		return bt
	}

	t.Run("full use of the declared depth", func(t *testing.T) {
		assert.Equal(t, 16, feed(16, 1, -3, 12000, -32768).effective())
		assert.Equal(t, 24, feed(24, 1, -3, 8388607, 77).effective())
	})

	t.Run("16-bit audio padded into 24 bits", func(t *testing.T) {
		// Every 24-bit sample is a 16-bit one shifted left by eight.
		assert.Equal(t, 16, feed(24, 1<<8, -3<<8, 12000<<8, 77<<8).effective())
	})

	t.Run("a single odd sample is enough to show full depth", func(t *testing.T) {
		assert.Equal(t, 24, feed(24, 1<<8, 2<<8, 3<<8, 5).effective())
	})

	t.Run("sign does not matter", func(t *testing.T) {
		assert.Equal(t, 16, feed(24, -(1<<8), -(5<<8)).effective())
	})

	t.Run("silence is undetermined", func(t *testing.T) {
		assert.Equal(t, 0, feed(24, 0, 0, 0).effective())
	})

	t.Run("unknown or unsupported depths are undetermined", func(t *testing.T) {
		assert.Equal(t, 0, feed(0, 1, 2, 3).effective())
		assert.Equal(t, 0, feed(32, 1, 2, 3).effective())
		assert.Equal(t, 0, feed(4, 1, 2, 3).effective())
	})

	t.Run("non-integer samples are undetermined", func(t *testing.T) {
		bt := newBitTracker(16)
		bt.observe(0.123456789)
		assert.Equal(t, 0, bt.effective())
		// And it stays that way.
		bt.observe(1.0 / 32768)
		assert.Equal(t, 0, bt.effective())
	})
}

func TestAssessBitDepth(t *testing.T) {
	th := DefaultThresholds()

	tests := []struct {
		name      string
		declared  int
		effective int
		want      bool
	}{
		{"24-bit holding 16-bit data", 24, 16, true},
		{"24-bit holding 12-bit data", 24, 12, true},
		{"24-bit using 20 bits", 24, 20, false},
		{"24-bit using all bits", 24, 24, false},
		{"16-bit using all bits", 16, 16, false},
		{"16-bit using fewer", 16, 14, false},
		{"unknown effective depth", 24, 0, false},
		{"unknown declared depth", 0, 16, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, ok := assessBitDepth(StreamInfo{BitsPerSample: tc.declared}, th, tc.effective)
			assert.Equal(t, tc.want, ok)
			if ok {
				assert.Equal(t, KindBitDepth, f.Kind)
				assert.Equal(t, VerdictSuspect, f.Level)
				assert.Contains(t, f.Detail, "only uses")
			}
		})
	}
}
