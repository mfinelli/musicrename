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

	"github.com/stretchr/testify/assert"
)

func TestNormalizeTimestamp(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "already correct mm:ss.xx is unchanged",
			input: "01:23.45",
			want:  "01:23.45",
		},
		{
			name:  "three-digit milliseconds truncated to centiseconds",
			input: "00:30.123",
			want:  "00:30.12",
		},
		{
			name:  "one-digit fractional left-padded to centiseconds",
			input: "00:30.5",
			want:  "00:30.50",
		},
		{
			name:  "absent fractional becomes .00",
			input: "00:30",
			want:  "00:30.00",
		},
		{
			name:  "zero timestamp",
			input: "00:00.00",
			want:  "00:00.00",
		},
		{
			name:  "overflow seconds corrected",
			input: "00:90.00",
			want:  "01:30.00",
		},
		{
			name:  "overflow minutes produces hours component",
			input: "90:00.00",
			want:  "01:30:00.00",
		},
		{
			name:  "existing hours component preserved",
			input: "01:30:00.50",
			want:  "01:30:00.50",
		},
		{
			name:  "explicit zero hours omitted from output",
			input: "00:01:23.45",
			want:  "01:23.45",
		},
		{
			name:  "milliseconds preserved when divisible by 10",
			input: "03:45.200",
			want:  "03:45.20",
		},
		{
			name:  "unrecognised string returned unchanged",
			input: "ar:Artist",
			want:  "ar:Artist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeTimestamp(tt.input))
		})
	}
}

func TestStandardizeLRC(t *testing.T) {
	t.Run("standardizes line timestamps throughout file", func(t *testing.T) {
		input := "[00:01.234] Hello\n[00:30.5] World\n[01:00.000] End"
		want := "[00:01.23] Hello\n[00:30.50] World\n[01:00.00] End"
		assert.Equal(t, want, standardizeLRC(input))
	})

	t.Run("standardizes word-by-word timestamps in angle brackets", func(t *testing.T) {
		input := "[00:01.00] <00:01.123>Hello <00:01.500>world"
		want := "[00:01.00] <00:01.12>Hello <00:01.50>world"
		assert.Equal(t, want, standardizeLRC(input))
	})

	t.Run("LRC metadata tags are not affected", func(t *testing.T) {
		input := "[ar:AC/DC]\n[al:Back In Black]\n[ti:Back In Black]\n[00:01.00] Line one"
		want := "[ar:AC/DC]\n[al:Back In Black]\n[ti:Back In Black]\n[00:01.00] Line one"
		assert.Equal(t, want, standardizeLRC(input))
	})

	t.Run("offset and length tags are not affected", func(t *testing.T) {
		input := "[offset:+200]\n[length:4:15]\n[00:01.00] Line one"
		want := "[offset:+200]\n[length:4:15]\n[00:01.00] Line one"
		assert.Equal(t, want, standardizeLRC(input))
	})

	t.Run("corrects overflow timestamps", func(t *testing.T) {
		input := "[00:90.00] Late line\n[00:00.00] First line"
		want := "[01:30.00] Late line\n[00:00.00] First line"
		assert.Equal(t, want, standardizeLRC(input))
	})

	t.Run("empty string returns empty string", func(t *testing.T) {
		assert.Equal(t, "", standardizeLRC(""))
	})

	t.Run("string with no timestamps is returned unchanged", func(t *testing.T) {
		input := "[ar:Artist]\nPlain text with no timestamps here."
		assert.Equal(t, input, standardizeLRC(input))
	})

	t.Run("mixed bracket types in same file", func(t *testing.T) {
		// Enhanced LRC format uses both [...] for lines and <...> for words.
		input := "[00:10.999] <00:10.999>Word <00:11.500>by <00:12.000>word"
		want := "[00:10.99] <00:10.99>Word <00:11.50>by <00:12.00>word"
		assert.Equal(t, want, standardizeLRC(input))
	})
}
