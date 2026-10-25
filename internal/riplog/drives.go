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

// driveProfile is what is known about one drive: the settings that depend on
// the drive rather than on the author's preferences.
type driveProfile struct {
	// Offset is the read offset correction the drive's rips should use. It is a
	// property of the drive, not of the disc: it says how far the drive's
	// reading is shifted from where the audio really is, and a wrong one shifts
	// every track.
	Offset int
	// Overread is whether "Overread into Lead-In and Lead-Out" should be on.
	// To correct for a drive's offset EAC needs a few samples from beyond the
	// edge of the audio: past the end of the last track for a positive offset,
	// before the start of the first for a negative one. A drive that can read
	// into the lead-in and lead-out supplies them; one that cannot leaves EAC
	// to fill them in with silence. Which kind a drive is cannot be told from
	// the log, so it is recorded here, as found out for the drive.
	Overread bool
}

// knownDrives maps a drive, by its normalized name (see normalizeDrive), to its
// profile.
//
// Each offset must be one AccurateRip has confirmed for the drive, that is, the
// offset used for a rip in which every track was accurately ripped. When a
// drive is added or replaced, run EAC's AccurateRip configuration, rip a disc
// AccurateRip knows, and add the drive here, with Overread set according to
// whether the drive can overread (EAC can test for it) and turned on or off in
// EAC to match. Verify reports an unknown drive whose offset AccurateRip
// confirmed, which is the cue to add it.
//
// This is deliberately a short list in code rather than a copy of the
// AccurateRip offset database: it holds only the drives actually used, so a
// match means something, and an unknown drive is noticed instead of being
// quietly matched to a similarly named one.
var knownDrives = map[string]driveProfile{
	"asus sdrw-08v1m-u": {Offset: 6, Overread: false},
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
