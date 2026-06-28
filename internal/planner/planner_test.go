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

package planner

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/metadata"
)

// makeAlbum constructs a metadata.Album with ResolvedArtist already set,
// bypassing ProcessLibrary so planner unit tests remain self-contained.
func makeAlbum(
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

// findMove returns the MoveOperation whose OldPath matches oldPath within the
// given AlbumPlan, or nil if no such operation exists.
func findMove(plan *AlbumPlan, oldPath string) *MoveOperation {
	for i, op := range plan.Moves {
		if op.OldPath == oldPath {
			return &plan.Moves[i]
		}
	}
	return nil
}

func TestNew(t *testing.T) {
	p := New("/library")
	assert.NotNil(t, p)
}

func TestPlanAlbum(t *testing.T) {
	t.Run("produces same result as PlanLibrary for a single album", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum("/src/beyonce", "Beyoncé", []*metadata.Track{
			{
				Path:        "/src/beyonce/01 crazy in love.flac",
				Title:       "Crazy In Love",
				Album:       "Dangerously In Love",
				Year:        "2003",
				TrackNumber: new(1),
			},
		}, nil)

		viaLibrary, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)
		require.Len(t, viaLibrary.Albums, 1)

		viaAlbum, err := PlanAlbum(lib, album)
		require.NoError(t, err)

		// Both routes must agree on the album-level fields and moves.
		assert.Equal(t, viaLibrary.Albums[0].AlbumArtist, viaAlbum.AlbumArtist)
		assert.Equal(t, viaLibrary.Albums[0].AlbumName, viaAlbum.AlbumName)
		assert.Equal(t, viaLibrary.Albums[0].DestDir, viaAlbum.DestDir)
		assert.Equal(t, viaLibrary.Albums[0].SourceDir, viaAlbum.SourceDir)
		require.Len(t, viaAlbum.Moves, len(viaLibrary.Albums[0].Moves))
		assert.Equal(t, viaLibrary.Albums[0].Moves[0].NewPath, viaAlbum.Moves[0].NewPath)
	})

	t.Run("does not detect cross-call collisions", func(t *testing.T) {
		// Two albums that resolve to the same destination would error in
		// PlanLibrary (shared destMap) but must each succeed independently in
		// PlanAlbum (fresh destMap per call).
		lib := t.TempDir()
		album := makeAlbum("/src", "Artist", []*metadata.Track{
			{Path: "/src/01.flac", Title: "Track", Album: "Album", Year: "2000", TrackNumber: new(1)},
		}, nil)

		_, err := PlanAlbum(lib, album)
		require.NoError(t, err)

		// A second independent call for the same album must also succeed.
		_, err = PlanAlbum(lib, album)
		require.NoError(t, err)
	})

	t.Run("propagates error for missing artist", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum("/src", "", []*metadata.Track{
			{Path: "/src/01.flac", Title: "T", Album: "A", Year: "2000", TrackNumber: new(1)},
		}, nil)

		_, err := PlanAlbum(lib, album)
		assert.Error(t, err)
	})

	t.Run("propagates error for inconsistent DISCNUMBER", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum("/src", "Artist", []*metadata.Track{
			{Path: "/src/t1.flac", Title: "One", Album: "A", Year: "2000", TrackNumber: new(1), DiscNumber: 1},
			{Path: "/src/t2.flac", Title: "Two", Album: "A", Year: "2000", TrackNumber: new(2), DiscNumber: 0},
		}, nil)

		_, err := PlanAlbum(lib, album)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "DISCNUMBER")
	})
}

func TestPlanLibrary_PathGeneration(t *testing.T) {
	t.Run("standard path with year and letter bucket", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum("/src/beyonce", "Beyoncé", []*metadata.Track{
			{
				Path:        "/src/beyonce/01 crazy in love.flac",
				Title:       "Crazy In Love",
				Album:       "Dangerously In Love",
				Year:        "2003",
				TrackNumber: new(1),
			},
		}, nil)

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)
		require.Len(t, plan.Albums, 1)

		op := findMove(&plan.Albums[0], "/src/beyonce/01 crazy in love.flac")
		require.NotNil(t, op)
		assert.Equal(t,
			filepath.Join(lib, "b", "beyonce", "[2003] dangerously in love", "01 crazy in love.flac"),
			op.NewPath,
		)
	})

	t.Run("artist starting with digit buckets into 0/", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum("/src/2pac", "2Pac", []*metadata.Track{
			{
				Path:        "/src/2pac/01 ambitionz.flac",
				Title:       "Ambitionz Az A Ridah",
				Album:       "All Eyez On Me",
				Year:        "1996",
				TrackNumber: new(1),
			},
		}, nil)

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		op := findMove(&plan.Albums[0], "/src/2pac/01 ambitionz.flac")
		require.NotNil(t, op)
		assert.Equal(t,
			filepath.Join(lib, "0", "2pac", "[1996] all eyez on me", "01 ambitionz az a ridah.flac"),
			op.NewPath,
		)
	})

	t.Run("missing year omits year prefix from folder", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum("/src/beyonce", "Beyoncé", []*metadata.Track{
			{
				Path:        "/src/beyonce/01 pray you catch me.flac",
				Title:       "Pray You Catch Me",
				Album:       "Lemonade",
				Year:        "",
				TrackNumber: new(1),
			},
		}, nil)

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		op := findMove(&plan.Albums[0], "/src/beyonce/01 pray you catch me.flac")
		require.NotNil(t, op)
		assert.Equal(t,
			filepath.Join(lib, "b", "beyonce", "lemonade", "01 pray you catch me.flac"),
			op.NewPath,
		)
	})

	t.Run("manual override artist uses override value and correct bucket", func(t *testing.T) {
		lib := t.TempDir()
		// AC/DC -> ac⁄dc (U+2044); first rune is 'a' -> a/ bucket.
		album := makeAlbum("/src/acdc", "AC/DC", []*metadata.Track{
			{
				Path:        "/src/acdc/01 back in black.flac",
				Title:       "Back In Black",
				Album:       "Back In Black",
				Year:        "1980",
				TrackNumber: new(1),
			},
		}, nil)

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		op := findMove(&plan.Albums[0], "/src/acdc/01 back in black.flac")
		require.NotNil(t, op)
		assert.Equal(t,
			filepath.Join(lib, "a", "ac⁄dc", "[1980] back in black", "01 back in black.flac"),
			op.NewPath,
		)
	})

	t.Run("AlbumPlan fields reflect sanitized artist and album name", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum("/src", "The Artist", []*metadata.Track{
			{Path: "/src/01.flac", Title: "Track", Album: "The Album", Year: "2000", TrackNumber: new(1)},
		}, nil)

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)
		require.Len(t, plan.Albums, 1)

		assert.Equal(t, "the artist", plan.Albums[0].AlbumArtist)
		assert.Equal(t, "[2000] the album", plan.Albums[0].AlbumName)
	})

	t.Run("non-ASCII characters in artist and title are transliterated", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum("/src", "Mötley Crüe", []*metadata.Track{
			{
				Path:        "/src/01.flac",
				Title:       "Décadence",
				Album:       "Dr. Feelgood",
				Year:        "1989",
				TrackNumber: new(1),
			},
		}, nil)

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		op := findMove(&plan.Albums[0], "/src/01.flac")
		require.NotNil(t, op)
		assert.Contains(t, op.NewPath, "motley crue")
		assert.True(t, strings.HasSuffix(op.NewPath, "01 decadence.flac"))
	})

	t.Run("special characters in title are stripped", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum("/src", "Artist", []*metadata.Track{
			{
				Path:        "/src/01.flac",
				Title:       "Hello (World)!",
				Album:       "Album",
				Year:        "2000",
				TrackNumber: new(1),
			},
		}, nil)

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		op := findMove(&plan.Albums[0], "/src/01.flac")
		require.NotNil(t, op)
		assert.True(t, strings.HasSuffix(op.NewPath, "01 hello world.flac"))
	})

	t.Run("ALBUMARTISTSORT determines bucket but not folder name", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum("/src/beatles", "The Beatles", []*metadata.Track{
			{
				Path:        "/src/beatles/01 come together.flac",
				Title:       "Come Together",
				Album:       "Abbey Road",
				Year:        "1969",
				TrackNumber: new(1),
			},
		}, nil)
		// Simulate what ProcessLibrary would set from ALBUMARTISTSORT.
		album.ResolvedArtistSort = "Beatles, The"

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		op := findMove(&plan.Albums[0], "/src/beatles/01 come together.flac")
		require.NotNil(t, op)
		// Bucket from sort tag ("b"), folder name from ALBUMARTIST ("the beatles").
		assert.Equal(t,
			filepath.Join(lib, "b", "the beatles", "[1969] abbey road", "01 come together.flac"),
			op.NewPath,
		)
	})

	t.Run("absent ALBUMARTISTSORT falls back to ALBUMARTIST for bucket", func(t *testing.T) {
		lib := t.TempDir()
		// No ResolvedArtistSort set.
		album := makeAlbum("/src/beatles", "The Beatles", []*metadata.Track{
			{
				Path:        "/src/beatles/01 come together.flac",
				Title:       "Come Together",
				Album:       "Abbey Road",
				Year:        "1969",
				TrackNumber: new(1),
			},
		}, nil)

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		op := findMove(&plan.Albums[0], "/src/beatles/01 come together.flac")
		require.NotNil(t, op)
		// No sort tag: "The Beatles" sanitizes to "the beatles", first letter "t".
		assert.Equal(t,
			filepath.Join(lib, "t", "the beatles", "[1969] abbey road", "01 come together.flac"),
			op.NewPath,
		)
	})
}

func TestPlanLibrary_TrackNumbering(t *testing.T) {
	t.Run("2-digit padding when all tracks are at most 99", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum("/src", "Artist", []*metadata.Track{
			{Path: "/src/t1.flac", Title: "One", Album: "A", Year: "2000", TrackNumber: new(1)},
			{Path: "/src/t9.flac", Title: "Nine", Album: "A", Year: "2000", TrackNumber: new(9)},
		}, nil)

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		op1 := findMove(&plan.Albums[0], "/src/t1.flac")
		op9 := findMove(&plan.Albums[0], "/src/t9.flac")
		require.NotNil(t, op1)
		require.NotNil(t, op9)
		assert.True(t, strings.HasSuffix(op1.NewPath, "01 one.flac"))
		assert.True(t, strings.HasSuffix(op9.NewPath, "09 nine.flac"))
	})

	t.Run("3-digit padding when any track exceeds 99", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum("/src", "Artist", []*metadata.Track{
			{Path: "/src/t1.flac", Title: "One", Album: "A", Year: "2000", TrackNumber: new(1)},
			{Path: "/src/t100.flac", Title: "Hundred", Album: "A", Year: "2000", TrackNumber: new(100)},
		}, nil)

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		op1 := findMove(&plan.Albums[0], "/src/t1.flac")
		op100 := findMove(&plan.Albums[0], "/src/t100.flac")
		require.NotNil(t, op1)
		require.NotNil(t, op100)
		assert.True(t, strings.HasSuffix(op1.NewPath, "001 one.flac"))
		assert.True(t, strings.HasSuffix(op100.NewPath, "100 hundred.flac"))
	})

	t.Run("multi-disc album includes disc prefix", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum("/src", "Artist", []*metadata.Track{
			{Path: "/src/d1t1.flac", Title: "D1 T1", Album: "A", Year: "2000", TrackNumber: new(1), DiscNumber: 1},
			{Path: "/src/d2t1.flac", Title: "D2 T1", Album: "A", Year: "2000", TrackNumber: new(1), DiscNumber: 2},
		}, nil)

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		op1 := findMove(&plan.Albums[0], "/src/d1t1.flac")
		op2 := findMove(&plan.Albums[0], "/src/d2t1.flac")
		require.NotNil(t, op1)
		require.NotNil(t, op2)
		assert.True(t, strings.HasSuffix(op1.NewPath, "1-01 d1 t1.flac"))
		assert.True(t, strings.HasSuffix(op2.NewPath, "2-01 d2 t1.flac"))
	})

	t.Run("all tracks on single disc with DISCNUMBER=1 have no disc prefix", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum("/src", "Artist", []*metadata.Track{
			{Path: "/src/t1.flac", Title: "One", Album: "A", Year: "2000", TrackNumber: new(1), DiscNumber: 1},
			{Path: "/src/t2.flac", Title: "Two", Album: "A", Year: "2000", TrackNumber: new(2), DiscNumber: 1},
		}, nil)

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		op := findMove(&plan.Albums[0], "/src/t1.flac")
		require.NotNil(t, op)
		assert.True(t, strings.HasSuffix(op.NewPath, "01 one.flac"))
		assert.NotContains(t, op.NewPath, "1-01")
	})

	t.Run("absent track number (nil) is formatted as 00", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum("/src", "Artist", []*metadata.Track{
			{Path: "/src/t.flac", Title: "Unknown", Album: "A", Year: "2000", TrackNumber: nil},
		}, nil)

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		op := findMove(&plan.Albums[0], "/src/t.flac")
		require.NotNil(t, op)
		assert.True(t, strings.HasSuffix(op.NewPath, "00 unknown.flac"))
	})

	t.Run("hidden track with TrackNumber zero is formatted as 00", func(t *testing.T) {
		lib := t.TempDir()
		// intPtr(0) is a present tag with value 0, distinct from nil.
		album := makeAlbum("/src", "Artist", []*metadata.Track{
			{Path: "/src/hidden.flac", Title: "Hidden", Album: "A", Year: "2000", TrackNumber: new(0)},
			{Path: "/src/t1.flac", Title: "One", Album: "A", Year: "2000", TrackNumber: new(1)},
		}, nil)

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		op := findMove(&plan.Albums[0], "/src/hidden.flac")
		require.NotNil(t, op)
		assert.True(t, strings.HasSuffix(op.NewPath, "00 hidden.flac"))
	})
}

func TestPlanLibrary_TitleFallback(t *testing.T) {
	t.Run("empty title falls back to sanitized filename stem", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum("/src", "Artist", []*metadata.Track{
			{
				Path:        "/src/03 original filename.flac",
				Title:       "",
				Album:       "Album",
				Year:        "2000",
				TrackNumber: new(3),
			},
		}, nil)

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		op := findMove(&plan.Albums[0], "/src/03 original filename.flac")
		require.NotNil(t, op)
		assert.True(t, strings.HasSuffix(op.NewPath, "03 03 original filename.flac"))
	})
}

func TestPlanLibrary_Extensions(t *testing.T) {
	t.Run("uppercase audio extension is lowercased", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum("/src", "Artist", []*metadata.Track{
			{Path: "/src/01.FLAC", Title: "Track", Album: "Album", Year: "2000", TrackNumber: new(1)},
		}, nil)

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		op := findMove(&plan.Albums[0], "/src/01.FLAC")
		require.NotNil(t, op)
		assert.True(t, strings.HasSuffix(op.NewPath, ".flac"))
	})
}

func TestPlanLibrary_Assets(t *testing.T) {
	const src = "/src"

	t.Run("primary art is renamed to folder.ext", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum(src, "Artist", []*metadata.Track{
			{Path: src + "/01.flac", Title: "T", Album: "A", Year: "2000", TrackNumber: new(1)},
		}, map[metadata.FileCategory][]string{
			metadata.CatPrimaryArt: {src + "/folder.jpg"},
		})

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		op := findMove(&plan.Albums[0], src+"/folder.jpg")
		require.NotNil(t, op)
		assert.True(t, strings.HasSuffix(op.NewPath, "folder.jpg"))
		assert.NotContains(t, op.NewPath, "artwork")
	})

	t.Run("artwork goes into artwork/ with sanitized stem", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum(src, "Artist", []*metadata.Track{
			{Path: src + "/01.flac", Title: "T", Album: "A", Year: "2000", TrackNumber: new(1)},
		}, map[metadata.FileCategory][]string{
			metadata.CatArtwork: {src + "/Back Cover!.jpg"},
		})

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		op := findMove(&plan.Albums[0], src+"/Back Cover!.jpg")
		require.NotNil(t, op)
		assert.Contains(t, op.NewPath, "artwork")
		assert.True(t, strings.HasSuffix(op.NewPath, "back cover.jpg"))
	})

	t.Run("scans go into scans/ with sanitized stem", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum(src, "Artist", []*metadata.Track{
			{Path: src + "/01.flac", Title: "T", Album: "A", Year: "2000", TrackNumber: new(1)},
		}, map[metadata.FileCategory][]string{
			metadata.CatScan: {src + "/scans/High Res Scan.tiff"},
		})

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		op := findMove(&plan.Albums[0], src+"/scans/High Res Scan.tiff")
		require.NotNil(t, op)
		assert.Contains(t, op.NewPath, "scans")
		assert.True(t, strings.HasSuffix(op.NewPath, "high res scan.tiff"))
	})

	t.Run("extras go into extras/ with sanitized stem", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum(src, "Artist", []*metadata.Track{
			{Path: src + "/01.flac", Title: "T", Album: "A", Year: "2000", TrackNumber: new(1)},
		}, map[metadata.FileCategory][]string{
			metadata.CatExtras: {src + "/extras/Booklet (High Quality).pdf"},
		})

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		op := findMove(&plan.Albums[0], src+"/extras/Booklet (High Quality).pdf")
		require.NotNil(t, op)
		assert.Contains(t, op.NewPath, "extras")
		assert.True(t, strings.HasSuffix(op.NewPath, "booklet high quality.pdf"))
	})

	t.Run("root text files are sanitized and placed at album root", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum(src, "Artist", []*metadata.Track{
			{Path: src + "/01.flac", Title: "T", Album: "A", Year: "2000", TrackNumber: new(1)},
		}, map[metadata.FileCategory][]string{
			metadata.CatRootText: {src + "/Rip Log!.log"},
		})

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		op := findMove(&plan.Albums[0], src+"/Rip Log!.log")
		require.NotNil(t, op)
		assert.True(t, strings.HasSuffix(op.NewPath, "rip log.log"))
		assert.NotContains(t, op.NewPath, "artwork")
		assert.NotContains(t, op.NewPath, "extras")
	})

	t.Run("unknown files produce no move operation", func(t *testing.T) {
		lib := t.TempDir()
		unknownPath := src + "/mysterious.exe"
		album := makeAlbum(src, "Artist", []*metadata.Track{
			{Path: src + "/01.flac", Title: "T", Album: "A", Year: "2000", TrackNumber: new(1)},
		}, map[metadata.FileCategory][]string{
			metadata.CatUnknown: {unknownPath},
		})

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		op := findMove(&plan.Albums[0], unknownPath)
		assert.Nil(t, op, "unknown files must not have a move operation")
	})

	t.Run("artwork stem is truncated to respect subdirectory offset", func(t *testing.T) {
		lib := t.TempDir()
		// "artwork" = 7 runes; effective stem limit = 40 - 7 - 1 = 32.
		// A 40-character stem must be cut to 32 characters.
		longStem := strings.Repeat("a", 40)
		album := makeAlbum(src, "Artist", []*metadata.Track{
			{Path: src + "/01.flac", Title: "T", Album: "A", Year: "2000", TrackNumber: new(1)},
		}, map[metadata.FileCategory][]string{
			metadata.CatArtwork: {src + "/" + longStem + ".jpg"},
		})

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		op := findMove(&plan.Albums[0], src+"/"+longStem+".jpg")
		require.NotNil(t, op)
		base := filepath.Base(op.NewPath)
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		assert.Equal(t, 32, len([]rune(stem)))
	})
}

func TestPlanLibrary_MoveOpTypes(t *testing.T) {
	t.Run("IsNoOp when file is already at its correct destination", func(t *testing.T) {
		lib := t.TempDir()
		// Place the track at the exact path the planner would compute.
		albumDir := filepath.Join(lib, "a", "artist", "[2000] album")
		trackPath := filepath.Join(albumDir, "01 track.flac")

		album := makeAlbum(albumDir, "Artist", []*metadata.Track{
			{Path: trackPath, Title: "Track", Album: "Album", Year: "2000", TrackNumber: new(1)},
		}, nil)

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		op := findMove(&plan.Albums[0], trackPath)
		require.NotNil(t, op)
		assert.True(t, op.IsNoOp)
		assert.False(t, op.IsCaseOnly)
	})

	t.Run("IsCaseOnly when paths differ only in case", func(t *testing.T) {
		lib := t.TempDir()
		// Source path uses "Artist" (capital A); the planner will produce
		// "artist" (lowercase), so the paths differ only in case.
		albumDir := filepath.Join(lib, "a", "Artist", "[2000] album")
		trackPath := filepath.Join(albumDir, "01 track.flac")

		album := makeAlbum(albumDir, "Artist", []*metadata.Track{
			{Path: trackPath, Title: "Track", Album: "Album", Year: "2000", TrackNumber: new(1)},
		}, nil)

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		op := findMove(&plan.Albums[0], trackPath)
		require.NotNil(t, op)
		assert.True(t, op.IsCaseOnly)
		assert.False(t, op.IsNoOp)
	})
}

func TestPlanLibrary_ErrorConditions(t *testing.T) {
	t.Run("empty ResolvedArtist returns error", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum("/src", "", []*metadata.Track{
			{Path: "/src/01.flac", Title: "T", Album: "A", Year: "2000", TrackNumber: new(1)},
		}, nil)

		_, err := New(lib).PlanLibrary([]*metadata.Album{album})
		assert.Error(t, err)
	})

	t.Run("inconsistent DISCNUMBER tags return error", func(t *testing.T) {
		lib := t.TempDir()
		// Track 1 has a disc number; track 2 does not.
		album := makeAlbum("/src", "Artist", []*metadata.Track{
			{Path: "/src/t1.flac", Title: "One", Album: "A", Year: "2000", TrackNumber: new(1), DiscNumber: 1},
			{Path: "/src/t2.flac", Title: "Two", Album: "A", Year: "2000", TrackNumber: new(2), DiscNumber: 0},
		}, nil)

		_, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "DISCNUMBER")
	})

	t.Run("collision within an album fails fast with error", func(t *testing.T) {
		lib := t.TempDir()
		// "Remix!" and "Remix?" both sanitize to "remix"; both are track 1,
		// so both resolve to "01 remix.flac" which is a collision.
		album := makeAlbum("/src", "Artist", []*metadata.Track{
			{Path: "/src/a.flac", Title: "Remix!", Album: "A", Year: "2000", TrackNumber: new(1)},
			{Path: "/src/b.flac", Title: "Remix?", Album: "A", Year: "2000", TrackNumber: new(1)},
		}, nil)

		_, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "collision")
	})

	t.Run("collision across albums fails fast with error", func(t *testing.T) {
		lib := t.TempDir()
		// Two albums with identical artist/album/year/track produce the same
		// destination path.
		albumA := makeAlbum("/src/a", "Artist", []*metadata.Track{
			{Path: "/src/a/01.flac", Title: "Track", Album: "Album", Year: "2000", TrackNumber: new(1)},
		}, nil)
		albumB := makeAlbum("/src/b", "Artist", []*metadata.Track{
			{Path: "/src/b/01.flac", Title: "Track", Album: "Album", Year: "2000", TrackNumber: new(1)},
		}, nil)

		_, err := New(lib).PlanLibrary([]*metadata.Album{albumA, albumB})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "collision")
	})
}

func TestPlanLibrary_MultipleAlbums(t *testing.T) {
	t.Run("multiple distinct albums produce independent plans", func(t *testing.T) {
		lib := t.TempDir()

		albumA := makeAlbum("/src/a", "Beyoncé", []*metadata.Track{
			{Path: "/src/a/01.flac", Title: "Pray You Catch Me", Album: "Lemonade", Year: "2016", TrackNumber: new(1)},
		}, nil)
		albumB := makeAlbum("/src/b", "2Pac", []*metadata.Track{
			{Path: "/src/b/01.flac", Title: "Ambitionz Az A Ridah", Album: "All Eyez On Me", Year: "1996", TrackNumber: new(1)},
		}, nil)

		plan, err := New(lib).PlanLibrary([]*metadata.Album{albumA, albumB})
		require.NoError(t, err)
		require.Len(t, plan.Albums, 2)

		opA := findMove(&plan.Albums[0], "/src/a/01.flac")
		opB := findMove(&plan.Albums[1], "/src/b/01.flac")
		require.NotNil(t, opA)
		require.NotNil(t, opB)
		assert.Contains(t, opA.NewPath, "beyonce")
		assert.Contains(t, opB.NewPath, "2pac")
	})
}

func TestPlanLibrary_DestDir(t *testing.T) {
	t.Run("DestDir is set to the computed album directory", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum("/src/beyonce", "Beyoncé", []*metadata.Track{
			{Path: "/src/beyonce/01.flac", Title: "Track", Album: "Lemonade", Year: "2016", TrackNumber: new(1)},
		}, nil)

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)
		require.Len(t, plan.Albums, 1)

		assert.Equal(t,
			filepath.Join(lib, "b", "beyonce", "[2016] lemonade"),
			plan.Albums[0].DestDir,
		)
	})

	t.Run("SourceDir is set to the album root path", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum("/src/beyonce", "Beyoncé", []*metadata.Track{
			{Path: "/src/beyonce/01.flac", Title: "Track", Album: "Lemonade", Year: "2016", TrackNumber: new(1)},
		}, nil)

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)
		require.Len(t, plan.Albums, 1)

		assert.Equal(t, "/src/beyonce", plan.Albums[0].SourceDir)
	})

	t.Run("DestDir allows correct relative path computation for subdirectory assets", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum("/src", "Artist", []*metadata.Track{
			{Path: "/src/01.flac", Title: "Track", Album: "Album", Year: "2000", TrackNumber: new(1)},
		}, map[metadata.FileCategory][]string{
			metadata.CatArtwork: {"/src/cover.jpg"},
		})

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		ap := plan.Albums[0]
		op := findMove(&ap, "/src/cover.jpg")
		require.NotNil(t, op)

		rel, err := filepath.Rel(ap.DestDir, op.NewPath)
		require.NoError(t, err)
		assert.Equal(t, filepath.Join("artwork", "cover.jpg"), rel)
	})
}

func TestPlanLibrary_Warnings(t *testing.T) {
	t.Run("well-tagged album produces no warnings", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum("/src", "Artist", []*metadata.Track{
			{Path: "/src/01.flac", Title: "Track", Album: "Album", Year: "2000", TrackNumber: new(1)},
		}, nil)

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)
		assert.Empty(t, plan.Albums[0].Warnings)
	})

	t.Run("missing YEAR tag produces a warning", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum("/src", "Artist", []*metadata.Track{
			{Path: "/src/01.flac", Title: "Track", Album: "Album", Year: "", TrackNumber: new(1)},
		}, nil)

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		warnings := plan.Albums[0].Warnings
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "YEAR")
	})

	t.Run("missing TITLE tag produces a warning and uses filename stem", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum("/src", "Artist", []*metadata.Track{
			{Path: "/src/03 original name.flac", Title: "", Album: "Album", Year: "2000", TrackNumber: new(3)},
		}, nil)

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		warnings := plan.Albums[0].Warnings
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "TITLE")
		assert.Contains(t, warnings[0], "/src/03 original name.flac")

		// Filename stem should still be used to produce a valid destination.
		op := findMove(&plan.Albums[0], "/src/03 original name.flac")
		require.NotNil(t, op)
		assert.True(t, strings.HasSuffix(op.NewPath, "03 03 original name.flac"))
	})

	t.Run("missing TRACKNUMBER tag produces a warning", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum("/src", "Artist", []*metadata.Track{
			{Path: "/src/t.flac", Title: "Track", Album: "Album", Year: "2000", TrackNumber: nil},
		}, nil)

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		warnings := plan.Albums[0].Warnings
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "TRACKNUMBER")
		assert.Contains(t, warnings[0], "/src/t.flac")
	})

	t.Run("unknown file produces a warning", func(t *testing.T) {
		lib := t.TempDir()
		unknownPath := "/src/mystery.exe"
		album := makeAlbum("/src", "Artist", []*metadata.Track{
			{Path: "/src/01.flac", Title: "Track", Album: "Album", Year: "2000", TrackNumber: new(1)},
		}, map[metadata.FileCategory][]string{
			metadata.CatUnknown: {unknownPath},
		})

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		warnings := plan.Albums[0].Warnings
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], unknownPath)
	})

	t.Run("multiple missing tags produce one warning each", func(t *testing.T) {
		lib := t.TempDir()
		album := makeAlbum("/src", "Artist", []*metadata.Track{
			// Missing TITLE and TRACKNUMBER on same track.
			{Path: "/src/t.flac", Title: "", Album: "Album", Year: "2000", TrackNumber: nil},
		}, nil)

		plan, err := New(lib).PlanLibrary([]*metadata.Album{album})
		require.NoError(t, err)

		warnings := plan.Albums[0].Warnings
		assert.Len(t, warnings, 2)
	})
}
