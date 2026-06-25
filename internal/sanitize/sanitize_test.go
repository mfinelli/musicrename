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
			name:     "Standard offset truncation",
			input:    "very_long_filename_that_should_be_cut",
			dirName:  "artwork", // 7 chars
			maxLimit: 40,        // effective limit: 33
			expected: "very_long_filename_that_should_be",
		},
		{
			name:     "No truncation needed with offset",
			input:    "short_file",
			dirName:  "extras", // 6 chars
			maxLimit: 40,       // effective limit: 34
			expected: "short_file",
		},
		{
			name:     "Extreme offset resulting in zero limit",
			input:    "some_file",
			dirName:  "this_directory_name_is_actually_longer_than_forty",
			maxLimit: 40,
			expected: "",
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
