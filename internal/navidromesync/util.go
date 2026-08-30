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

// Package navidromesync reconciles the library-wide playlists/ tree
// against a Navidrome server (internal/navidrome). It assumes any required
// library scan has already been triggered and completed by the caller; this
// package is purely about reconciling playlist content, not about scan timing.
package navidromesync

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/mfinelli/musicrename/internal/sanitize"
)

// libraryRootRootFor derives a playlist file's library-root-root by walking
// up from its path to find the nearest ancestor directory named "playlists"
// and returning that directory's parent.
//
// Falls back to the file's immediate parent directory if no "playlists"
// ancestor is found (shouldn't happen for a file actually inside the
// playlists/ tree, but avoids looping to the filesystem root on an
// unexpected path rather than erroring outright).
func libraryRootRootFor(path string) string {
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

// newPlaylistFilename derives a filename (including the .m3u8 extension,
// excluding the directory) for a newly-discovered remote playlist with the
// given name (sanitized the same way as everything else in this project) and
// disambiguated against whatever already exists in playlistsDir, so two
// differently-cased or punctuated remote names that sanitize to the same stem
// don't collide.
func newPlaylistFilename(playlistsDir, name string) string {
	stem := sanitize.Truncate(sanitize.CleanString(name, sanitize.TrackOverride), 40)
	if stem == "" {
		stem = "playlist"
	}

	candidate := stem
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(playlistsDir, candidate+".m3u8")); os.IsNotExist(err) {
			return candidate + ".m3u8"
		}
		candidate = fmt.Sprintf("%s-%d", stem, i)
	}
}

// stringSlicesEqual reports whether a and b contain the same strings in
// the same order.
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// stringSetsEqual reports whether a and b contain the same strings, in any
// order. Used for comparing target lists: order there carries no meaning,
// and since both sides may reflect content set by a hand edit (locally, or
// remotely via the Navidrome UI/another client) rather than something
// musicrename itself last wrote in its own canonical sorted order, an
// order-sensitive comparison would flag a merely differently ordered but
// otherwise identical list as "changed" and rewrite it for no real reason.
func stringSetsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sortedA := append([]string(nil), a...)
	sortedB := append([]string(nil), b...)
	sort.Strings(sortedA)
	sort.Strings(sortedB)
	return stringSlicesEqual(sortedA, sortedB)
}
