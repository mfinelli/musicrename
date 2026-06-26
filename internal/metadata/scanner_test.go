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

func TestCategorizeRootFile(t *testing.T) {
	tests := []struct {
		filename string
		expected FileCategory
	}{
		// Audio
		{"song.flac", CatAudio},
		{"song.mp3", CatAudio},
		{"song.m4a", CatAudio},
		// Root text
		{"info.log", CatRootText},
		{"album.cue", CatRootText},
		{"playlist.m3u", CatRootText},
		{"playlist.m3u8", CatRootText},
		{"notes.txt", CatRootText},
		{"sums.md5", CatRootText},
		// Primary art (only the exact folder.* names qualify)
		{"folder.jpg", CatPrimaryArt},
		{"folder.jpeg", CatPrimaryArt},
		{"folder.png", CatPrimaryArt},
		// These look similar but are not primary art
		{"folder2.jpg", CatArtwork},
		{"folderbig.png", CatArtwork},
		// Supplementary artwork
		{"cover.jpg", CatArtwork},
		{"back.jpeg", CatArtwork},
		{"booklet.png", CatArtwork},
		// Scans
		{"highres.tiff", CatScan},
		{"scan.tif", CatScan},
		// Unknown
		{"readme.exe", CatUnknown},
		{"data.bin", CatUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			assert.Equal(t, tt.expected, categorizeRootFile(tt.filename))
		})
	}
}
