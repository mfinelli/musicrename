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
	"sort"
	"strings"
)

// playlistNamePrefix is the extended-M3U directive line carrying a
// library-wide playlist's real display name, independent of its
// (ASCII-sanitized) filename.
const playlistNamePrefix = "#PLAYLIST:"

// sortPrefix is the extended-M3U directive line carrying a library-wide
// playlist's remembered `playlist sort` criteria, so a later re-run with no
// explicit fields (or --shuffle) can reapply the same choice without the
// caller needing to remember or retype it.
const sortPrefix = "#SORT:"

// playlistDirectivePrefixes lists every directive prefix a library-wide
// playlist file recognizes, in the fixed order they're written in by
// [WriteGlobalPlaylist] (not necessarily file order, for a stable,
// predictable [DuplicateDirectives] result).
var playlistDirectivePrefixes = []string{playlistNamePrefix, navidromeIDPrefix, targetsPrefix, sortPrefix}

// DuplicateDirectives returns the directive prefixes that appear more than
// once in the playlist file at path (in playlistDirectivePrefixes order),
// or nil if none do. A missing file returns (nil, nil).
//
// A duplicate is never treated as an error by any reader in this package:
// [ReadNavidromeID] and [ReadTargets] each resolve to whichever occurrence
// they encounter first, while [ReadGlobalPlaylist] resolves to whichever it
// encounters last (parsed via a single switch over all lines in order)
// so behavior is well-defined either way, just silently inconsistent
// between readers. DuplicateDirectives exists purely so `playlist check`
// can surface this as a passive audit finding.
func DuplicateDirectives(path string) ([]string, error) {
	lines, err := readLines(path)
	if err != nil {
		return nil, err
	}

	counts := make(map[string]int, len(playlistDirectivePrefixes))
	for _, line := range lines {
		for _, prefix := range playlistDirectivePrefixes {
			if strings.HasPrefix(line, prefix) {
				counts[prefix]++
				break
			}
		}
	}

	var dups []string
	for _, prefix := range playlistDirectivePrefixes {
		if counts[prefix] > 1 {
			dups = append(dups, prefix)
		}
	}
	return dups, nil
}

// GlobalPlaylist is the parsed content of a library-wide playlist file
// (playlists/*.m3u8): its extended-M3U directives plus its plain entries,
// in order. Unlike an album-local manifest, entry order here is meaningful.
//
// The three "Has*"-paired fields (NavidromeID, Targets, and Sort need one;
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
	// Sort is the #SORT: directive's value, split on commas, if present:
	// either a field-name list (e.g. ["artist", "album", "track"]) or the
	// single-element ["shuffle"] sentinel. Interpreting which (and
	// validating field names) is `playlist sort`'s job, not this
	// package's; ReadGlobalPlaylist/WriteGlobalPlaylist treat it exactly
	// as mechanically as Targets, no semantic awareness of its contents.
	Sort []string
	// HasSort reports whether a #SORT: directive was present at all. When
	// false, there's no remembered sort criteria to reapply.
	HasSort bool
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
				for p := range strings.SplitSeq(raw, ",") {
					p = strings.TrimSpace(p)
					if p != "" {
						gp.Targets = append(gp.Targets, p)
					}
				}
			}
		case strings.HasPrefix(line, sortPrefix):
			gp.HasSort = true
			gp.Sort = []string{}
			raw := strings.TrimSpace(strings.TrimPrefix(line, sortPrefix))
			if raw != "" {
				for p := range strings.SplitSeq(raw, ",") {
					p = strings.TrimSpace(p)
					if p != "" {
						gp.Sort = append(gp.Sort, p)
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
// #PLAYLIST:, #NAVIDROME-ID:, #TARGETS:, #SORT: (each only if its
// corresponding Has*/non-empty condition holds), followed by the entries,
// one per line. Unlike Targets (alphabetized on write, since it's a set and
// order carries no meaning), Sort is written in the exact order given: for
// a field list, that order determines sort precedence, so alphabetizing it
// would silently corrupt the very thing it's meant to remember.
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
		sorted := append([]string(nil), gp.Targets...)
		sort.Strings(sorted)
		sb.WriteString(targetsPrefix)
		sb.WriteString(strings.Join(sorted, ","))
		sb.WriteString("\n")
	}
	if gp.HasSort {
		sb.WriteString(sortPrefix)
		sb.WriteString(strings.Join(gp.Sort, ","))
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
