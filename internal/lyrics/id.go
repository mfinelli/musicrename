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

package lyrics

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseID extracts a numeric LRCLIB track ID from s. s may be:
//
//   - a bare numeric ID ("12345")
//   - a full LRCLIB URL ending in that ID, with or without a trailing slash
//     or query string/fragment (e.g. "https://lrclib.net/api/get/12345",
//     "https://lrclib.net/12345/", "lrclib.net/12345?foo=bar")
//
// It returns an error if no numeric ID can be found at the end of s.
func ParseID(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty lrclib id/url")
	}

	if id, err := strconv.Atoi(s); err == nil {
		return id, nil
	}

	trimmed := s
	if i := strings.IndexAny(trimmed, "?#"); i >= 0 {
		trimmed = trimmed[:i]
	}
	trimmed = strings.TrimRight(trimmed, "/")

	parts := strings.Split(trimmed, "/")
	last := parts[len(parts)-1]

	id, err := strconv.Atoi(last)
	if err != nil {
		return 0, fmt.Errorf("could not find a numeric lrclib id in %q", s)
	}
	return id, nil
}
