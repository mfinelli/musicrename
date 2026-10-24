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

func TestStreamInfoDescribe(t *testing.T) {
	tests := []struct {
		name string
		info StreamInfo
		want string
	}{
		{"cd", StreamInfo{SampleRate: 44100, BitsPerSample: 16, Channels: 2, ChannelLayout: "stereo"}, "44.1 kHz · 16-bit · stereo"},
		{"hi-res", StreamInfo{SampleRate: 96000, BitsPerSample: 24, Channels: 2, ChannelLayout: "stereo"}, "96 kHz · 24-bit · stereo"},
		{"odd rate", StreamInfo{SampleRate: 176400, BitsPerSample: 24, Channels: 2, ChannelLayout: "stereo"}, "176.4 kHz · 24-bit · stereo"},
		{"surround", StreamInfo{SampleRate: 48000, BitsPerSample: 16, Channels: 6, ChannelLayout: "5.1"}, "48 kHz · 16-bit · 5.1"},
		{"unknown depth and layout", StreamInfo{SampleRate: 44100, Channels: 2}, "44.1 kHz · 2 ch"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.info.Describe())
		})
	}
}

func TestRateKHz(t *testing.T) {
	assert.Equal(t, "44.1 kHz", rateKHz(44100))
	assert.Equal(t, "48 kHz", rateKHz(48000))
	assert.Equal(t, "192 kHz", rateKHz(192000))
	assert.Equal(t, "88.2 kHz", rateKHz(88200))
}

func TestVerdictFlagged(t *testing.T) {
	assert.False(t, VerdictInconclusive.Flagged())
	assert.False(t, VerdictOK.Flagged())
	assert.True(t, VerdictSuspect.Flagged())
	assert.True(t, VerdictLikelyFake.Flagged())
}

func TestResultSummary(t *testing.T) {
	cd := StreamInfo{SampleRate: 44100, BitsPerSample: 16, Channels: 2, ChannelLayout: "stereo"}

	t.Run("full bandwidth", func(t *testing.T) {
		r := &Result{Info: cd, Verdict: VerdictOK, BandwidthHz: 22050}
		assert.Equal(t, "full bandwidth; content reaches 22.1 kHz", r.Summary())
		assert.Empty(t, r.More())
	})

	t.Run("an unremarkable edge", func(t *testing.T) {
		r := &Result{Info: cd, Verdict: VerdictOK, Cliff: &Cliff{CutoffHz: 21500, DropDB: 30}, BandwidthHz: 21500}
		assert.Equal(t, "content ends at about 21.5 kHz, not unusual for a 44.1 kHz file", r.Summary())
	})

	t.Run("inconclusive", func(t *testing.T) {
		r := &Result{Info: cd, Verdict: VerdictInconclusive}
		assert.Equal(t, "not enough non-silent audio to judge", r.Summary())
	})

	t.Run("the most serious finding is the summary", func(t *testing.T) {
		r := &Result{
			Info:    cd,
			Verdict: VerdictLikelyFake,
			Findings: []Finding{
				{Kind: KindBitDepth, Level: VerdictSuspect, Detail: "padded"},
				{Kind: KindCutoff, Level: VerdictLikelyFake, Detail: "lowpass"},
			},
		}
		assert.Equal(t, "lowpass", r.Summary())
		assert.Equal(t, []string{"padded"}, r.More())
	})

	t.Run("equal findings keep their order", func(t *testing.T) {
		r := &Result{
			Verdict: VerdictSuspect,
			Findings: []Finding{
				{Level: VerdictSuspect, Detail: "first"},
				{Level: VerdictSuspect, Detail: "second"},
			},
		}
		assert.Equal(t, "first", r.Summary())
		assert.Equal(t, []string{"second"}, r.More())
	})
}

func TestChannelResultDescribe(t *testing.T) {
	assert.Equal(t, "channel 1: no cliff · 512 frames",
		ChannelResult{Index: 0, Frames: 512}.Describe())
	assert.Equal(t, "channel 2: cutoff 16.1 kHz (37 dB drop) · 300 frames",
		ChannelResult{Index: 1, Frames: 300, Cliff: &Cliff{CutoffHz: 16100, DropDB: 37.2}}.Describe())
	assert.Equal(t, "channel 4: skipped (LFE channel)",
		ChannelResult{Index: 3, Skipped: SkipLFE}.Describe())
}

func TestResultVerbose(t *testing.T) {
	r := &Result{
		Info:          StreamInfo{SampleRate: 44100, BitsPerSample: 24, Channels: 2},
		Window:        4096,
		EffectiveBits: 16,
		Best:          1,
		Channels: []ChannelResult{
			{Index: 0, Frames: 10, Cliff: &Cliff{CutoffHz: 16000, DropDB: 40}},
			{Index: 1, Frames: 10},
		},
	}

	lines := r.Verbose()

	assert.Equal(t, []string{
		"window: 4096 samples (10.8 Hz bins), 50% overlap",
		"bit depth: 24 declared, 16 used",
		"channel 1: cutoff 16.0 kHz (40 dB drop) · 10 frames",
		"channel 2: no cliff · 10 frames (verdict based on this channel)",
	}, lines)

	t.Run("unknown depths", func(t *testing.T) {
		r.Info.BitsPerSample = 0
		assert.Equal(t, "bit depth: unknown", r.Verbose()[1])

		r.Info.BitsPerSample = 24
		r.EffectiveBits = 0
		assert.Equal(t, "bit depth: 24 declared, use not determined", r.Verbose()[1])
	})
}
