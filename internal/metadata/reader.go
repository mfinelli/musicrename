package metadata

import (
	"fmt"
	"sort"
	"strconv"

	"go.senan.xyz/taglib"
)

// Reader handles the extraction of metadata from audio files
type Reader struct{}

func NewReader() *Reader {
	return &Reader{}
}

// ReadTrack extracts metadata from a single file and populates a Track object
func (r *Reader) ReadTrack(t *Track) error {
	// Use OpenReadOnly as established
	file, err := taglib.OpenReadOnly(t.Path)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// The WASM implementation returns a map of all tags
	tags := file.Tags()
	if tags == nil {
		return fmt.Errorf("no tags found in file")
	}

	// Helper to get the first value of a tag if it exists
	getFirst := func(key string) string {
		if vals, ok := tags[key]; ok && len(vals) > 0 {
			return vals[0]
		}
		return ""
	}

	// Map the standardized constants from the library to our struct
	t.Title = getFirst(taglib.Title)
	t.Artist = getFirst(taglib.Artist)
	t.Album = getFirst(taglib.Album)
	t.Year = getFirst(taglib.Date) // Use Date for Year
	t.AlbumArtist = getFirst(taglib.AlbumArtist)

	// Track Number: comes as a string in the map, needs conversion to int
	trackStr := getFirst(taglib.TrackNumber)
	if trackStr != "" {
		if val, err := strconv.Atoi(trackStr); err == nil {
			t.TrackNumber = val
		}
	}

	// Disc Number: comes as a string in the map, needs conversion to int
	discStr := getFirst(taglib.DiscNumber)
	if discStr != "" {
		if val, err := strconv.Atoi(discStr); err == nil {
			t.DiscNumber = val
		}
	}

	return nil
}

// ResolveAlbumArtist implements the fallback logic for the album's primary artist
func (a *Album) ResolveAlbumArtist() string {
	if len(a.Tracks) == 0 {
		return ""
	}

	// 1. Check if any track has an AlbumArtist set
	// We check all tracks because some might be missing it while others have it
	for _, t := range a.Tracks {
		if t.AlbumArtist != "" {
			return t.AlbumArtist
		}
	}

	// 2. Fallback: Artist of the track with the lowest track number
	// We need to sort tracks by number first to be sure
	sort.Slice(a.Tracks, func(i, j int) bool {
		return a.Tracks[i].TrackNumber < a.Tracks[j].TrackNumber
	})

	// Find the first track that actually has a track number and an artist
	for _, t := range a.Tracks {
		if t.TrackNumber > 0 && t.Artist != "" {
			return t.Artist
		}
	}

	// 3. Last resort: Just return the artist of the first track in the slice
	if len(a.Tracks) > 0 && a.Tracks[0].Artist != "" {
		return a.Tracks[0].Artist
	}

	return ""
}
