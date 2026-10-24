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
	"fmt"
	"strings"
)

// Describe returns a short description of the stream, such as
// "44.1 kHz · 16-bit · stereo".
func (i StreamInfo) Describe() string {
	parts := []string{rateKHz(i.SampleRate)}
	if i.BitsPerSample > 0 {
		parts = append(parts, fmt.Sprintf("%d-bit", i.BitsPerSample))
	}
	if i.ChannelLayout != "" {
		parts = append(parts, i.ChannelLayout)
	} else {
		parts = append(parts, fmt.Sprintf("%d ch", i.Channels))
	}
	return strings.Join(parts, " · ")
}

// primary returns the index of the most serious finding (the first, among
// equals), or -1 if there are none.
func (r *Result) primary() int {
	best := -1
	for i, f := range r.Findings {
		if best < 0 || f.Level > r.Findings[best].Level {
			best = i
		}
	}
	return best
}

// Summary returns a one-line plain-language statement of the outcome. For a
// flagged result it is the most serious finding; otherwise it says what was
// seen instead, so that an "ok" always shows what it was based on.
func (r *Result) Summary() string {
	if p := r.primary(); p >= 0 {
		return r.Findings[p].Detail
	}

	switch {
	case r.Verdict == VerdictInconclusive:
		return "not enough non-silent audio to judge"
	case r.Cliff == nil:
		return fmt.Sprintf("full bandwidth; content reaches %s", kHz(r.BandwidthHz))
	default:
		return fmt.Sprintf("content ends at about %s, not unusual for a %s file",
			kHz(r.Cliff.CutoffHz), rateKHz(r.Info.SampleRate))
	}
}

// More returns the details of every finding other than the one Summary
// reports, for a result with more than one.
func (r *Result) More() []string {
	p := r.primary()
	var out []string
	for i, f := range r.Findings {
		if i != p {
			out = append(out, f.Detail)
		}
	}
	return out
}

// Describe returns a one-line description of the channel's analysis.
func (c ChannelResult) Describe() string {
	label := fmt.Sprintf("channel %d", c.Index+1)
	switch {
	case c.Skipped != "":
		return fmt.Sprintf("%s: skipped (%s)", label, c.Skipped)
	case c.Cliff == nil:
		return fmt.Sprintf("%s: no cliff · %d frames", label, c.Frames)
	default:
		return fmt.Sprintf("%s: cutoff %s (%.0f dB drop) · %d frames",
			label, kHz(c.Cliff.CutoffHz), c.Cliff.DropDB, c.Frames)
	}
}

// Verbose returns the technical detail behind the result, one item per line:
// the window used, the bit depth, and each channel's analysis.
func (r *Result) Verbose() []string {
	binHz := float64(r.Info.SampleRate) / float64(r.Window)
	lines := []string{
		fmt.Sprintf("window: %d samples (%.1f Hz bins), 50%% overlap", r.Window, binHz),
	}

	switch {
	case r.Info.BitsPerSample == 0:
		lines = append(lines, "bit depth: unknown")
	case r.EffectiveBits == 0:
		lines = append(lines, fmt.Sprintf("bit depth: %d declared, use not determined", r.Info.BitsPerSample))
	default:
		lines = append(lines, fmt.Sprintf("bit depth: %d declared, %d used", r.Info.BitsPerSample, r.EffectiveBits))
	}

	for i, c := range r.Channels {
		line := c.Describe()
		if i == r.Best {
			line += " (verdict based on this channel)"
		}
		lines = append(lines, line)
	}
	return lines
}
