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

// Names lists every valid target name, in a stable order for display
// (e.g. in error messages and command help text).
var Names = []string{"ipod", "sdcard"}

// Valid reports whether name is a recognized target.
func Valid(name string) bool {
	for _, n := range Names {
		if n == name {
			return true
		}
	}
	return false
}
