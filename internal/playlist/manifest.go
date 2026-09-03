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

// Package playlist handles reading and writing album-local target selection
// manifests: files like ipod.m3u8 or sdcard.m3u8 that live in an album
// directory alongside sums.md5 and list, by filename, which tracks from that
// album are selected for a given sync target.
package playlist

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Extension is the file extension (without the leading dot) used for both
// album-local target-selection manifests (ipod.m3u8, sdcard.m3u8) and
// library-wide playlist files (playlists/*.m3u8). The two share a format
// (extended M3U) even though their content conventions differ.
const Extension = "m3u8"

// Ext is [Extension] with its leading dot (".m3u8"), the form matched
// against filepath.Ext's output.
const Ext = "." + Extension

// ManifestFilename returns the album-local selection manifest filename for
// the given target, e.g. "ipod.m3u8".
func ManifestFilename(target string) string {
	return target + Ext
}

// ReadManifest returns the filenames currently listed in albumDir's
// {target}.m3u8, in file order. A missing manifest is not an error (it
// returns an empty, non-nil slice). Blank lines are ignored; despite reusing
// the .m3u8 extension, album-local manifests carry no ordering semantics and
// no extended-M3U directives (contrast with the library-wide playlists/tree),
// so any line starting with "#" is skipped too, purely as defensive robustness
// rather than because one is expected to appear here.
func ReadManifest(albumDir, target string) ([]string, error) {
	path := filepath.Join(albumDir, ManifestFilename(target))
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}

	names := []string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		names = append(names, line)
	}
	return names, nil
}

// RenameEntry replaces oldName with newName in albumDir's {target}.m3u8, if
// present, preserving every other line and the file's order. changed
// reports whether oldName was found (and thus renamed). A missing
// manifest, or one that doesn't reference oldName, is not an error; it
// simply returns (false, nil), since most tracks are never expected to be
// on any given target's selection list.
func RenameEntry(albumDir, target, oldName, newName string) (bool, error) {
	names, err := ReadManifest(albumDir, target)
	if err != nil {
		return false, err
	}

	changed := false
	for i, n := range names {
		if n == oldName {
			names[i] = newName
			changed = true
		}
	}
	if !changed {
		return false, nil
	}

	if err := WriteManifest(albumDir, target, names); err != nil {
		return false, err
	}
	return true, nil
}

// WriteManifest writes albumDir's {target}.m3u8 to contain exactly names, one
// per line, in the given order.
//
// If names is empty, any existing manifest is deleted instead of writing an
// empty file.
func WriteManifest(albumDir, target string, names []string) error {
	path := filepath.Join(albumDir, ManifestFilename(target))

	if len(names) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}

	var sb strings.Builder
	for _, n := range names {
		sb.WriteString(n)
		sb.WriteString("\n")
	}
	return os.WriteFile(path, []byte(sb.String()), 0644)
}
