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
	playlistsDir := filepath.Join(libraryRootRoot, "playlists")

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
		if strings.ToLower(filepath.Ext(path)) != ".m3u8" {
			return nil
		}
		return fn(path)
	})
	if err != nil {
		return fmt.Errorf("scanning playlists at %s: %w", playlistsDir, err)
	}
	return nil
}
