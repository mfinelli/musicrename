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
	"crypto/sha1"
	"encoding/base64"
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

// maxTracks is the most tracks a disc can have, and so the number of offsets
// the disc ID is computed from.
const maxTracks = 99

// DiscID returns the disc's MusicBrainz disc ID, computed from the log's table
// of contents as libdiscid does from a disc's: a SHA-1 hash of the first and
// last track numbers and the offset of the lead-out and of each of 99 track
// slots (zero for a track the disc does not have), written in upper-case hex,
// then base64-encoded with "+", "/" and "=" replaced by ".", "_" and "-". Every
// offset counts from the physical start of the disc's audio, which is 150
// sectors before the sector 0 EAC counts from. It returns "" if the log has no
// table of contents, or a track number outside 1 to 99.
func (l *Log) DiscID() string {
	if len(l.TOC) == 0 {
		return ""
	}

	var offsets [maxTracks + 1]int // offsets[0] is the lead-out
	offsets[0] = l.TOC[len(l.TOC)-1].End + 1 + leadInFrames
	for _, t := range l.TOC {
		if t.Number < 1 || t.Number > maxTracks {
			return ""
		}
		offsets[t.Number] = t.Start + leadInFrames
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%02X%02X", l.TOC[0].Number, l.TOC[len(l.TOC)-1].Number)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%08X", o)
	}

	sum := sha1.Sum([]byte(b.String()))
	return strings.NewReplacer("+", ".", "/", "_", "=", "-").
		Replace(base64.StdEncoding.EncodeToString(sum[:]))
}
