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

func TestParseOffset(t *testing.T) {
	t.Run("parses positive offset with plus sign", func(t *testing.T) {
		assert.Equal(t, 500, parseOffset("[offset:+500]\n[00:01.00] Line"))
	})

	t.Run("parses negative offset", func(t *testing.T) {
		assert.Equal(t, -200, parseOffset("[offset:-200]\n[00:01.00] Line"))
	})

	t.Run("parses offset without explicit sign", func(t *testing.T) {
		assert.Equal(t, 100, parseOffset("[offset:100]\n[00:01.00] Line"))
	})

	t.Run("returns zero when no offset tag present", func(t *testing.T) {
		assert.Equal(t, 0, parseOffset("[ar:Artist]\n[00:01.00] Line"))
	})

	t.Run("returns zero for empty string", func(t *testing.T) {
		assert.Equal(t, 0, parseOffset(""))
	})

	t.Run("offset tag is case-insensitive", func(t *testing.T) {
		assert.Equal(t, 300, parseOffset("[OFFSET:+300]\n[00:01.00] Line"))
	})
}

func TestIsHeaderLine(t *testing.T) {
	headers := []struct {
		name string
		line string
	}{
		{"title tag", "[ti:Song Title]"},
		{"artist tag", "[ar:AC/DC]"},
		{"album tag", "[al:Back In Black]"},
		{"author tag", "[au:Songwriter]"},
		{"lyricist tag", "[lr:Lyricist Name]"},
		{"length tag", "[length:4:15]"},
		{"by tag", "[by:LRC Creator]"},
		{"offset tag", "[offset:+500]"},
		{"re tag", "[re:LRC Editor]"},
		{"tool tag", "[tool:Some Tool]"},
		{"version tag", "[ve:1.0]"},
		{"uppercase tag", "[AR:Artist]"},
		{"tag with whitespace padding", "  [ti:Title]  "},
	}
	for _, tt := range headers {
		t.Run(tt.name, func(t *testing.T) {
			assert.True(t, isHeaderLine(tt.line))
		})
	}

	nonHeaders := []struct {
		name string
		line string
	}{
		{"lyric line with timestamp", "[00:01.00] Hello"},
		{"comment line", "# This is a comment"},
		{"empty line", ""},
		{"whitespace only", "   "},
		{"plain text", "Just some lyrics"},
	}
	for _, tt := range nonHeaders {
		t.Run("not a header: "+tt.name, func(t *testing.T) {
			assert.False(t, isHeaderLine(tt.line))
		})
	}
}

func TestIsCommentLine(t *testing.T) {
	t.Run("line starting with hash is a comment", func(t *testing.T) {
		assert.True(t, isCommentLine("# This is a comment"))
	})

	t.Run("hash with leading whitespace is a comment", func(t *testing.T) {
		assert.True(t, isCommentLine("  # indented comment"))
	})

	t.Run("lyric line is not a comment", func(t *testing.T) {
		assert.False(t, isCommentLine("[00:01.00] Hello"))
	})

	t.Run("empty line is not a comment", func(t *testing.T) {
		assert.False(t, isCommentLine(""))
	})
}

func TestStandardizeLRC(t *testing.T) {
	t.Run("normalizes timestamps and strips spaces", func(t *testing.T) {
		input := "[00:01.234] Hello\n[00:30.5] World\n[01:00.000] End"
		want := "[00:01.23]Hello\n[00:30.50]World\n[01:00.00]End"
		assert.Equal(t, want, standardizeLRC(input))
	})

	t.Run("strips all known header metadata lines", func(t *testing.T) {
		input := "[ti:Song Title]\n[ar:Artist]\n[al:Album]\n[au:Author]\n[by:Creator]\n[re:Tool]\n[ve:1.0]\n[00:01.00] Line one"
		want := "[00:01.00]Line one"
		assert.Equal(t, want, standardizeLRC(input))
	})

	t.Run("strips comment lines", func(t *testing.T) {
		input := "# This is a comment\n[00:01.00] Line one\n# Another comment\n[00:05.00] Line two"
		want := "[00:01.00]Line one\n[00:05.00]Line two"
		assert.Equal(t, want, standardizeLRC(input))
	})

	t.Run("applies positive offset and discards offset tag", func(t *testing.T) {
		// +500ms shifts [00:01.00] (1000ms) to [00:01.50] (1500ms)
		input := "[offset:+500]\n[00:01.00] Line one"
		want := "[00:01.50]Line one"
		assert.Equal(t, want, standardizeLRC(input))
	})

	t.Run("applies negative offset and discards offset tag", func(t *testing.T) {
		// -200ms shifts [00:01.00] (1000ms) to [00:00.80] (800ms)
		input := "[offset:-200]\n[00:01.00] Line one"
		want := "[00:00.80]Line one"
		assert.Equal(t, want, standardizeLRC(input))
	})

	t.Run("negative offset clamped to zero rather than going negative", func(t *testing.T) {
		input := "[offset:-5000]\n[00:01.00] Line one"
		want := "[00:00.00]Line one"
		assert.Equal(t, want, standardizeLRC(input))
	})

	t.Run("strips single space after line-level timestamp", func(t *testing.T) {
		assert.Equal(t, "[00:01.00]Hello", standardizeLRC("[00:01.00] Hello"))
	})

	t.Run("strips multiple spaces after line-level timestamp", func(t *testing.T) {
		assert.Equal(t, "[00:01.00]Hello", standardizeLRC("[00:01.00]   Hello"))
	})

	t.Run("does not strip space after word-level angle-bracket timestamp", func(t *testing.T) {
		// Spaces after <...> are part of word content and must be preserved.
		input := "[00:01.00] <00:01.123>Hello <00:01.500>world"
		want := "[00:01.00]<00:01.12>Hello <00:01.50>world"
		assert.Equal(t, want, standardizeLRC(input))
	})

	t.Run("corrects overflow timestamps", func(t *testing.T) {
		input := "[00:90.00] Late line\n[00:00.00] First line"
		want := "[01:30.00]Late line\n[00:00.00]First line"
		assert.Equal(t, want, standardizeLRC(input))
	})

	t.Run("preserves blank lines between lyrics", func(t *testing.T) {
		input := "[00:01.00] Line one\n\n[00:05.00] Line two"
		want := "[00:01.00]Line one\n\n[00:05.00]Line two"
		assert.Equal(t, want, standardizeLRC(input))
	})

	t.Run("trims leading and trailing blank lines", func(t *testing.T) {
		input := "\n\n[00:01.00] Line one\n\n"
		want := "[00:01.00]Line one"
		assert.Equal(t, want, standardizeLRC(input))
	})

	t.Run("empty string returns empty string", func(t *testing.T) {
		assert.Equal(t, "", standardizeLRC(""))
	})

	t.Run("plain text lines with no timestamps are preserved", func(t *testing.T) {
		input := "[00:01.00] Line one\nno timestamp here\n[00:05.00] Line two"
		want := "[00:01.00]Line one\nno timestamp here\n[00:05.00]Line two"
		assert.Equal(t, want, standardizeLRC(input))
	})

	t.Run("handles full LRC file with headers offset and mixed timestamps", func(t *testing.T) {
		input := "[ar:AC/DC]\n[ti:Back In Black]\n[offset:+100]\n# ripped by someone\n[00:18.00] I'm rolling thunder\n[00:21.00] Pouring rain"
		// +100ms: 18000+100=18100ms -> 18.10, 21000+100=21100ms -> 21.10
		want := "[00:18.10]I'm rolling thunder\n[00:21.10]Pouring rain"
		assert.Equal(t, want, standardizeLRC(input))
	})
}
