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

package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewReader(t *testing.T) {
	// NewReader has no configurable state; this test confirms the constructor
	// returns a non-nil value and that two calls produce independent instances.
	r1 := NewReader()
	r2 := NewReader()
	assert.NotNil(t, r1)
	assert.NotNil(t, r2)
	assert.NotSame(t, r1, r2)
}

func TestResolveAlbumArtist(t *testing.T) {
	tests := []struct {
		name     string
		tracks   []*Track
		expected string
	}{
		{
			name: "AlbumArtist tag present",
			tracks: []*Track{
				{Artist: "Track Artist 1", AlbumArtist: "Main Artist", TrackNumber: 1},
				{Artist: "Track Artist 2", AlbumArtist: "Main Artist", TrackNumber: 2},
			},
			expected: "Main Artist",
		},
		{
			name: "AlbumArtist missing, fallback to lowest track number",
			tracks: []*Track{
				{Artist: "Secondary Artist", TrackNumber: 2},
				{Artist: "Primary Artist", TrackNumber: 1},
			},
			expected: "Primary Artist",
		},
		{
			name: "AlbumArtist missing, all track numbers zero, fallback to first in slice",
			tracks: []*Track{
				{Artist: "First Track Artist", TrackNumber: 0},
				{Artist: "Second Track Artist", TrackNumber: 0},
			},
			expected: "First Track Artist",
		},
		{
			name:     "Empty album",
			tracks:   []*Track{},
			expected: "",
		},
		{
			name: "Lowest-numbered track has no artist, skip to next",
			tracks: []*Track{
				{Artist: "", TrackNumber: 1},
				{Artist: "The Real Artist", TrackNumber: 2},
			},
			expected: "The Real Artist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			album := &Album{Tracks: tt.tracks}

			// Capture original order before the call to verify no mutation.
			originalOrder := make([]*Track, len(tt.tracks))
			copy(originalOrder, tt.tracks)

			result := album.ResolveAlbumArtist()
			assert.Equal(t, tt.expected, result)

			// ResolveAlbumArtist must not reorder the album's Tracks slice.
			assert.Equal(t, originalOrder, album.Tracks, "Tracks slice must not be mutated")
		})
	}
}
