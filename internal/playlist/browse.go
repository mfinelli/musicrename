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
	"os"
	"strings"
)

// ListSubdirectories returns dir's direct subdirectory names, excluding
// dotfiles, for an interactive entry browser. Reads no file content or tags
// (just directory entries) so it stays cheap regardless of how large the
// library is.
//
// This covers every level of a browse session except the library-root-root
// level itself, which instead needs the reserved playlists/videos names
// excluded too (see devicesync.LibraryRoots); that special case is the
// caller's responsibility rather than this function's, since
// internal/devicesync already imports internal/playlist and a reverse
// import would cycle.
func ListSubdirectories(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		names = append(names, e.Name())
	}
	return names, nil
}

// FilterNames returns the subset of names containing query as a
// case-insensitive substring, in their original relative order. An empty
// query returns names completely unchanged (the same slice, not a copy).
func FilterNames(names []string, query string) []string {
	if query == "" {
		return names
	}

	q := strings.ToLower(query)
	filtered := make([]string, 0, len(names))
	for _, n := range names {
		if strings.Contains(strings.ToLower(n), q) {
			filtered = append(filtered, n)
		}
	}
	return filtered
}
