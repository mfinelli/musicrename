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

	"github.com/mfinelli/musicrename/internal/hasher"
)

// ApplySort replaces path's entry order with entries and, in the same
// write, records sortSpec as the new remembered #SORT: directive: a
// field-name list (e.g. ["artist", "album", "track"]), the single-element
// ["shuffle"] sentinel, or nil to remove the directive entirely (a later
// `playlist sort` with no explicit fields/--shuffle would then have nothing
// to reapply, matching [GlobalPlaylist.HasSort]'s meaning). Every other
// directive is preserved exactly as it was.
//
// This is a combined SetEntries+"set #SORT:" in one read and one write,
// rather than two separate calls, so the entries and the criteria that
// produced them never briefly disagree on disk between them. Like
// [SetEntries], it performs no existence validation of entries against any
// library root (validation already happened upstream, during
// [ResolveEntryRows] for a field-based sort, or doesn't apply at all for a
// pure positional [ShuffleEntries]).
//
// path must already exist; ApplySort never creates a new playlist file.
//
// libraryRootRoot is used only to resolve path relative to the playlists/
// tree root, for updating playlists/sums.md5: this file's entry is
// refreshed via hasher.UpdateFile if that file already exists. warning is
// non-empty (with a nil error) if that update fails, matching every other
// mutating function in this package's posture toward secondary checksum
// bookkeeping. This function never creates a sums.md5 from scratch.
func ApplySort(libraryRootRoot, path string, entries []string, sortSpec []string) (warning string, err error) {
	if _, statErr := os.Stat(path); statErr != nil {
		return "", fmt.Errorf("%s: %w", path, statErr)
	}

	gp, err := ReadGlobalPlaylist(path)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}

	gp.Entries = entries
	gp.Sort = sortSpec
	gp.HasSort = sortSpec != nil

	if err := WriteGlobalPlaylist(path, gp); err != nil {
		return "", err
	}

	playlistsDir := Dir(libraryRootRoot)
	if _, statErr := os.Stat(filepath.Join(playlistsDir, hasher.SumsFilename)); statErr != nil {
		return "", nil
	}

	rel, relErr := filepath.Rel(playlistsDir, path)
	if relErr != nil {
		return fmt.Sprintf("updating %s: could not resolve %s relative to %s", hasher.SumsFilename, path, playlistsDir), nil
	}
	if updErr := hasher.UpdateFile(playlistsDir, rel); updErr != nil {
		return fmt.Sprintf("updating %s: %v", hasher.SumsFilename, updErr), nil
	}

	return "", nil
}
