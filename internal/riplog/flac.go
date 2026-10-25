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
	"errors"
	"fmt"
	"io"
	"os"
)

// streamInfo is the STREAMINFO block every FLAC file begins with: the
// properties of the audio, as the encoder recorded them.
type streamInfo struct {
	SampleRate    int
	Channels      int
	BitsPerSample int
	// TotalSamples is the number of samples in each channel, or zero if the
	// encoder did not know it, as when it wrote to a pipe it could not seek in.
	TotalSamples uint64
	// MD5 is the MD5 of the audio, as the encoder worked it out; all zeros if
	// it did not.
	MD5 [16]byte
}

// HasMD5 reports whether the file stores an MD5 of its audio.
func (si streamInfo) HasMD5() bool { return si.MD5 != [16]byte{} }

// errNotFLAC is returned (wrapped, with the reason) for a file that is not a
// well-formed FLAC file.
var errNotFLAC = errors.New("not a FLAC file")

// streamInfoLen is the size of a STREAMINFO block's body, which the FLAC format
// fixes.
const streamInfoLen = 34

// readStreamInfo reads the properties of the FLAC file at path from its
// STREAMINFO block, which the format requires to come first: the signature
// "fLaC", a four-byte block header, and the block itself. That is the first 42
// bytes of the file, so nothing else is read, and no decoder or external tool
// is needed.
//
// The block packs its fields into bits, not bytes. After 10 bytes of block and
// frame sizes come 20 bits of sample rate, 3 of channel count (less one), 5 of
// bits per sample (less one) and 36 of total samples, filling 8 bytes, then the
// 16 bytes of the MD5 signature of the audio.
func readStreamInfo(path string) (streamInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return streamInfo{}, err
	}
	defer f.Close()

	var b [8 + streamInfoLen]byte
	if _, err := io.ReadFull(f, b[:]); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return streamInfo{}, fmt.Errorf("%w: it is too short", errNotFLAC)
		}
		return streamInfo{}, err
	}

	if string(b[:4]) != "fLaC" {
		return streamInfo{}, fmt.Errorf("%w: it does not begin with the FLAC signature", errNotFLAC)
	}
	// The block header is a last-block flag (the top bit), a type (STREAMINFO
	// is 0) and a 24-bit length.
	if b[4]&0x7F != 0 {
		return streamInfo{}, fmt.Errorf("%w: its first block is not STREAMINFO", errNotFLAC)
	}
	if n := int(b[5])<<16 | int(b[6])<<8 | int(b[7]); n != streamInfoLen {
		return streamInfo{}, fmt.Errorf("%w: its STREAMINFO block is %d bytes, not %d", errNotFLAC, n, streamInfoLen)
	}

	p := b[8+10 : 8+18] // the 64 packed bits
	si := streamInfo{
		SampleRate:    int(p[0])<<12 | int(p[1])<<4 | int(p[2])>>4,
		Channels:      int(p[2]>>1&0x07) + 1,
		BitsPerSample: int(p[2]&0x01)<<4 | int(p[3]>>4) + 1,
		TotalSamples: uint64(p[3]&0x0F)<<32 | uint64(p[4])<<24 |
			uint64(p[5])<<16 | uint64(p[6])<<8 | uint64(p[7]),
	}
	copy(si.MD5[:], b[8+18:])
	return si, nil
}
