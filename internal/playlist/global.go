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
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// playlistNamePrefix is the extended-M3U directive line carrying a
// library-wide playlist's real display name, independent of its
// (ASCII-sanitized) filename.
const playlistNamePrefix = "#PLAYLIST:"

// GlobalPlaylist is the parsed content of a library-wide playlist file
// (playlists/*.m3u8): its extended-M3U directives plus its plain entries,
// in order. Unlike an album-local manifest, entry order here is meaningful.
//
// The three "Has*"-paired fields (only NavidromeID and Targets need one;
// Name is always meaningful whether empty or not) distinguish "directive
// absent" from "directive present but empty," since those mean different
// things: an absent #TARGETS: means "applies to every target", while
// present-but-empty means an explicit, deliberate empty list.
type GlobalPlaylist struct {
	// Name is the #PLAYLIST: directive's value (the playlist's real
	// display name, independent of its filename).
	Name string
	// NavidromeID is the #NAVIDROME-ID: directive's value, if present.
	NavidromeID string
	// HasNavidromeID reports whether a #NAVIDROME-ID: directive was
	// present at all — a playlist that has never been pushed has none.
	HasNavidromeID bool
	// Targets is the #TARGETS: directive's value, split on commas, if
	// present.
	Targets []string
	// HasTargets reports whether a #TARGETS: directive was present at
	// all. When false, the playlist applies to every target; Targets
	// itself doesn't distinguish "absent" from "present but empty" on its
	// own, since both can be a nil/empty slice.
	HasTargets bool
	// Entries are the plain, non-directive lines, in file order.
	Entries []string
}

// ReadGlobalPlaylist parses the library-wide playlist file at path in a
// single pass. A missing file returns an empty, non-nil *GlobalPlaylist
// (all fields at their zero value) rather than an error, matching
// [ReadEntries]'s treatment of a missing file.
func ReadGlobalPlaylist(path string) (*GlobalPlaylist, error) {
	lines, err := readLines(path)
	if err != nil {
		return nil, err
	}

	gp := &GlobalPlaylist{}
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, playlistNamePrefix):
			gp.Name = strings.TrimSpace(strings.TrimPrefix(line, playlistNamePrefix))
		case strings.HasPrefix(line, navidromeIDPrefix):
			gp.NavidromeID = strings.TrimSpace(strings.TrimPrefix(line, navidromeIDPrefix))
			gp.HasNavidromeID = true
		case strings.HasPrefix(line, targetsPrefix):
			gp.HasTargets = true
			gp.Targets = []string{}
			raw := strings.TrimSpace(strings.TrimPrefix(line, targetsPrefix))
			if raw != "" {
				for _, p := range strings.Split(raw, ",") {
					p = strings.TrimSpace(p)
					if p != "" {
						gp.Targets = append(gp.Targets, p)
					}
				}
			}
		case line == "" || strings.HasPrefix(line, "#"):
			// Blank line, or a comment/directive this package doesn't
			// know about (skip rather than treat as an entry, so a
			// forward-compatible directive added later doesn't get
			// mistaken for a track path).
			continue
		default:
			gp.Entries = append(gp.Entries, line)
		}
	}
	return gp, nil
}

// WriteGlobalPlaylist writes gp to path as a library-wide playlist file,
// creating its parent directory if necessary and overwriting whatever was
// there before. Directive lines are written in the fixed order
// #PLAYLIST:, #NAVIDROME-ID:, #TARGETS: (each only if its corresponding
// Has*/non-empty condition holds), followed by the entries, one per line.
func WriteGlobalPlaylist(path string, gp *GlobalPlaylist) error {
	var sb strings.Builder

	if gp.Name != "" {
		sb.WriteString(playlistNamePrefix)
		sb.WriteString(gp.Name)
		sb.WriteString("\n")
	}
	if gp.HasNavidromeID {
		sb.WriteString(navidromeIDPrefix)
		sb.WriteString(gp.NavidromeID)
		sb.WriteString("\n")
	}
	if gp.HasTargets {
		sb.WriteString(targetsPrefix)
		sb.WriteString(strings.Join(gp.Targets, ","))
		sb.WriteString("\n")
	}
	for _, e := range gp.Entries {
		sb.WriteString(e)
		sb.WriteString("\n")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
