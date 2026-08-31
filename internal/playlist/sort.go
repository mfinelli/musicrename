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
	"math/rand"
	"sort"
	"strings"

	"github.com/mfinelli/musicrename/internal/metadata"
)

// SortField identifies one metadata.Track field usable as a `playlist sort`
// key.
type SortField string

const (
	SortArtist      SortField = "artist"
	SortAlbumArtist SortField = "albumartist"
	SortAlbum       SortField = "album"
	SortYear        SortField = "year"
	SortDisc        SortField = "disc"
	SortTrack       SortField = "track"
	SortTitle       SortField = "title"
)

// ValidSortFields lists every recognized SortField, in a stable order for
// --help text and error messages.
var ValidSortFields = []SortField{
	SortArtist, SortAlbumArtist, SortAlbum, SortYear, SortDisc, SortTrack, SortTitle,
}

// ValidSortField reports whether s names a recognized SortField.
func ValidSortField(s string) bool {
	for _, f := range ValidSortFields {
		if string(f) == s {
			return true
		}
	}
	return false
}

// SortEntries returns a new ordering of rows' Rel values, stably sorted by
// fields in precedence order (the first field breaks the most ties, and so
// on down); anything left unresolved by every given field keeps its
// original relative order, matching sort.SliceStable's own guarantee.
//
// A row whose Track couldn't be resolved (Missing: see [ResolveEntryRows])
// sorts after every resolved row, in original relative order among
// themselves; there's no metadata to sort it by. Within a resolved row, an
// absent value for the field being compared (empty string, or a DiscNumber
// of zero) sorts after any present value for that same field, on either side
// of the comparison. String fields compare case-insensitively.
//
// rows is never itself reordered; only the returned slice reflects the new
// order. fields must already be validated (see [ValidSortField]) so an
// unrecognized field is silently treated as a no-op tiebreaker rather than
// an error, since validation is the caller's responsibility, not this
// function's.
func SortEntries(rows []EntryRow, fields []SortField) []string {
	sorted := make([]EntryRow, len(rows))
	copy(sorted, rows)

	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]

		aMissing, bMissing := a.Track == nil, b.Track == nil
		if aMissing != bMissing {
			return !aMissing // the resolved row sorts first
		}
		if aMissing {
			return false // both unresolved: preserve original relative order
		}

		for _, f := range fields {
			if c := compareSortField(a.Track, b.Track, f); c != 0 {
				return c < 0
			}
		}
		return false // fully tied on every given field: preserve original order
	})

	rels := make([]string, len(sorted))
	for i, r := range sorted {
		rels[i] = r.Rel
	}
	return rels
}

// compareSortField compares a and b on a single field, returning a negative
// number if a sorts first, positive if b does, or zero if they're tied on
// this field (including "both absent"). An unrecognized field is always a
// tie (see [SortEntries]'s doc comment on why that's the caller's problem,
// not an error here).
func compareSortField(a, b *metadata.Track, f SortField) int {
	switch f {
	case SortArtist:
		return compareSortStrings(a.Artist, b.Artist)
	case SortAlbumArtist:
		return compareSortStrings(a.AlbumArtist, b.AlbumArtist)
	case SortAlbum:
		return compareSortStrings(a.Album, b.Album)
	case SortYear:
		return compareSortStrings(a.Year, b.Year)
	case SortTitle:
		return compareSortStrings(a.Title, b.Title)
	case SortDisc:
		// Zero means "tag absent" for DiscNumber (metadata.Track's
		// documented semantics) unlike TrackNumber, there's no explicit
		// "hidden disc zero" concept to preserve.
		return compareSortAbsentLast(a.DiscNumber == 0, b.DiscNumber == 0, a.DiscNumber-b.DiscNumber)
	case SortTrack:
		return compareSortTrackNumber(a.TrackNumber, b.TrackNumber)
	default:
		return 0
	}
}

// compareSortStrings compares a and b case-insensitively; an empty string
// sorts after any non-empty one on either side, matching every string
// field's "tag absent" convention in metadata.Track.
func compareSortStrings(a, b string) int {
	return compareSortAbsentLast(a == "", b == "", strings.Compare(strings.ToLower(a), strings.ToLower(b)))
}

// compareSortTrackNumber compares two *int: a nil pointer (tag absent)
// sorts after any present value, including the explicit zero value (a
// hidden/pre-gap track), which is a real, present value distinct from
// "absent" per metadata.Track's own documented TrackNumber semantics
// unlike DiscNumber, zero must not be treated as missing here.
func compareSortTrackNumber(a, b *int) int {
	aAbsent, bAbsent := a == nil, b == nil
	if aAbsent || bAbsent {
		return compareSortAbsentLast(aAbsent, bAbsent, 0)
	}
	return *a - *b
}

// compareSortAbsentLast is the shared "absent sorts after present"
// tiebreak: if exactly one side is absent, it sorts second regardless of
// cmp; if both or neither are absent, cmp (the real field comparison, or 0
// for "both absent") decides.
func compareSortAbsentLast(aAbsent, bAbsent bool, cmp int) int {
	if aAbsent != bAbsent {
		if aAbsent {
			return 1
		}
		return -1
	}
	if aAbsent {
		return 0
	}
	return cmp
}

// ShuffleEntries returns entries in a new random order. Unlike SortEntries,
// no track metadata is needed at all, so a caller doesn't need to resolve
// entries via [ResolveEntryRows] first for this path since shuffling is purely
// positional.
//
// entries is never itself reordered; only the returned slice reflects the
// new order.
func ShuffleEntries(entries []string) []string {
	shuffled := append([]string(nil), entries...)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	return shuffled
}
