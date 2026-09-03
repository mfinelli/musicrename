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
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Dir returns the library-wide playlists/ directory under libraryRootRoot.
// Exported so callers outside this package (cmd/playlist_sums.go and future
// mutating playlist commands) reference the same "playlists" name WalkTree
// uses internally, rather than each hardcoding it separately.
func Dir(libraryRootRoot string) string {
	return filepath.Join(libraryRootRoot, "playlists")
}

// LibraryRootRootFor derives a playlist file's library-root-root by walking
// up from its path to find the nearest ancestor directory named "playlists"
// and returning that directory's parent (the inverse of [Dir]). Useful for
// a command whose only input is a single playlist file path (no separate
// library-root-root argument), so it can still resolve playlists/sums.md5
// and other library-relative concerns from that path alone.
//
// Falls back to the file's immediate parent directory if no "playlists"
// ancestor is found (shouldn't happen for a file actually inside the
// playlists/ tree, but avoids looping to the filesystem root on an
// unexpected path rather than erroring outright).
func LibraryRootRootFor(path string) string {
	dir := filepath.Dir(path)
	for {
		if filepath.Base(dir) == "playlists" {
			return filepath.Dir(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Dir(path)
		}
		dir = parent
	}
}

// WalkTree calls fn once for every .m3u8 file found by walking
// libraryRootRoot's playlists/ directory recursively. Subdirectories
// carry no scoping meaning under the flat, #TARGETS:-based structure
// (they're purely organizational if used at all) so the walk doesn't
// assume a flat layout.
//
// A libraryRootRoot with no playlists/ directory at all is not an error;
// WalkTree simply calls fn zero times. fn is called with each file's full
// path; a non-nil error from fn stops the walk and is returned as-is.
func WalkTree(libraryRootRoot string, fn func(path string) error) error {
	playlistsDir := Dir(libraryRootRoot)

	err := filepath.WalkDir(playlistsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == playlistsDir {
				// No playlists/ directory at all; nothing to walk.
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != Ext {
			return nil
		}
		return fn(path)
	})
	if err != nil {
		return fmt.Errorf("scanning playlists at %s: %w", playlistsDir, err)
	}
	return nil
}
