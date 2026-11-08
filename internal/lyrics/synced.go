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

package lyrics

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// lrcStampRe matches one leading line timestamp of normalized LRC, [mm:ss.xx]
// or [hh:mm:ss.xx] (see standardizeLRC).
var lrcStampRe = regexp.MustCompile(`^\[(?:(\d+):)?(\d+):(\d{2})\.(\d{2})\]`)

// inlineStampRe matches the <mm:ss.xx> word-level timestamps of enhanced LRC,
// which are timing and not part of the words.
var inlineStampRe = regexp.MustCompile(`<(?:\d+:)?\d+:\d{2}(?:\.\d{1,3})?>`)

// lrcEntry is one timed line of lyrics.
type lrcEntry struct {
	At   time.Duration
	Text string
}

// parseLRC reads the timed lines of normalized LRC. A line with several
// leading timestamps ("[00:10.00][00:50.00]chorus", for a line sung more than
// once) becomes one entry per timestamp, so that it lines up with the same
// line in another version however it was written. Entries with no text, which
// mark gaps, and lines with no timestamp are dropped: neither is something to
// compare. The result is in time order.
func parseLRC(lrc string) []lrcEntry {
	var entries []lrcEntry
	for _, line := range strings.Split(lrc, "\n") {
		line = strings.TrimRight(line, "\r")

		var stamps []time.Duration
		for {
			m := lrcStampRe.FindStringSubmatch(line)
			if m == nil {
				break
			}
			var h, min, sec, cs int
			if m[1] != "" {
				h, _ = strconv.Atoi(m[1])
			}
			min, _ = strconv.Atoi(m[2])
			sec, _ = strconv.Atoi(m[3])
			cs, _ = strconv.Atoi(m[4])
			stamps = append(stamps, time.Duration(h)*time.Hour+
				time.Duration(min)*time.Minute+
				time.Duration(sec)*time.Second+
				time.Duration(cs)*10*time.Millisecond)
			line = line[len(m[0]):]
		}
		if len(stamps) == 0 {
			continue
		}

		text := strings.TrimSpace(inlineStampRe.ReplaceAllString(line, ""))
		if text == "" {
			continue
		}
		for _, at := range stamps {
			entries = append(entries, lrcEntry{At: at, Text: text})
		}
	}

	slices.SortStableFunc(entries, func(a, b lrcEntry) int { return cmp.Compare(a.At, b.At) })
	return entries
}

// formatStamp formats d like a normalized LRC timestamp: [mm:ss.xx], or
// [hh:mm:ss.xx] from an hour on.
func formatStamp(d time.Duration) string {
	h := int(d / time.Hour)
	d -= time.Duration(h) * time.Hour
	min := int(d / time.Minute)
	d -= time.Duration(min) * time.Minute
	sec := int(d / time.Second)
	d -= time.Duration(sec) * time.Second
	cs := int(d / (10 * time.Millisecond))

	if h > 0 {
		return fmt.Sprintf("[%02d:%02d:%02d.%02d]", h, min, sec, cs)
	}
	return fmt.Sprintf("[%02d:%02d.%02d]", min, sec, cs)
}

// Timing summarizes how the times of matching lines differ between two
// versions of the lyrics: the lines being compared are the ones whose text
// is the same in both, and a line is retimed if its time differs.
type Timing struct {
	// Compared is how many lines were compared, and Retimed how many of them
	// are at a different time.
	Compared, Retimed int

	// Uniform is true when every compared line moved by the same amount, to
	// within a couple of centiseconds, which is what an offset correction
	// looks like. Shift is then that amount.
	Uniform bool
	Shift   time.Duration

	// Median is the median change over the retimed lines, positive for later,
	// and MaxAbs is the largest change in either direction.
	Median, MaxAbs time.Duration
}

// uniformTolerance is how far apart the changes of a set of retimed lines can
// be and still count as one uniform shift: two centiseconds, the precision of
// an LRC timestamp, so that rounding alone can't break uniformity.
const uniformTolerance = 20 * time.Millisecond

// summarizeTiming builds a Timing from the signed time change of each of the
// compared lines.
func summarizeTiming(deltas []time.Duration) Timing {
	var changed []time.Duration
	for _, d := range deltas {
		if d != 0 {
			changed = append(changed, d)
		}
	}
	return timingOf(len(deltas), changed)
}

// timingOf builds a Timing from how many items were compared and the nonzero
// time changes among them.
func timingOf(compared int, changed []time.Duration) Timing {
	t := Timing{Compared: compared, Retimed: len(changed)}
	if t.Retimed == 0 {
		return t
	}

	changed = slices.Clone(changed)
	slices.Sort(changed)
	t.Median = changed[len(changed)/2]
	if len(changed)%2 == 0 {
		t.Median = (changed[len(changed)/2-1] + changed[len(changed)/2]) / 2
	}
	t.MaxAbs = max(changed[len(changed)-1], -changed[0])

	if t.Retimed == t.Compared && changed[len(changed)-1]-changed[0] <= uniformTolerance {
		t.Uniform = true
		t.Shift = t.Median
	}
	return t
}

// Retime is one line that is at a different time in the two versions.
type Retime struct {
	Text     string
	Old, New time.Duration
}

// SyncedDiff compares the synced lyrics of a file with those of another
// version. Both are compared in normalized form, so differences in how the
// LRC is written, which standardizeLRC removes before embedding, don't count.
type SyncedDiff struct {
	// Text is a diff of the words of the lines, with their timestamps taken
	// out of the comparison (each line shows its timestamp, but isn't
	// different for having a different one).
	Text TextDiff

	// Timing is how the matching lines' times differ, and Retimed lists those
	// lines, in time order.
	Timing  Timing
	Retimed []Retime

	// FormatOnly is true when the two versions are the same lines at the same
	// times and only the text of the old LRC differs from the normalized form
	// that would be written, so rewriting it would change the tag but not
	// what it says.
	FormatOnly bool
}

// Same reports whether the two versions have the same lines at the same times.
func (d SyncedDiff) Same() bool { return d.Text.Same && d.Timing.Retimed == 0 }

// diffSynced compares oldLRC with newLRC. context is as for diffLines.
func diffSynced(oldLRC, newLRC string, context int) SyncedDiff {
	newNorm := standardizeLRC(newLRC)
	oldEntries := parseLRC(standardizeLRC(oldLRC))
	newEntries := parseLRC(newNorm)

	src := func(entries []lrcEntry) lineSource {
		s := lineSource{Text: make([]string, len(entries)), Stamps: make([]string, len(entries))}
		for i, e := range entries {
			s.Text[i] = e.Text
			s.Stamps[i] = formatStamp(e.At)
		}
		return s
	}
	oldSrc, newSrc := src(oldEntries), src(newEntries)

	d := SyncedDiff{Text: diffLines(oldSrc, newSrc, context)}

	var deltas []time.Duration
	for _, e := range diffSeq(oldSrc.Text, newSrc.Text) {
		if e.Kind != OpEqual {
			continue
		}
		delta := newEntries[e.B].At - oldEntries[e.A].At
		deltas = append(deltas, delta)
		if delta != 0 {
			d.Retimed = append(d.Retimed, Retime{
				Text: newEntries[e.B].Text,
				Old:  oldEntries[e.A].At,
				New:  newEntries[e.B].At,
			})
		}
	}
	d.Timing = summarizeTiming(deltas)

	d.FormatOnly = d.Same() && strings.TrimSpace(oldLRC) != newNorm
	return d
}
