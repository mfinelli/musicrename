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

import "strings"

// CheckDirectiveOrder reports whether the directives present in the playlist
// file at path appear in the same relative order [WriteGlobalPlaylist] itself
// would write them in: #PLAYLIST:, #NAVIDROME-ID:, #TARGETS:, #SORT:
// (filtered down to whichever of those this particular file actually has); a
// directive that's absent from the file doesn't create a gap or otherwise
// affect the comparison. This is a consistency check, not a correctness one:
// every reader in this package is prefix-based and reads each directive
// independently of the others' position, so a misordered file isn't
// functionally broken it's just inconsistent with every file `musicrename`
// would produce.
//
// Repetition isn't this function's concern (see [DuplicateDirectives] for
// that) (a repeated directive's position in got reflects only its first
// occurrence in the file).
//
// ok is true, with got and want both nil, when the order is already
// canonical (including the trivial case of zero or one directive present,
// which can never be "out of order"). Otherwise ok is false and got/want
// hold the actual first-occurrence order and the order it should be in,
// respectively, for a caller to build a finding message from. A missing
// file returns ok=true, err=nil (nothing to check, and not a finding since
// playlist check has its own, separate handling for a file that doesn't
// resolve at all).
func CheckDirectiveOrder(path string) (ok bool, got, want []string, err error) {
	lines, err := readLines(path)
	if err != nil {
		return false, nil, nil, err
	}

	seen := make(map[string]bool, len(playlistDirectivePrefixes))
	for _, line := range lines {
		for _, prefix := range playlistDirectivePrefixes {
			if strings.HasPrefix(line, prefix) {
				if !seen[prefix] {
					seen[prefix] = true
					got = append(got, prefix)
				}
				break
			}
		}
	}

	present := make(map[string]bool, len(got))
	for _, p := range got {
		present[p] = true
	}
	for _, p := range playlistDirectivePrefixes {
		if present[p] {
			want = append(want, p)
		}
	}

	if directiveOrderEqual(got, want) {
		return true, nil, nil, nil
	}
	return false, got, want, nil
}

// directiveOrderEqual compares two directive-prefix slices for exact,
// order-sensitive equality. want is always built from got's own contents
// (see CheckDirectiveOrder), so the two are guaranteed equal length
// whenever they're equal at all (a length mismatch here would indicate a
// bug in that construction, not a legitimately different result).
func directiveOrderEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
