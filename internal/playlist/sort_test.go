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

package playlist

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mfinelli/musicrename/internal/metadata"
)

func TestValidSortField(t *testing.T) {
	t.Run("every ValidSortFields entry is valid", func(t *testing.T) {
		for _, f := range ValidSortFields {
			assert.True(t, ValidSortField(string(f)))
		}
	})

	t.Run("an unrecognized name is invalid", func(t *testing.T) {
		assert.False(t, ValidSortField("nonsense"))
		assert.False(t, ValidSortField("shuffle"))
	})
}

func TestValidSortFieldNames(t *testing.T) {
	t.Run("is ValidSortFields as plain strings, same order", func(t *testing.T) {
		wantNames := make([]string, len(ValidSortFields))
		for i, f := range ValidSortFields {
			wantNames[i] = string(f)
		}
		assert.Equal(t, wantNames, ValidSortFieldNames)
	})

	t.Run("every name is itself a valid sort field", func(t *testing.T) {
		for _, n := range ValidSortFieldNames {
			assert.True(t, ValidSortField(n))
		}
	})
}

func TestSortEntries(t *testing.T) {
	rowFor := func(rel string, track *metadata.Track) EntryRow {
		return EntryRow{Rel: rel, Track: track}
	}

	t.Run("single field, case-insensitive", func(t *testing.T) {
		rows := []EntryRow{
			rowFor("a.flac", &metadata.Track{Artist: "Pink Floyd"}),
			rowFor("b.flac", &metadata.Track{Artist: "the beatles"}),
			rowFor("c.flac", &metadata.Track{Artist: "ABBA"}),
		}
		got := SortEntries(rows, []SortField{SortArtist})
		assert.Equal(t, []string{"c.flac", "a.flac", "b.flac"}, got)
	})

	t.Run("multi-field: second field breaks ties in the first", func(t *testing.T) {
		rows := []EntryRow{
			rowFor("a.flac", &metadata.Track{Album: "Z", TrackNumber: new(2)}),
			rowFor("b.flac", &metadata.Track{Album: "A", TrackNumber: new(5)}),
			rowFor("c.flac", &metadata.Track{Album: "Z", TrackNumber: new(1)}),
		}
		got := SortEntries(rows, []SortField{SortAlbum, SortTrack})
		assert.Equal(t, []string{"b.flac", "c.flac", "a.flac"}, got)
	})

	t.Run("empty string values sort last within the field", func(t *testing.T) {
		rows := []EntryRow{
			rowFor("a.flac", &metadata.Track{Artist: ""}),
			rowFor("b.flac", &metadata.Track{Artist: "ABBA"}),
		}
		got := SortEntries(rows, []SortField{SortArtist})
		assert.Equal(t, []string{"b.flac", "a.flac"}, got)
	})

	t.Run("a nil TrackNumber sorts last, but explicit zero (hidden track) does not", func(t *testing.T) {
		rows := []EntryRow{
			rowFor("nil.flac", &metadata.Track{TrackNumber: nil}),
			rowFor("zero.flac", &metadata.Track{TrackNumber: new(0)}),
			rowFor("one.flac", &metadata.Track{TrackNumber: new(1)}),
		}
		got := SortEntries(rows, []SortField{SortTrack})
		assert.Equal(t, []string{"zero.flac", "one.flac", "nil.flac"}, got)
	})

	t.Run("a zero DiscNumber is treated as absent, sorts last", func(t *testing.T) {
		rows := []EntryRow{
			rowFor("zero.flac", &metadata.Track{DiscNumber: 0}),
			rowFor("two.flac", &metadata.Track{DiscNumber: 2}),
			rowFor("one.flac", &metadata.Track{DiscNumber: 1}),
		}
		got := SortEntries(rows, []SortField{SortDisc})
		assert.Equal(t, []string{"one.flac", "two.flac", "zero.flac"}, got)
	})

	t.Run("unresolved (Missing) rows always sort after resolved ones", func(t *testing.T) {
		rows := []EntryRow{
			{Rel: "missing1.flac", Track: nil, Missing: true},
			rowFor("z.flac", &metadata.Track{Artist: "Z"}),
			{Rel: "missing2.flac", Track: nil, Missing: true},
			rowFor("a.flac", &metadata.Track{Artist: "A"}),
		}
		got := SortEntries(rows, []SortField{SortArtist})
		assert.Equal(t, []string{"a.flac", "z.flac", "missing1.flac", "missing2.flac"}, got)
	})

	t.Run("fully tied rows preserve original relative order (stable)", func(t *testing.T) {
		rows := []EntryRow{
			rowFor("a.flac", &metadata.Track{Artist: "Same"}),
			rowFor("b.flac", &metadata.Track{Artist: "Same"}),
			rowFor("c.flac", &metadata.Track{Artist: "Same"}),
		}
		got := SortEntries(rows, []SortField{SortArtist})
		assert.Equal(t, []string{"a.flac", "b.flac", "c.flac"}, got)
	})

	t.Run("an empty fields list preserves original order entirely", func(t *testing.T) {
		rows := []EntryRow{
			rowFor("b.flac", &metadata.Track{Artist: "B"}),
			rowFor("a.flac", &metadata.Track{Artist: "A"}),
		}
		got := SortEntries(rows, nil)
		assert.Equal(t, []string{"b.flac", "a.flac"}, got)
	})

	t.Run("does not mutate the input rows slice", func(t *testing.T) {
		rows := []EntryRow{
			rowFor("b.flac", &metadata.Track{Artist: "B"}),
			rowFor("a.flac", &metadata.Track{Artist: "A"}),
		}
		SortEntries(rows, []SortField{SortArtist})
		assert.Equal(t, "b.flac", rows[0].Rel, "the original slice must be untouched")
	})

	t.Run("albumartist, year, and title fields all participate", func(t *testing.T) {
		rows := []EntryRow{
			rowFor("a.flac", &metadata.Track{AlbumArtist: "B", Year: "2020", Title: "Z"}),
			rowFor("b.flac", &metadata.Track{AlbumArtist: "A", Year: "1999", Title: "A"}),
		}
		assert.Equal(t, []string{"b.flac", "a.flac"}, SortEntries(rows, []SortField{SortAlbumArtist}))
		assert.Equal(t, []string{"b.flac", "a.flac"}, SortEntries(rows, []SortField{SortYear}))
		assert.Equal(t, []string{"b.flac", "a.flac"}, SortEntries(rows, []SortField{SortTitle}))
	})
}

func TestShuffleEntries(t *testing.T) {
	t.Run("returns every original entry exactly once", func(t *testing.T) {
		entries := []string{"a.flac", "b.flac", "c.flac", "d.flac", "e.flac"}
		got := ShuffleEntries(entries)
		assert.ElementsMatch(t, entries, got)
		assert.Len(t, got, len(entries))
	})

	t.Run("does not mutate the input slice", func(t *testing.T) {
		entries := []string{"a.flac", "b.flac", "c.flac"}
		original := append([]string(nil), entries...)
		ShuffleEntries(entries)
		assert.Equal(t, original, entries)
	})

	t.Run("empty input returns empty output, no panic", func(t *testing.T) {
		assert.NotPanics(t, func() {
			got := ShuffleEntries([]string{})
			assert.Empty(t, got)
		})
	})

	t.Run("a single entry returns unchanged", func(t *testing.T) {
		got := ShuffleEntries([]string{"only.flac"})
		assert.Equal(t, []string{"only.flac"}, got)
	})
}
