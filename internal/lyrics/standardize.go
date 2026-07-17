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

package lyrics

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	// timestampRe matches LRC timestamps inside either [...] or <...> brackets.
	// It handles optional hours (hh:), required minutes and seconds (mm:ss),
	// and an optional fractional component of 1–3 digits (.x, .xx, or .xxx).
	//
	// LRC metadata tags such as [ar:Artist] or [al:Album] are intentionally
	// excluded because their inner content starts with letters, not digits.
	timestampRe = regexp.MustCompile(`([\[<])((?:\d{1,2}:)?\d{1,3}:\d{1,2}(?:\.\d{1,3})?)([\]>])`)

	// splitRe decomposes a raw timestamp string into its components:
	// optional hours, minutes, seconds, and optional fractional digits.
	splitRe = regexp.MustCompile(`^(?:(\d{1,2}):)?(\d{1,3}):(\d{1,2})(?:\.(\d{1,3}))?$`)

	// headerLineRe matches known LRC metadata header tag lines. The tag must
	// occupy the entire line (ignoring surrounding whitespace) to avoid
	// accidentally matching lyric lines that happen to contain bracket text.
	headerLineRe = regexp.MustCompile(`(?i)^\s*\[(ti|ar|al|au|lr|length|by|offset|re|tool|ve):[^\]]*\]\s*$`)

	// offsetTagRe extracts the millisecond offset value from [offset:±N] lines.
	offsetTagRe = regexp.MustCompile(`(?i)^\s*\[offset:\s*([+-]?\d+)\s*\]\s*$`)

	// spaceAfterTimestampRe strips whitespace immediately following a
	// normalised square-bracket timestamp. Applied after normalization so the
	// pattern can rely on the fixed [NN:NN.NN] or [NN:NN:NN.NN] shape.
	// Angle-bracket <...> timestamps are intentionally excluded: spaces after
	// word-level sync markers are part of the lyric content.
	spaceAfterTimestampRe = regexp.MustCompile(`(\[\d{2}:\d{2}(?::\d{2})?\.\d{2}\])\s+`)
)

// standardizeLRC normalises an LRC string through a four-step pipeline:
//
//  1. Parse the [offset:±N] tag (milliseconds) if present.
//  2. Strip all header metadata lines (ti, ar, al, au, lr, length, by, offset,
//     re, tool, ve) and comment lines (# …).
//  3. Normalise all timestamps to [mm:ss.xx] / [hh:mm:ss.xx] form, applying
//     the offset so the embedded result is self-contained.
//  4. Remove any whitespace between the closing ] of a line-level timestamp
//     and the start of the lyric text, as required by the LRC spec.
//
// The returned string has leading and trailing blank lines trimmed. Internal
// blank lines (representing instrumental breaks) are preserved.
func standardizeLRC(lrc string) string {
	offsetMs := parseOffset(lrc)

	lines := strings.Split(lrc, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, "\r") // handle Windows line endings
		if isHeaderLine(line) || isCommentLine(line) {
			continue
		}
		out = append(out, applyTimestamps(line, offsetMs))
	}

	return strings.TrimSpace(strings.Join(out, "\n"))
}

// parseOffset scans lrc for an [offset:±N] tag and returns its value in
// milliseconds. Returns 0 if no offset tag is present.
func parseOffset(lrc string) int {
	for line := range strings.SplitSeq(lrc, "\n") {
		if m := offsetTagRe.FindStringSubmatch(strings.TrimRight(line, "\r")); m != nil {
			v, _ := strconv.Atoi(m[1])
			return v
		}
	}
	return 0
}

// isHeaderLine reports whether line is a recognised LRC metadata tag that
// should be stripped during standardisation.
func isHeaderLine(line string) bool {
	return headerLineRe.MatchString(line)
}

// isCommentLine reports whether line is an LRC comment (starts with #).
func isCommentLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "#")
}

// applyTimestamps normalises all timestamps in line (applying offsetMs) and
// strips any whitespace between the last line-level timestamp and the lyric text.
func applyTimestamps(line string, offsetMs int) string {
	result := timestampRe.ReplaceAllStringFunc(line, func(match string) string {
		open := match[0]
		close := match[len(match)-1]
		inner := match[1 : len(match)-1]
		return string(open) + normalizeTimestampWithOffset(inner, offsetMs) + string(close)
	})
	return spaceAfterTimestampRe.ReplaceAllString(result, "$1")
}

// normalizeTimestamp parses and normalises a single raw timestamp string
// (without surrounding brackets) to mm:ss.xx or hh:mm:ss.xx form. If the
// string cannot be parsed it is returned unchanged.
func normalizeTimestamp(ts string) string {
	return normalizeTimestampWithOffset(ts, 0)
}

// normalizeTimestampWithOffset is the same as normalizeTimestamp but adds
// offsetMs milliseconds before formatting. Negative results are clamped to zero.
func normalizeTimestampWithOffset(ts string, offsetMs int) string {
	m := splitRe.FindStringSubmatch(ts)
	if m == nil {
		return ts
	}

	var hours, minutes, seconds int
	if m[1] != "" {
		hours, _ = strconv.Atoi(m[1])
	}
	minutes, _ = strconv.Atoi(m[2])
	seconds, _ = strconv.Atoi(m[3])

	// Convert the fractional field to milliseconds by left-padding to three
	// digits, so that ".5" -> 500 ms and ".12" -> 120 ms.
	var ms int
	if m[4] != "" {
		padded := m[4]
		for len(padded) < 3 {
			padded += "0"
		}
		ms, _ = strconv.Atoi(padded[:3])
	}

	// Accumulate into a Duration so Go handles any overflow transparently,
	// then apply the global offset.
	total := max(time.Duration(hours)*time.Hour+
		time.Duration(minutes)*time.Minute+
		time.Duration(seconds)*time.Second+
		time.Duration(ms)*time.Millisecond+
		time.Duration(offsetMs)*time.Millisecond, 0)

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
