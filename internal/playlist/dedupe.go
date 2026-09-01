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

// DedupeEntries returns entries with duplicate rel paths removed, keeping
// each one's first occurrence and dropping every subsequent repeat;
// relative order is otherwise preserved exactly. removed is how many
// entries were dropped (0 if entries had no duplicates at all).
//
// entries is never itself mutated; only the returned slice reflects the
// deduped result.
func DedupeEntries(entries []string) (deduped []string, removed int) {
	seen := make(map[string]bool, len(entries))
	deduped = make([]string, 0, len(entries))
	for _, e := range entries {
		if seen[e] {
			removed++
			continue
		}
		seen[e] = true
		deduped = append(deduped, e)
	}
	return deduped, removed
}
