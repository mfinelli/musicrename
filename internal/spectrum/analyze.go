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

// Package spectrum decides whether an audio file that claims to be lossless
// (a FLAC, say) actually looks like one, or looks like a lossy encode that was
// later wrapped in a lossless container.
//
// Lossy encoders save space by throwing away detail the ear is unlikely to
// miss, most notably everything above a cutoff frequency (16 kHz for a typical
// 128 kbps MP3). Re-encoding the result losslessly does not bring that detail
// back, so the cutoff remains visible as a sharp edge in the file's spectrum.
// Spectrum measures the file's long-term average spectrum and looks for that
// edge. A few related giveaways are checked too: a hi-res file with nothing
// above CD bandwidth (upsampled) and a wide container holding narrow audio.
//
// The analysis is a heuristic. It produces evidence and a graded verdict, not
// a proof; see SPECTRUM.md for what it can and cannot detect.
//
// Audio is decoded by ffmpeg (see Analyze) and everything after that, the FFT
// included, is plain Go.
package spectrum

import (
	"context"
	"fmt"
	"io"
)

// Reasons a channel was left out of the analysis.
const (
	SkipLFE   = "LFE channel"
	SkipQuiet = "too little signal"
)

// ChannelResult is the analysis of a single channel.
type ChannelResult struct {
	// Index is the channel's position in the stream.
	Index int
	// Skipped is non-empty if the channel was not analyzed, and says why.
	Skipped string
	// Frames is the number of non-silent frames the spectrum averages.
	Frames int
	// Curve is the channel's averaged spectrum (empty if skipped).
	Curve Curve
	// Cliff is the channel's lowpass edge, nil if none was found.
	Cliff *Cliff
}

// Result is the outcome of analyzing one file.
type Result struct {
	Info StreamInfo
	// Window is the FFT window length in samples.
	Window int
	// Channels holds the per-channel analysis.
	Channels []ChannelResult
	// Best is the index into Channels of the channel the verdict is based
	// on, or -1 if no channel could be analyzed. It is the channel with the
	// widest bandwidth: one channel reaching the top of the band is enough
	// to show the file is not band-limited as a whole.
	Best int
	// Cliff is the Best channel's edge, nil if there is none.
	Cliff *Cliff
	// BandwidthHz is where the audio's content ends: the cutoff, or the
	// Nyquist frequency if no edge was found. It is zero if the file could
	// not be analyzed (Verdict is VerdictInconclusive).
	BandwidthHz float64
	// EffectiveBits is the number of bits of resolution the samples really
	// use, or 0 if unknown.
	EffectiveBits int
	Findings      []Finding
	Verdict       Verdict
}

// Analyze decodes the audio file at path with ffmpeg and analyzes it. The file
// must use a lossless codec; otherwise a *NotLosslessError is returned before
// any decoding happens.
func Analyze(ctx context.Context, path string, th Thresholds) (*Result, error) {
	return analyze(ctx, execProbeRunner{}, execSource{}, path, th)
}

func analyze(ctx context.Context, pr probeRunner, src source, path string, th Thresholds) (*Result, error) {
	info, err := probe(ctx, pr, path)
	if err != nil {
		return nil, err
	}
	if !IsLosslessCodec(info.Codec) {
		return nil, &NotLosslessError{Codec: info.Codec}
	}

	rc, err := src.Open(ctx, path)
	if err != nil {
		return nil, err
	}

	res, err := AnalyzeStream(rc, info, th)
	cerr := rc.Close()
	if err != nil {
		return nil, err
	}
	if cerr != nil {
		return nil, cerr
	}
	return res, nil
}

// AnalyzeStream analyzes interleaved 32-bit little-endian float PCM read from
// r, with the given stream properties. It is the core of the package and takes
// no file or process, which is what lets the tests drive it with synthetic
// audio.
func AnalyzeStream(r io.Reader, info StreamInfo, th Thresholds) (*Result, error) {
	if info.SampleRate <= 0 || info.Channels <= 0 {
		return nil, fmt.Errorf("invalid stream: %d Hz, %d channels", info.SampleRate, info.Channels)
	}

	window := WindowSize(info.SampleRate)
	w := newWelch(window, info.Channels, th.SilenceDB)
	bt := newBitTracker(info.BitsPerSample)

	err := readPCM(r, info.Channels, func(chans [][]float64) {
		for c, samples := range chans {
			for _, v := range samples {
				bt.observe(v)
			}
			w.feed(c, samples)
		}
	})
	if err != nil {
		return nil, err
	}

	res := &Result{
		Info:          info,
		Window:        window,
		Best:          -1,
		EffectiveBits: bt.effective(),
	}

	nyq := float64(info.SampleRate) / 2
	lfe := lfeIndex(info.ChannelLayout)
	bestBW := -1.0

	for c := range info.Channels {
		cr := ChannelResult{Index: c}

		if c == lfe {
			cr.Skipped = SkipLFE
			res.Channels = append(res.Channels, cr)
			continue
		}

		power, frames := w.average(c)
		cr.Frames = frames
		if frames < th.MinFrames {
			cr.Skipped = SkipQuiet
			res.Channels = append(res.Channels, cr)
			continue
		}

		cr.Curve = newCurve(power, info.SampleRate, window, th.BandHz)
		bw := nyq
		if cliff, ok := findCliff(cr.Curve, th); ok {
			cr.Cliff = &cliff
			bw = cliff.CutoffHz
		}
		res.Channels = append(res.Channels, cr)

		if bw > bestBW {
			bestBW = bw
			res.Best = len(res.Channels) - 1
		}
	}

	if res.Best < 0 {
		res.Verdict = VerdictInconclusive
		return res, nil
	}

	best := res.Channels[res.Best]
	res.Cliff = best.Cliff
	res.BandwidthHz = bestBW

	if f, ok := assessCliff(info, th, res.Cliff); ok {
		res.Findings = append(res.Findings, f)
	}
	if f, ok := assessBitDepth(info, th, res.EffectiveBits); ok {
		res.Findings = append(res.Findings, f)
	}

	res.Verdict = VerdictOK
	for _, f := range res.Findings {
		if f.Level > res.Verdict {
			res.Verdict = f.Level
		}
	}
	return res, nil
}
