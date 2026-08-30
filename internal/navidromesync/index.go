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

package navidromesync

import (
	"strconv"

	subsonic "github.com/supersonic-app/go-subsonic/subsonic"
)

// songIndexPageSize is how many songs buildSongIndex requests per search3
// page. Navidrome specifically optimizes empty-query search3 pagination for
// full-library sync: this is a documented, server-side-optimized pattern
// ("the way clients like Symfonium mirror the whole library"), not an
// undocumented trick, so one reasonably large page size keeps the number
// of requests small without needing to guess at a hard server-side maximum.
const songIndexPageSize = 500

// buildSongIndex enumerates the server's entire song catalog once, via
// paginated search3 calls with an empty query, and returns a map of path ->
// Navidrome song ID.
//
// This exists specifically to avoid one search request per track being
// pushed: a single playlist with thousands of entries, or PushAll pushing
// several playlists in one run, would otherwise mean thousands of individual
// search requests. The index is built once per push run and reused for every
// entry resolved during that run.
func buildSongIndex(client *subsonic.Client) (map[string]string, error) {
	index := make(map[string]string)

	for offset := 0; ; offset += songIndexPageSize {
		result, err := client.Search3("", map[string]string{
			"songCount":   strconv.Itoa(songIndexPageSize),
			"songOffset":  strconv.Itoa(offset),
			"albumCount":  "0",
			"artistCount": "0",
		})
		if err != nil {
			return nil, err
		}

		for _, song := range result.Song {
			if song.Path != "" {
				index[song.Path] = song.ID
			}
		}

		if len(result.Song) < songIndexPageSize {
			break
		}
	}

	return index, nil
}
