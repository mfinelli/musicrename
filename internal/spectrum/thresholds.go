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

// Thresholds holds every tunable number the analysis uses. The defaults come
// from measurements of real encoders on synthetic signals (see SPECTRUM.md),
// not from experience with a large body of real music, so expect to adjust
// them if they prove too eager or too timid on your own files.
type Thresholds struct {
	// SilenceDB: frames whose RMS level is below this many dB relative to
	// full scale are left out of the average. Digital silence and fade
	// tails would otherwise dilute the spectrum.
	SilenceDB float64

	// MinFrames is the least number of non-silent frames a channel needs
	// before it is analyzed at all (20 frames is about a second of audio).
	MinFrames int

	// BandHz is the width of the bands the spectrum is condensed into
	// before looking for a cliff. Individual FFT bins (about 11 Hz) are too
	// noisy to compare directly.
	BandHz float64

	// CompareWindowHz is how wide a stretch of spectrum is averaged on each
	// side of a candidate edge when measuring how far the level drops.
	// Wide enough to see the gradual edge of a sloppy resampler, narrow
	// enough that a lossy encoder's lowpass still shows its full height.
	CompareWindowHz float64

	// SearchFromHz: edges below this frequency are ignored.
	SearchFromHz float64

	// MinDropDB is the smallest level drop across an edge that counts as a
	// cliff at all; StrongDropDB is the drop needed for the stronger
	// ("likely") verdicts.
	MinDropDB    float64
	StrongDropDB float64

	// An edge at or above NaturalFrac of the Nyquist frequency, or at or
	// above NaturalHz, is ordinary anti-alias filtering at the top of the
	// band, not a sign of anything.
	NaturalFrac float64
	NaturalHz   float64

	// LossyEdgeHz: a strong edge below this frequency is typical of a
	// lossy encoder's lowpass. Between this and NaturalHz it is ambiguous,
	// because high-bitrate encodes and plain mastering filters both end up
	// there.
	LossyEdgeHz float64

	// HiResRate is the lowest sample rate treated as "hi-res". For such
	// files, a strong edge at or below HiResLimitHz means the file holds
	// nothing beyond what a CD-rate (or lossy) source could carry. The
	// limit sits well above the 22.05 kHz CD band to allow for the long
	// transition band of an ordinary resampler.
	HiResRate    int
	HiResLimitHz float64

	// PaddedBitsMax: a container wider than this many bits that only ever
	// uses this many bits or fewer is flagged (typically a 16-bit source
	// stored as 24-bit).
	PaddedBitsMax int
}

// DefaultThresholds returns the thresholds used unless a caller overrides
// them.
func DefaultThresholds() Thresholds {
	return Thresholds{
		SilenceDB:       -75,
		MinFrames:       20,
		BandHz:          100,
		CompareWindowHz: 1000,
		SearchFromHz:    5000,
		MinDropDB:       15,
		StrongDropDB:    25,
		NaturalFrac:     0.95,
		NaturalHz:       21000,
		LossyEdgeHz:     19500,
		HiResRate:       88200,
		HiResLimitHz:    27000,
		PaddedBitsMax:   16,
	}
}
