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
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MetadataFilename is the name of the per-album snapshot file this
// package reads and writes, at the album's root alongside sums.md5. Like
// folder.jpg, this exact name is hardcoded by the move-planner rather
// than run through its usual filename sanitizer.
const MetadataFilename = "musicbrainz.json.gz"

// ReadSnapshot reads and decompresses the snapshot at dir/MetadataFilename.
// It returns nil, nil (not an error) if no snapshot exists yet then it's the
// caller's job to tell "no baseline recorded yet" apart from a real
// Release with every field zero.
func ReadSnapshot(dir string) (*Release, error) {
	path := filepath.Join(dir, MetadataFilename)

	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("decompressing %s: %w", path, err)
	}
	defer gz.Close()

	var release Release
	if err := json.NewDecoder(gz).Decode(&release); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &release, nil
}

// WriteSnapshot gzip-compresses release as JSON and writes it to
// dir/MetadataFilename, overwriting any existing snapshot.
func WriteSnapshot(dir string, release *Release) error {
	path := filepath.Join(dir, MetadataFilename)

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}

	gz := gzip.NewWriter(f)
	if err := json.NewEncoder(gz).Encode(release); err != nil {
		gz.Close()
		f.Close()
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	if err := gz.Close(); err != nil {
		f.Close()
		return fmt.Errorf("closing gzip writer for %s: %w", path, err)
	}
	// Explicit Close (in addition to gz.Close() above, which only flushes
	// the gzip stream into f's own internal buffer) so a write-flush
	// failure on f itself is actually caught and returned.
	return f.Close()
}

// Diff compares old and current snapshot-then vs. MusicBrainz-now, both
// referring to the same release MBID and returns every field that
// differs between them as a human-readable description, in a stable
// order. An empty slice means no differences. old or current may be nil
// (an empty Release is used in its place), which happens for Diff's
// convenience but should not occur in normal use: the caller (a fresh
// baseline with no prior snapshot) should skip calling Diff entirely
// rather than diff against a nil "old".
func Diff(old, current *Release) []string {
	if old == nil {
		old = &Release{}
	}
	if current == nil {
		current = &Release{}
	}

	var changes []string
	report := func(field, oldVal, newVal string) {
		if oldVal != newVal {
			changes = append(changes, fmt.Sprintf("%s: %q → %q", field, oldVal, newVal))
		}
	}

	report("title", old.Title, current.Title)
	report("disambiguation", old.Disambiguation, current.Disambiguation)
	report("status", old.Status, current.Status)
	report("date", old.Date, current.Date)
	report("country", old.Country, current.Country)
	report("barcode", old.Barcode, current.Barcode)
	report("quality", old.Quality, current.Quality)
	report("packaging", old.Packaging, current.Packaging)

	changes = append(changes, diffArtistCredits("artist", old.ArtistCredit, current.ArtistCredit)...)
	changes = append(changes, diffGenres("release", old.Genres, current.Genres)...)
	changes = append(changes, diffReleaseGroup(old.ReleaseGroup, current.ReleaseGroup)...)
	changes = append(changes, diffLabelInfo(old.LabelInfo, current.LabelInfo)...)
	changes = append(changes, diffMedia(old.Media, current.Media)...)

	return changes
}

// diffReleaseGroup compares two ReleaseGroup values.
func diffReleaseGroup(old, current ReleaseGroup) []string {
	var changes []string
	report := func(field, oldVal, newVal string) {
		if oldVal != newVal {
			changes = append(changes, fmt.Sprintf("release group %s: %q → %q", field, oldVal, newVal))
		}
	}

	report("title", old.Title, current.Title)
	report("disambiguation", old.Disambiguation, current.Disambiguation)
	report("first release date", old.FirstReleaseDate, current.FirstReleaseDate)
	report("primary type", old.PrimaryType, current.PrimaryType)
	report("secondary types", strings.Join(old.SecondaryTypes, ", "), strings.Join(current.SecondaryTypes, ", "))

	changes = append(changes, diffArtistCredits("release group artist", old.ArtistCredit, current.ArtistCredit)...)
	changes = append(changes, diffGenres("release group", old.Genres, current.Genres)...)
	return changes
}

// diffLabelInfo compares two LabelInfo lists positionally, plus reports a
// changed count if the lists differ in length.
func diffLabelInfo(old, current []LabelInfo) []string {
	var changes []string
	if len(old) != len(current) {
		changes = append(changes, fmt.Sprintf("label count: %d → %d", len(old), len(current)))
	}
	for i := 0; i < len(old) && i < len(current); i++ {
		o, c := old[i], current[i]
		prefix := fmt.Sprintf("label %d", i+1)
		if o.CatalogNumber != c.CatalogNumber {
			changes = append(changes, fmt.Sprintf("%s catalog number: %q → %q", prefix, o.CatalogNumber, c.CatalogNumber))
		}
		if o.Label.ID != c.Label.ID {
			changes = append(changes, fmt.Sprintf("%s id: %q → %q", prefix, o.Label.ID, c.Label.ID))
		}
		if o.Label.Name != c.Label.Name {
			changes = append(changes, fmt.Sprintf("%s name: %q → %q", prefix, o.Label.Name, c.Label.Name))
		}
		if o.Label.SortName != c.Label.SortName {
			changes = append(changes, fmt.Sprintf("%s sort name: %q → %q", prefix, o.Label.SortName, c.Label.SortName))
		}
		if o.Label.Disambiguation != c.Label.Disambiguation {
			changes = append(changes, fmt.Sprintf("%s disambiguation: %q → %q", prefix, o.Label.Disambiguation, c.Label.Disambiguation))
		}
		if o.Label.Type != c.Label.Type {
			changes = append(changes, fmt.Sprintf("%s type: %q → %q", prefix, o.Label.Type, c.Label.Type))
		}
	}
	return changes
}

// diffGenres compares two genre lists as sets by name rather than
// positionally: unlike an artist-credit or track list, a genre list is
// community vote data that can reorder or have its vote counts shift
// without anything meaningful actually changing, so only genres added or
// removed are reported. A vote-count change on an already-present genre
// is never reported.
func diffGenres(label string, old, current []Genre) []string {
	oldNames := make(map[string]bool, len(old))
	for _, g := range old {
		oldNames[g.Name] = true
	}
	currentNames := make(map[string]bool, len(current))
	for _, g := range current {
		currentNames[g.Name] = true
	}

	var changes []string
	for _, g := range current {
		if !oldNames[g.Name] {
			changes = append(changes, fmt.Sprintf("%s genre tag added: %q", label, g.Name))
		}
	}
	for _, g := range old {
		if !currentNames[g.Name] {
			changes = append(changes, fmt.Sprintf("%s genre tag removed: %q", label, g.Name))
		}
	}
	return changes
}

// diffISRCs compares two ISRC lists positionally, plus reports a changed
// count if the lists differ in length.
func diffISRCs(label string, old, current []string) []string {
	var changes []string
	if len(old) != len(current) {
		changes = append(changes, fmt.Sprintf("%s ISRC count: %d → %d", label, len(old), len(current)))
	}
	for i := 0; i < len(old) && i < len(current); i++ {
		if old[i] != current[i] {
			changes = append(changes, fmt.Sprintf("%s ISRC %d: %q → %q", label, i+1, old[i], current[i]))
		}
	}
	return changes
}

// diffArtistCredits compares two artist-credit lists positionally (index
// N of old against index N of current) under label, plus reports a
// changed credit count if the lists differ in length.
func diffArtistCredits(label string, old, current []ArtistCredit) []string {
	var changes []string
	if len(old) != len(current) {
		changes = append(changes, fmt.Sprintf("%s credit count: %d → %d", label, len(old), len(current)))
	}
	for i := 0; i < len(old) && i < len(current); i++ {
		o, c := old[i], current[i]
		prefix := fmt.Sprintf("%s credit %d", label, i+1)
		if o.Name != c.Name {
			changes = append(changes, fmt.Sprintf("%s name: %q → %q", prefix, o.Name, c.Name))
		}
		if o.JoinPhrase != c.JoinPhrase {
			changes = append(changes, fmt.Sprintf("%s join phrase: %q → %q", prefix, o.JoinPhrase, c.JoinPhrase))
		}
		if o.Artist.ID != c.Artist.ID {
			changes = append(changes, fmt.Sprintf("%s artist id: %q → %q", prefix, o.Artist.ID, c.Artist.ID))
		}
		if o.Artist.Name != c.Artist.Name {
			changes = append(changes, fmt.Sprintf("%s artist name: %q → %q", prefix, o.Artist.Name, c.Artist.Name))
		}
		if o.Artist.SortName != c.Artist.SortName {
			changes = append(changes, fmt.Sprintf("%s artist sort name: %q → %q", prefix, o.Artist.SortName, c.Artist.SortName))
		}
		if o.Artist.Disambiguation != c.Artist.Disambiguation {
			changes = append(changes, fmt.Sprintf("%s artist disambiguation: %q → %q", prefix, o.Artist.Disambiguation, c.Artist.Disambiguation))
		}
		if o.Artist.Type != c.Artist.Type {
			changes = append(changes, fmt.Sprintf("%s artist type: %q → %q", prefix, o.Artist.Type, c.Artist.Type))
		}
		if o.Artist.Country != c.Artist.Country {
			changes = append(changes, fmt.Sprintf("%s artist country: %q → %q", prefix, o.Artist.Country, c.Artist.Country))
		}
		changes = append(changes, diffGenres(prefix+" artist", o.Artist.Genres, c.Artist.Genres)...)
	}
	return changes
}

// diffComposers compares two [Composer] lists (from [Recording.Composers])
// positionally under label, plus reports a changed count if the lists
// differ in length.
func diffComposers(label string, old, current []Composer) []string {
	var changes []string
	if len(old) != len(current) {
		changes = append(changes, fmt.Sprintf("%s composer count: %d → %d", label, len(old), len(current)))
	}
	for i := 0; i < len(old) && i < len(current); i++ {
		o, c := old[i], current[i]
		prefix := fmt.Sprintf("%s composer %d", label, i+1)
		if o.Type != c.Type {
			changes = append(changes, fmt.Sprintf("%s relationship type: %q → %q", prefix, o.Type, c.Type))
		}
		if o.Artist.ID != c.Artist.ID {
			changes = append(changes, fmt.Sprintf("%s artist id: %q → %q", prefix, o.Artist.ID, c.Artist.ID))
		}
		if o.Artist.Name != c.Artist.Name {
			changes = append(changes, fmt.Sprintf("%s artist name: %q → %q", prefix, o.Artist.Name, c.Artist.Name))
		}
	}
	return changes
}

// diffMedia compares two media (disc/side) lists positionally, and each
// medium's track list positionally in turn.
func diffMedia(old, current []Medium) []string {
	var changes []string
	if len(old) != len(current) {
		changes = append(changes, fmt.Sprintf("medium count: %d → %d", len(old), len(current)))
	}
	for i := 0; i < len(old) && i < len(current); i++ {
		o, c := old[i], current[i]
		label := fmt.Sprintf("medium %d", i+1)
		if o.Format != c.Format {
			changes = append(changes, fmt.Sprintf("%s format: %q → %q", label, o.Format, c.Format))
		}
		if o.Title != c.Title {
			changes = append(changes, fmt.Sprintf("%s title: %q → %q", label, o.Title, c.Title))
		}
		if len(o.Tracks) != len(c.Tracks) {
			changes = append(changes, fmt.Sprintf("%s track count: %d → %d", label, len(o.Tracks), len(c.Tracks)))
		}
		for j := 0; j < len(o.Tracks) && j < len(c.Tracks); j++ {
			ot, ct := o.Tracks[j], c.Tracks[j]
			tlabel := fmt.Sprintf("%s track %d", label, j+1)
			if ot.Title != ct.Title {
				changes = append(changes, fmt.Sprintf("%s title: %q → %q", tlabel, ot.Title, ct.Title))
			}
			if ot.Number != ct.Number {
				changes = append(changes, fmt.Sprintf("%s number: %q → %q", tlabel, ot.Number, ct.Number))
			}
			if ot.Length != ct.Length {
				changes = append(changes, fmt.Sprintf("%s length: %dms → %dms", tlabel, ot.Length, ct.Length))
			}
			changes = append(changes, diffArtistCredits(tlabel+" artist", ot.ArtistCredit, ct.ArtistCredit)...)

			or, cr := ot.Recording, ct.Recording
			rlabel := tlabel + " recording"
			if or.ID != cr.ID {
				changes = append(changes, fmt.Sprintf("%s id: %q → %q", rlabel, or.ID, cr.ID))
			}
			if or.Title != cr.Title {
				changes = append(changes, fmt.Sprintf("%s title: %q → %q", rlabel, or.Title, cr.Title))
			}
			if or.Length != cr.Length {
				changes = append(changes, fmt.Sprintf("%s length: %dms → %dms", rlabel, or.Length, cr.Length))
			}
			if or.Disambiguation != cr.Disambiguation {
				changes = append(changes, fmt.Sprintf("%s disambiguation: %q → %q", rlabel, or.Disambiguation, cr.Disambiguation))
			}
			if or.FirstReleaseDate != cr.FirstReleaseDate {
				changes = append(changes, fmt.Sprintf("%s first release date: %q → %q", rlabel, or.FirstReleaseDate, cr.FirstReleaseDate))
			}
			changes = append(changes, diffArtistCredits(rlabel+" artist", or.ArtistCredit, cr.ArtistCredit)...)
			changes = append(changes, diffComposers(rlabel, or.Composers(), cr.Composers())...)
			changes = append(changes, diffISRCs(rlabel, or.ISRCs, cr.ISRCs)...)
			changes = append(changes, diffGenres(rlabel, or.Genres, cr.Genres)...)
		}
	}
	return changes
}
