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

// ownedAudioTagKeys are the tags WriteDerivedAudioTags manages (the only
// ones it's entitled to add, change, or remove). Everything else on the file
// (REPLAYGAIN_* written separately by internal/replaygain, whatever the
// source encoder happened to leave behind, anything else) is preserved
// untouched.
var ownedAudioTagKeys = map[string]bool{
	taglib.Title:  true,
	taglib.Artist: true,
	taglib.Album:  true,
	taglib.Date:   true,
}

// WriteDerivedAudioTags writes path's ARTIST/TITLE (and ALBUM/YEAR, if
// present) tags to exactly match nfo, including removing a tag nfo no
// longer has (e.g., a `video edit` that dropped Album since the last
// extraction). The four tags in ownedAudioTagKeys above authoritatively
// reflect nfo's current state after this call.
//
// This needs a read-modify-write, not a plain [taglib.Clear] write of just
// those four tags: Clear wipes *every* existing tag on the file first, and
// path may already carry tags this function has no business touching
// (REPLAYGAIN_TRACK_GAIN/PEAK in particular, written separately by
// internal/replaygain), which a `--retag` run (rewriting tags without
// touching ReplayGain, by design) must leave completely alone.
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

	existing, err := taglib.ReadTags(path)
	if err != nil {
		return fmt.Errorf("reading existing tags from %s: %w", path, err)
	}

	tags := make(map[string][]string, len(existing)+4)
	for k, v := range existing {
		if !ownedAudioTagKeys[k] {
			tags[k] = v
		}
	}

	tags[taglib.Title] = []string{nfo.Title}
	tags[taglib.Artist] = []string{nfo.Artist}
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
