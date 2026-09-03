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

// Package target defines the small, hardcoded set of device sync targets.
// Targets are intentionally not user-configurable: a short, stable,
// personally-curated list lives in code rather than behind a config file.
package target

import "slices"

// Names lists every valid target name, in a stable order for display
// (e.g. in error messages and command help text).
var Names = []string{"ipod", "sdcard"}

// Valid reports whether name is a recognized target.
func Valid(name string) bool {
	return slices.Contains(Names, name)
}

// VideoCapableNames lists every target name whose Definition has
// SupportsVideo set, in the same stable order as Names.
var VideoCapableNames = videoCapableNames()

func videoCapableNames() []string {
	names := make([]string, 0, len(Names))
	for _, n := range Names {
		if def, ok := DefinitionFor(n); ok && def.SupportsVideo {
			names = append(names, n)
		}
	}
	return names
}

// SrcSumsFilename returns the on-device {target}.src.md5 sidecar filename
// for the given target: the source-hash record for a derived file (transcoded
// audio, resized artwork), sharing the same md5sum-compatible line format as
// sums.md5 itself (internal/hasher.ReadSums) but keyed by which source
// produced each on-device file, not the on-device file's own content.
func SrcSumsFilename(name string) string {
	return name + ".src.md5"
}
