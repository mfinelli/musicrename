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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAssessCliff(t *testing.T) {
	th := DefaultThresholds()

	cd := StreamInfo{SampleRate: 44100}
	dvd := StreamInfo{SampleRate: 48000}
	hi96 := StreamInfo{SampleRate: 96000}
	hi192 := StreamInfo{SampleRate: 192000}
	low := StreamInfo{SampleRate: 32000}

	cliff := func(hz, drop float64) *Cliff { return &Cliff{CutoffHz: hz, DropDB: drop} }

	tests := []struct {
		name     string
		info     StreamInfo
		cliff    *Cliff
		wantKind FindingKind // empty: no finding expected
		want     Verdict
	}{
		{"no cliff", cd, nil, "", 0},

		// CD-rate and DVD-rate files.
		{"strong 16 kHz edge", cd, cliff(16000, 40), KindCutoff, VerdictLikelyFake},
		{"strong edge just under the lossy limit", cd, cliff(19400, 40), KindCutoff, VerdictLikelyFake},
		{"weak 16 kHz edge", cd, cliff(16000, 20), KindCutoff, VerdictSuspect},
		{"strong 20 kHz edge is ambiguous", cd, cliff(20000, 50), KindCutoff, VerdictSuspect},
		{"strong 20.6 kHz edge is ambiguous", dvd, cliff(20600, 40), KindCutoff, VerdictSuspect},
		{"edge at 21 kHz is a normal anti-alias filter", cd, cliff(21000, 40), "", 0},
		{"edge at the very top of the band", cd, cliff(21800, 30), "", 0},
		{"48 kHz file with a CD-band edge", dvd, cliff(22050, 60), "", 0},
		{"low rate file with an edge near Nyquist", low, cliff(15500, 40), "", 0},

		// Hi-res files.
		{"hi-res limited to CD bandwidth", hi96, cliff(22000, 60), KindUpsampled, VerdictLikelyFake},
		{"hi-res with a resampler's gradual edge", hi96, cliff(25800, 41), KindUpsampled, VerdictLikelyFake},
		{"hi-res from a 48 kHz source", hi96, cliff(24000, 50), KindUpsampled, VerdictLikelyFake},
		{"192 kHz limited to CD bandwidth", hi192, cliff(22050, 60), KindUpsampled, VerdictLikelyFake},
		{"hi-res with a lossy-style edge", hi96, cliff(16000, 50), KindCutoff, VerdictLikelyFake},
		{"hi-res, weak edge in the CD band", hi96, cliff(22000, 20), KindUpsampled, VerdictSuspect},
		{"hi-res with extended content to 35 kHz", hi96, cliff(35000, 40), "", 0},
		{"hi-res with an edge at the top", hi96, cliff(46000, 40), "", 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, ok := assessCliff(tc.info, th, tc.cliff)

			if tc.wantKind == "" {
				assert.False(t, ok, "unexpected finding: %+v", f)
				return
			}
			assert.True(t, ok)
			assert.Equal(t, tc.wantKind, f.Kind)
			assert.Equal(t, tc.want, f.Level)
			assert.NotEmpty(t, f.Detail)
		})
	}

	t.Run("sample rates are written without trailing zeros", func(t *testing.T) {
		f, ok := assessCliff(hi96, th, cliff(22000, 60))
		assert.True(t, ok)
		assert.Contains(t, f.Detail, "96 kHz file")
	})

	t.Run("the detail names the cutoff and the drop", func(t *testing.T) {
		f, ok := assessCliff(cd, th, cliff(16100, 37))
		assert.True(t, ok)
		assert.Contains(t, f.Detail, "16.1 kHz")
		assert.Contains(t, f.Detail, "37 dB")
	})
}

func TestVerdictString(t *testing.T) {
	assert.Equal(t, "inconclusive", VerdictInconclusive.String())
	assert.Equal(t, "ok", VerdictOK.String())
	assert.Equal(t, "suspect", VerdictSuspect.String())
	assert.Equal(t, "likely fake", VerdictLikelyFake.String())
	assert.Equal(t, "Verdict(9)", Verdict(9).String())
}

func TestVerdictOrdering(t *testing.T) {
	// Callers (and AnalyzeStream) rely on worse verdicts comparing greater.
	assert.Less(t, int(VerdictInconclusive), int(VerdictOK))
	assert.Less(t, int(VerdictOK), int(VerdictSuspect))
	assert.Less(t, int(VerdictSuspect), int(VerdictLikelyFake))
}
