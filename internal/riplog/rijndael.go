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

import "math/bits"

// This file is Rijndael, the cipher AES was chosen from, with the block size left
// open. AES fixes the block at 128 bits; Rijndael also allows 192 and 256, and
// Exact Audio Copy signs its logs with the 256-bit form, which Go's crypto/aes
// does not offer. Only encryption is needed. It follows the Rijndael
// specification (FIPS 197 describes the 128-bit special case) and is checked
// against crypto/aes for the 128-bit block, where the two must agree.

// sbox is Rijndael's substitution box: each byte replaced by its multiplicative
// inverse in GF(2^8), then put through an affine transformation. It is generated
// from that definition instead of being written out.
var sbox = makeSbox()

func makeSbox() (box [256]byte) {
	p, q := byte(1), byte(1)
	for {
		// p steps through every nonzero element by repeated multiplication by 3
		// (x+1 in the field), and q follows as p's inverse, by repeated division
		// by 3 (multiplication by 0xf6).
		p = p ^ (p << 1) ^ reduction(p)

		q ^= q << 1
		q ^= q << 2
		q ^= q << 4
		if q&0x80 != 0 {
			q ^= 0x09
		}

		x := q ^ bits.RotateLeft8(q, 1) ^ bits.RotateLeft8(q, 2) ^ bits.RotateLeft8(q, 3) ^ bits.RotateLeft8(q, 4)
		box[p] = x ^ 0x63

		if p == 1 {
			break
		}
	}
	box[0] = 0x63
	return box
}

// reduction returns what must be XORed into b<<1 to keep it in the field when
// the shift carried out of the byte: the field's polynomial, x^8+x^4+x^3+x+1,
// less its top term.
func reduction(b byte) byte {
	if b&0x80 != 0 {
		return 0x1b
	}
	return 0
}

// xtime multiplies b by x in GF(2^8).
func xtime(b byte) byte {
	return b<<1 ^ reduction(b)
}

// rijndael is a Rijndael cipher with a particular key and block size.
type rijndael struct {
	nb     int    // block size, in 32-bit words
	rounds int    // number of rounds
	rk     []byte // round keys, one block's worth per round plus one
	shift  [4]int // how far each row of the state rotates in ShiftRows
}

// maxBlock is the largest block this implementation handles, in bytes.
const maxBlock = 32

// newRijndael returns a cipher for the given key (16, 24 or 32 bytes) and block
// size in bytes (16, 24 or 32). It panics for anything else, which is a
// programming error: the only use is one fixed key and block size.
func newRijndael(key []byte, blockBytes int) *rijndael {
	nk, nb := len(key)/4, blockBytes/4
	if len(key)%4 != 0 || nk < 4 || nk > 8 || nk == 5 || nk == 7 ||
		blockBytes%4 != 0 || (nb != 4 && nb != 6 && nb != 8) {
		panic("riplog: unsupported Rijndael key or block size")
	}

	c := &rijndael{nb: nb, rounds: max(nk, nb) + 6}
	// The rows of the state rotate by 0, 1, 2 and 3 places, except that a
	// 256-bit block uses 0, 1, 3 and 4.
	if nb == 8 {
		c.shift = [4]int{0, 1, 3, 4}
	} else {
		c.shift = [4]int{0, 1, 2, 3}
	}

	// Key expansion: a block's worth of words per round, plus one, each derived
	// from the word nk places back and the one before it.
	words := nb * (c.rounds + 1)
	w := make([]byte, 4*words)
	copy(w, key)
	rcon := byte(1)
	for i := nk; i < words; i++ {
		t := [4]byte{w[4*(i-1)], w[4*(i-1)+1], w[4*(i-1)+2], w[4*(i-1)+3]}
		switch {
		case i%nk == 0:
			// Rotate the word, substitute its bytes, and add a round constant.
			t = [4]byte{sbox[t[1]] ^ rcon, sbox[t[2]], sbox[t[3]], sbox[t[0]]}
			rcon = xtime(rcon)
		case nk > 6 && i%nk == 4:
			t = [4]byte{sbox[t[0]], sbox[t[1]], sbox[t[2]], sbox[t[3]]}
		}
		for j := range t {
			w[4*i+j] = w[4*(i-nk)+j] ^ t[j]
		}
	}
	c.rk = w
	return c
}

// blockSize returns the cipher's block size in bytes.
func (c *rijndael) blockSize() int { return 4 * c.nb }

// encrypt encrypts one block of src into dst. Both must be a block long. The
// state is laid out as the specification has it: byte i of the block is row i%4
// of column i/4.
func (c *rijndael) encrypt(dst, src []byte) {
	n := c.blockSize()
	var s, t [maxBlock]byte
	copy(s[:n], src[:n])

	addRoundKey(s[:n], c.rk[:n])
	for round := 1; round <= c.rounds; round++ {
		for i := range n {
			s[i] = sbox[s[i]]
		}

		// ShiftRows
		for col := range c.nb {
			for row := range 4 {
				t[row+4*col] = s[row+4*((col+c.shift[row])%c.nb)]
			}
		}
		s = t

		if round < c.rounds {
			mixColumns(s[:n])
		}
		addRoundKey(s[:n], c.rk[round*n:(round+1)*n])
	}
	copy(dst[:n], s[:n])
}

func addRoundKey(s, k []byte) {
	for i := range s {
		s[i] ^= k[i]
	}
}

// mixColumns multiplies each column of the state by Rijndael's fixed matrix.
func mixColumns(s []byte) {
	for i := 0; i < len(s); i += 4 {
		a0, a1, a2, a3 := s[i], s[i+1], s[i+2], s[i+3]
		s[i] = xtime(a0) ^ (xtime(a1) ^ a1) ^ a2 ^ a3
		s[i+1] = a0 ^ xtime(a1) ^ (xtime(a2) ^ a2) ^ a3
		s[i+2] = a0 ^ a1 ^ xtime(a2) ^ (xtime(a3) ^ a3)
		s[i+3] = (xtime(a0) ^ a0) ^ a1 ^ a2 ^ xtime(a3)
	}
}
