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

package navidrome

import "fmt"

// ErrCodeNotFound is the Subsonic API error code for "the requested data
// was not found". The confirmed-absent case a pull/delete operation is
// allowed to self-heal on, as opposed to a generic failure (auth, network,
// 5xx) which must never be treated as confirmed absence.
const ErrCodeNotFound = 70

// ErrCode extracts a Subsonic API error code from an error returned by the
// go-subsonic library's typed methods (GetPlaylist, and similar).
//
// The library discards the structured error object it parses internally
// and returns only a formatted string, "Error #<code>: <message>" with no
// typed error a caller could otherwise inspect via errors.As. This parses
// that fixed, verified format back out.
//
// ok is false whenever err doesn't match that exact shape (including when
// err is nil, a network-level error, or anything else unexpected) so a
// caller never mistakes an unrecognized failure for a specific, known
// error code. Callers must check ok, not just compare the returned code
// against a sentinel, precisely to preserve that distinction.
func ErrCode(err error) (code int, ok bool) {
	if err == nil {
		return 0, false
	}

	var c int
	n, scanErr := fmt.Sscanf(err.Error(), "Error #%d:", &c)
	if scanErr != nil || n != 1 {
		return 0, false
	}
	return c, true
}
