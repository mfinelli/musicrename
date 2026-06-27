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

package checker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.senan.xyz/taglib"

	"github.com/mfinelli/musicrename/internal/hasher"
	"github.com/mfinelli/musicrename/internal/metadata"
)

// makeCheckerAlbum constructs a metadata.Album with ResolvedArtist already
// set, bypassing ProcessLibrary so checker unit tests remain self-contained.
// Mirrors the makeAlbum helper in the planner tests; redefined here because
// _test.go helpers cannot be imported across packages.
func makeCheckerAlbum(
	rootPath, resolvedArtist string,
	tracks []*metadata.Track,
	assets map[metadata.FileCategory][]string,
) *metadata.Album {
	a := metadata.NewAlbum(rootPath)
	a.ResolvedArtist = resolvedArtist
	a.Tracks = tracks
	if assets != nil {
		a.Assets = assets
	}
	return a
}

// makeAudioFile generates a one-second silent audio file at dir/name and sets
// the provided metadata tags on it. The format is inferred from the file
// extension (.flac, .mp3, .m4a). ffmpeg must be on PATH.
//
// Mirrors the helper in the metadata package; redefined here because _test.go
// helpers cannot be imported across packages.
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

// minimalPNG is a 1×1 white-pixel PNG used to embed artwork in test audio
// files. Using a real PNG rather than arbitrary bytes ensures that taglib
// recognises it as a valid image when writing via WriteImage.
var minimalPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
	0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41,
	0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
	0x00, 0x00, 0x02, 0x00, 0x01, 0xe2, 0x21, 0xbc,
	0x33, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
	0x44, 0xae, 0x42, 0x60, 0x82,
}

// findWarning returns the first Warning in ar.Warnings whose Message contains
// substr, or nil if no such warning exists.
func findWarning(ar *AlbumResult, substr string) *Warning {
	for i, w := range ar.Warnings {
		if strings.Contains(w.Message, substr) {
			return &ar.Warnings[i]
		}
	}
	return nil
}

func TestResult_HasWarnings(t *testing.T) {
	t.Run("false when no albums", func(t *testing.T) {
		assert.False(t, (&Result{}).HasWarnings())
	})

	t.Run("false when all albums have empty warning slices", func(t *testing.T) {
		r := &Result{Albums: []AlbumResult{
			{AlbumPath: "/a"},
			{AlbumPath: "/b"},
		}}
		assert.False(t, r.HasWarnings())
	})

	t.Run("true when any album has at least one warning", func(t *testing.T) {
		r := &Result{Albums: []AlbumResult{
			{AlbumPath: "/a"},
			{AlbumPath: "/b", Warnings: []Warning{{Path: "/b/t.flac", Message: "test"}}},
		}}
		assert.True(t, r.HasWarnings())
	})
}

func TestCheckTrackTags_MissingTitle(t *testing.T) {
	t.Run("empty TITLE produces warning", func(t *testing.T) {
		track := &metadata.Track{Path: "/a/t.flac", Title: "", Artist: "A", TrackNumber: new(1), Year: "2000"}
		ar := &AlbumResult{}
		checkTrackTags(track, ar)
		assert.NotNil(t, findWarning(ar, "TITLE"))
	})

	t.Run("non-empty TITLE produces no warning", func(t *testing.T) {
		track := &metadata.Track{Path: "/a/t.flac", Title: "Track", Artist: "A", TrackNumber: new(1), Year: "2000"}
		ar := &AlbumResult{}
		checkTrackTags(track, ar)
		assert.Nil(t, findWarning(ar, "TITLE"))
	})
}

func TestCheckTrackTags_MissingTrackNumber(t *testing.T) {
	t.Run("nil TrackNumber produces warning", func(t *testing.T) {
		track := &metadata.Track{Path: "/a/t.flac", Title: "T", Artist: "A", TrackNumber: nil, Year: "2000"}
		ar := &AlbumResult{}
		checkTrackTags(track, ar)
		assert.NotNil(t, findWarning(ar, "TRACKNUMBER"))
	})

	t.Run("zero TrackNumber (hidden track) produces no warning", func(t *testing.T) {
		track := &metadata.Track{Path: "/a/t.flac", Title: "T", Artist: "A", TrackNumber: new(0), Year: "2000"}
		ar := &AlbumResult{}
		checkTrackTags(track, ar)
		assert.Nil(t, findWarning(ar, "TRACKNUMBER"))
	})
}

func TestCheckTrackTags_MissingDate(t *testing.T) {
	t.Run("empty Year produces warning", func(t *testing.T) {
		track := &metadata.Track{Path: "/a/t.flac", Title: "T", Artist: "A", TrackNumber: new(1), Year: ""}
		ar := &AlbumResult{}
		checkTrackTags(track, ar)
		assert.NotNil(t, findWarning(ar, "DATE"))
	})

	t.Run("non-empty Year produces no warning", func(t *testing.T) {
		track := &metadata.Track{Path: "/a/t.flac", Title: "T", Artist: "A", TrackNumber: new(1), Year: "2000"}
		ar := &AlbumResult{}
		checkTrackTags(track, ar)
		assert.Nil(t, findWarning(ar, "DATE"))
	})
}

func TestCheckTrackTags_MissingArtist(t *testing.T) {
	t.Run("both Artist and AlbumArtist empty produces warning", func(t *testing.T) {
		track := &metadata.Track{Path: "/a/t.flac", Title: "T", Artist: "", AlbumArtist: "", TrackNumber: new(1), Year: "2000"}
		ar := &AlbumResult{}
		checkTrackTags(track, ar)
		assert.NotNil(t, findWarning(ar, "ARTIST"))
	})

	t.Run("Artist set but AlbumArtist empty produces no warning", func(t *testing.T) {
		track := &metadata.Track{Path: "/a/t.flac", Title: "T", Artist: "A", AlbumArtist: "", TrackNumber: new(1), Year: "2000"}
		ar := &AlbumResult{}
		checkTrackTags(track, ar)
		assert.Nil(t, findWarning(ar, "ARTIST"))
	})

	t.Run("AlbumArtist set but Artist empty produces no warning", func(t *testing.T) {
		track := &metadata.Track{Path: "/a/t.flac", Title: "T", Artist: "", AlbumArtist: "VA", TrackNumber: new(1), Year: "2000"}
		ar := &AlbumResult{}
		checkTrackTags(track, ar)
		assert.Nil(t, findWarning(ar, "ARTIST"))
	})
}

func TestCheckTrackTags_WellTaggedTrack(t *testing.T) {
	t.Run("fully tagged track produces no warnings", func(t *testing.T) {
		track := &metadata.Track{
			Path:        "/a/t.flac",
			Title:       "Track",
			Artist:      "Artist",
			AlbumArtist: "Artist",
			TrackNumber: new(1),
			Year:        "2000",
		}
		ar := &AlbumResult{}
		checkTrackTags(track, ar)
		assert.Empty(t, ar.Warnings)
	})
}

func TestCheckAlbumTags_InconsistentAlbumArtist(t *testing.T) {
	t.Run("differing ALBUMARTIST across tracks produces warning", func(t *testing.T) {
		album := makeCheckerAlbum("/a", "Artist", []*metadata.Track{
			{Path: "/a/t1.flac", AlbumArtist: "Artist A", TrackNumber: new(1)},
			{Path: "/a/t2.flac", AlbumArtist: "Artist B", TrackNumber: new(2)},
		}, nil)
		ar := &AlbumResult{AlbumPath: "/a"}
		checkAlbumTags(album, ar)
		assert.NotNil(t, findWarning(ar, "ALBUMARTIST"))
	})

	t.Run("consistent ALBUMARTIST produces no warning", func(t *testing.T) {
		album := makeCheckerAlbum("/a", "Artist", []*metadata.Track{
			{Path: "/a/t1.flac", AlbumArtist: "Artist", TrackNumber: new(1)},
			{Path: "/a/t2.flac", AlbumArtist: "Artist", TrackNumber: new(2)},
		}, nil)
		ar := &AlbumResult{AlbumPath: "/a"}
		checkAlbumTags(album, ar)
		assert.Nil(t, findWarning(ar, "ALBUMARTIST"))
	})
}

func TestCheckAlbumTags_InconsistentAlbum(t *testing.T) {
	t.Run("differing ALBUM across tracks produces warning", func(t *testing.T) {
		album := makeCheckerAlbum("/a", "Artist", []*metadata.Track{
			{Path: "/a/t1.flac", Album: "Album A", TrackNumber: new(1)},
			{Path: "/a/t2.flac", Album: "Album B", TrackNumber: new(2)},
		}, nil)
		ar := &AlbumResult{AlbumPath: "/a"}
		checkAlbumTags(album, ar)
		assert.NotNil(t, findWarning(ar, "ALBUM"))
	})
}

func TestCheckAlbumTags_PartialDiscNumber(t *testing.T) {
	t.Run("some tracks missing DISCNUMBER produces warning", func(t *testing.T) {
		album := makeCheckerAlbum("/a", "Artist", []*metadata.Track{
			{Path: "/a/t1.flac", DiscNumber: 1, TrackNumber: new(1)},
			{Path: "/a/t2.flac", DiscNumber: 0, TrackNumber: new(2)},
		}, nil)
		ar := &AlbumResult{AlbumPath: "/a"}
		checkAlbumTags(album, ar)
		w := findWarning(ar, "DISCNUMBER")
		require.NotNil(t, w)
		assert.Contains(t, w.Message, "partial")
	})

	t.Run("all tracks having DISCNUMBER produces no warning", func(t *testing.T) {
		album := makeCheckerAlbum("/a", "Artist", []*metadata.Track{
			{Path: "/a/t1.flac", DiscNumber: 1, TrackNumber: new(1)},
			{Path: "/a/t2.flac", DiscNumber: 1, TrackNumber: new(2)},
		}, nil)
		ar := &AlbumResult{AlbumPath: "/a"}
		checkAlbumTags(album, ar)
		assert.Nil(t, findWarning(ar, "DISCNUMBER"))
	})

	t.Run("no tracks having DISCNUMBER produces no warning", func(t *testing.T) {
		album := makeCheckerAlbum("/a", "Artist", []*metadata.Track{
			{Path: "/a/t1.flac", DiscNumber: 0, TrackNumber: new(1)},
			{Path: "/a/t2.flac", DiscNumber: 0, TrackNumber: new(2)},
		}, nil)
		ar := &AlbumResult{AlbumPath: "/a"}
		checkAlbumTags(album, ar)
		assert.Nil(t, findWarning(ar, "DISCNUMBER"))
	})
}

func TestCheckAlbumTags_DuplicateTrackNumbers(t *testing.T) {
	t.Run("two tracks with the same number produce warning on second", func(t *testing.T) {
		album := makeCheckerAlbum("/a", "Artist", []*metadata.Track{
			{Path: "/a/t1a.flac", TrackNumber: new(3)},
			{Path: "/a/t1b.flac", TrackNumber: new(3)},
		}, nil)
		ar := &AlbumResult{AlbumPath: "/a"}
		checkAlbumTags(album, ar)
		w := findWarning(ar, "duplicate track number")
		require.NotNil(t, w)
		assert.Equal(t, "/a/t1b.flac", w.Path)
	})

	t.Run("same track number on different discs is not a duplicate", func(t *testing.T) {
		album := makeCheckerAlbum("/a", "Artist", []*metadata.Track{
			{Path: "/a/d1t1.flac", DiscNumber: 1, TrackNumber: new(1)},
			{Path: "/a/d2t1.flac", DiscNumber: 2, TrackNumber: new(1)},
		}, nil)
		ar := &AlbumResult{AlbumPath: "/a"}
		checkAlbumTags(album, ar)
		assert.Nil(t, findWarning(ar, "duplicate"))
	})

	t.Run("tracks with nil TrackNumber are excluded from duplicate detection", func(t *testing.T) {
		// Two tracks without track numbers should not be flagged as duplicates
		// of each other; the missing tag is already flagged by checkTrackTags.
		album := makeCheckerAlbum("/a", "Artist", []*metadata.Track{
			{Path: "/a/t1.flac", TrackNumber: nil},
			{Path: "/a/t2.flac", TrackNumber: nil},
		}, nil)
		ar := &AlbumResult{AlbumPath: "/a"}
		checkAlbumTags(album, ar)
		assert.Nil(t, findWarning(ar, "duplicate"))
	})

	t.Run("single-track album produces no duplicate warning", func(t *testing.T) {
		album := makeCheckerAlbum("/a", "Artist", []*metadata.Track{
			{Path: "/a/t.flac", TrackNumber: new(1)},
		}, nil)
		ar := &AlbumResult{AlbumPath: "/a"}
		checkAlbumTags(album, ar)
		assert.Nil(t, findWarning(ar, "duplicate"))
	})
}

func TestCheckAlbumTags_EmptyAlbum(t *testing.T) {
	t.Run("album with no tracks produces no warnings", func(t *testing.T) {
		album := makeCheckerAlbum("/a", "Artist", nil, nil)
		ar := &AlbumResult{AlbumPath: "/a"}
		checkAlbumTags(album, ar)
		assert.Empty(t, ar.Warnings)
	})
}

func TestCheckTrackAudio_ReplayGain(t *testing.T) {
	t.Run("missing REPLAYGAIN_TRACK_GAIN produces warning", func(t *testing.T) {
		path := makeAudioFile(t, t.TempDir(), "track.flac", map[string]string{
			"TITLE":  "Track",
			"ARTIST": "Artist",
			// Intentionally no ReplayGain tags.
		})
		track := &metadata.Track{Path: path}
		ar := &AlbumResult{}
		checkTrackAudio(track, ar)
		assert.NotNil(t, findWarning(ar, "REPLAYGAIN_TRACK_GAIN"))
	})

	t.Run("missing REPLAYGAIN_ALBUM_GAIN produces warning", func(t *testing.T) {
		path := makeAudioFile(t, t.TempDir(), "track.flac", map[string]string{
			"TITLE":                 "Track",
			"ARTIST":                "Artist",
			"REPLAYGAIN_TRACK_GAIN": "+0.50 dB",
			// Intentionally no REPLAYGAIN_ALBUM_GAIN.
		})
		track := &metadata.Track{Path: path}
		ar := &AlbumResult{}
		checkTrackAudio(track, ar)
		assert.Nil(t, findWarning(ar, "REPLAYGAIN_TRACK_GAIN"))
		assert.NotNil(t, findWarning(ar, "REPLAYGAIN_ALBUM_GAIN"))
	})

	t.Run("both ReplayGain tags present produces no ReplayGain warnings", func(t *testing.T) {
		path := makeAudioFile(t, t.TempDir(), "track.flac", map[string]string{
			"TITLE":                 "Track",
			"ARTIST":                "Artist",
			"REPLAYGAIN_TRACK_GAIN": "+0.50 dB",
			"REPLAYGAIN_ALBUM_GAIN": "+1.00 dB",
		})
		track := &metadata.Track{Path: path}
		ar := &AlbumResult{}
		checkTrackAudio(track, ar)
		assert.Nil(t, findWarning(ar, "REPLAYGAIN_TRACK_GAIN"))
		assert.Nil(t, findWarning(ar, "REPLAYGAIN_ALBUM_GAIN"))
	})
}

func TestCheckTrackAudio_EmbeddedArtwork(t *testing.T) {
	t.Run("embedded artwork produces warning", func(t *testing.T) {
		path := makeAudioFile(t, t.TempDir(), "track.flac", map[string]string{
			"TITLE":  "Track",
			"ARTIST": "Artist",
		})
		require.NoError(t, taglib.WriteImage(path, minimalPNG))

		track := &metadata.Track{Path: path}
		ar := &AlbumResult{}
		checkTrackAudio(track, ar)
		assert.NotNil(t, findWarning(ar, "embedded artwork"))
	})

	t.Run("no embedded artwork produces no warning", func(t *testing.T) {
		path := makeAudioFile(t, t.TempDir(), "track.flac", map[string]string{
			"TITLE":  "Track",
			"ARTIST": "Artist",
		})
		track := &metadata.Track{Path: path}
		ar := &AlbumResult{}
		checkTrackAudio(track, ar)
		assert.Nil(t, findWarning(ar, "embedded artwork"))
	})
}

func TestCheckTrackAudio_UnreadableFile(t *testing.T) {
	t.Run("unreadable file is silently skipped", func(t *testing.T) {
		// The primary scan phase warns about unreadable files; checkTrackAudio
		// must not add a duplicate warning.
		track := &metadata.Track{Path: "/nonexistent/track.flac"}
		ar := &AlbumResult{}
		checkTrackAudio(track, ar)
		assert.Empty(t, ar.Warnings)
	})
}

func TestCheckArtwork(t *testing.T) {
	t.Run("no primary art produces warning", func(t *testing.T) {
		album := makeCheckerAlbum("/a", "Artist", nil, nil)
		// No CatPrimaryArt in Assets (zero value map has no key).
		ar := &AlbumResult{AlbumPath: "/a"}
		checkArtwork(album, ar)
		assert.NotNil(t, findWarning(ar, "missing primary artwork"))
	})

	t.Run("exactly one primary art file produces no warning", func(t *testing.T) {
		album := makeCheckerAlbum("/a", "Artist", nil, map[metadata.FileCategory][]string{
			metadata.CatPrimaryArt: {"/a/folder.jpg"},
		})
		ar := &AlbumResult{AlbumPath: "/a"}
		checkArtwork(album, ar)
		assert.Empty(t, ar.Warnings)
	})

	t.Run("multiple primary art files produce warning", func(t *testing.T) {
		album := makeCheckerAlbum("/a", "Artist", nil, map[metadata.FileCategory][]string{
			metadata.CatPrimaryArt: {"/a/folder.jpg", "/a/folder.png"},
		})
		ar := &AlbumResult{AlbumPath: "/a"}
		checkArtwork(album, ar)
		assert.NotNil(t, findWarning(ar, "multiple primary artwork"))
	})
}

func TestCheckIntegrity(t *testing.T) {
	t.Run("missing sums.md5 produces warning", func(t *testing.T) {
		dir := t.TempDir()
		album := makeCheckerAlbum(dir, "Artist", nil, nil)
		ar := &AlbumResult{AlbumPath: dir}
		checkIntegrity(album, ar)
		w := findWarning(ar, hasher.SumsFilename)
		require.NotNil(t, w)
		assert.Equal(t, dir, w.Path)
	})

	t.Run("present sums.md5 produces no warning", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, hasher.SumsFilename), []byte(""), 0o644))
		album := makeCheckerAlbum(dir, "Artist", nil, nil)
		ar := &AlbumResult{AlbumPath: dir}
		checkIntegrity(album, ar)
		assert.Empty(t, ar.Warnings)
	})
}

func TestCheckUnknownFiles(t *testing.T) {
	t.Run("CatUnknown file produces warning with its path", func(t *testing.T) {
		unknownPath := "/a/mystery.exe"
		album := makeCheckerAlbum("/a", "Artist", nil, map[metadata.FileCategory][]string{
			metadata.CatUnknown: {unknownPath},
		})
		ar := &AlbumResult{AlbumPath: "/a"}
		checkUnknownFiles(album, ar)
		require.Len(t, ar.Warnings, 1)
		assert.Equal(t, unknownPath, ar.Warnings[0].Path)
		assert.Contains(t, ar.Warnings[0].Message, "unknown file")
	})

	t.Run("CatExtras file produces no warning", func(t *testing.T) {
		// Files inside extras/ are CatExtras, not CatUnknown (they should
		// never be flagged here).
		album := makeCheckerAlbum("/a", "Artist", nil, map[metadata.FileCategory][]string{
			metadata.CatExtras: {"/a/extras/booklet.pdf"},
		})
		ar := &AlbumResult{AlbumPath: "/a"}
		checkUnknownFiles(album, ar)
		assert.Empty(t, ar.Warnings)
	})

	t.Run("album with no unknown files produces no warning", func(t *testing.T) {
		album := makeCheckerAlbum("/a", "Artist", nil, nil)
		ar := &AlbumResult{AlbumPath: "/a"}
		checkUnknownFiles(album, ar)
		assert.Empty(t, ar.Warnings)
	})
}

func TestCheckNaming(t *testing.T) {
	t.Run("skipped when libraryRoot is empty", func(t *testing.T) {
		// Even a completely wrong path should produce no warnings when there
		// is no library root to compute expected paths against.
		album := makeCheckerAlbum("/wrong/path", "Artist", []*metadata.Track{
			{Path: "/wrong/path/BAD FILENAME.flac", Title: "Track", Album: "Album", Year: "2000", TrackNumber: new(1)},
		}, nil)
		ar := &AlbumResult{AlbumPath: "/wrong/path"}
		checkNaming(album, "", ar)
		assert.Empty(t, ar.Warnings)
	})

	t.Run("no warnings when album and files are at the correct paths", func(t *testing.T) {
		lib := t.TempDir()
		// Artist "Artist" -> "artist", bucket "a", album "[2000] album"
		albumDir := filepath.Join(lib, "a", "artist", "[2000] album")
		require.NoError(t, os.MkdirAll(albumDir, 0o755))
		trackPath := filepath.Join(albumDir, "01 track.flac")

		album := makeCheckerAlbum(albumDir, "Artist", []*metadata.Track{
			{Path: trackPath, Title: "Track", Album: "Album", Year: "2000", TrackNumber: new(1)},
		}, nil)
		ar := &AlbumResult{AlbumPath: albumDir}
		checkNaming(album, lib, ar)
		assert.Empty(t, ar.Warnings)
	})

	t.Run("warning when album directory does not match spec", func(t *testing.T) {
		lib := t.TempDir()
		// Wrong: no year bracket, wrong casing.
		albumDir := filepath.Join(lib, "a", "artist", "Album")
		require.NoError(t, os.MkdirAll(albumDir, 0o755))
		trackPath := filepath.Join(albumDir, "01 track.flac")

		album := makeCheckerAlbum(albumDir, "Artist", []*metadata.Track{
			{Path: trackPath, Title: "Track", Album: "Album", Year: "2000", TrackNumber: new(1)},
		}, nil)
		ar := &AlbumResult{AlbumPath: albumDir}
		checkNaming(album, lib, ar)
		assert.NotNil(t, findWarning(ar, "album directory does not match spec"))
	})

	t.Run("warning when track filename does not match spec", func(t *testing.T) {
		lib := t.TempDir()
		albumDir := filepath.Join(lib, "a", "artist", "[2000] album")
		require.NoError(t, os.MkdirAll(albumDir, 0o755))
		// Correct dir, wrong filename (unsanitized title in name).
		trackPath := filepath.Join(albumDir, "Track One.flac")

		album := makeCheckerAlbum(albumDir, "Artist", []*metadata.Track{
			{Path: trackPath, Title: "Track One", Album: "Album", Year: "2000", TrackNumber: new(1)},
		}, nil)
		ar := &AlbumResult{AlbumPath: albumDir}
		checkNaming(album, lib, ar)
		w := findWarning(ar, "filename does not match spec")
		require.NotNil(t, w)
		assert.Equal(t, trackPath, w.Path)
		assert.Contains(t, w.Message, "01 track one.flac")
	})

	t.Run("planning error produces a single conformance-check warning", func(t *testing.T) {
		lib := t.TempDir()
		// Empty ResolvedArtist -> PlanAlbum will error.
		album := makeCheckerAlbum("/a", "", []*metadata.Track{
			{Path: "/a/t.flac", Title: "T", Album: "A", Year: "2000", TrackNumber: new(1)},
		}, nil)
		ar := &AlbumResult{AlbumPath: "/a"}
		checkNaming(album, lib, ar)
		assert.NotNil(t, findWarning(ar, "could not compute expected path"))
	})
}

func TestCheckLibrary(t *testing.T) {
	t.Run("empty library returns empty result with no warnings", func(t *testing.T) {
		root := t.TempDir()
		result, err := CheckLibrary(root)
		require.NoError(t, err)
		assert.Empty(t, result.Albums)
		assert.False(t, result.HasWarnings())
	})

	t.Run("well-formed album produces warnings only for missing sums and artwork", func(t *testing.T) {
		// After rename, a typical album still won't have sums.md5 or
		// artwork until sums/lyrics are run. We verify those are the only
		// warnings for an otherwise correct album.
		lib := t.TempDir()
		albumDir := filepath.Join(lib, "a", "artist", "[2000] album")
		require.NoError(t, os.MkdirAll(albumDir, 0o755))

		makeAudioFile(t, albumDir, "01 track.flac", map[string]string{
			"TITLE":                 "Track",
			"ARTIST":                "Artist",
			"ALBUMARTIST":           "Artist",
			"ALBUM":                 "Album",
			"DATE":                  "2000",
			"TRACKNUMBER":           "1",
			"REPLAYGAIN_TRACK_GAIN": "+0.50 dB",
			"REPLAYGAIN_ALBUM_GAIN": "+1.00 dB",
		})

		result, err := CheckLibrary(lib)
		require.NoError(t, err)
		require.Len(t, result.Albums, 1)

		ar := result.Albums[0]
		// Expect exactly two warnings: missing artwork + missing sums.md5.
		messages := make([]string, len(ar.Warnings))
		for i, w := range ar.Warnings {
			messages[i] = w.Message
		}
		assert.Len(t, ar.Warnings, 2, "unexpected warnings: %v", messages)
		assert.NotNil(t, findWarning(&ar, "primary artwork"))
		assert.NotNil(t, findWarning(&ar, hasher.SumsFilename))
	})

	t.Run("multiple albums are all checked", func(t *testing.T) {
		lib := t.TempDir()
		for _, sub := range []string{
			filepath.Join("a", "artist a", "[2000] album a"),
			filepath.Join("b", "artist b", "[2001] album b"),
		} {
			dir := filepath.Join(lib, sub)
			require.NoError(t, os.MkdirAll(dir, 0o755))
			makeAudioFile(t, dir, "01 track.flac", map[string]string{
				"TITLE": "Track", "ARTIST": "Artist",
			})
		}
		result, err := CheckLibrary(lib)
		require.NoError(t, err)
		assert.Len(t, result.Albums, 2)
	})
}

func TestCheckAlbum(t *testing.T) {
	t.Run("no audio files in directory returns error", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.txt"), []byte(""), 0o644))
		_, err := CheckAlbum(dir, "")
		assert.Error(t, err)
	})

	t.Run("directory with audio returns an AlbumResult", func(t *testing.T) {
		dir := t.TempDir()
		makeAudioFile(t, dir, "01 track.flac", map[string]string{
			"TITLE": "Track", "ARTIST": "Artist", "TRACKNUMBER": "1", "DATE": "2000",
		})
		ar, err := CheckAlbum(dir, "")
		require.NoError(t, err)
		assert.Equal(t, dir, ar.AlbumPath)
	})

	t.Run("empty libraryRoot skips path conformance check", func(t *testing.T) {
		// Even with a badly-named directory, no naming warnings should appear
		// when libraryRoot is empty.
		dir := t.TempDir()
		makeAudioFile(t, dir, "BAD FILENAME.flac", map[string]string{
			"TITLE": "Track", "ARTIST": "Artist", "TRACKNUMBER": "1", "DATE": "2000",
			"ALBUMARTIST": "Artist", "ALBUM": "Album",
			"REPLAYGAIN_TRACK_GAIN": "+0.5 dB", "REPLAYGAIN_ALBUM_GAIN": "+1.0 dB",
		})
		require.NoError(t, os.WriteFile(filepath.Join(dir, "folder.jpg"), []byte(""), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, hasher.SumsFilename), []byte(""), 0o644))

		ar, err := CheckAlbum(dir, "")
		require.NoError(t, err)
		assert.Nil(t, findWarning(ar, "filename does not match spec"))
		assert.Nil(t, findWarning(ar, "album directory"))
	})
}

func TestCheckTrack(t *testing.T) {
	t.Run("well-tagged track produces no warnings", func(t *testing.T) {
		path := makeAudioFile(t, t.TempDir(), "01 track.flac", map[string]string{
			"TITLE":                 "Track",
			"ARTIST":                "Artist",
			"ALBUMARTIST":           "Artist",
			"ALBUM":                 "Album",
			"DATE":                  "2000",
			"TRACKNUMBER":           "1",
			"REPLAYGAIN_TRACK_GAIN": "+0.50 dB",
			"REPLAYGAIN_ALBUM_GAIN": "+1.00 dB",
		})
		ar, err := CheckTrack(path)
		require.NoError(t, err)
		assert.Empty(t, ar.Warnings)
	})

	t.Run("missing tags produce per-track warnings", func(t *testing.T) {
		path := makeAudioFile(t, t.TempDir(), "track.flac", map[string]string{
			// No TITLE, no TRACKNUMBER, no DATE, no ARTIST/ALBUMARTIST.
		})
		ar, err := CheckTrack(path)
		require.NoError(t, err)
		assert.NotNil(t, findWarning(ar, "TITLE"))
		assert.NotNil(t, findWarning(ar, "TRACKNUMBER"))
		assert.NotNil(t, findWarning(ar, "DATE"))
		assert.NotNil(t, findWarning(ar, "ARTIST"))
	})

	t.Run("non-existent file returns error", func(t *testing.T) {
		_, err := CheckTrack("/nonexistent/track.flac")
		assert.Error(t, err)
	})

	t.Run("AlbumPath is set to the file's parent directory", func(t *testing.T) {
		dir := t.TempDir()
		path := makeAudioFile(t, dir, "track.flac", map[string]string{"TITLE": "T", "ARTIST": "A"})
		ar, err := CheckTrack(path)
		require.NoError(t, err)
		assert.Equal(t, dir, ar.AlbumPath)
	})

	t.Run("mismatched filename produces warning", func(t *testing.T) {
		// "01 Hells Bells.flac" should be "01 hells bells.flac" after sanitization.
		path := makeAudioFile(t, t.TempDir(), "01 Hells Bells.flac", map[string]string{
			"TITLE":                 "Hells Bells",
			"ARTIST":                "AC/DC",
			"ALBUMARTIST":           "AC/DC",
			"ALBUM":                 "Back in Black",
			"DATE":                  "1980",
			"TRACKNUMBER":           "1",
			"REPLAYGAIN_TRACK_GAIN": "+0.50 dB",
			"REPLAYGAIN_ALBUM_GAIN": "+1.00 dB",
		})
		ar, err := CheckTrack(path)
		require.NoError(t, err)
		w := findWarning(ar, "filename does not match spec")
		require.NotNil(t, w)
		assert.Contains(t, w.Message, "01 hells bells.flac")
	})

	t.Run("correct filename produces no filename warning", func(t *testing.T) {
		path := makeAudioFile(t, t.TempDir(), "01 track.flac", map[string]string{
			"TITLE":                 "Track",
			"ARTIST":                "Artist",
			"ALBUMARTIST":           "Artist",
			"ALBUM":                 "Album",
			"DATE":                  "2000",
			"TRACKNUMBER":           "1",
			"REPLAYGAIN_TRACK_GAIN": "+0.50 dB",
			"REPLAYGAIN_ALBUM_GAIN": "+1.00 dB",
		})
		ar, err := CheckTrack(path)
		require.NoError(t, err)
		assert.Nil(t, findWarning(ar, "filename does not match spec"))
	})

	t.Run("missing artist skips filename check without panicking", func(t *testing.T) {
		path := makeAudioFile(t, t.TempDir(), "BAD NAME.flac", map[string]string{
			"TITLE": "Track",
			// No ARTIST or ALBUMARTIST (can't plan, should skip silently).
		})
		ar, err := CheckTrack(path)
		require.NoError(t, err)
		// Missing-artist warning present, but no filename conformance warning.
		assert.NotNil(t, findWarning(ar, "ARTIST"))
		assert.Nil(t, findWarning(ar, "filename does not match spec"))
	})
}
