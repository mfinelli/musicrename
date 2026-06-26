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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeAudioFile generates a one-second silent audio file at dir/name and sets
// the provided metadata tags on it. The format is inferred from the file
// extension (.flac, .mp3, .m4a). ffmpeg must be installed and on PATH.
//
// For FLAC, use uppercase Vorbis comment key names (TITLE, ARTIST, ALBUMARTIST,
// DATE, TRACKNUMBER, DISCNUMBER). For MP3 and M4A, ffmpeg's lowercase generic
// keys (title, artist, album, album_artist, track, date) are more reliable.
func makeAudioFile(t *testing.T, dir, name string, tags map[string]string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	ext := strings.ToLower(filepath.Ext(name))

	var codec string
	switch ext {
	case ".flac":
		codec = "flac"
	case ".mp3":
		codec = "libmp3lame"
	case ".m4a":
		codec = "aac"
	default:
		t.Fatalf("makeAudioFile: unsupported extension %q", ext)
	}

	args := []string{
		"-y",
		"-f", "lavfi",
		"-i", "anullsrc=r=44100:cl=stereo",
		"-t", "1",
		"-c:a", codec,
	}
	for k, v := range tags {
		args = append(args, "-metadata", fmt.Sprintf("%s=%s", k, v))
	}
	args = append(args, path)

	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("makeAudioFile: ffmpeg failed: %v\n%s", err, out)
	}

	return path
}

func TestNewReader(t *testing.T) {
	// NewReader has no configurable state; this test confirms the constructor
	// returns a non-nil value and that two calls produce independent instances.
	r1 := NewReader()
	r2 := NewReader()
	assert.NotNil(t, r1)
	assert.NotNil(t, r2)
	assert.NotSame(t, r1, r2)
}

func TestReadTrack(t *testing.T) {
	r := NewReader()

	t.Run("reads all standard tags from FLAC", func(t *testing.T) {
		path := makeAudioFile(t, t.TempDir(), "track.flac", map[string]string{
			"TITLE":       "Track One",
			"ARTIST":      "Test Artist",
			"ALBUMARTIST": "Album Artist",
			"ALBUM":       "Test Album",
			"DATE":        "2003",
			"TRACKNUMBER": "3",
			"DISCNUMBER":  "1",
		})
		track := &Track{Path: path}
		require.NoError(t, r.ReadTrack(track))
		assert.Equal(t, "Track One", track.Title)
		assert.Equal(t, "Test Artist", track.Artist)
		assert.Equal(t, "Album Artist", track.AlbumArtist)
		assert.Equal(t, "Test Album", track.Album)
		assert.Equal(t, "2003", track.Year)
		require.NotNil(t, track.TrackNumber)
		assert.Equal(t, 3, *track.TrackNumber)
		assert.Equal(t, 1, track.DiscNumber)
	})

	t.Run("full ISO-8601 date is trimmed to year", func(t *testing.T) {
		path := makeAudioFile(t, t.TempDir(), "track.flac", map[string]string{
			"TITLE":  "Dated Track",
			"ARTIST": "Test Artist",
			"DATE":   "2003-01-14",
		})
		track := &Track{Path: path}
		require.NoError(t, r.ReadTrack(track))
		assert.Equal(t, "2003", track.Year)
	})

	t.Run("year-month date is trimmed to year", func(t *testing.T) {
		path := makeAudioFile(t, t.TempDir(), "track.flac", map[string]string{
			"TITLE":  "Dated Track",
			"ARTIST": "Test Artist",
			"DATE":   "2003-01",
		})
		track := &Track{Path: path}
		require.NoError(t, r.ReadTrack(track))
		assert.Equal(t, "2003", track.Year)
	})

	t.Run("absent optional tags leave zero values", func(t *testing.T) {
		// Only TITLE and ARTIST set; everything else should remain at zero.
		path := makeAudioFile(t, t.TempDir(), "track.flac", map[string]string{
			"TITLE":  "Minimal Track",
			"ARTIST": "Solo Artist",
		})
		track := &Track{Path: path}
		require.NoError(t, r.ReadTrack(track))
		assert.Equal(t, "Minimal Track", track.Title)
		assert.Equal(t, "Solo Artist", track.Artist)
		assert.Empty(t, track.AlbumArtist)
		assert.Empty(t, track.Album)
		assert.Empty(t, track.Year)
		assert.Nil(t, track.TrackNumber) // nil means tag was absent
		assert.Zero(t, track.DiscNumber)
	})

	t.Run("reads basic tags from MP3", func(t *testing.T) {
		// MP3 uses ffmpeg's lowercase generic keys which map to ID3v2 frames.
		path := makeAudioFile(t, t.TempDir(), "track.mp3", map[string]string{
			"title":  "MP3 Track",
			"artist": "MP3 Artist",
			"album":  "MP3 Album",
		})
		track := &Track{Path: path}
		require.NoError(t, r.ReadTrack(track))
		assert.Equal(t, "MP3 Track", track.Title)
		assert.Equal(t, "MP3 Artist", track.Artist)
		assert.Equal(t, "MP3 Album", track.Album)
	})

	t.Run("reads basic tags from M4A", func(t *testing.T) {
		// M4A uses ffmpeg's lowercase generic keys which map to MP4 atoms.
		path := makeAudioFile(t, t.TempDir(), "track.m4a", map[string]string{
			"title":  "M4A Track",
			"artist": "M4A Artist",
			"album":  "M4A Album",
		})
		track := &Track{Path: path}
		require.NoError(t, r.ReadTrack(track))
		assert.Equal(t, "M4A Track", track.Title)
		assert.Equal(t, "M4A Artist", track.Artist)
		assert.Equal(t, "M4A Album", track.Album)
	})

	t.Run("non-existent file returns an error", func(t *testing.T) {
		track := &Track{Path: "/nonexistent/track.flac"}
		assert.Error(t, r.ReadTrack(track))
	})
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
				{Artist: "Track Artist 1", AlbumArtist: "Main Artist", TrackNumber: new(1)},
				{Artist: "Track Artist 2", AlbumArtist: "Main Artist", TrackNumber: new(2)},
			},
			expected: "Main Artist",
		},
		{
			name: "AlbumArtist missing, fallback to lowest track number",
			tracks: []*Track{
				{Artist: "Secondary Artist", TrackNumber: new(2)},
				{Artist: "Primary Artist", TrackNumber: new(1)},
			},
			expected: "Primary Artist",
		},
		{
			// All tracks carry TrackNumber=0 (hidden/pre-gap tracks). None
			// qualify for the positive-track-number path so resolution falls
			// back to the first track in slice order.
			name: "AlbumArtist missing, all track numbers zero, fallback to first in slice",
			tracks: []*Track{
				{Artist: "First Track Artist", TrackNumber: new(0)},
				{Artist: "Second Track Artist", TrackNumber: new(0)},
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
				{Artist: "", TrackNumber: new(1)},
				{Artist: "The Real Artist", TrackNumber: new(2)},
			},
			expected: "The Real Artist",
		},
		{
			// nil TrackNumber (tag absent) is treated the same as 0 for sort
			// purposes and is skipped during the positive-track-number pass.
			name: "Tracks with nil TrackNumber fall back to slice order",
			tracks: []*Track{
				{Artist: "First Artist", TrackNumber: nil},
				{Artist: "Second Artist", TrackNumber: nil},
			},
			expected: "First Artist",
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

func TestProcessLibrary(t *testing.T) {
	t.Run("populates tags for all tracks in an album", func(t *testing.T) {
		root := t.TempDir()
		makeAudioFile(t, root, "01 track one.flac", map[string]string{
			"TITLE": "Track One", "ARTIST": "Test Artist",
			"ALBUMARTIST": "Album Artist", "ALBUM": "Test Album",
			"DATE": "2003", "TRACKNUMBER": "1",
		})
		makeAudioFile(t, root, "02 track two.flac", map[string]string{
			"TITLE": "Track Two", "ARTIST": "Test Artist",
			"ALBUMARTIST": "Album Artist", "ALBUM": "Test Album",
			"DATE": "2003", "TRACKNUMBER": "2",
		})

		albums, err := ProcessLibrary(root)
		require.NoError(t, err)
		require.Len(t, albums, 1)
		require.Len(t, albums[0].Tracks, 2)

		for _, tr := range albums[0].Tracks {
			assert.Equal(t, "Test Artist", tr.Artist)
			assert.Equal(t, "Album Artist", tr.AlbumArtist)
			assert.Equal(t, "Test Album", tr.Album)
			assert.Equal(t, "2003", tr.Year)
			assert.NotEmpty(t, tr.Title)
			assert.NotNil(t, tr.TrackNumber) // tag was present
		}
	})

	t.Run("falls back to track Artist when AlbumArtist is absent", func(t *testing.T) {
		root := t.TempDir()
		makeAudioFile(t, root, "01 track.flac", map[string]string{
			"TITLE": "Solo Track", "ARTIST": "The Artist",
			"ALBUM": "Solo Album", "TRACKNUMBER": "1",
		})

		albums, err := ProcessLibrary(root)
		require.NoError(t, err)
		require.Len(t, albums, 1)
		// ResolvedArtist is populated by ProcessLibrary via ResolveAlbumArtist.
		assert.Equal(t, "The Artist", albums[0].ResolvedArtist)
	})

	t.Run("unreadable track is skipped but album still returned", func(t *testing.T) {
		root := t.TempDir()
		makeAudioFile(t, root, "01 good.flac", map[string]string{
			"TITLE": "Good Track", "ARTIST": "Good Artist",
		})
		// A zero-byte file with a .flac extension is discovered by the scanner
		// but taglib cannot parse it, exercising the warning-and-continue path.
		require.NoError(t, os.WriteFile(
			filepath.Join(root, "02 bad.flac"), []byte{}, 0o644,
		))

		albums, err := ProcessLibrary(root)
		require.NoError(t, err)
		require.Len(t, albums, 1)
		assert.Len(t, albums[0].Tracks, 2)

		var good *Track
		for _, tr := range albums[0].Tracks {
			if tr.Title == "Good Track" {
				good = tr
				break
			}
		}
		require.NotNil(t, good, "good track should have its tags populated")
		assert.Equal(t, "Good Artist", good.Artist)
	})

	t.Run("multiple nested albums are all processed", func(t *testing.T) {
		root := t.TempDir()
		albumA := filepath.Join(root, "a", "artist a", "2001 album a")
		albumB := filepath.Join(root, "b", "artist b", "2002 album b")
		require.NoError(t, os.MkdirAll(albumA, 0o755))
		require.NoError(t, os.MkdirAll(albumB, 0o755))

		makeAudioFile(t, albumA, "01 track.flac", map[string]string{
			"TITLE": "A Track", "ARTIST": "Artist A",
		})
		makeAudioFile(t, albumB, "01 track.flac", map[string]string{
			"TITLE": "B Track", "ARTIST": "Artist B",
		})

		albums, err := ProcessLibrary(root)
		require.NoError(t, err)
		assert.Len(t, albums, 2)
	})
}
