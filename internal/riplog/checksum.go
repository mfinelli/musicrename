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
	"encoding/binary"
	"encoding/hex"
	"strings"
	"unicode/utf16"
)

// Exact Audio Copy ends a log with a line of the form
//
//	==== Log checksum 6FAD...14AD ====
//
// which is a signature of the text above it. A log that has been edited since
// EAC wrote it, or damaged, no longer matches its checksum. The scheme is not
// documented by EAC; it was worked out by others, and this follows their
// descriptions and checks against real logs.
//
// The text above the checksum line, with every line ending and byte order mark
// removed, is encoded as UTF-16 little-endian and run through a CBC-MAC: each
// 32-byte block (the last padded with zeros) is XORed with the previous result and
// encrypted with Rijndael with a 256-bit block and a fixed 256-bit key, starting
// from zeros. The last result, in upper-case hex, is the checksum. The key is
// the same in every copy of EAC; it protects against accidents and casual
// editing, and not against someone determined to forge a log.

// eacKey is the key EAC signs its logs with.
var eacKey = mustDecodeHex("9378716cf13e4265ae55338e940b376184da389e50647726b35f6f341ee3efd9")

// eacCipher is the cipher a log's checksum is made with.
var eacCipher = newRijndael(eacKey, 32)

func mustDecodeHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic("riplog: bad hex constant: " + err.Error())
	}
	return b
}

// checksumIgnored removes what the checksum does not see: line endings, which
// vary with how a log has been saved or converted, and byte order marks.
var checksumIgnored = strings.NewReplacer("\r", "", "\n", "", "\ufeff", "", "\ufffe", "")

// computeChecksum returns the checksum EAC would write for the text of a log,
// which is everything before its checksum line.
func computeChecksum(text string) string {
	units := utf16.Encode([]rune(checksumIgnored.Replace(text)))
	plain := make([]byte, 2*len(units))
	for i, u := range units {
		binary.LittleEndian.PutUint16(plain[2*i:], u)
	}

	const block = 32
	var sum, in [block]byte
	for i := 0; i < len(plain); i += block {
		in = [block]byte{} // the last block is padded with zeros
		copy(in[:], plain[i:])
		for j := range in {
			in[j] ^= sum[j]
		}
		eacCipher.encrypt(sum[:], in[:])
	}
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}
