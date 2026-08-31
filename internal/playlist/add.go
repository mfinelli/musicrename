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

// AddEntries appends relPaths, in the order given, to path's entry list,
// then rewrites the file. Each relPath must already be relative to
// libraryRootRoot, matching every other entry already in a GlobalPlaylist
// (see [ReadGlobalPlaylist]) and resolving a caller-supplied path (which may
// be relative to the current working directory, or absolute) into that form
// is the caller's responsibility, not this function's.
//
// A relPath that doesn't resolve to a real file under libraryRootRoot is
// skipped and reported as a warning rather than accepted silently (playlist
// check already audits an already-written playlist for exactly this, so
// validating here is a fail-fast convenience at add time, not a substitute
// for that audit. Every other relPath is still processed even if an earlier
// one is skipped.
//
// This is always exactly one read, and (only if there's anything to
// actually add) one write, regardless of how many relPaths are given: no
// library-wide scan of any kind is performed, so this stays cheap
// however large the library is.
//
// path must already exist; AddEntries never creates a new playlist file
// (use [Create] for that). If playlists/sums.md5 already exists, path's
// entry in it is refreshed via hasher.UpdateFile; a failure there is
// reported as a warning too, not an error, since the entries have already
// been added successfully by that point (the same posture every other
// mutating command in this package already takes toward secondary checksum
// bookkeeping).
func AddEntries(libraryRootRoot, path string, relPaths []string) (added, warnings []string, err error) {
	if _, statErr := os.Stat(path); statErr != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, statErr)
	}

	gp, err := ReadGlobalPlaylist(path)
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", path, err)
	}

	for _, rel := range relPaths {
		if _, statErr := os.Stat(filepath.Join(libraryRootRoot, rel)); statErr != nil {
			warnings = append(warnings, fmt.Sprintf("%q does not resolve to a file; skipped", rel))
			continue
		}
		gp.Entries = append(gp.Entries, rel)
		added = append(added, rel)
	}

	if len(added) == 0 {
		return added, warnings, nil
	}

	if err := WriteGlobalPlaylist(path, gp); err != nil {
		return nil, warnings, err
	}

	playlistsDir := Dir(libraryRootRoot)
	if _, statErr := os.Stat(filepath.Join(playlistsDir, hasher.SumsFilename)); statErr == nil {
		sumsRel, relErr := filepath.Rel(playlistsDir, path)
		if relErr != nil {
			warnings = append(warnings, fmt.Sprintf(
				"updating %s: could not resolve %s relative to %s", hasher.SumsFilename, path, playlistsDir,
			))
		} else if updErr := hasher.UpdateFile(playlistsDir, sumsRel); updErr != nil {
			warnings = append(warnings, fmt.Sprintf("updating %s: %v", hasher.SumsFilename, updErr))
		}
	}

	return added, warnings, nil
}
