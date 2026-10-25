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
	"encoding/binary"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// FLACComments returns the vendor string and the Vorbis comments of a FLAC file,
// each as KEY=value, in the order they are in the file. It reads the file itself,
// so that tests do not need metaflac.
func FLACComments(t *testing.T, path string) (vendor string, comments []string) {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(data), 4)
	require.Equal(t, "fLaC", string(data[:4]), "not a FLAC file")

	for at := 4; at+4 <= len(data); {
		last := data[at]&0x80 != 0
		kind := data[at] & 0x7f
		size := int(data[at+1])<<16 | int(data[at+2])<<8 | int(data[at+3])
		body := data[at+4:]
		require.LessOrEqual(t, size, len(body), "a metadata block runs past the end of the file")
		body = body[:size]

		if kind == 4 { // VORBIS_COMMENT
			n := int(binary.LittleEndian.Uint32(body))
			vendor = string(body[4 : 4+n])
			body = body[4+n:]
			count := int(binary.LittleEndian.Uint32(body))
			body = body[4:]
			for range count {
				n := int(binary.LittleEndian.Uint32(body))
				comments = append(comments, string(body[4:4+n]))
				body = body[4+n:]
			}
			return vendor, comments
		}
		if last {
			break
		}
		at += 4 + size
	}
	return "", nil
}
