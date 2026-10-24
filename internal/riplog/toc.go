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
	"fmt"
	"strings"
)

// framesPerSecond is the number of sectors (frames) in a second of CD audio.
const framesPerSecond = 75

// msf formats a count of frames as minutes, seconds and frames, "mm:ss:ff", as
// a cue sheet writes a position.
func msf(frames int) string {
	return fmt.Sprintf("%02d:%02d:%02d",
		frames/(framesPerSecond*60), frames/framesPerSecond%60, frames%framesPerSecond)
}

// leadInFrames is the two seconds between the physical start of a disc's audio
// and sector 0. The table of contents a disc stores, and the disc IDs computed
// from it, count from the former; EAC's log counts from the latter.
const leadInFrames = 150

// FreeDBID returns the disc's freedb (CDDB) ID, the eight hex digits EAC writes
// as REM DISCID in the cue sheet. It is computed from the log's table of
// contents, so it identifies the disc's track layout, not its audio. It returns
// "" if the log has no table of contents.
func (l *Log) FreeDBID() string {
	if len(l.TOC) == 0 {
		return ""
	}

	digitSum := func(n int) int {
		s := 0
		for ; n > 0; n /= 10 {
			s += n % 10
		}
		return s
	}

	sum := 0
	for _, t := range l.TOC {
		sum += digitSum((t.Start + leadInFrames) / framesPerSecond)
	}

	leadout := l.TOC[len(l.TOC)-1].End + 1 + leadInFrames
	length := leadout/framesPerSecond - (l.TOC[0].Start+leadInFrames)/framesPerSecond

	return strings.ToUpper(fmt.Sprintf("%08x", (sum%255)<<24|length<<8|len(l.TOC)))
}
