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
	"context"
	"fmt"
	"io"
	"math"
	"sort"
)

const (
	// gridRows is the number of frequency rows in a grid, from 0 Hz at the
	// bottom to the Nyquist frequency at the top. Every analysis window
	// holds a power-of-two number of bins, at least 2048, so the bins divide
	// evenly into 512 rows.
	gridRows = 512

	// gridMaxColumns is how many time columns a grid is allowed to keep
	// before neighboring pairs are merged. A grid therefore holds between
	// gridMaxColumns and twice that many columns, however long the audio is.
	// Half the picture's width, so that there is never more than one column
	// per pixel and none have to be skipped when drawing.
	gridMaxColumns = plotW / 2
)

// gridColumn is a stretch of the audio, condensed: the summed power of every
// non-silent frame in it, one value per frequency row.
type gridColumn struct {
	power   []float64 // summed (not averaged) power per row
	frames  int       // non-silent frames summed into power
	covered int       // frames the column spans, silent ones included
}

// grid is a spectrogram: the audio's power over time and frequency, in
// columns of time and rows of frequency. It exists only for drawing a picture
// and is built by a second pass over the audio, so the analysis itself never
// pays for it.
type grid struct {
	sampleRate int
	window     int
	cols       []gridColumn
}

// covered returns the total number of frames the grid spans.
func (g *grid) covered() int {
	var n int
	for _, c := range g.cols {
		n += c.covered
	}
	return n
}

// seconds returns the length of audio the grid covers.
func (g *grid) seconds() float64 {
	return float64(g.covered()) * float64(g.window/2) / float64(g.sampleRate)
}

// levelDB returns the mean level of row r in column c in dB relative to a
// full-scale sine (as in Curve), and false if the column holds no non-silent
// audio.
func (g *grid) levelDB(c, r int) (float64, bool) {
	col := g.cols[c]
	if col.frames == 0 {
		return 0, false
	}
	return 10 * math.Log10(col.power[r]/float64(col.frames)+powerFloor), true
}

// gridBuilder condenses a stream of frame spectra into columns without
// holding them all. Each column spans `span` frames; when the number of
// finished columns reaches twice maxCols, neighboring pairs are merged and
// span doubles. Memory is therefore bounded by maxCols however long the audio.
type gridBuilder struct {
	rows       int
	binsPerRow int
	maxCols    int
	span       int

	cur       []float64 // power summed into the column being built
	curFrames int       // non-silent frames in it
	curCover  int       // frames it spans so far

	cols []gridColumn
}

// newGridBuilder returns a builder for frames from a window of `window`
// samples, producing at most about 2*maxCols columns.
func newGridBuilder(window, maxCols int) *gridBuilder {
	return &gridBuilder{
		rows:       gridRows,
		binsPerRow: (window / 2) / gridRows,
		maxCols:    maxCols,
		span:       1,
		cur:        make([]float64, gridRows),
	}
}

// add takes one frame's power spectrum (window/2+1 bins), or nil for a frame
// that was skipped as silent.
func (b *gridBuilder) add(power []float64) {
	b.curCover++
	if power != nil {
		for r := range b.rows {
			var sum float64
			for _, p := range power[r*b.binsPerRow : (r+1)*b.binsPerRow] {
				sum += p
			}
			b.cur[r] += sum / float64(b.binsPerRow)
		}
		b.curFrames++
	}
	if b.curCover == b.span {
		b.flush()
	}
}

// flush finishes the column in progress, merging all columns if there are now
// too many.
func (b *gridBuilder) flush() {
	b.cols = append(b.cols, gridColumn{
		power:   append([]float64(nil), b.cur...),
		frames:  b.curFrames,
		covered: b.curCover,
	})
	clear(b.cur)
	b.curFrames, b.curCover = 0, 0

	if len(b.cols) < 2*b.maxCols {
		return
	}

	merged := make([]gridColumn, 0, b.maxCols)
	for i := 0; i+1 < len(b.cols); i += 2 {
		a, c := b.cols[i], b.cols[i+1]
		for r := range a.power {
			a.power[r] += c.power[r]
		}
		a.frames += c.frames
		a.covered += c.covered
		merged = append(merged, a)
	}
	b.cols = merged
	b.span *= 2
}

// finish returns the columns, including any partial column still in progress.
func (b *gridBuilder) finish() []gridColumn {
	if b.curCover > 0 {
		b.flush()
	}
	return b.cols
}

// buildGrid reads interleaved float PCM from r (see AnalyzeStream) and builds
// a spectrogram of one channel of it.
func buildGrid(r io.Reader, info StreamInfo, channel int, silenceDB float64) (*grid, error) {
	if info.SampleRate <= 0 || channel < 0 || channel >= info.Channels {
		return nil, fmt.Errorf("invalid stream or channel: %d Hz, %d channels, channel %d",
			info.SampleRate, info.Channels, channel)
	}

	window := WindowSize(info.SampleRate)
	b := newGridBuilder(window, gridMaxColumns)

	// Only the chosen channel is analyzed. The accumulator's own average is
	// unused; what is wanted is each frame as it goes by.
	w := newWelch(window, 1, silenceDB)
	w.hook = func(_ int, power []float64) { b.add(power) }

	err := readPCM(r, info.Channels, func(chans [][]float64) {
		w.feed(0, chans[channel])
	})
	if err != nil {
		return nil, err
	}

	return &grid{
		sampleRate: info.SampleRate,
		window:     window,
		cols:       b.finish(),
	}, nil
}

// levelRange returns the dB levels to map onto the color scale: the top is
// the 99.9th percentile of all non-silent levels in the grid (so one stray
// spike does not squash everything else), and the bottom is dynamicRange dB
// below it.
func (g *grid) levelRange(dynamicRange float64) (lo, hi float64) {
	var levels []float64
	for c := range g.cols {
		for r := range gridRows {
			if v, ok := g.levelDB(c, r); ok {
				levels = append(levels, v)
			}
		}
	}
	if len(levels) == 0 {
		return -dynamicRange, 0
	}
	sort.Float64s(levels)
	hi = levels[int(0.999*float64(len(levels)-1))]
	return hi - dynamicRange, hi
}

// openGrid decodes path through src and builds a grid of one channel.
func openGrid(ctx context.Context, src source, path string, info StreamInfo, channel int, silenceDB float64) (*grid, error) {
	rc, err := src.Open(ctx, path)
	if err != nil {
		return nil, err
	}

	g, err := buildGrid(rc, info, channel, silenceDB)
	cerr := rc.Close()
	if err != nil {
		return nil, err
	}
	if cerr != nil {
		return nil, cerr
	}
	return g, nil
}
