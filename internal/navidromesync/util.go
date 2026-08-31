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

	"github.com/mfinelli/musicrename/internal/hasher"
	"github.com/mfinelli/musicrename/internal/playlist"
	"github.com/mfinelli/musicrename/internal/sanitize"
)

// updateLocalSums updates path's entry in playlists/sums.md5 via
// [hasher.UpdateFile], for callers that just wrote (created or overwrote)
// path's content. A no-op if playlists/sums.md5 doesn't exist at all since
// this package never creates one from scratch.
func updateLocalSums(libraryRootRoot, path string) error {
	playlistsDir := playlist.Dir(libraryRootRoot)
	rel, err := filepath.Rel(playlistsDir, path)
	if err != nil {
		return fmt.Errorf("resolving %s relative to %s: %w", path, playlistsDir, err)
	}
	return hasher.UpdateFile(playlistsDir, rel)
}

// removeLocalSums removes path's entry from playlists/sums.md5 via
// [hasher.RemoveFile], for callers that just deleted path from disk. A
// no-op if playlists/sums.md5 doesn't exist, or has no entry for path.
func removeLocalSums(libraryRootRoot, path string) error {
	playlistsDir := playlist.Dir(libraryRootRoot)
	rel, err := filepath.Rel(playlistsDir, path)
	if err != nil {
		return fmt.Errorf("resolving %s relative to %s: %w", path, playlistsDir, err)
	}
	return hasher.RemoveFile(playlistsDir, rel)
}

// libraryRootRootFor is a thin alias for [playlist.LibraryRootRootFor],
// kept so call sites in this package don't need the playlist package
// qualifier for something used this frequently.
func libraryRootRootFor(path string) string {
	return playlist.LibraryRootRootFor(path)
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
