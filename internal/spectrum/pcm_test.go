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
	"bytes"
	"io"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadPCM(t *testing.T) {
	// ramp builds interleaved samples where channel c of frame f is f*10+c,
	// small enough to be exactly representable as float32.
	ramp := func(frames, channels int) []byte {
		chans := make([][]float64, channels)
		for c := range chans {
			chans[c] = make([]float64, frames)
			for f := range frames {
				chans[c][f] = float64(f*10 + c)
			}
		}
		return pcm(chans...)
	}

	collect := func(r io.Reader, channels int) ([][]float64, error) {
		got := make([][]float64, channels)
		err := readPCM(r, channels, func(chans [][]float64) {
			for c := range chans {
				got[c] = append(got[c], chans[c]...)
			}
		})
		return got, err
	}

	t.Run("splits interleaved channels", func(t *testing.T) {
		got, err := collect(bytes.NewReader(ramp(5, 2)), 2)

		require.NoError(t, err)
		assert.Equal(t, []float64{0, 10, 20, 30, 40}, got[0])
		assert.Equal(t, []float64{1, 11, 21, 31, 41}, got[1])
	})

	t.Run("spans chunk boundaries without losing or repeating samples", func(t *testing.T) {
		frames := pcmChunkFrames*2 + 123
		got, err := collect(bytes.NewReader(ramp(frames, 3)), 3)

		require.NoError(t, err)
		for c := range 3 {
			require.Len(t, got[c], frames)
			assert.Equal(t, float64(0*10+c), got[c][0])
			assert.Equal(t, float64((pcmChunkFrames-1)*10+c), got[c][pcmChunkFrames-1])
			assert.Equal(t, float64(pcmChunkFrames*10+c), got[c][pcmChunkFrames])
			assert.Equal(t, float64((frames-1)*10+c), got[c][frames-1])
		}
	})

	t.Run("works when the reader returns data in tiny pieces", func(t *testing.T) {
		got, err := collect(iotest.OneByteReader(bytes.NewReader(ramp(40, 2))), 2)

		require.NoError(t, err)
		assert.Len(t, got[0], 40)
		assert.Equal(t, float64(390), got[0][39])
	})

	t.Run("ignores a trailing partial frame", func(t *testing.T) {
		data := append(ramp(4, 2), 9, 9, 9)

		got, err := collect(bytes.NewReader(data), 2)

		require.NoError(t, err)
		assert.Len(t, got[0], 4)
	})

	t.Run("an empty stream is fine", func(t *testing.T) {
		called := false
		err := readPCM(bytes.NewReader(nil), 2, func([][]float64) { called = true })

		require.NoError(t, err)
		assert.False(t, called)
	})

	t.Run("a read error is returned", func(t *testing.T) {
		_, err := collect(iotest.ErrReader(assert.AnError), 2)
		assert.ErrorContains(t, err, "reading decoded audio")
	})

	t.Run("samples keep their value", func(t *testing.T) {
		got, err := collect(bytes.NewReader(pcm([]float64{0.5, -0.25, 1})), 1)

		require.NoError(t, err)
		assert.Equal(t, []float64{0.5, -0.25, 1}, got[0])
	})
}
