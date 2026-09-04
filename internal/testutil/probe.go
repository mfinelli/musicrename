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

package testutil

import (
	"os/exec"
	"strings"
	"testing"
)

// ProbeText runs a real ffprobe against path with the given -show_entries
// value and returns its raw stdout text, for tests that verify real
// transcode/tagging output (container streams, codecs, dimensions,
// REPLAYGAIN_* tags, etc.) rather than only a fake runner's canned
// response. ofOpts, if given, are appended (comma-joined) to the default
// "noprint_wrappers=1" -of format, for example ProbeText(t, path,
// "stream=codec_type", "nokey=1") to get bare values with no "codec_type="
// prefix on each line.
//
// Requires a real ffprobe binary on PATH.
func ProbeText(t *testing.T, path, showEntries string, ofOpts ...string) string {
	t.Helper()

	of := append([]string{"noprint_wrappers=1"}, ofOpts...)

	out, err := exec.Command(
		"ffprobe", "-v", "error",
		"-show_entries", showEntries,
		"-of", "default="+strings.Join(of, ":"),
		path,
	).Output()
	if err != nil {
		t.Fatalf("ProbeText: ffprobe failed: %v", err)
	}

	return string(out)
}
