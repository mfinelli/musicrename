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

package musicbrainz

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadWriteSnapshot(t *testing.T) {
	t.Run("round-trips a Release through gzip+JSON", func(t *testing.T) {
		dir := t.TempDir()
		release := &Release{ID: "d1d196ec-bb2b-4636-81a0-b8f5c5774514", Title: "Back in Black"}

		require.NoError(t, WriteSnapshot(dir, release))
		got, err := ReadSnapshot(dir)
		require.NoError(t, err)
		assert.Equal(t, release, got)
	})

	t.Run("writing again overwrites the previous snapshot", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, WriteSnapshot(dir, &Release{Title: "old"}))
		require.NoError(t, WriteSnapshot(dir, &Release{Title: "new"}))

		got, err := ReadSnapshot(dir)
		require.NoError(t, err)
		assert.Equal(t, "new", got.Title)
	})

	t.Run("returns nil, nil when no snapshot exists yet", func(t *testing.T) {
		got, err := ReadSnapshot(t.TempDir())
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("errors on a corrupt (non-gzip) snapshot file", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, MetadataFilename), []byte("not gzip"), 0o644))

		_, err := ReadSnapshot(dir)
		assert.Error(t, err)
	})
}

func TestDiff(t *testing.T) {
	t.Run("no differences yields an empty slice", func(t *testing.T) {
		r := &Release{Title: "Back in Black"}
		assert.Empty(t, Diff(r, r))
	})

	t.Run("nil old or current is treated as an empty Release, not a panic", func(t *testing.T) {
		assert.NotPanics(t, func() { Diff(nil, &Release{Title: "X"}) })
		assert.NotPanics(t, func() { Diff(&Release{Title: "X"}, nil) })
		assert.Contains(t, Diff(nil, &Release{Title: "X"}), `title: "" → "X"`)
	})

	t.Run("every top-level scalar field is reported when changed", func(t *testing.T) {
		old := &Release{
			Title: "a", Disambiguation: "a", Status: "a", Date: "a",
			Country: "a", Barcode: "a", Quality: "a", Packaging: "a",
		}
		current := &Release{
			Title: "b", Disambiguation: "b", Status: "b", Date: "b",
			Country: "b", Barcode: "b", Quality: "b", Packaging: "b",
		}
		changes := Diff(old, current)
		assert.Contains(t, changes, `title: "a" → "b"`)
		assert.Contains(t, changes, `disambiguation: "a" → "b"`)
		assert.Contains(t, changes, `status: "a" → "b"`)
		assert.Contains(t, changes, `date: "a" → "b"`)
		assert.Contains(t, changes, `country: "a" → "b"`)
		assert.Contains(t, changes, `barcode: "a" → "b"`)
		assert.Contains(t, changes, `quality: "a" → "b"`)
		assert.Contains(t, changes, `packaging: "a" → "b"`)
	})
}

func TestDiffArtistCredits(t *testing.T) {
	t.Run("no changes yields nothing", func(t *testing.T) {
		ac := []ArtistCredit{{Name: "AC/DC", Artist: Artist{ID: "x", Name: "AC/DC"}}}
		assert.Empty(t, diffArtistCredits("artist", ac, ac))
	})

	t.Run("a changed credit count is reported", func(t *testing.T) {
		changes := diffArtistCredits("artist", []ArtistCredit{{}}, []ArtistCredit{{}, {}})
		assert.Contains(t, changes, "artist credit count: 1 → 2")
	})

	t.Run("every credit and nested artist field is reported when changed", func(t *testing.T) {
		old := []ArtistCredit{{
			Name: "a", JoinPhrase: "a",
			Artist: Artist{ID: "a", Name: "a", SortName: "a", Disambiguation: "a", Type: "a", Country: "a"},
		}}
		current := []ArtistCredit{{
			Name: "b", JoinPhrase: "b",
			Artist: Artist{ID: "b", Name: "b", SortName: "b", Disambiguation: "b", Type: "b", Country: "b"},
		}}
		changes := diffArtistCredits("artist", old, current)
		assert.Contains(t, changes, `artist credit 1 name: "a" → "b"`)
		assert.Contains(t, changes, `artist credit 1 join phrase: "a" → "b"`)
		assert.Contains(t, changes, `artist credit 1 artist id: "a" → "b"`)
		assert.Contains(t, changes, `artist credit 1 artist name: "a" → "b"`)
		assert.Contains(t, changes, `artist credit 1 artist sort name: "a" → "b"`)
		assert.Contains(t, changes, `artist credit 1 artist disambiguation: "a" → "b"`)
		assert.Contains(t, changes, `artist credit 1 artist type: "a" → "b"`)
		assert.Contains(t, changes, `artist credit 1 artist country: "a" → "b"`)
	})

	t.Run("the nested artist's genres are diffed too", func(t *testing.T) {
		old := []ArtistCredit{{Artist: Artist{Genres: []Genre{{Name: "rock"}}}}}
		current := []ArtistCredit{{Artist: Artist{Genres: []Genre{{Name: "metal"}}}}}
		changes := diffArtistCredits("artist", old, current)
		assert.Contains(t, changes, `artist credit 1 artist genre tag added: "metal"`)
		assert.Contains(t, changes, `artist credit 1 artist genre tag removed: "rock"`)
	})
}

func TestDiffComposers(t *testing.T) {
	t.Run("no changes yields nothing", func(t *testing.T) {
		c := []Composer{{Type: "writer", Artist: Artist{Name: "Brian Johnson"}}}
		assert.Empty(t, diffComposers("recording", c, c))
	})

	t.Run("a changed composer count is reported", func(t *testing.T) {
		changes := diffComposers("recording", []Composer{{}}, []Composer{{}, {}})
		assert.Contains(t, changes, "recording composer count: 1 → 2")
	})

	t.Run("relationship type and artist fields are reported when changed", func(t *testing.T) {
		old := []Composer{{Type: "writer", Artist: Artist{ID: "a", Name: "a"}}}
		current := []Composer{{Type: "composer", Artist: Artist{ID: "b", Name: "b"}}}
		changes := diffComposers("recording", old, current)
		assert.Contains(t, changes, `recording composer 1 relationship type: "writer" → "composer"`)
		assert.Contains(t, changes, `recording composer 1 artist id: "a" → "b"`)
		assert.Contains(t, changes, `recording composer 1 artist name: "a" → "b"`)
	})
}

func TestDiffGenres(t *testing.T) {
	t.Run("no changes yields nothing", func(t *testing.T) {
		g := []Genre{{Name: "rock", Count: 5}}
		assert.Empty(t, diffGenres("release", g, g))
	})

	t.Run("an added genre is reported", func(t *testing.T) {
		changes := diffGenres("release", nil, []Genre{{Name: "metal"}})
		assert.Equal(t, []string{`release genre tag added: "metal"`}, changes)
	})

	t.Run("a removed genre is reported", func(t *testing.T) {
		changes := diffGenres("release", []Genre{{Name: "metal"}}, nil)
		assert.Equal(t, []string{`release genre tag removed: "metal"`}, changes)
	})

	t.Run("a vote-count-only change on an existing genre is never reported", func(t *testing.T) {
		old := []Genre{{Name: "rock", Count: 5}}
		current := []Genre{{Name: "rock", Count: 500}}
		assert.Empty(t, diffGenres("release", old, current))
	})

	t.Run("reordering with no add/remove is never reported", func(t *testing.T) {
		old := []Genre{{Name: "rock"}, {Name: "metal"}}
		current := []Genre{{Name: "metal"}, {Name: "rock"}}
		assert.Empty(t, diffGenres("release", old, current))
	})
}

func TestDiffISRCs(t *testing.T) {
	t.Run("no changes yields nothing", func(t *testing.T) {
		isrcs := []string{"AUAP08000041"}
		assert.Empty(t, diffISRCs("recording", isrcs, isrcs))
	})

	t.Run("a changed count is reported", func(t *testing.T) {
		changes := diffISRCs("recording", []string{"A"}, []string{"A", "B"})
		assert.Contains(t, changes, "recording ISRC count: 1 → 2")
	})

	t.Run("a changed value at the same position is reported", func(t *testing.T) {
		changes := diffISRCs("recording", []string{"A"}, []string{"B"})
		assert.Contains(t, changes, `recording ISRC 1: "A" → "B"`)
	})
}

func TestDiffReleaseGroup(t *testing.T) {
	t.Run("no changes yields nothing", func(t *testing.T) {
		rg := ReleaseGroup{Title: "Back in Black", PrimaryType: "Album"}
		assert.Empty(t, diffReleaseGroup(rg, rg))
	})

	t.Run("every scalar field is reported when changed", func(t *testing.T) {
		old := ReleaseGroup{
			Title: "a", Disambiguation: "a", FirstReleaseDate: "a", PrimaryType: "a",
			SecondaryTypes: []string{"a"},
		}
		current := ReleaseGroup{
			Title: "b", Disambiguation: "b", FirstReleaseDate: "b", PrimaryType: "b",
			SecondaryTypes: []string{"b"},
		}
		changes := diffReleaseGroup(old, current)
		assert.Contains(t, changes, `release group title: "a" → "b"`)
		assert.Contains(t, changes, `release group disambiguation: "a" → "b"`)
		assert.Contains(t, changes, `release group first release date: "a" → "b"`)
		assert.Contains(t, changes, `release group primary type: "a" → "b"`)
		assert.Contains(t, changes, `release group secondary types: "a" → "b"`)
	})

	t.Run("nested artist-credit and genres are diffed too", func(t *testing.T) {
		old := ReleaseGroup{
			ArtistCredit: []ArtistCredit{{Name: "a"}},
			Genres:       []Genre{{Name: "rock"}},
		}
		current := ReleaseGroup{
			ArtistCredit: []ArtistCredit{{Name: "b"}},
			Genres:       []Genre{{Name: "metal"}},
		}
		changes := diffReleaseGroup(old, current)
		assert.Contains(t, changes, `release group artist credit 1 name: "a" → "b"`)
		assert.Contains(t, changes, `release group genre tag added: "metal"`)
		assert.Contains(t, changes, `release group genre tag removed: "rock"`)
	})
}

func TestDiffLabelInfo(t *testing.T) {
	t.Run("no changes yields nothing", func(t *testing.T) {
		li := []LabelInfo{{CatalogNumber: "X", Label: Label{Name: "Columbia"}}}
		assert.Empty(t, diffLabelInfo(li, li))
	})

	t.Run("a changed label count is reported", func(t *testing.T) {
		changes := diffLabelInfo(nil, []LabelInfo{{}})
		assert.Contains(t, changes, "label count: 0 → 1")
	})

	t.Run("every field is reported when changed", func(t *testing.T) {
		old := []LabelInfo{{
			CatalogNumber: "a",
			Label:         Label{ID: "a", Name: "a", SortName: "a", Disambiguation: "a", Type: "a"},
		}}
		current := []LabelInfo{{
			CatalogNumber: "b",
			Label:         Label{ID: "b", Name: "b", SortName: "b", Disambiguation: "b", Type: "b"},
		}}
		changes := diffLabelInfo(old, current)
		assert.Contains(t, changes, `label 1 catalog number: "a" → "b"`)
		assert.Contains(t, changes, `label 1 id: "a" → "b"`)
		assert.Contains(t, changes, `label 1 name: "a" → "b"`)
		assert.Contains(t, changes, `label 1 sort name: "a" → "b"`)
		assert.Contains(t, changes, `label 1 disambiguation: "a" → "b"`)
		assert.Contains(t, changes, `label 1 type: "a" → "b"`)
	})
}

func TestDiffMedia(t *testing.T) {
	t.Run("no changes yields nothing", func(t *testing.T) {
		m := []Medium{{Format: "CD", Tracks: []Track{{Title: "Hells Bells"}}}}
		assert.Empty(t, diffMedia(m, m))
	})

	t.Run("a changed medium count is reported", func(t *testing.T) {
		changes := diffMedia(nil, []Medium{{}})
		assert.Contains(t, changes, "medium count: 0 → 1")
	})

	t.Run("medium format and title are reported when changed", func(t *testing.T) {
		old := []Medium{{Format: "a", Title: "a"}}
		current := []Medium{{Format: "b", Title: "b"}}
		changes := diffMedia(old, current)
		assert.Contains(t, changes, `medium 1 format: "a" → "b"`)
		assert.Contains(t, changes, `medium 1 title: "a" → "b"`)
	})

	t.Run("a changed track count is reported", func(t *testing.T) {
		changes := diffMedia([]Medium{{Tracks: nil}}, []Medium{{Tracks: []Track{{}}}})
		assert.Contains(t, changes, "medium 1 track count: 0 → 1")
	})

	t.Run("track title, number, and length are reported when changed", func(t *testing.T) {
		old := []Medium{{Tracks: []Track{{Title: "a", Number: "a", Length: 1}}}}
		current := []Medium{{Tracks: []Track{{Title: "b", Number: "b", Length: 2}}}}
		changes := diffMedia(old, current)
		assert.Contains(t, changes, `medium 1 track 1 title: "a" → "b"`)
		assert.Contains(t, changes, `medium 1 track 1 number: "a" → "b"`)
		assert.Contains(t, changes, "medium 1 track 1 length: 1ms → 2ms")
	})

	t.Run("track-level artist credits are diffed", func(t *testing.T) {
		old := []Medium{{Tracks: []Track{{ArtistCredit: []ArtistCredit{{Name: "a"}}}}}}
		current := []Medium{{Tracks: []Track{{ArtistCredit: []ArtistCredit{{Name: "b"}}}}}}
		changes := diffMedia(old, current)
		assert.Contains(t, changes, `medium 1 track 1 artist credit 1 name: "a" → "b"`)
	})

	t.Run("recording id, title, length, disambiguation, and first release date are reported", func(t *testing.T) {
		old := []Medium{{Tracks: []Track{{Recording: Recording{
			ID: "a", Title: "a", Length: 1, Disambiguation: "a", FirstReleaseDate: "a",
		}}}}}
		current := []Medium{{Tracks: []Track{{Recording: Recording{
			ID: "b", Title: "b", Length: 2, Disambiguation: "b", FirstReleaseDate: "b",
		}}}}}
		changes := diffMedia(old, current)
		assert.Contains(t, changes, `medium 1 track 1 recording id: "a" → "b"`)
		assert.Contains(t, changes, `medium 1 track 1 recording title: "a" → "b"`)
		assert.Contains(t, changes, "medium 1 track 1 recording length: 1ms → 2ms")
		assert.Contains(t, changes, `medium 1 track 1 recording disambiguation: "a" → "b"`)
		assert.Contains(t, changes, `medium 1 track 1 recording first release date: "a" → "b"`)
	})

	t.Run("recording-level artist credits, composers, ISRCs, and genres are all diffed", func(t *testing.T) {
		old := []Medium{{Tracks: []Track{{Recording: Recording{
			ArtistCredit: []ArtistCredit{{Name: "a"}},
			ISRCs:        []string{"AAA"},
			Genres:       []Genre{{Name: "rock"}},
			Relations: []RecordingRelation{{Type: "performance", TargetType: "work", Work: &Work{
				Relations: []WorkRelation{{Type: "writer", TargetType: "artist", Artist: Artist{Name: "a"}}},
			}}},
		}}}}}
		current := []Medium{{Tracks: []Track{{Recording: Recording{
			ArtistCredit: []ArtistCredit{{Name: "b"}},
			ISRCs:        []string{"BBB"},
			Genres:       []Genre{{Name: "metal"}},
			Relations: []RecordingRelation{{Type: "performance", TargetType: "work", Work: &Work{
				Relations: []WorkRelation{{Type: "writer", TargetType: "artist", Artist: Artist{Name: "b"}}},
			}}},
		}}}}}
		changes := diffMedia(old, current)
		assert.Contains(t, changes, `medium 1 track 1 recording artist credit 1 name: "a" → "b"`)
		assert.Contains(t, changes, `medium 1 track 1 recording composer 1 artist name: "a" → "b"`)
		assert.Contains(t, changes, `medium 1 track 1 recording ISRC 1: "AAA" → "BBB"`)
		assert.Contains(t, changes, `medium 1 track 1 recording genre tag added: "metal"`)
		assert.Contains(t, changes, `medium 1 track 1 recording genre tag removed: "rock"`)
	})
}
