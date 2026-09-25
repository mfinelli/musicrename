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

import "errors"

// ErrNotFound is returned by getRelease when MusicBrainz has no release
// matching the given MBID (HTTP 404) which means that the MBID may have been
// merged into another release, or deleted.
var ErrNotFound = errors.New("musicbrainz: release not found")

// Release is the subset of a MusicBrainz release lookup's response this
// package tracks: everything relevant to what a tagger such as Picard
// would write back to a file. Not MusicBrainz's full response schema
// (relationships, tags, ratings, and similar are not fetched or stored) —
// just release, artist-credit, and track-listing fields, fetched via
// inc=recordings+artist-credits.
type Release struct {
	ID             string         `json:"id"`
	Title          string         `json:"title"`
	Disambiguation string         `json:"disambiguation"`
	Status         string         `json:"status"`
	Date           string         `json:"date"`
	Country        string         `json:"country"`
	Barcode        string         `json:"barcode"`
	Quality        string         `json:"quality"`
	Packaging      string         `json:"packaging"`
	ArtistCredit   []ArtistCredit `json:"artist-credit"`
	Media          []Medium       `json:"media"`
	ReleaseGroup   ReleaseGroup   `json:"release-group"`
	LabelInfo      []LabelInfo    `json:"label-info"`
	Genres         []Genre        `json:"genres"`
}

// ReleaseGroup is the abstract "album" concept a Release is one specific
// edition of e.g., "Back in Black" the release group, as distinct from a
// particular 2003 US CD reissue of it (the Release). Corresponds to the
// MUSICBRAINZ_RELEASEGROUPID tag.
type ReleaseGroup struct {
	ID               string         `json:"id"`
	Title            string         `json:"title"`
	Disambiguation   string         `json:"disambiguation"`
	FirstReleaseDate string         `json:"first-release-date"`
	PrimaryType      string         `json:"primary-type"` // e.g. "Album", "EP", "Single"
	SecondaryTypes   []string       `json:"secondary-types"`
	ArtistCredit     []ArtistCredit `json:"artist-credit"`
	Genres           []Genre        `json:"genres"`
}

// LabelInfo is one release-to-label association, corresponding to the
// LABEL and CATALOGNUMBER tags. A release can be issued by more than one
// label at once, hence the slice on Release rather than a single value.
type LabelInfo struct {
	CatalogNumber string `json:"catalog-number"`
	Label         Label  `json:"label"`
}

// Label is the subset of a MusicBrainz label entity embedded in a
// LabelInfo.
type Label struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	SortName       string `json:"sort-name"`
	Disambiguation string `json:"disambiguation"`
	Type           string `json:"type"` // e.g. "Imprint", "Original Production"
}

// Genre is one community-voted genre tag, on a Release, ReleaseGroup,
// Recording, or Artist. Unlike this package's other fields, a genre list
// isn't editorial data with one correct answer: it's a folksonomy tally
// (Count is how many users applied it) that can reorder or have its
// counts shift as people tag and un-tag things, without anything having
// actually been corrected. Diff treats a genre list accordingly, as a set
// of names rather than a positional list, and never reports on Count.
type Genre struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Disambiguation string `json:"disambiguation"`
	Count          int    `json:"count"`
}

// ArtistCredit is one entry in a release's, a track's, or a recording's
// artist-credit list: the credited name (which may differ from the
// artist's canonical name e.g., a featured-artist stylization)
// alongside the underlying artist entity, plus the join phrase
// MusicBrainz uses to string multiple credits together into a single
// display string ("X feat. Y").
type ArtistCredit struct {
	Name       string `json:"name"`
	JoinPhrase string `json:"joinphrase"`
	Artist     Artist `json:"artist"`
}

// Artist is the subset of a MusicBrainz artist entity embedded in an
// ArtistCredit.
type Artist struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	SortName       string  `json:"sort-name"`
	Disambiguation string  `json:"disambiguation"`
	Type           string  `json:"type"` // e.g. "Group", "Person"
	Country        string  `json:"country"`
	Genres         []Genre `json:"genres"`
}

// Medium is one disc/side of a release (MusicBrainz's term for a single
// physical or logical unit of media within a release). Title is usually
// empty for a single-disc release; a multi-disc release may label one,
// e.g. "Bonus Disc".
type Medium struct {
	Position int     `json:"position"`
	Format   string  `json:"format"`
	Title    string  `json:"title"`
	Tracks   []Track `json:"tracks"`
}

// Track is one track on a Medium: its own position/title/length/artist-
// credit as they appear on *this* release, plus the underlying Recording
// it's a performance of. A track's own fields can differ from its
// Recording's e.g., a different length due to a radio edit, or a
// different artist-credit on a compilation than the recording's own
// canonical one and so both are tracked independently rather than treating
// them as redundant.
type Track struct {
	ID           string         `json:"id"`
	Position     int            `json:"position"`
	Number       string         `json:"number"`
	Title        string         `json:"title"`
	Length       int            `json:"length"` // milliseconds
	ArtistCredit []ArtistCredit `json:"artist-credit"`
	Recording    Recording      `json:"recording"`
}

// Recording is the subset of a MusicBrainz recording entity embedded in a
// Track i.e., the abstract "performance" a track is an instance of, which can
// be shared across multiple releases (for example an album track and a later
// compilation's copy of it).
type Recording struct {
	ID               string              `json:"id"`
	Title            string              `json:"title"`
	Length           int                 `json:"length"` // milliseconds
	Disambiguation   string              `json:"disambiguation"`
	FirstReleaseDate string              `json:"first-release-date"`
	ArtistCredit     []ArtistCredit      `json:"artist-credit"`
	Relations        []RecordingRelation `json:"relations"`
	ISRCs            []string            `json:"isrcs"`
	Genres           []Genre             `json:"genres"`
}

// RecordingRelation is one entry in a Recording's relations list, which
// mixes personnel credits (engineer, producer, instrument, vocal, ...)
// with, for a "performance" relation, the linked Work which is the abstract
// composition this recording is a performance of. musicrename's tags carry
// composer credit but not personnel credit, so only the latter is
// surfaced (via [Recording.Composers]); personnel-credit relations are
// decoded here (Go's JSON decoder can't selectively skip parts of a
// heterogeneous array) but are never read back out.
type RecordingRelation struct {
	Type       string `json:"type"`
	TargetType string `json:"target-type"`
	Work       *Work  `json:"work"`
}

// Work is the abstract composition a Recording is a performance of
// e.g., "Hells Bells" the song, as distinct from AC/DC's specific 1980
// recording of it.
type Work struct {
	ID        string         `json:"id"`
	Title     string         `json:"title"`
	Relations []WorkRelation `json:"relations"`
}

// WorkRelation is one entry in a Work's relations list: either a writer/
// composer/lyricist credit to an Artist, or a Work-to-Work link ("based
// on", "other version", etc. so Artist is zero-valued for these, and
// [Recording.Composers] skips them via TargetType). Type is preserved
// exactly as MusicBrainz reports it rather than assumed to be "writer":
// its relationship-type vocabulary distinguishes composer, lyricist, and
// writer credits as separate values.
type WorkRelation struct {
	Type       string `json:"type"`
	TargetType string `json:"target-type"`
	Artist     Artist `json:"artist"`
}

// Composer is one writer/composer/lyricist credit on a Recording's
// linked Work, as returned by [Recording.Composers].
type Composer struct {
	Type   string `json:"type"`
	Artist Artist `json:"artist"`
}

// Composers returns every writer/composer/lyricist credit found on the
// Work(s) linked to this Recording via a "performance" relation
// (typically just one Work, but the data model allows more: a medley or
// interpolation for example). Personnel credits and Work-to-Work relations
// are ignored. Order matches the order MusicBrainz returned the underlying
// relations in.
func (r Recording) Composers() []Composer {
	var composers []Composer
	for _, rel := range r.Relations {
		if rel.TargetType != "work" || rel.Work == nil {
			continue
		}
		for _, wrel := range rel.Work.Relations {
			if wrel.TargetType != "artist" {
				continue
			}
			composers = append(composers, Composer{Type: wrel.Type, Artist: wrel.Artist})
		}
	}
	return composers
}
