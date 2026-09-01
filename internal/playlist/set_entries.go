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

// SetEntries replaces path's entire entry list with entries, in the order
// given, and rewrites the file. Every other directive is preserved exactly
// as it was (read via ReadGlobalPlaylist, then rewritten via
// WriteGlobalPlaylist).
//
// Unlike [AddEntries], SetEntries performs no existence validation against
// any library root on any entry (it just writes the exact list it's
// given). That's intentional: a caller computing a removal (dropping some
// entries from an already-read list, which may itself already contain a
// dangling reference [CheckPlaylists] would report) or a reordering
// shouldn't have a reference silently dropped by a function whose only job
// is "write this list." Validation of a genuinely new entry belongs to
// [AddEntries]; auditing an already-written file's entries belongs to
// [CheckPlaylists].
//
// path must already exist; SetEntries never creates a new playlist file
// (use [Create] for that).
//
// libraryRootRoot is used only to resolve path relative to the playlists/
// tree root (see [Dir]), for updating playlists/sums.md5: this file's entry
// is refreshed via hasher.UpdateFile if that file already exists. warning is
// non-empty (with a nil error) if that update fails, since the entry list
// change itself already succeeded by that point (the same posture
// [SetTargets] and every other mutating function in this package already
// take toward secondary checksum bookkeeping). This function never creates a
// sums.md5 from scratch.
func SetEntries(libraryRootRoot, path string, entries []string) (warning string, err error) {
	if _, statErr := os.Stat(path); statErr != nil {
		return "", fmt.Errorf("%s: %w", path, statErr)
	}

	gp, err := ReadGlobalPlaylist(path)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}

	gp.Entries = entries

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
