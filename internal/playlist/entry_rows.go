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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mfinelli/musicrename/internal/metadata"
)

// EntryRow is one playlist entry together with whatever could be resolved
// about it against the library on disk, for a caller (such as `playlist
// entries remove`) to display and, optionally, filter-match on.
type EntryRow struct {
	// Rel is the entry exactly as it appears in the playlist file,
	// relative to the library root it was resolved against.
	Rel string
	// Track holds the resolved track's metadata, or nil if Rel doesn't
	// resolve to a file with readable tags (Missing is true in that case).
	Track *metadata.Track
	// Label is a plain-text display label: "Artist — Title" when both are
	// known, falling back to the filename stem when the title tag is
	// absent, or an explanatory note when Missing.
	Label string
	// Missing is true when Rel doesn't resolve to a file on disk, or the
	// file exists but its tags couldn't be read.
	Missing bool
}

// ResolveEntryRows resolves each of entries (relative to libraryRootRoot)
// against the filesystem and, when the file exists and its tags are
// readable, reads them via a fresh [metadata.Reader]. This never scans
// anything beyond the given entries themselves so the cost is always exactly
// len(entries) stats and, for files that exist, len(entries) tag reads, not
// a library-wide scan of any kind so it stays cheap regardless of how
// large the library itself is.
func ResolveEntryRows(libraryRootRoot string, entries []string) []EntryRow {
	reader := metadata.NewReader()
	rows := make([]EntryRow, 0, len(entries))

	for _, rel := range entries {
		abs := filepath.Join(libraryRootRoot, rel)
		row := EntryRow{Rel: rel}

		if _, err := os.Stat(abs); err != nil {
			row.Missing = true
			row.Label = rel + "  (missing — no matching file found)"
			rows = append(rows, row)
			continue
		}

		track := &metadata.Track{Path: abs}
		if err := reader.ReadTrack(track); err != nil {
			row.Missing = true
			row.Label = rel + "  (tags unreadable)"
			rows = append(rows, row)
			continue
		}

		row.Track = track
		row.Label = formatEntryRowLabel(track, rel)
		rows = append(rows, row)
	}

	return rows
}

// formatEntryRowLabel builds a plain-text display label for one resolved
// entry. Unlike a single-album context's disc/track-number label, playlist
// entries can span arbitrary albums and artists, so this favors
// "Artist — Title" instead.
func formatEntryRowLabel(t *metadata.Track, rel string) string {
	title := t.Title
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	}
	if t.Artist != "" {
		return fmt.Sprintf("%s — %s", t.Artist, title)
	}
	return title
}

// FilterEntryRows splits rows into (kept, removed) by matching artist/album
// (case-insensitive, exact match) against each row's Track tags. Either
// filter may be given as an empty string to mean "don't filter on this
// field"; when both are non-empty, both must match (AND, not OR). If both
// are empty (no filter criteria at all) every row is kept and nothing is
// removed, rather than the (dangerously wrong) alternative of treating "no
// criteria to fail" as "everything matches." A row with no Track (Missing)
// never matches, regardless of filters since there's nothing to compare
// against and is always kept.
func FilterEntryRows(rows []EntryRow, artist, album string) (kept, removed []string) {
	if artist == "" && album == "" {
		for _, r := range rows {
			kept = append(kept, r.Rel)
		}
		return kept, nil
	}

	for _, r := range rows {
		if r.Track == nil {
			kept = append(kept, r.Rel)
			continue
		}
		if artist != "" && !strings.EqualFold(r.Track.Artist, artist) {
			kept = append(kept, r.Rel)
			continue
		}
		if album != "" && !strings.EqualFold(r.Track.Album, album) {
			kept = append(kept, r.Rel)
			continue
		}
		removed = append(removed, r.Rel)
	}
	return kept, removed
}
