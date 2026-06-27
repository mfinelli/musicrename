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

// Package lyrics fetches lyrics from LRCLIB and embeds them into audio file
// tags. It supports FLAC, MP3, and M4A files with format-appropriate storage:
//
//   - FLAC: synced LRC text in LYRICS, plain text in UNSYNCEDLYRICS.
//   - MP3:  plain text in USLT (via go-taglib's normalised LYRICS key).
//   - M4A:  plain text in ©lyr (via go-taglib's normalised LYRICS key).
//
// All LRC timestamps are normalised to [mm:ss.xx] / [hh:mm:ss.xx] format
// (two-digit centiseconds) before embedding, matching the behaviour of the
// --standardize force.xx flag from the previous Python workflow.
//
// Lyrics are fetched from the LRCLIB public API using a four-step strategy
// that progressively relaxes the duration constraint before falling back to a
// fuzzy title/artist/album search. Requests are rate-limited client-side to
// five per second as a courtesy to the free public API.
//
// The typical call sequence is:
//
//	summary, err := lyrics.Fetch(ctx, tracks, false, func(path string, status lyrics.LyricStatus) {
//	    fmt.Printf("\r  → %s", filepath.Base(path))
//	})
package lyrics

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

var (
	// timestampRe matches LRC timestamps inside either [...] or <...> brackets.
	// It handles optional hours (hh:), required minutes and seconds (mm:ss),
	// and an optional fractional component of 2 or 3 digits (.xx or .xxx).
	//
	// LRC metadata tags such as [ar:Artist] or [al:Album] are intentionally
	// excluded because their inner content starts with letters, not digits.
	timestampRe = regexp.MustCompile(`([\[<])((?:\d{1,2}:)?\d{1,3}:\d{1,2}(?:\.\d{1,3})?)([\]>])`)

	// splitRe decomposes a raw timestamp string into its named groups:
	// optional hours, minutes, seconds, and optional fractional digits.
	splitRe = regexp.MustCompile(`^(?:(\d{1,2}):)?(\d{1,3}):(\d{1,2})(?:\.(\d{1,3}))?$`)
)

// standardizeLRC normalises all LRC timestamps in lrc to [mm:ss.xx] or
// [hh:mm:ss.xx] format (two-digit centiseconds). Timestamps inside word-by-word
// <...> brackets are normalised in the same way. Overflow values such as
// 75 seconds are corrected via duration arithmetic. LRC metadata header tags
// (e.g. [ar:Artist]) are left untouched because they do not match the digit-
// only timestamp pattern.
//
// This function replicates the behaviour of the Python lyrict tool's
// --standardize force.xx mode that was used in the previous workflow.
func standardizeLRC(lrc string) string {
	return timestampRe.ReplaceAllStringFunc(lrc, func(match string) string {
		open := match[0]
		close := match[len(match)-1]
		inner := match[1 : len(match)-1]
		return string(open) + normalizeTimestamp(inner) + string(close)
	})
}

// normalizeTimestamp parses a single raw timestamp string (without surrounding
// brackets) and returns it normalised to mm:ss.xx or hh:mm:ss.xx. If the
// timestamp cannot be parsed it is returned unchanged.
func normalizeTimestamp(ts string) string {
	m := splitRe.FindStringSubmatch(ts)
	if m == nil {
		return ts
	}

	// m[1] = optional hours, m[2] = minutes, m[3] = seconds, m[4] = fractional.
	var hours, minutes, seconds int
	if m[1] != "" {
		hours, _ = strconv.Atoi(m[1])
	}
	minutes, _ = strconv.Atoi(m[2])
	seconds, _ = strconv.Atoi(m[3])

	// Convert the fractional field to milliseconds by left-padding to three
	// digits, so that ".5" → 500 ms and ".12" → 120 ms. This matches the
	// ljust(3, '0') behaviour of the Python lyrict --standardize force.xx mode.
	var ms int
	if m[4] != "" {
		padded := m[4]
		for len(padded) < 3 {
			padded += "0"
		}
		ms, _ = strconv.Atoi(padded[:3])
	}

	// Accumulate into a single Duration so that Go handles any overflow
	// (e.g. 75 seconds → 1 minute 15 seconds) transparently.
	total := time.Duration(hours)*time.Hour +
		time.Duration(minutes)*time.Minute +
		time.Duration(seconds)*time.Second +
		time.Duration(ms)*time.Millisecond

	h := int(total / time.Hour)
	total -= time.Duration(h) * time.Hour
	min := int(total / time.Minute)
	total -= time.Duration(min) * time.Minute
	sec := int(total / time.Second)
	total -= time.Duration(sec) * time.Second
	cs := int(total.Milliseconds()) / 10 // centiseconds 0–99

	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d.%02d", h, min, sec, cs)
	}
	return fmt.Sprintf("%02d:%02d.%02d", min, sec, cs)
}
