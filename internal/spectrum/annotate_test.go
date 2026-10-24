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
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// luma is the perceived brightness of a color, 0 to 255.
func luma(c color.RGBA) float64 {
	return 0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)
}

func TestHeat(t *testing.T) {
	t.Run("runs from black to near-white", func(t *testing.T) {
		assert.Less(t, luma(heat(0)), 5.0)
		assert.Greater(t, luma(heat(1)), 220.0)
	})

	t.Run("louder is never darker", func(t *testing.T) {
		prev := -1.0
		for i := 0; i <= 200; i++ {
			l := luma(heat(float64(i) / 200))
			assert.GreaterOrEqual(t, l, prev, "at %d/200", i)
			prev = l
		}
	})

	t.Run("out-of-range values are clamped", func(t *testing.T) {
		assert.Equal(t, heat(0), heat(-3))
		assert.Equal(t, heat(1), heat(7))
	})

	t.Run("never produces the color reserved for the cutoff marker", func(t *testing.T) {
		for i := 0; i <= 1000; i++ {
			assert.NotEqual(t, colCutoff, heat(float64(i)/1000))
		}
	})
}

func TestGeometry(t *testing.T) {
	t.Run("frequency maps top to Nyquist and bottom to zero", func(t *testing.T) {
		assert.Equal(t, marginT, freqY(22050, 22050))
		assert.Equal(t, marginT+plotH, freqY(0, 22050))
		assert.Equal(t, marginT+plotH/2, freqY(11025, 22050))
	})

	t.Run("higher frequencies are higher up", func(t *testing.T) {
		assert.Less(t, freqY(16000, 22050), freqY(8000, 22050))
	})

	t.Run("time maps left to right", func(t *testing.T) {
		assert.Equal(t, marginL, timeX(0, 30))
		assert.Equal(t, marginL+plotW, timeX(30, 30))
		assert.Equal(t, marginL+plotW/2, timeX(15, 30))
		assert.Equal(t, marginL, timeX(5, 0), "no duration gives the left edge")
	})
}

func TestNiceStep(t *testing.T) {
	t.Run("frequency ticks", func(t *testing.T) {
		assert.Equal(t, 2000.0, niceStep(freqSteps, 22050))
		assert.Equal(t, 2000.0, niceStep(freqSteps, 24000))
		assert.Equal(t, 5000.0, niceStep(freqSteps, 48000))
		assert.Equal(t, 10000.0, niceStep(freqSteps, 96000))
	})

	t.Run("time ticks", func(t *testing.T) {
		assert.Equal(t, 5.0, niceStep(timeSteps, 30))
		assert.Equal(t, 30.0, niceStep(timeSteps, 210))
		assert.Equal(t, 300.0, niceStep(timeSteps, 3000))
	})

	t.Run("never more than the maximum number of ticks, when a step allows it", func(t *testing.T) {
		for _, span := range []float64{3, 30, 90, 240, 900, 2400} {
			assert.LessOrEqual(t, span/niceStep(timeSteps, span), float64(maxTicks))
		}
	})

	t.Run("a huge span falls back to the largest step", func(t *testing.T) {
		assert.Equal(t, 3600.0, niceStep(timeSteps, 1e7))
	})
}

func TestFormatters(t *testing.T) {
	assert.Equal(t, "0", formatKHz(0))
	assert.Equal(t, "2", formatKHz(2000))
	assert.Equal(t, "2.5", formatKHz(2500))
	assert.Equal(t, "22", formatKHz(22000))

	assert.Equal(t, "0:00", formatClock(0))
	assert.Equal(t, "0:30", formatClock(30))
	assert.Equal(t, "1:15", formatClock(75))
	assert.Equal(t, "10:00", formatClock(600))
	assert.Equal(t, "1:02:05", formatClock(3725))
}

func TestAnnotatedChannel(t *testing.T) {
	assert.Equal(t, 2, annotatedChannel(&Result{
		Best:     1,
		Channels: []ChannelResult{{Index: 0}, {Index: 2}},
	}), "the channel's own index, not its position")
	assert.Equal(t, 0, annotatedChannel(&Result{Best: -1}))
}

func TestVerdictColor(t *testing.T) {
	assert.Equal(t, colOK, verdictColor(VerdictOK))
	assert.Equal(t, colSuspect, verdictColor(VerdictSuspect))
	assert.Equal(t, colFake, verdictColor(VerdictLikelyFake))
	assert.Equal(t, colMuted, verdictColor(VerdictInconclusive))
}

// lowpassGrid returns a grid of `frames` identical frames whose spectrum is
// -60 dB below cutHz and -130 dB above it.
func lowpassGrid(sampleRate int, cutHz float64, frames int) *grid {
	window := WindowSize(sampleRate)
	frame := make([]float64, window/2+1)
	binHz := float64(sampleRate) / float64(window)
	for k := range frame {
		if float64(k)*binHz < cutHz {
			frame[k] = 1e-6
		} else {
			frame[k] = 1e-13
		}
	}

	b := newGridBuilder(window, gridMaxColumns)
	for range frames {
		b.add(frame)
	}
	return &grid{sampleRate: sampleRate, window: window, cols: b.finish()}
}

func resultFor(sampleRate int, cliff *Cliff, v Verdict) *Result {
	return &Result{
		Info: StreamInfo{
			Codec: "flac", SampleRate: sampleRate, Channels: 2,
			ChannelLayout: "stereo", BitsPerSample: 16,
		},
		Window:   WindowSize(sampleRate),
		Best:     0,
		Channels: []ChannelResult{{Index: 0}},
		Cliff:    cliff,
		Verdict:  v,
	}
}

// count returns the number of pixels in r with color col.
func count(img *image.RGBA, r image.Rectangle, col color.RGBA) int {
	var n int
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if img.RGBAAt(x, y) == col {
				n++
			}
		}
	}
	return n
}

var plotRect = image.Rect(marginL, marginT, marginL+plotW, marginT+plotH)

func TestRenderAnnotated(t *testing.T) {
	const sr = 44100
	nyq := float64(sr) / 2
	g := lowpassGrid(sr, 16000, 400)
	cliff := &Cliff{CutoffHz: 16100, DropDB: 43}
	res := resultFor(sr, cliff, VerdictLikelyFake)

	img := renderAnnotated(g, res, 0, "track.flac", AnnotateOptions{})

	t.Run("has the expected size and background", func(t *testing.T) {
		assert.Equal(t, image.Rect(0, 0, imageW, imageH), img.Bounds())
		assert.Equal(t, colBackground, img.RGBAAt(0, 0))
		assert.Equal(t, colBackground, img.RGBAAt(imageW-1, imageH-1))
	})

	t.Run("the cutoff line is drawn across the plot at the right height", func(t *testing.T) {
		y := freqY(16100, nyq)
		for _, x := range []int{marginL, marginL + plotW/2, marginL + plotW - 1} {
			assert.Equal(t, colCutoff, img.RGBAAt(x, y), "x=%d", x)
			assert.Equal(t, colCutoff, img.RGBAAt(x, y+1), "x=%d", x)
		}
		assert.Greater(t, count(img, plotRect, colCutoff), 2*plotW-1)
	})

	t.Run("the cutoff label is drawn", func(t *testing.T) {
		// Label text is cyan too, so well above the two line rows there
		// are cyan pixels near the line's left end.
		y := freqY(16100, nyq)
		above := image.Rect(marginL, y-14, marginL+200, y-2)
		assert.Greater(t, count(img, above, colCutoff), 20)
	})

	t.Run("the spectrum is loud below the cutoff and quiet above it", func(t *testing.T) {
		below := img.RGBAAt(marginL+plotW/2, freqY(5000, nyq))
		above := img.RGBAAt(marginL+plotW/2, freqY(19000, nyq))

		assert.Greater(t, luma(below), luma(above)+60)
	})

	t.Run("the color bar runs from loud at the top to quiet at the bottom", func(t *testing.T) {
		x := marginL + plotW + 18 + 5
		assert.Equal(t, heat(1), img.RGBAAt(x, marginT))
		assert.Equal(t, heat(0), img.RGBAAt(x, marginT+plotH-1))
	})

	t.Run("the header, axes and footer are drawn", func(t *testing.T) {
		header := image.Rect(0, 0, imageW, marginT-2)
		footer := image.Rect(0, marginT+plotH+30, imageW, imageH)
		assert.Greater(t, countNot(img, header, colBackground), 150)
		assert.Greater(t, countNot(img, footer, colBackground), 150)
		axis := image.Rect(0, marginT, marginL-1, marginT+plotH+4)
		assert.Greater(t, countNot(img, axis, colBackground), 100)
	})

	t.Run("the verdict is drawn in its color", func(t *testing.T) {
		header := image.Rect(0, 30, imageW, 42)
		assert.Greater(t, count(img, header, colFake), 10)
	})

	t.Run("CD bandwidth is not marked on a CD-rate file", func(t *testing.T) {
		assert.Equal(t, 0, count(img, img.Bounds(), colCDBand))
	})
}

func countNot(img *image.RGBA, r image.Rectangle, col color.RGBA) int {
	return r.Dx()*r.Dy() - count(img, r, col)
}

func TestRenderAnnotatedWithoutACliff(t *testing.T) {
	g := lowpassGrid(44100, 22050, 400)
	res := resultFor(44100, nil, VerdictOK)

	img := renderAnnotated(g, res, 0, "good.flac", AnnotateOptions{})

	assert.Equal(t, 0, count(img, img.Bounds(), colCutoff), "no cutoff, so no marker")
	assert.Greater(t, count(img, image.Rect(0, 30, imageW, 42), colOK), 5)
}

func TestRenderAnnotatedHiRes(t *testing.T) {
	const sr = 96000
	nyq := float64(sr) / 2
	g := lowpassGrid(sr, 25800, 200)
	res := resultFor(sr, &Cliff{CutoffHz: 25800, DropDB: 38}, VerdictLikelyFake)

	img := renderAnnotated(g, res, 0, "up.flac", AnnotateOptions{})

	t.Run("marks the top of CD bandwidth with a dashed line", func(t *testing.T) {
		y := freqY(22050, nyq)
		n := 0
		for x := marginL; x < marginL+plotW; x++ {
			if img.RGBAAt(x, y) == colCDBand {
				n++
			}
		}
		// Six on, four off: about 60% of the row.
		assert.Greater(t, n, plotW/2)
		assert.Less(t, n, plotW*3/4+60)
	})

	t.Run("marks the cutoff above it", func(t *testing.T) {
		assert.Equal(t, colCutoff, img.RGBAAt(marginL+plotW/2, freqY(25800, nyq)))
		assert.Less(t, freqY(25800, nyq), freqY(22050, nyq))
	})
}

func TestRenderAnnotatedEdgeCases(t *testing.T) {
	t.Run("an empty grid draws a blank plot without failing", func(t *testing.T) {
		g := &grid{sampleRate: 44100, window: 4096}
		res := resultFor(44100, nil, VerdictInconclusive)

		img := renderAnnotated(g, res, 0, "empty.flac", AnnotateOptions{})

		assert.Equal(t, image.Rect(0, 0, imageW, imageH), img.Bounds())
		assert.Greater(t, countNot(img, image.Rect(0, 0, imageW, marginT-2), colBackground), 100)
	})

	t.Run("a cutoff right at the top still gets a legible label", func(t *testing.T) {
		g := lowpassGrid(44100, 22000, 100)
		res := resultFor(44100, &Cliff{CutoffHz: 21990, DropDB: 40}, VerdictSuspect)

		img := renderAnnotated(g, res, 0, "top.flac", AnnotateOptions{})

		y := freqY(21990, 22050)
		assert.Equal(t, colCutoff, img.RGBAAt(marginL+plotW/2, y))
		// The label goes below the line when there is no room above it.
		below := image.Rect(marginL, y+3, marginL+220, y+20)
		assert.Greater(t, count(img, below, colCutoff), 20)
	})

	t.Run("silent stretches are drawn as the quietest color", func(t *testing.T) {
		window := WindowSize(44100)
		b := newGridBuilder(window, gridMaxColumns)
		frame := make([]float64, window/2+1)
		for k := range frame {
			frame[k] = 1e-6
		}
		for range 50 {
			b.add(frame)
		}
		for range 50 {
			b.add(nil)
		}
		g := &grid{sampleRate: 44100, window: window, cols: b.finish()}
		res := resultFor(44100, nil, VerdictOK)

		img := renderAnnotated(g, res, 0, "gap.flac", AnnotateOptions{})

		y := freqY(5000, 22050)
		assert.Greater(t, luma(img.RGBAAt(marginL+plotW/4, y)), 100.0)
		assert.Equal(t, heat(0), img.RGBAAt(marginL+plotW*3/4, y))
	})
}

// decodePNG reads the PNG at path into an RGBA image.
func decodePNG(t *testing.T, path string) *image.RGBA {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	m, err := png.Decode(f)
	require.NoError(t, err)

	rgba := image.NewRGBA(m.Bounds())
	for y := m.Bounds().Min.Y; y < m.Bounds().Max.Y; y++ {
		for x := m.Bounds().Min.X; x < m.Bounds().Max.X; x++ {
			r, g, b, a := m.At(x, y).RGBA()
			rgba.SetRGBA(x, y, color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)})
		}
	}
	return rgba
}

type failSource struct{ err error }

func (f failSource) Open(context.Context, string) (io.ReadCloser, error) { return nil, f.err }

func TestWriteAnnotated(t *testing.T) {
	ctx := context.Background()
	th := DefaultThresholds()

	l := quantize(musicSignal(44100, synthSamples, 1, 16000), 16)
	r := quantize(musicSignal(44100, synthSamples, 2, 16000), 16)
	data := pcm(l, r)
	info := stereo44(16)

	res, err := AnalyzeStream(bytes.NewReader(data), info, th)
	require.NoError(t, err)
	require.NotNil(t, res.Cliff)

	t.Run("writes a PNG with the cutoff marked", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "out.png")

		err := writeAnnotated(ctx, &fakeSource{data: data}, "/music/01 track.flac", dst, res, th, AnnotateOptions{})

		require.NoError(t, err)
		img := decodePNG(t, dst)
		assert.Equal(t, image.Rect(0, 0, imageW, imageH), img.Bounds())
		y := freqY(res.Cliff.CutoffHz, 22050)
		assert.Equal(t, colCutoff, img.RGBAAt(marginL+plotW/2, y))
	})

	t.Run("closes the source", func(t *testing.T) {
		src := &fakeSource{data: data}
		require.NoError(t, writeAnnotated(ctx, src, "x.flac", filepath.Join(t.TempDir(), "o.png"), res, th, AnnotateOptions{}))
		assert.True(t, src.closed)
	})

	t.Run("a source that cannot be opened is an error and writes nothing", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "out.png")

		err := writeAnnotated(ctx, failSource{errors.New("boom")}, "x.flac", dst, res, th, AnnotateOptions{})

		assert.ErrorContains(t, err, "boom")
		_, statErr := os.Stat(dst)
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("a decode failure reported on close is an error and writes nothing", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "out.png")
		src := &fakeSource{data: data, closeErr: errors.New("ffmpeg: corrupt")}

		err := writeAnnotated(ctx, src, "x.flac", dst, res, th, AnnotateOptions{})

		assert.ErrorContains(t, err, "corrupt")
		_, statErr := os.Stat(dst)
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("a destination that cannot be created is an error", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "no-such-dir", "out.png")

		err := writeAnnotated(ctx, &fakeSource{data: data}, "x.flac", dst, res, th, AnnotateOptions{})

		assert.Error(t, err)
	})
}

// flatGrid returns a grid of identical frames in which every bin has power p.
func flatGrid(sampleRate int, p float64, frames int) *grid {
	window := WindowSize(sampleRate)
	b := newGridBuilder(window, gridMaxColumns)
	for range frames {
		b.add(flatFrame(window, p))
	}
	return &grid{sampleRate: sampleRate, window: window, cols: b.finish()}
}

func TestRenderAnnotatedFixedScale(t *testing.T) {
	const sr = 44100
	res := resultFor(sr, nil, VerdictOK)

	loud := flatGrid(sr, 1e-6, 100)  // -60 dB in every bin
	quiet := flatGrid(sr, 1e-9, 100) // -90 dB in every bin

	// A point in the middle of the plot, clear of the axes and markers.
	probe := func(img *image.RGBA) color.RGBA {
		return img.RGBAAt(marginL+plotW/2, freqY(8000, sr/2))
	}
	span := fixedScaleTopDB - fixedScaleBottomDB

	t.Run("the fixed range is sensible", func(t *testing.T) {
		assert.Greater(t, fixedScaleTopDB, fixedScaleBottomDB)
		assert.Equal(t, 120.0, span)
		// A 16-bit file's rounding-noise floor (about -129 dB) is inside
		// the scale, just above its bottom, rather than off the end of it.
		assert.Greater(t, -129.0, fixedScaleBottomDB)
		assert.Less(t, -129.0, fixedScaleBottomDB+0.15*span)
	})

	t.Run("by default each picture is stretched to its own range", func(t *testing.T) {
		a := renderAnnotated(loud, res, 0, "a.flac", AnnotateOptions{})
		b := renderAnnotated(quiet, res, 0, "b.flac", AnnotateOptions{})

		// Loud and quiet look identical: each is as loud as it gets.
		assert.Equal(t, probe(a), probe(b))
		assert.Equal(t, heat(1), probe(a))
	})

	t.Run("with a fixed scale the same level is always the same color", func(t *testing.T) {
		opts := AnnotateOptions{FixedScale: true}
		a := renderAnnotated(loud, res, 0, "a.flac", opts)
		b := renderAnnotated(quiet, res, 0, "b.flac", opts)

		assert.Equal(t, heat((-60-fixedScaleBottomDB)/span), probe(a))
		assert.Equal(t, heat((-90-fixedScaleBottomDB)/span), probe(b))
		assert.Greater(t, luma(probe(a)), luma(probe(b))+20, "the quieter file is darker")
	})

	t.Run("a fixed scale gives the same color for the same file however the rest sounds", func(t *testing.T) {
		// The point of the option: what a pixel means does not depend on
		// anything else in the picture. Add a very loud passage to one
		// file and a given quiet pixel must not change color.
		opts := AnnotateOptions{FixedScale: true}
		window := WindowSize(sr)
		b := newGridBuilder(window, gridMaxColumns)
		for range 50 {
			b.add(flatFrame(window, 1e-9))
		}
		for range 50 {
			b.add(flatFrame(window, 1e-2)) // -20 dB: the top of the scale
		}
		mixed := &grid{sampleRate: sr, window: window, cols: b.finish()}

		img := renderAnnotated(mixed, res, 0, "m.flac", opts)

		quietSide := img.RGBAAt(marginL+plotW/4, freqY(8000, sr/2))
		assert.Equal(t, heat((-90-fixedScaleBottomDB)/span), quietSide)
	})

	t.Run("levels beyond either end saturate", func(t *testing.T) {
		opts := AnnotateOptions{FixedScale: true}
		hot := renderAnnotated(flatGrid(sr, 1, 100), res, 0, "h.flac", opts)      // 0 dB
		cold := renderAnnotated(flatGrid(sr, 1e-18, 100), res, 0, "c.flac", opts) // -180 dB

		assert.Equal(t, heat(1), probe(hot))
		assert.Equal(t, heat(0), probe(cold))
	})

	t.Run("the color bar still runs loud to quiet", func(t *testing.T) {
		img := renderAnnotated(loud, res, 0, "a.flac", AnnotateOptions{FixedScale: true})

		x := marginL + plotW + 18 + 5
		assert.Equal(t, heat(1), img.RGBAAt(x, marginT))
		assert.Equal(t, heat(0), img.RGBAAt(x, marginT+plotH-1))
	})

	t.Run("the footer says which scale was used", func(t *testing.T) {
		rel := renderAnnotated(loud, res, 0, "a.flac", AnnotateOptions{})
		fix := renderAnnotated(loud, res, 0, "a.flac", AnnotateOptions{FixedScale: true})

		footer := image.Rect(marginL, imageH-26, imageW, imageH)
		differs := false
		for y := footer.Min.Y; y < footer.Max.Y && !differs; y++ {
			for x := footer.Min.X; x < footer.Max.X; x++ {
				if rel.RGBAAt(x, y) != fix.RGBAAt(x, y) {
					differs = true
					break
				}
			}
		}
		assert.True(t, differs)
	})

	t.Run("the option reaches the written file", func(t *testing.T) {
		l := quantize(musicSignal(44100, synthSamples, 1, 0), 16)
		r := quantize(musicSignal(44100, synthSamples, 2, 0), 16)
		data := pcm(l, r)
		th := DefaultThresholds()
		resp, err := AnalyzeStream(bytes.NewReader(data), stereo44(16), th)
		require.NoError(t, err)

		dir := t.TempDir()
		plain, fixed := filepath.Join(dir, "plain.png"), filepath.Join(dir, "fixed.png")
		require.NoError(t, writeAnnotated(context.Background(), &fakeSource{data: data}, "x.flac", plain, resp, th, AnnotateOptions{}))
		require.NoError(t, writeAnnotated(context.Background(), &fakeSource{data: data}, "x.flac", fixed, resp, th, AnnotateOptions{FixedScale: true}))

		// Same audio, different scale: the two files cannot be identical.
		a, errA := os.ReadFile(plain)
		b, errB := os.ReadFile(fixed)
		require.NoError(t, errA)
		require.NoError(t, errB)
		assert.NotEqual(t, a, b)
	})
}
