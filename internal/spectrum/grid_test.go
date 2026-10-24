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
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flatFrame returns a frame spectrum (window/2+1 bins) with every bin at p.
func flatFrame(window int, p float64) []float64 {
	f := make([]float64, window/2+1)
	for i := range f {
		f[i] = p
	}
	return f
}

func TestGridBuilder(t *testing.T) {
	const window = 4096

	t.Run("each frame is its own column until the limit", func(t *testing.T) {
		b := newGridBuilder(window, 100)
		for range 50 {
			b.add(flatFrame(window, 1))
		}

		cols := b.finish()

		assert.Len(t, cols, 50)
		for _, c := range cols {
			assert.Equal(t, 1, c.covered)
			assert.Equal(t, 1, c.frames)
		}
	})

	t.Run("memory stays bounded however long the audio", func(t *testing.T) {
		b := newGridBuilder(window, 10)
		const frames = 10000
		for range frames {
			b.add(flatFrame(window, 1))
			assert.LessOrEqual(t, len(b.cols), 2*10)
		}

		cols := b.finish()

		assert.GreaterOrEqual(t, len(cols), 10)
		assert.LessOrEqual(t, len(cols), 2*10)
		var covered int
		for _, c := range cols {
			covered += c.covered
		}
		assert.Equal(t, frames, covered, "no frame is lost in merging")
	})

	t.Run("merging preserves the mean level", func(t *testing.T) {
		b := newGridBuilder(window, 4)
		for range 1000 {
			b.add(flatFrame(window, 2.5))
		}

		for _, c := range b.finish() {
			for _, p := range c.power {
				assert.InDelta(t, 2.5, p/float64(c.frames), 1e-9)
			}
		}
	})

	t.Run("merging keeps different moments apart", func(t *testing.T) {
		b := newGridBuilder(window, 4)
		// First half quiet, second half loud.
		for range 500 {
			b.add(flatFrame(window, 1))
		}
		for range 500 {
			b.add(flatFrame(window, 100))
		}

		cols := b.finish()

		first, last := cols[0], cols[len(cols)-1]
		assert.InDelta(t, 1, first.power[0]/float64(first.frames), 1e-9)
		assert.InDelta(t, 100, last.power[0]/float64(last.frames), 1e-9)
	})

	t.Run("silent frames count toward time but not level", func(t *testing.T) {
		b := newGridBuilder(window, 100)
		b.add(flatFrame(window, 4))
		b.add(nil)

		cols := b.finish()

		require.Len(t, cols, 2)
		assert.Equal(t, 1, cols[1].covered)
		assert.Equal(t, 0, cols[1].frames)

		g := &grid{sampleRate: 44100, window: window, cols: cols}
		_, ok := g.levelDB(1, 0)
		assert.False(t, ok, "a silent column has no level")
		lvl, ok := g.levelDB(0, 0)
		assert.True(t, ok)
		assert.InDelta(t, 10*math.Log10(4), lvl, 1e-6)
	})

	t.Run("a merged column of silence and sound averages only the sound", func(t *testing.T) {
		b := newGridBuilder(window, 1)
		b.add(flatFrame(window, 6))
		b.add(nil) // merged with the first: span 2, one non-silent frame

		cols := b.finish()

		require.Len(t, cols, 1)
		assert.Equal(t, 2, cols[0].covered)
		assert.Equal(t, 1, cols[0].frames)
		assert.InDelta(t, 6, cols[0].power[0]/float64(cols[0].frames), 1e-9)
	})

	t.Run("a partial final column is kept", func(t *testing.T) {
		b := newGridBuilder(window, 2)
		for range 11 {
			b.add(flatFrame(window, 1))
		}

		cols := b.finish()

		var covered int
		for _, c := range cols {
			covered += c.covered
		}
		assert.Equal(t, 11, covered)
	})

	t.Run("bins are averaged into rows", func(t *testing.T) {
		b := newGridBuilder(window, 10)
		frame := make([]float64, window/2+1)
		// 2048 usable bins into 512 rows: 4 bins per row. Make row 3's bins
		// 4, 8, 12, 16 (mean 10) and everything else zero.
		for i, v := range []float64{4, 8, 12, 16} {
			frame[3*4+i] = v
		}
		b.add(frame)

		cols := b.finish()

		assert.InDelta(t, 10, cols[0].power[3], 1e-9)
		assert.Equal(t, 0.0, cols[0].power[2])
		assert.Equal(t, 0.0, cols[0].power[4])
	})
}

func TestBuildGrid(t *testing.T) {
	info := stereo44(16)

	t.Run("a lowpassed signal is dark above its cutoff", func(t *testing.T) {
		l := quantize(musicSignal(44100, synthSamples, 1, 16000), 16)
		r := quantize(musicSignal(44100, synthSamples, 2, 16000), 16)

		g, err := buildGrid(bytes.NewReader(pcm(l, r)), info, 0, -75)

		require.NoError(t, err)
		require.NotEmpty(t, g.cols)
		below, above := meanLevel(g, rowOf(8000, 44100)), meanLevel(g, rowOf(19000, 44100))
		assert.Greater(t, below-above, 40.0, "content below the cutoff, almost nothing above")
	})

	t.Run("the duration is right", func(t *testing.T) {
		l := quantize(musicSignal(44100, synthSamples, 1, 0), 16)
		r := quantize(musicSignal(44100, synthSamples, 2, 0), 16)

		g, err := buildGrid(bytes.NewReader(pcm(l, r)), info, 1, -75)

		require.NoError(t, err)
		want := float64(synthSamples) / 44100
		assert.InDelta(t, want, g.seconds(), 0.2)
	})

	t.Run("the chosen channel is the one drawn", func(t *testing.T) {
		l := quantize(musicSignal(44100, synthSamples, 1, 16000), 16)
		r := quantize(musicSignal(44100, synthSamples, 2, 0), 16)
		data := pcm(l, r)

		gl, err := buildGrid(bytes.NewReader(data), info, 0, -75)
		require.NoError(t, err)
		gr, err := buildGrid(bytes.NewReader(data), info, 1, -75)
		require.NoError(t, err)

		row := rowOf(19000, 44100)
		assert.Less(t, meanLevel(gl, row), meanLevel(gr, row)-40)
	})

	t.Run("memory is bounded for a long stream", func(t *testing.T) {
		l := quantize(musicSignal(44100, synthSamples, 1, 0), 16)
		r := quantize(musicSignal(44100, synthSamples, 2, 0), 16)
		data := pcm(l, r)
		// Repeat the clip to make the stream many times longer than the
		// column limit would allow one column per frame for.
		long := bytes.Repeat(data, 3)

		g, err := buildGrid(bytes.NewReader(long), info, 0, -75)

		require.NoError(t, err)
		assert.LessOrEqual(t, len(g.cols), 2*gridMaxColumns)
	})

	t.Run("bad arguments", func(t *testing.T) {
		_, err := buildGrid(bytes.NewReader(nil), info, 2, -75)
		assert.ErrorContains(t, err, "invalid")
		_, err = buildGrid(bytes.NewReader(nil), info, -1, -75)
		assert.Error(t, err)
		_, err = buildGrid(bytes.NewReader(nil), StreamInfo{Channels: 2}, 0, -75)
		assert.Error(t, err)
	})

	t.Run("an empty stream gives an empty grid", func(t *testing.T) {
		g, err := buildGrid(bytes.NewReader(nil), info, 0, -75)
		require.NoError(t, err)
		assert.Empty(t, g.cols)
		assert.Equal(t, 0.0, g.seconds())
	})
}

func TestLevelRange(t *testing.T) {
	const window = 4096

	t.Run("the top is a high percentile and the bottom is below it", func(t *testing.T) {
		b := newGridBuilder(window, 10)
		for range 20 {
			b.add(flatFrame(window, 1e-6)) // -60 dB everywhere
		}
		g := &grid{sampleRate: 44100, window: window, cols: b.finish()}

		lo, hi := g.levelRange(110)

		assert.InDelta(t, -60, hi, 1e-6)
		assert.InDelta(t, -170, lo, 1e-6)
	})

	t.Run("one stray spike does not set the scale", func(t *testing.T) {
		b := newGridBuilder(window, 2000)
		for range 2000 {
			b.add(flatFrame(window, 1e-6))
		}
		spike := flatFrame(window, 1e-6)
		for k := range 4 {
			spike[k] = 1 // a single row at 0 dB
		}
		b.add(spike)
		g := &grid{sampleRate: 44100, window: window, cols: b.finish()}

		_, hi := g.levelRange(110)

		assert.Less(t, hi, -50.0)
	})

	t.Run("an all-silent grid has a default range", func(t *testing.T) {
		b := newGridBuilder(window, 10)
		b.add(nil)
		g := &grid{sampleRate: 44100, window: window, cols: b.finish()}

		lo, hi := g.levelRange(110)

		assert.Equal(t, 0.0, hi)
		assert.Equal(t, -110.0, lo)
	})
}

// rowOf returns the grid row containing frequency hz for a given sample rate.
func rowOf(hz float64, sampleRate int) int {
	return int(hz / (float64(sampleRate) / 2) * gridRows)
}

// meanLevel returns the mean dB level of a row over all non-silent columns.
func meanLevel(g *grid, row int) float64 {
	var sum float64
	var n int
	for c := range g.cols {
		if v, ok := g.levelDB(c, row); ok {
			sum += v
			n++
		}
	}
	return sum / float64(n)
}
