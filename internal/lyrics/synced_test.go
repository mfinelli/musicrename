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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLRC(t *testing.T) {
	t.Run("reads the timed lines", func(t *testing.T) {
		got := parseLRC("[00:12.30]Hello\n[01:05.00]World")
		assert.Equal(t, []lrcEntry{
			{At: 12*time.Second + 300*time.Millisecond, Text: "Hello"},
			{At: time.Minute + 5*time.Second, Text: "World"},
		}, got)
	})

	t.Run("reads hours", func(t *testing.T) {
		got := parseLRC("[01:02:03.04]Late")
		require.Len(t, got, 1)
		assert.Equal(t, time.Hour+2*time.Minute+3*time.Second+40*time.Millisecond, got[0].At)
	})

	t.Run("a line with several timestamps becomes an entry for each, in time order", func(t *testing.T) {
		got := parseLRC("[00:10.00][00:50.00]Chorus\n[00:30.00]Verse")
		assert.Equal(t, []lrcEntry{
			{At: 10 * time.Second, Text: "Chorus"},
			{At: 30 * time.Second, Text: "Verse"},
			{At: 50 * time.Second, Text: "Chorus"},
		}, got)
	})

	t.Run("drops gaps and lines with no timestamp", func(t *testing.T) {
		got := parseLRC("[00:01.00]One\n[00:02.00]\nplain text\n\n[00:03.00]Three")
		require.Len(t, got, 2)
		assert.Equal(t, "One", got[0].Text)
		assert.Equal(t, "Three", got[1].Text)
	})

	t.Run("word-level timestamps are not part of the text", func(t *testing.T) {
		got := parseLRC("[00:01.00]<00:01.00>Hello <00:01.50>world")
		require.Len(t, got, 1)
		assert.Equal(t, "Hello world", got[0].Text)
	})

	t.Run("handles Windows line endings", func(t *testing.T) {
		got := parseLRC("[00:01.00]One\r\n[00:02.00]Two\r\n")
		require.Len(t, got, 2)
		assert.Equal(t, "Two", got[1].Text)
	})
}

func TestFormatStamp(t *testing.T) {
	assert.Equal(t, "[00:00.00]", formatStamp(0))
	assert.Equal(t, "[01:01.50]", formatStamp(61*time.Second+500*time.Millisecond))
	assert.Equal(t, "[12:34.56]", formatStamp(12*time.Minute+34*time.Second+560*time.Millisecond))
	assert.Equal(t, "[01:00:00.00]", formatStamp(time.Hour))

	t.Run("agrees with parseLRC", func(t *testing.T) {
		for _, d := range []time.Duration{0, 1230 * time.Millisecond, 59*time.Minute + 59*time.Second + 990*time.Millisecond, 90 * time.Minute} {
			got := parseLRC(formatStamp(d) + "x")
			require.Len(t, got, 1)
			assert.Equal(t, d, got[0].At)
		}
	})
}

func TestSummarizeTiming(t *testing.T) {
	const cs = 10 * time.Millisecond

	t.Run("nothing moved", func(t *testing.T) {
		got := summarizeTiming([]time.Duration{0, 0, 0})
		assert.Equal(t, Timing{Compared: 3}, got)
	})

	t.Run("no lines compared", func(t *testing.T) {
		assert.Equal(t, Timing{}, summarizeTiming(nil))
	})

	t.Run("every line moved by the same amount is a uniform shift", func(t *testing.T) {
		got := summarizeTiming([]time.Duration{30 * cs, 30 * cs, 30 * cs})
		assert.True(t, got.Uniform)
		assert.Equal(t, 300*time.Millisecond, got.Shift)
		assert.Equal(t, 3, got.Retimed)
	})

	t.Run("rounding of a centisecond or two keeps a shift uniform", func(t *testing.T) {
		got := summarizeTiming([]time.Duration{30 * cs, 31 * cs, 29 * cs})
		assert.True(t, got.Uniform)
	})

	t.Run("a shift with some lines untouched is not uniform", func(t *testing.T) {
		got := summarizeTiming([]time.Duration{30 * cs, 0, 30 * cs})
		assert.False(t, got.Uniform)
		assert.Equal(t, 2, got.Retimed)
		assert.Equal(t, 3, got.Compared)
	})

	t.Run("uneven changes report the median and the largest", func(t *testing.T) {
		got := summarizeTiming([]time.Duration{10 * cs, -50 * cs, 200 * cs, 0})
		assert.False(t, got.Uniform)
		assert.Equal(t, 3, got.Retimed)
		assert.Equal(t, 10*cs, got.Median)
		assert.Equal(t, 200*cs, got.MaxAbs)
	})

	t.Run("the largest change counts in either direction", func(t *testing.T) {
		got := summarizeTiming([]time.Duration{10 * cs, -500 * cs})
		assert.Equal(t, 500*cs, got.MaxAbs)
	})

	t.Run("an even number of changes averages the middle two", func(t *testing.T) {
		got := summarizeTiming([]time.Duration{10 * cs, 20 * cs})
		assert.Equal(t, 15*cs, got.Median)
	})
}

func TestDiffSynced(t *testing.T) {
	const base = "[00:01.00]Hello world\n[00:05.00]Second line\n[00:09.00]Third line"

	t.Run("identical lyrics are the same", func(t *testing.T) {
		d := diffSynced(base, base, 3)
		assert.True(t, d.Same())
		assert.False(t, d.FormatOnly)
		assert.Equal(t, 3, d.Timing.Compared)
		assert.Empty(t, d.Retimed)
	})

	t.Run("a uniform retiming changes no text", func(t *testing.T) {
		shifted := "[00:01.30]Hello world\n[00:05.30]Second line\n[00:09.30]Third line"
		d := diffSynced(base, shifted, 3)

		assert.True(t, d.Text.Same, "the words didn't change, so a retimed line is not a changed line")
		assert.False(t, d.Same())
		assert.True(t, d.Timing.Uniform)
		assert.Equal(t, 300*time.Millisecond, d.Timing.Shift)
		require.Len(t, d.Retimed, 3)
		assert.Equal(t, Retime{Text: "Hello world", Old: time.Second, New: 1300 * time.Millisecond}, d.Retimed[0])
	})

	t.Run("a reworded line is a text change with the rest still compared", func(t *testing.T) {
		edited := "[00:01.00]Hello world\n[00:05.00]Second lines\n[00:09.00]Third line"
		d := diffSynced(base, edited, 3)

		assert.False(t, d.Text.Same)
		assert.Equal(t, 1, d.Text.Added)
		assert.Equal(t, 1, d.Text.Removed)
		assert.Equal(t, 2, d.Timing.Compared) // the two untouched lines
		assert.Zero(t, d.Timing.Retimed)
	})

	t.Run("added and removed lines show their timestamps", func(t *testing.T) {
		more := base + "\n[00:13.00]Fourth line"
		d := diffSynced(base, more, FullContext)
		require.Len(t, d.Text.Hunks, 1)
		var inserted []string
		for _, ln := range d.Text.Hunks[0].Lines {
			if ln.Kind == OpInsert {
				inserted = append(inserted, ln.Stamp+ln.Text)
			}
		}
		assert.Equal(t, []string{"[00:13.00]Fourth line"}, inserted)
	})

	t.Run("a retimed and reworded set of lines reports both", func(t *testing.T) {
		both := "[00:01.50]Hello world\n[00:05.50]Second lines\n[00:09.50]Third line"
		d := diffSynced(base, both, 3)
		assert.False(t, d.Text.Same)
		assert.Equal(t, 2, d.Timing.Retimed)
	})

	t.Run("formatting of the LRC does not matter", func(t *testing.T) {
		messy := "[ar:Someone]\n[offset:0]\n[0:01.0] Hello world\n[00:05.00]    Second line\n[00:09.000]Third line\n"
		d := diffSynced(messy, base, 3)
		assert.True(t, d.Same())
		assert.True(t, d.FormatOnly, "rewriting it would change the tag, if not what it says")
	})

	t.Run("an offset header is applied before comparing", func(t *testing.T) {
		withOffset := "[offset:+500]\n[00:00.50]Hello world\n[00:04.50]Second line\n[00:08.50]Third line"
		d := diffSynced(withOffset, base, 3)
		assert.True(t, d.Same())
	})

	t.Run("a repeated line is matched to its own repeat", func(t *testing.T) {
		old := "[00:10.00]Chorus\n[00:20.00]Verse\n[00:30.00]Chorus"
		updated := "[00:10.00][00:30.00]Chorus\n[00:20.00]Verse"
		d := diffSynced(old, updated, 3)
		assert.True(t, d.Same())
	})

	t.Run("lyrics against nothing are all removed", func(t *testing.T) {
		d := diffSynced(base, "", 3)
		assert.Equal(t, 3, d.Text.Removed)
		assert.Zero(t, d.Timing.Compared)
	})
}
