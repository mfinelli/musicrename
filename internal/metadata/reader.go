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
	"fmt"
	"sort"
	"strconv"
	"strings"

	"go.senan.xyz/taglib"
)

// Reader handles the extraction of metadata from audio files.
type Reader struct{}

// NewReader returns a Reader ready for use. Reader holds no configuration
// state; the constructor exists for consistency and to allow future extension
// without breaking call sites.
func NewReader() *Reader {
	return &Reader{}
}

// ReadTrack extracts metadata from a single file and populates a Track object.
func (r *Reader) ReadTrack(t *Track) error {
	file, err := taglib.OpenReadOnly(t.Path)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// The library uses a WASM implementation that returns a map of all tags.
	tags := file.Tags()
	if tags == nil {
		return fmt.Errorf("no tags found in file")
	}

	// Helper to get the first value of a tag if it exists.
	getFirst := func(key string) string {
		if vals, ok := tags[key]; ok && len(vals) > 0 {
			return vals[0]
		}
		return ""
	}

	t.Title = getFirst(taglib.Title)
	t.Artist = getFirst(taglib.Artist)
	t.Album = getFirst(taglib.Album)
	t.AlbumArtist = getFirst(taglib.AlbumArtist)

	// MusicBrainz commonly stores full ISO-8601 dates in the DATE tag
	// (e.g. "2003-01-14" or "2003-01"). Extract only the four-character year
	// component for use in directory names; the rest is discarded.
	if raw := getFirst(taglib.Date); raw != "" {
		t.Year = strings.SplitN(raw, "-", 2)[0]
	}

	// TrackNumber is a pointer to distinguish "absent" (nil) from the valid
	// value zero, which represents a hidden/pre-gap track.
	trackStr := strings.SplitN(getFirst(taglib.TrackNumber), "/", 2)[0]
	if trackStr != "" {
		if val, err := strconv.Atoi(trackStr); err == nil {
			t.TrackNumber = &val
		}
	}

	discStr := strings.SplitN(getFirst(taglib.DiscNumber), "/", 2)[0]
	if discStr != "" {
		if val, err := strconv.Atoi(discStr); err == nil {
			t.DiscNumber = val
		}
	}

	return nil
}

// ResolveAlbumArtist returns the album-level artist using the following precedence:
//  1. The AlbumArtist tag from any track (all tracks share the same value when present).
//  2. The Artist tag of the track with the lowest positive TrackNumber. Tracks
//     with a nil TrackNumber (tag absent) or a zero TrackNumber (hidden track)
//     are skipped at this step. If the lowest-numbered track has an empty
//     Artist, the next lowest-numbered track with a non-empty Artist is used.
//  3. If no track has a positive TrackNumber, the Artist of the first track in
//     slice order that has a non-empty Artist is returned as a last resort.
//
// The method never mutates the album's Tracks slice.
func (a *Album) ResolveAlbumArtist() string {
	if len(a.Tracks) == 0 {
		return ""
	}

	// 1. Any track with an AlbumArtist tag wins immediately.
	for _, t := range a.Tracks {
		if t.AlbumArtist != "" {
			return t.AlbumArtist
		}
	}

	// 2. Sort a copy by TrackNumber so the original slice order is preserved.
	// Nil (absent) and zero (hidden track) both sort before positive track
	// numbers and are skipped in the loop below. SliceStable ensures that
	// tracks with equal numbers keep their original relative order.
	sorted := make([]*Track, len(a.Tracks))
	copy(sorted, a.Tracks)
	sort.SliceStable(sorted, func(i, j int) bool {
		ni, nj := 0, 0
		if sorted[i].TrackNumber != nil {
			ni = *sorted[i].TrackNumber
		}
		if sorted[j].TrackNumber != nil {
			nj = *sorted[j].TrackNumber
		}
		return ni < nj
	})

	for _, t := range sorted {
		if t.TrackNumber != nil && *t.TrackNumber > 0 && t.Artist != "" {
			return t.Artist
		}
	}

	// 3. No track has a positive TrackNumber. Fall back to the first track in
	// the original slice order that has any Artist set.
	for _, t := range a.Tracks {
		if t.Artist != "" {
			return t.Artist
		}
	}

	return ""
}
