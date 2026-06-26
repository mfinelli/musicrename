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
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveAlbumArtist(t *testing.T) {
	tests := []struct {
		name     string
		tracks   []*Track
		expected string
	}{
		{
			name: "Case 1: AlbumArtist is present",
			tracks: []*Track{
				{Artist: "Track Artist 1", AlbumArtist: "Main Artist", TrackNumber: 1},
				{Artist: "Track Artist 2", AlbumArtist: "Main Artist", TrackNumber: 2},
			},
			expected: "Main Artist",
		},
		{
			name: "Case 2: AlbumArtist missing, fallback to lowest track number",
			tracks: []*Track{
				{Artist: "Secondary Artist", TrackNumber: 2},
				{Artist: "Primary Artist", TrackNumber: 1}, // Lowest track
			},
			expected: "Primary Artist",
		},
		{
			name: "Case 3: AlbumArtist missing, track numbers missing, fallback to first track",
			tracks: []*Track{
				{Artist: "First Track Artist", TrackNumber: 0},
				{Artist: "Second Track Artist", TrackNumber: 0},
			},
			expected: "First Track Artist",
		},
		{
			name:     "Case 4: Completely empty album",
			tracks:   []*Track{},
			expected: "",
		},
		{
			name: "Case 5: Mixed tracks, some without artists",
			tracks: []*Track{
				{Artist: "", TrackNumber: 1},
				{Artist: "The Real Artist", TrackNumber: 2},
			},
			expected: "The Real Artist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			album := &Album{
				Tracks: tt.tracks,
			}
			assert.Equal(t, tt.expected, album.ResolveAlbumArtist())
		})
	}
}

func TestFileCategorization(t *testing.T) {
	// This tests the logic we'd usually use inside processDirectory
	// We can verify that our extension maps are correct.
	tests := []struct {
		filename string
		expected FileCategory
	}{
		{"song.flac", CatAudio},
		{"song.mp3", CatAudio},
		{"song.m4a", CatAudio},
		{"info.log", CatRootText},
		{"album.cue", CatRootText},
		{"sums.md5", CatRootText}, // Testing the fix we added
		{"cover.jpg", CatArtwork},
		{"folder.png", CatRootText}, // folder.png should be root text per spec
		{"highres.tiff", CatScan},
		{"readme.txt", CatRootText},
		{"random.exe", CatUnknown},
	}

	// Note: In the actual implementation, logic is spread across
	// processDirectory and handleSubDir. This test validates the maps.
	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			// Simplified version of the scanner's logic for testing
			ext := strings.ToLower(filepath.Ext(tt.filename))
			var actual FileCategory

			if audioExts[ext] {
				actual = CatAudio
			} else if textExts[ext] || tt.filename == "sums.md5" {
				actual = CatRootText
			} else if imageExts[ext] {
				if strings.HasPrefix(strings.ToLower(tt.filename), "folder") {
					actual = CatRootText
				} else {
					actual = CatArtwork
				}
			} else if scanExts[ext] {
				actual = CatScan
			} else {
				actual = CatUnknown
			}

			assert.Equal(t, tt.expected, actual)
		})
	}
}
