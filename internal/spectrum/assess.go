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

import "fmt"

// Verdict is the overall conclusion about a track. The values are ordered by
// severity so that the worst of several findings is simply the greatest.
type Verdict int

const (
	// VerdictInconclusive means there was too little signal to judge (a
	// silent or very short track).
	VerdictInconclusive Verdict = iota
	// VerdictOK means nothing suggests the audio is anything but what its
	// container says. It is not proof: a high-bitrate lossy encode can
	// pass.
	VerdictOK
	// VerdictSuspect means the audio shows a feature that lossy encoding
	// commonly causes but that a genuine lossless source can also have.
	// Worth a look at the spectrogram.
	VerdictSuspect
	// VerdictLikelyFake means the evidence is strong: the audio is very
	// probably a transcode or an upsample, not what it claims to be.
	VerdictLikelyFake
)

func (v Verdict) String() string {
	switch v {
	case VerdictInconclusive:
		return "inconclusive"
	case VerdictOK:
		return "ok"
	case VerdictSuspect:
		return "suspect"
	case VerdictLikelyFake:
		return "likely fake"
	default:
		return fmt.Sprintf("Verdict(%d)", int(v))
	}
}

// FindingKind identifies the type of evidence behind a Finding.
type FindingKind string

const (
	// KindCutoff is a sharp lowpass edge at a frequency typical of lossy
	// encoders.
	KindCutoff FindingKind = "cutoff"
	// KindUpsampled is a hi-res file with no content beyond what a
	// CD-rate source could hold.
	KindUpsampled FindingKind = "upsampled"
	// KindBitDepth is a wide container holding narrower audio.
	KindBitDepth FindingKind = "bit-depth"
)

// Finding is one piece of evidence and how seriously to take it.
type Finding struct {
	Kind   FindingKind
	Level  Verdict // VerdictSuspect or VerdictLikelyFake
	Detail string  // a one-line explanation for display
}

// assessCliff decides what, if anything, a detected cliff means for a stream.
func assessCliff(info StreamInfo, th Thresholds, c *Cliff) (Finding, bool) {
	if c == nil {
		return Finding{}, false
	}

	nyq := float64(info.SampleRate) / 2
	f := c.CutoffHz
	hiRes := info.SampleRate >= th.HiResRate
	strong := c.DropDB >= th.StrongDropDB

	edge := fmt.Sprintf("content ends at about %s (level drops %.0f dB across the edge)",
		kHz(f), c.DropDB)

	switch {
	case f >= th.NaturalFrac*nyq:
		// Ordinary anti-alias roll-off at the top of the band.
		return Finding{}, false

	case hiRes && f <= th.HiResLimitHz && strong:
		if f < th.LossyEdgeHz {
			return Finding{KindCutoff, VerdictLikelyFake, edge +
				", a typical lossy-encoder lowpass, in a " + kHz(float64(info.SampleRate)) + " file"}, true
		}
		return Finding{KindUpsampled, VerdictLikelyFake, edge +
			": nothing above CD/DVD bandwidth in a " + kHz(float64(info.SampleRate)) +
			" file, so it looks upsampled"}, true

	case hiRes:
		if f > th.HiResLimitHz {
			// A cliff well above the CD band is a mastering filter
			// on genuinely extended content.
			return Finding{}, false
		}
		return Finding{KindUpsampled, VerdictSuspect, edge +
			", below what a hi-res file would normally reach"}, true

	case f >= th.NaturalHz:
		return Finding{}, false

	case f < th.LossyEdgeHz && strong:
		return Finding{KindCutoff, VerdictLikelyFake, edge +
			", typical of a lossy encoder's lowpass"}, true

	default:
		return Finding{KindCutoff, VerdictSuspect, edge +
			"; could be a lossy encode or a mastering filter"}, true
	}
}

// assessBitDepth flags a wide container that only ever uses a narrow range of
// bits, for example 16-bit audio padded out to 24 bits.
func assessBitDepth(info StreamInfo, th Thresholds, effective int) (Finding, bool) {
	if effective == 0 || info.BitsPerSample <= th.PaddedBitsMax || effective > th.PaddedBitsMax {
		return Finding{}, false
	}
	return Finding{
		Kind:  KindBitDepth,
		Level: VerdictSuspect,
		Detail: fmt.Sprintf("%d-bit file only uses %d bits of resolution",
			info.BitsPerSample, effective),
	}, true
}

// kHz formats a frequency in Hz as kilohertz with one decimal place.
func kHz(hz float64) string {
	return fmt.Sprintf("%.1f kHz", hz/1000)
}
