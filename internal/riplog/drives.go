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

package riplog

import (
	"regexp"
	"strings"
)

// knownDrives maps a drive, by its normalized name (see normalizeDrive), to the
// read offset correction its rips should use. The offset is a property of the
// drive, not of the disc: it says how far the drive's reading is shifted from
// where the audio really is, and a wrong one shifts every track.
//
// Each value here must be one AccurateRip has confirmed for the drive, that is,
// the offset used for a rip in which every track was accurately ripped. When a
// drive is added or replaced, run EAC's AccurateRip configuration, rip a disc
// AccurateRip knows, and add the drive here. Verify reports an unknown drive
// whose offset AccurateRip confirmed, which is the cue to add it.
//
// This is deliberately a short list in code rather than a copy of the
// AccurateRip offset database: it holds only the drives actually used, so a
// match means something, and an unknown drive is noticed instead of being
// quietly matched to a similarly named one.
var knownDrives = map[string]int{
	"asus sdrw-08v1m-u": 6,
}

var (
	reDriveAdapter  = regexp.MustCompile(`(?i)\s+Adapter:.*$`)
	reDriveRevision = regexp.MustCompile(`(?i)\(revision [^)]*\)`)
	reSpaces        = regexp.MustCompile(`\s+`)
)

// normalizeDrive reduces EAC's description of a drive to a name that is the
// same on every rip with that drive. The log writes it with the drive's
// internal padding and the adapter and ID it was reached through, which
// vary: "ASUS    SDRW-08V1M-U   Adapter: 1  ID: 0" becomes
// "asus sdrw-08v1m-u".
func normalizeDrive(s string) string {
	s = reDriveAdapter.ReplaceAllString(s, "")
	s = reDriveRevision.ReplaceAllString(s, "")
	s = reSpaces.ReplaceAllString(s, " ")
	s = strings.TrimLeft(s, " _-")
	return strings.ToLower(strings.TrimSpace(s))
}
