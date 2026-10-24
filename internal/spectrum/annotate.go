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
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// Layout of an annotated spectrogram, in pixels. The plot area is one pixel
// per grid row vertically and at most one grid column per pixel horizontally.
const (
	plotW   = 1000
	plotH   = gridRows
	marginL = 76
	marginR = 100
	marginT = 52
	marginB = 56

	imageW = marginL + plotW + marginR
	imageH = marginT + plotH + marginB

	// dynamicRangeDB is how many dB below the loudest part of the audio the
	// color scale reaches, when the scale is set from the audio itself.
	dynamicRangeDB = 110.0

	// fixedScaleTopDB and fixedScaleBottomDB are the levels at the two ends
	// of the color scale when it is fixed (see AnnotateOptions). The top sits
	// 20 dB under a full-scale sine, which real music's strongest bins rarely
	// exceed; the bottom is 11 dB below the rounding-noise floor of a 16-bit
	// file (about -129 dB), so that floor shows as a faint dark blue, clearly
	// distinct from the pure black of anything quieter, such as a 24-bit
	// file's much lower floor.
	fixedScaleTopDB    = -20.0
	fixedScaleBottomDB = -140.0
)

// AnnotateOptions adjusts how an annotated spectrogram is drawn. The zero value
// gives the default picture.
type AnnotateOptions struct {
	// FixedScale draws with a color scale that is the same for every file
	// (fixedScaleBottomDB to fixedScaleTopDB) instead of one set from the
	// file's loudness. By default each picture is stretched to use its
	// whole color range however loud or quiet the audio, which shows each
	// file's shape clearly but makes pictures of different files look alike.
	// With a fixed scale the same color always means the same level, so a
	// quiet recording looks darker than a loud one and pictures can be
	// compared directly.
	//
	// Levels are per FFT bin, and the window grows with the sample rate (see
	// WindowSize), which shifts the level of noise-like sound by about 3 dB
	// per doubling. Pictures of files at the same sample rate are therefore
	// exactly comparable; across sample rates, only roughly.
	FixedScale bool
}

var (
	colBackground = color.RGBA{16, 16, 16, 255}
	colText       = color.RGBA{230, 230, 230, 255}
	colMuted      = color.RGBA{150, 150, 150, 255}
	colAxis       = color.RGBA{120, 120, 120, 255}
	colCutoff     = color.RGBA{0, 255, 255, 255}
	colCDBand     = color.RGBA{200, 200, 200, 255}
	colOK         = color.RGBA{80, 220, 120, 255}
	colSuspect    = color.RGBA{255, 210, 60, 255}
	colFake       = color.RGBA{255, 90, 90, 255}
)

// heatStops are the colors of the spectrogram, from the quietest to the
// loudest, interpolated linearly between. The progression runs from black
// through purple and red to yellow-white, and is deliberately free of cyan,
// which is reserved for the cutoff marker.
var heatStops = []color.RGBA{
	{0, 0, 4, 255},
	{40, 11, 84, 255},
	{101, 21, 110, 255},
	{159, 42, 99, 255},
	{212, 72, 66, 255},
	{245, 125, 21, 255},
	{250, 193, 39, 255},
	{252, 255, 164, 255},
}

// heat maps t in [0, 1] (quiet to loud) to a color.
func heat(t float64) color.RGBA {
	t = math.Max(0, math.Min(1, t))
	pos := t * float64(len(heatStops)-1)
	i := int(pos)
	if i >= len(heatStops)-1 {
		return heatStops[len(heatStops)-1]
	}
	f := pos - float64(i)
	a, b := heatStops[i], heatStops[i+1]
	mix := func(x, y uint8) uint8 { return uint8(math.Round(float64(x) + (float64(y)-float64(x))*f)) }
	return color.RGBA{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B), 255}
}

// freqY returns the pixel row of frequency hz on a plot whose top is the
// Nyquist frequency nyq and whose bottom is 0 Hz.
func freqY(hz, nyq float64) int {
	return marginT + int(math.Round((1-hz/nyq)*float64(plotH)))
}

// timeX returns the pixel column of time sec on a plot spanning total seconds.
func timeX(sec, total float64) int {
	if total <= 0 {
		return marginL
	}
	return marginL + int(math.Round(sec/total*float64(plotW)))
}

var (
	freqSteps = []float64{500, 1000, 2000, 2500, 5000, 10000, 20000, 50000}
	timeSteps = []float64{1, 2, 5, 10, 15, 30, 60, 120, 300, 600, 1800, 3600}
)

// maxTicks is the most labelled ticks drawn along an axis.
const maxTicks = 12

// niceStep returns the smallest of steps that gives no more than maxTicks
// ticks across span.
func niceStep(steps []float64, span float64) float64 {
	for _, s := range steps {
		if span/s <= maxTicks {
			return s
		}
	}
	return steps[len(steps)-1]
}

// formatKHz formats a frequency in Hz as kilohertz without trailing zeros.
func formatKHz(hz float64) string {
	return strconv.FormatFloat(hz/1000, 'f', -1, 64)
}

// formatClock formats seconds as m:ss, or h:mm:ss from an hour up.
func formatClock(sec float64) string {
	s := int(math.Round(sec))
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s%3600/60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// canvas wraps an image with the few drawing operations the picture needs.
type canvas struct {
	img *image.RGBA
}

func (c canvas) set(x, y int, col color.RGBA) {
	if image.Pt(x, y).In(c.img.Rect) {
		c.img.SetRGBA(x, y, col)
	}
}

func (c canvas) hline(x0, x1, y int, col color.RGBA) {
	for x := x0; x <= x1; x++ {
		c.set(x, y, col)
	}
}

func (c canvas) vline(x, y0, y1 int, col color.RGBA) {
	for y := y0; y <= y1; y++ {
		c.set(x, y, col)
	}
}

// dashed draws a horizontal dashed line: dash pixels on, gap pixels off.
func (c canvas) dashed(x0, x1, y, dash, gap int, col color.RGBA) {
	for x := x0; x <= x1; x++ {
		if (x-x0)%(dash+gap) < dash {
			c.set(x, y, col)
		}
	}
}

func (c canvas) fill(r image.Rectangle, col color.RGBA) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c.set(x, y, col)
		}
	}
}

// face is the font every label is drawn in: a small fixed-width bitmap font.
// Its glyphs cover plain ASCII only, so every label sticks to it.
var face = basicfont.Face7x13

func textWidth(s string) int {
	return font.MeasureString(face, s).Ceil()
}

// text draws s with its left edge at x and its baseline at y.
func (c canvas) text(x, y int, s string, col color.RGBA) {
	d := &font.Drawer{
		Dst:  c.img,
		Src:  image.NewUniform(col),
		Face: face,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(s)
}

// textRight draws s with its right edge at x.
func (c canvas) textRight(x, y int, s string, col color.RGBA) {
	c.text(x-textWidth(s), y, s, col)
}

// label draws s on an opaque background box so it stays legible over the
// spectrogram. The box sits just above the baseline y.
func (c canvas) label(x, y int, s string, col color.RGBA) {
	w := textWidth(s)
	c.fill(image.Rect(x-3, y-12, x+w+3, y+4), colBackground)
	c.text(x, y, s, col)
}

// annotatedChannel returns the channel an annotated spectrogram should show:
// the one the verdict was based on, or the first channel if there was none.
func annotatedChannel(res *Result) int {
	if res.Best >= 0 && res.Best < len(res.Channels) {
		return res.Channels[res.Best].Index
	}
	return 0
}

// verdictColor returns the color used for a verdict's name.
func verdictColor(v Verdict) color.RGBA {
	switch v {
	case VerdictOK:
		return colOK
	case VerdictSuspect:
		return colSuspect
	case VerdictLikelyFake:
		return colFake
	default:
		return colMuted
	}
}

// renderAnnotated draws g as a spectrogram, with axes, a color scale, and
// markers drawn from the analysis in res: the detected cutoff, and for hi-res
// files the edge of CD bandwidth.
func renderAnnotated(g *grid, res *Result, channel int, title string, opts AnnotateOptions) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, imageW, imageH))
	c := canvas{img}
	c.fill(img.Rect, colBackground)

	nyq := float64(g.sampleRate) / 2
	lo, hi := g.levelRange(dynamicRangeDB)
	if opts.FixedScale {
		lo, hi = fixedScaleBottomDB, fixedScaleTopDB
	}
	total := g.seconds()

	c.drawPlot(g, lo, hi)
	c.drawAxes(nyq, total)
	c.drawColorBar(lo, hi)
	c.drawHeader(res, channel, title)
	c.drawFooter(g, lo, hi, opts.FixedScale)
	c.drawMarkers(res, nyq)

	return img
}

// drawPlot fills the plot area with the spectrogram.
func (c canvas) drawPlot(g *grid, lo, hi float64) {
	// starts[i] is the first frame column i covers.
	starts := make([]int, len(g.cols))
	var n int
	for i, col := range g.cols {
		starts[i] = n
		n += col.covered
	}
	if n == 0 {
		return
	}

	for x := range plotW {
		frame := int((float64(x) + 0.5) / float64(plotW) * float64(n))
		ci := sort.Search(len(starts), func(i int) bool { return starts[i] > frame }) - 1
		if ci < 0 {
			ci = 0
		}

		for y := range plotH {
			level, ok := g.levelDB(ci, plotH-1-y)
			if !ok {
				c.set(marginL+x, marginT+y, heat(0))
				continue
			}
			c.set(marginL+x, marginT+y, heat((level-lo)/(hi-lo)))
		}
	}
}

// drawAxes draws the frame, the frequency ticks and labels on the left, and the
// time ticks and labels along the bottom.
func (c canvas) drawAxes(nyq, total float64) {
	right, bottom := marginL+plotW, marginT+plotH
	c.hline(marginL-1, right, marginT-1, colAxis)
	c.hline(marginL-1, right, bottom, colAxis)
	c.vline(marginL-1, marginT-1, bottom, colAxis)
	c.vline(right, marginT-1, bottom, colAxis)

	c.textRight(marginL-8, marginT-8, "kHz", colMuted)

	step := niceStep(freqSteps, nyq)
	for hz := 0.0; hz <= nyq+1e-9; hz += step {
		y := freqY(hz, nyq)
		c.hline(marginL-5, marginL-2, min(y, bottom), colAxis)
		c.textRight(marginL-8, min(y, bottom)+4, formatKHz(hz), colText)
	}

	if total <= 0 {
		return
	}
	tstep := niceStep(timeSteps, total)
	for t := 0.0; t <= total+1e-9; t += tstep {
		x := timeX(t, total)
		c.vline(min(x, right), bottom+1, bottom+4, colAxis)
		c.text(min(x, right)-textWidth(formatClock(t))/2, bottom+17, formatClock(t), colText)
	}
}

// drawColorBar draws the color scale to the right of the plot.
func (c canvas) drawColorBar(lo, hi float64) {
	x0 := marginL + plotW + 18
	for y := range plotH {
		t := 1 - float64(y)/float64(plotH-1)
		col := heat(t)
		for x := x0; x < x0+14; x++ {
			c.set(x, marginT+y, col)
		}
	}
	c.text(x0, marginT-8, "dB", colMuted)

	step := 20.0
	if hi-lo <= 60 {
		step = 10
	}
	// Ticks at multiples of step, counting down from the top.
	for v := math.Floor(hi/step) * step; v >= lo; v -= step {
		y := marginT + int(math.Round((hi-v)/(hi-lo)*float64(plotH-1)))
		c.hline(x0+14, x0+17, y, colAxis)
		c.text(x0+21, y+4, fmt.Sprintf("%.0f", v), colText)
	}
}

// drawHeader writes the file name, the stream, and the verdict above the plot.
func (c canvas) drawHeader(res *Result, channel int, title string) {
	c.text(marginL, 18, title, colText)

	info := strings.ReplaceAll(res.Info.Describe(), "·", "|") +
		fmt.Sprintf(" | channel %d | ", channel+1)
	c.text(marginL, 38, info, colMuted)
	c.text(marginL+textWidth(info), 38, res.Verdict.String(), verdictColor(res.Verdict))
}

// drawFooter explains what the picture shows, including which color scale it
// uses, so that a picture is never mistaken for one on the other scale.
func (c canvas) drawFooter(g *grid, lo, hi float64, fixed bool) {
	binHz := float64(g.sampleRate) / float64(g.window)
	scale := "color scale relative to this file"
	if fixed {
		scale = "fixed color scale"
	}
	c.text(marginL, imageH-12, fmt.Sprintf(
		"Hann window %d samples (%.1f Hz bins) | %s, %.0f to %.0f dB re full-scale sine, per bin",
		g.window, binHz, scale, lo, hi), colMuted)
}

// drawMarkers draws the analysis onto the picture: the detected cutoff and, for
// hi-res files, the top of CD bandwidth.
func (c canvas) drawMarkers(res *Result, nyq float64) {
	left, right := marginL, marginL+plotW-1

	// A label goes just above its line, or just below when the line is too
	// close to the top of the plot for that.
	baseline := func(y int) int {
		if y-12 < marginT+2 {
			return y + 17
		}
		return y - 6
	}

	const cdBand = 22050.0
	if res.Info.SampleRate >= 88200 {
		y := freqY(cdBand, nyq)
		c.dashed(left, right, y, 6, 4, colCDBand)
		s := "CD bandwidth, 22.05 kHz"
		c.label(right-textWidth(s)-6, baseline(y), s, colCDBand)
	}

	if res.Cliff != nil {
		y := freqY(res.Cliff.CutoffHz, nyq)
		c.hline(left, right, y, colCutoff)
		c.hline(left, right, y+1, colCutoff)
		c.label(left+8, baseline(y), fmt.Sprintf("cutoff %.1f kHz (%.0f dB drop)",
			res.Cliff.CutoffHz/1000, res.Cliff.DropDB), colCutoff)
	}
}

// WriteAnnotatedSpectrogram draws a spectrogram of the audio file at src with
// the analysis in res marked on it, and writes it as a PNG to dst. res must be
// the result of analyzing the same file; th should be the thresholds it was
// analyzed with.
//
// Unlike WriteSpectrogram, which asks ffmpeg for a picture, this one makes its
// own from a second pass over the audio, so that what it draws is exactly what
// was measured: the same channel, window and silence handling, with the
// detected cutoff drawn on it. The extra pass costs about as much as the
// analysis did.
func WriteAnnotatedSpectrogram(ctx context.Context, src, dst string, res *Result, th Thresholds, opts AnnotateOptions) error {
	return writeAnnotated(ctx, execSource{}, src, dst, res, th, opts)
}

func writeAnnotated(ctx context.Context, srcr source, srcPath, dst string, res *Result, th Thresholds, opts AnnotateOptions) error {
	channel := annotatedChannel(res)

	g, err := openGrid(ctx, srcr, srcPath, res.Info, channel, th.SilenceDB)
	if err != nil {
		return err
	}

	img := renderAnnotated(g, res, channel, filepath.Base(srcPath), opts)

	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		os.Remove(dst)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	return nil
}
