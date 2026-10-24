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
	"bytes"
	"errors"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// errNotLog is returned by decode for data that is not text at all.
var errNotLog = errors.New("not a text file")

// decode converts the bytes of a log to a string with "\n" line endings.
//
// EAC writes its logs as UTF-16 little-endian with a byte order mark, with
// CRLF line endings. UTF-16 big-endian and UTF-8 (with or without a byte order
// mark) are also accepted, which covers a log that has been converted by an
// editor or by another tool.
func decode(data []byte) (string, error) {
	var text string

	switch {
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}):
		text = decodeUTF16(data[2:], false)
	case bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		text = decodeUTF16(data[2:], true)
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		text = string(data[3:])
	case looksLikeUTF16LE(data):
		// No byte order mark, but every other byte is zero, as in mostly-ASCII
		// UTF-16 text.
		text = decodeUTF16(data, false)
	default:
		text = string(data)
	}

	if !utf8.ValidString(text) {
		return "", errNotLog
	}

	// EAC pads the end of a log with NULs in some versions; the text ends at
	// the first one.
	if i := strings.IndexByte(text, 0); i >= 0 {
		text = text[:i]
	}

	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text, nil
}

// decodeUTF16 decodes UTF-16 text without its byte order mark.
func decodeUTF16(b []byte, bigEndian bool) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		if bigEndian {
			u[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		} else {
			u[i] = uint16(b[2*i+1])<<8 | uint16(b[2*i])
		}
	}
	return string(utf16.Decode(u))
}

// looksLikeUTF16LE reports whether data with no byte order mark is probably
// UTF-16LE: at least one byte in four is a zero in an odd position.
func looksLikeUTF16LE(data []byte) bool {
	n := len(data)
	if n < 4 || n%2 != 0 {
		return false
	}
	zeros := 0
	for i := 1; i < n; i += 2 {
		if data[i] == 0 {
			zeros++
		}
	}
	return zeros*4 >= n/2
}
