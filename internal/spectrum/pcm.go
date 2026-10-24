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

package spectrum

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// pcmChunkFrames is how many sample frames (one sample per channel) readPCM
// reads and delivers at a time.
const pcmChunkFrames = 8192

// readPCM reads interleaved 32-bit little-endian float PCM from r until it
// ends, calling fn with each chunk split by channel (chans[c] holds channel
// c's samples). The slices are reused between calls, so fn must not keep them.
// A trailing partial sample frame, as from a truncated stream, is ignored.
func readPCM(r io.Reader, channels int, fn func(chans [][]float64)) error {
	frameBytes := 4 * channels
	buf := make([]byte, pcmChunkFrames*frameBytes)
	chans := make([][]float64, channels)
	for c := range chans {
		chans[c] = make([]float64, 0, pcmChunkFrames)
	}

	for {
		n, err := io.ReadFull(r, buf)

		if frames := n / frameBytes; frames > 0 {
			for c := range chans {
				chans[c] = chans[c][:0]
			}
			for f := range frames {
				for c := range channels {
					bits := binary.LittleEndian.Uint32(buf[(f*channels+c)*4:])
					chans[c] = append(chans[c], float64(math.Float32frombits(bits)))
				}
			}
			fn(chans)
		}

		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading decoded audio: %w", err)
		}
	}
}
