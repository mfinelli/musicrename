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

package sanitize

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCleanStringResult(t *testing.T) {
	t.Run("manual override sets ManualOverride true and skips pipeline", func(t *testing.T) {
		result := CleanStringResult("AC/DC", ArtistOverride)
		assert.True(t, result.ManualOverride)
		assert.Equal(t, "ac⁄dc", result.Value)
	})

	t.Run("manual override for wrong context falls through to pipeline", func(t *testing.T) {
		// AC/DC has an override for ArtistOverride only; AlbumOverride should
		// run the standard pipeline instead.
		result := CleanStringResult("AC/DC", AlbumOverride)
		assert.False(t, result.ManualOverride)
		assert.Equal(t, "acdc", result.Value)
	})

	t.Run("standard pipeline sets ManualOverride false", func(t *testing.T) {
		result := CleanStringResult("Beyoncé", ArtistOverride)
		assert.False(t, result.ManualOverride)
		assert.Equal(t, "beyonce", result.Value)
	})

	t.Run("CleanString and CleanStringResult produce identical values", func(t *testing.T) {
		inputs := []struct {
			s    string
			kind OverrideType
		}{
			{"AC/DC", ArtistOverride},
			{"P!nk", ArtistOverride},
			{"Dangerously In Love", AlbumOverride},
			{"Crazy In Love", TrackOverride},
		}
		for _, tc := range inputs {
			assert.Equal(t,
				CleanString(tc.s, tc.kind),
				CleanStringResult(tc.s, tc.kind).Value,
				"mismatch for %q kind=%d", tc.s, tc.kind,
			)
		}
	})
}

func TestCleanString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		kind     OverrideType
		expected string
	}{
		{
			name:     "Manual Override - Artist",
			input:    "AC/DC",
			kind:     ArtistOverride,
			expected: "ac⁄dc",
		},
		{
			name:     "Manual Override - Not applied for Track",
			input:    "AC/DC",
			kind:     TrackOverride,
			expected: "acdc", // Pipeline removes slash and lowercases
		},
		{
			name:     "Manual Override - P!nk Artist",
			input:    "P!nk",
			kind:     ArtistOverride,
			expected: "pink",
		},
		{
			name:     "Transliteration and Lowercase",
			input:    "Beyoncé",
			kind:     ArtistOverride,
			expected: "beyonce",
		},
		{
			name:     "Transliteration and Lowercase - Complex",
			input:    "Mötley Crüe",
			kind:     AlbumOverride,
			expected: "motley crue",
		},
		{
			// ß transliterates to "ss", not "s".
			name:     "Ligature - sharp s",
			input:    "Straße",
			kind:     AlbumOverride,
			expected: "strasse",
		},
		{
			// æ -> "ae", œ -> "oe".
			name:     "Ligatures - ae and oe",
			input:    "Æsop Œuvre",
			kind:     ArtistOverride,
			expected: "aesop oeuvre",
		},
		{
			name:     "Regex Stripping of Special Characters",
			input:    "Hello World!!! (2023 Edition)",
			kind:     TrackOverride,
			expected: "hello world 2023 edition",
		},
		{
			name:     "Whitespace Consolidation and Trimming",
			input:    "  Too   Many    Spaces  ",
			kind:     AlbumOverride,
			expected: "too many spaces",
		},
		{
			// Non-standard whitespace is converted to spaces before
			// stripping, preserving word boundaries.
			name:     "Tab converted to space preserving word boundary",
			input:    "Dark\tSide",
			kind:     AlbumOverride,
			expected: "dark side",
		},
		{
			// Newlines and other non-space whitespace follow the same rule.
			name:     "Newline converted to space preserving word boundary",
			input:    "line\none",
			kind:     TrackOverride,
			expected: "line one",
		},
		{
			name:     "Alphanumeric preservation",
			input:    "123-Artist-456!",
			kind:     ArtistOverride,
			expected: "123artist456",
		},
		{
			name:     "Combined pipeline test",
			input:    "  L'Âme  Sœur  (Feat. Artist!)  ",
			kind:     TrackOverride,
			expected: "lame soeur feat artist",
		},
		{
			// Verifies that the caller must handle an empty return value.
			// Stripping-only input produces no usable output.
			name:     "All characters stripped produces empty string",
			input:    "!!!",
			kind:     ArtistOverride,
			expected: "",
		},
		{
			// Whitespace-only input collapses and trims to empty.
			name:     "Whitespace-only input produces empty string",
			input:    "   ",
			kind:     AlbumOverride,
			expected: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			res := CleanString(test.input, test.kind)
			assert.Equal(t, test.expected, res)
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		limit    int
		expected string
	}{
		{
			name:     "No truncation needed",
			input:    "short",
			limit:    10,
			expected: "short",
		},
		{
			name:     "Truncate at exact limit",
			input:    "exactly ten",
			limit:    11,
			expected: "exactly ten",
		},
		{
			name:     "Truncate long string",
			input:    "this is a very long string that needs cutting",
			limit:    10,
			expected: "this is a ",
		},
		{
			name:     "Truncate to zero",
			input:    "something",
			limit:    0,
			expected: "",
		},
		{
			// Negative limits should not panic; treat as zero.
			name:     "Negative limit returns empty string",
			input:    "something",
			limit:    -1,
			expected: "",
		},
		{
			// ac⁄dc contains a 3-byte UTF-8 fraction slash (U+2044).
			// Byte length is 7; rune length is 5. Truncating at 4 runes
			// must not cut mid-sequence or return the wrong length.
			name:     "Multi-byte rune string truncated by rune count",
			input:    "ac⁄dc",
			limit:    4,
			expected: "ac⁄d",
		},
		{
			// Truncating right before the multi-byte rune.
			name:     "Multi-byte rune string truncated before multi-byte rune",
			input:    "ac⁄dc",
			limit:    2,
			expected: "ac",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			res := Truncate(test.input, test.limit)
			assert.Equal(t, test.expected, res)
		})
	}
}

func TestTruncateWithOffset(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		dirName  string
		maxLimit int
		expected string
	}{
		{
			// artwork = 7 chars; effective limit = 40 - 7 - 1 (slash) = 32.
			name:     "Standard offset truncation",
			input:    "very long filename that should be cut",
			dirName:  "artwork",
			maxLimit: 40,
			expected: "very long filename that should b",
		},
		{
			// extras = 6 chars; effective limit = 40 - 6 - 1 = 33.
			// "short file" is 10 chars, well within limit.
			name:     "No truncation needed with offset",
			input:    "short file",
			dirName:  "extras",
			maxLimit: 40,
			expected: "short file",
		},
		{
			// Directory name longer than maxLimit; limit clamps to zero.
			name:     "Extreme offset resulting in zero limit",
			input:    "some file",
			dirName:  "this directory name is actually longer than forty",
			maxLimit: 40,
			expected: "",
		},
		{
			// scans = 5 chars; effective limit = 40 - 5 - 1 = 34.
			// Verifies the -1 slash offset is applied for all directories.
			name:     "Slash offset applied for scans directory",
			input:    "this is a filename that is exactly 35 ch",
			dirName:  "scans",
			maxLimit: 40,
			expected: "this is a filename that is exactly",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			res := TruncateWithOffset(test.input, test.dirName, test.maxLimit)
			assert.Equal(t, test.expected, res)
		})
	}
}

func TestGetFirstLetterPath(t *testing.T) {
	tests := []struct {
		name     string
		artist   string
		expected string
		wantErr  bool
	}{
		{
			name:     "Standard artist",
			artist:   "beyonce",
			expected: "b/beyonce",
			wantErr:  false,
		},
		{
			name:     "Artist with space",
			artist:   "daft punk",
			expected: "d/daft punk",
			wantErr:  false,
		},
		{
			name:     "Numeric start",
			artist:   "123 artist",
			expected: "0/123 artist",
			wantErr:  false,
		},
		{
			name:     "Symbol start",
			artist:   "!important",
			expected: "0/!important",
			wantErr:  false,
		},
		{
			name:     "Empty artist string",
			artist:   "",
			expected: "",
			wantErr:  true,
		},
		{
			// ångström starts with 'å' (U+00E5), a 2-byte UTF-8 rune.
			// Byte-indexing would read 0xC3 and misidentify the first
			// character; rune-decoding must be used instead.
			name:     "Artist starting with multi-byte Unicode letter",
			artist:   "ångström",
			expected: "å/ångström",
			wantErr:  false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			res, err := GetFirstLetterPath(test.artist)
			if test.wantErr {
				assert.Error(t, err)
				assert.Empty(t, res)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, test.expected, res)
			}
		})
	}
}

func TestBucketOverride(t *testing.T) {
	t.Run("returns override and true for known artist", func(t *testing.T) {
		bucket, ok := BucketOverride("Dave Matthews Band")
		assert.True(t, ok)
		assert.Equal(t, "d", bucket)
	})

	t.Run("returns empty string and false for unknown artist", func(t *testing.T) {
		bucket, ok := BucketOverride("Unknown Artist")
		assert.False(t, ok)
		assert.Empty(t, bucket)
	})
}
