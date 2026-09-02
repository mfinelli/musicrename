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

package video

import (
	"fmt"

	"go.senan.xyz/taglib"
)

// WriteDerivedAudioTags writes path's ARTIST/TITLE (and ALBUM/YEAR, if
// present) tags to exactly match nfo and replaces whatever tags path
// currently has entirely ([taglib.Clear]) rather than merging into them,
// since path's tags should authoritatively reflect nfo's current state after
// this call, including removing a tag nfo no longer has (e.g. a `video edit`
// that dropped Album since the last extraction), not just adding whatever nfo
// currently lists on top of whatever was already there.
//
// nfo.Title and nfo.Artist are required since "video check" already flags a
// missing title/artist as its own finding well before extraction would ever
// reach this function, so this is a defensive check against a caller bug,
// not an expected runtime condition. Album/Year are written only if
// non-empty, matching their optional (omitempty) status on NFO itself.
func WriteDerivedAudioTags(path string, nfo NFO) error {
	if nfo.Title == "" || nfo.Artist == "" {
		return fmt.Errorf("%s: nfo is missing title or artist", path)
	}

	tags := map[string][]string{
		taglib.Title:  {nfo.Title},
		taglib.Artist: {nfo.Artist},
	}
	if nfo.Album != "" {
		tags[taglib.Album] = []string{nfo.Album}
	}
	if nfo.Year != "" {
		tags[taglib.Date] = []string{nfo.Year}
	}

	if err := taglib.WriteTags(path, tags, taglib.Clear); err != nil {
		return fmt.Errorf("writing tags to %s: %w", path, err)
	}
	return nil
}
