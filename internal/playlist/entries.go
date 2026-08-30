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
	"errors"
	"os"
	"strings"
)

// navidromeIDPrefix is the extended-M3U directive line recording a
// library-wide playlist's correlated Navidrome playlist ID.
const navidromeIDPrefix = "#NAVIDROME-ID:"

// targetsPrefix is the extended-M3U directive line declaring which sync
// targets a library-wide playlist applies to. Absent entirely, a playlist
// applies to every target.
const targetsPrefix = "#TARGETS:"

// readLines reads path and returns its lines, trimmed of surrounding
// whitespace. A missing file returns (nil, nil) rather than an error.
func readLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	raw := strings.Split(string(data), "\n")
	lines := make([]string, len(raw))
	for i, l := range raw {
		lines[i] = strings.TrimSpace(l)
	}
	return lines, nil
}

// ReadEntries reads the plain (non-comment, non-blank) lines from the
// playlist file at path, in file order.
//
// Unlike [ReadManifest], which is keyed by an album directory and target
// name (album-local manifests), this takes an explicit file path: playlists
// in the library-wide playlists/ tree live at arbitrary discovered locations,
// not a predictable per-album filename.
//
// A missing file returns an empty, non-nil slice, matching ReadManifest's
// treatment of a missing manifest.
func ReadEntries(path string) ([]string, error) {
	lines, err := readLines(path)
	if err != nil {
		return nil, err
	}

	entries := []string{}
	for _, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		entries = append(entries, line)
	}
	return entries, nil
}

// ReadNavidromeID returns the value of a #NAVIDROME-ID: directive line
// in the playlist file at path, if present. ok is false if the file has no
// such line, including when the file does not exist.
func ReadNavidromeID(path string) (id string, ok bool, err error) {
	lines, err := readLines(path)
	if err != nil {
		return "", false, err
	}

	for _, line := range lines {
		if after, ok0 := strings.CutPrefix(line, navidromeIDPrefix); ok0 {
			return strings.TrimSpace(after), true, nil
		}
	}
	return "", false, nil
}

// ReadTargets returns the target names declared by a #TARGETS: directive
// line in the playlist file at path, if present. ok is false if the file has
// no such line, including when the file does not exist, the caller should
// treat that as "applies to every target," not as an error or an empty
// selection.
//
// A present-but-empty directive (e.g. "#TARGETS:" with nothing after it)
// returns (empty slice, true, nil): an explicit, deliberate empty list is
// kept distinct from "no directive at all."
func ReadTargets(path string) (names []string, ok bool, err error) {
	lines, err := readLines(path)
	if err != nil {
		return nil, false, err
	}

	for _, line := range lines {
		if !strings.HasPrefix(line, targetsPrefix) {
			continue
		}

		raw := strings.TrimSpace(strings.TrimPrefix(line, targetsPrefix))
		if raw == "" {
			return []string{}, true, nil
		}

		names = []string{}
		for p := range strings.SplitSeq(raw, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				names = append(names, p)
			}
		}
		return names, true, nil
	}
	return nil, false, nil
}
