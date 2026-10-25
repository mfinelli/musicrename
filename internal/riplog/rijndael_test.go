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
	"crypto/aes"
	"encoding/hex"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return b
}

// sequence returns n bytes counting up from zero.
func sequence(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

func encryptHex(t *testing.T, key []byte, block []byte) string {
	t.Helper()
	out := make([]byte, len(block))
	newRijndael(key, len(block)).encrypt(out, block)
	return hex.EncodeToString(out)
}

func TestSbox(t *testing.T) {
	t.Run("known entries", func(t *testing.T) {
		// From the table in FIPS 197.
		assert.Equal(t, byte(0x63), sbox[0x00])
		assert.Equal(t, byte(0x7c), sbox[0x01])
		assert.Equal(t, byte(0xed), sbox[0x53])
		assert.Equal(t, byte(0x16), sbox[0xff])
	})

	t.Run("a permutation with no fixed points", func(t *testing.T) {
		seen := map[byte]bool{}
		for i, v := range sbox {
			seen[v] = true
			assert.NotEqual(t, byte(i), v, "byte %#02x maps to itself", i)
		}
		assert.Len(t, seen, 256)
	})
}

func TestXtime(t *testing.T) {
	// FIPS 197 section 4.2.1: {57}•{02} = {ae}, •{04} = {47}, •{08} = {8e}.
	assert.Equal(t, byte(0xae), xtime(0x57))
	assert.Equal(t, byte(0x47), xtime(0xae))
	assert.Equal(t, byte(0x8e), xtime(0x47))
}

func TestRijndaelAES(t *testing.T) {
	// A 128-bit block is AES, which has published vectors and a standard
	// implementation to compare with.
	pt := unhex(t, "00112233445566778899aabbccddeeff")

	t.Run("the vectors in FIPS 197 appendix C", func(t *testing.T) {
		assert.Equal(t, "69c4e0d86a7b0430d8cdb78070b4c55a", encryptHex(t, sequence(16), pt))
		assert.Equal(t, "dda97ca4864cdfe06eaf70a0ec0d7191", encryptHex(t, sequence(24), pt))
		assert.Equal(t, "8ea2b7ca516745bfeafc49904b496089", encryptHex(t, sequence(32), pt))
	})

	t.Run("agrees with crypto/aes", func(t *testing.T) {
		rng := rand.New(rand.NewSource(1))
		for _, keyLen := range []int{16, 24, 32} {
			for range 200 {
				key, block := make([]byte, keyLen), make([]byte, 16)
				rng.Read(key)
				rng.Read(block)

				ref, err := aes.NewCipher(key)
				require.NoError(t, err)
				want := make([]byte, 16)
				ref.Encrypt(want, block)

				assert.Equal(t, hex.EncodeToString(want), encryptHex(t, key, block), "key length %d", keyLen)
			}
		}
	})
}

func TestRijndaelWiderBlocks(t *testing.T) {
	// crypto/aes has no wider block, so these are known answers from pprp, the
	// pure-Python Rijndael that eac_logsigner uses, for inputs chosen to
	// exercise each block and key size.
	tests := []struct {
		name  string
		key   []byte
		block []byte
		want  string
	}{
		{
			"256-bit block, zero key, zero block",
			make([]byte, 32), make([]byte, 32),
			"c6227e7740b7e53b5cb77865278eab0726f62366d9aabad908936123a1fc8af3",
		},
		{
			"256-bit block, zero key, counting block",
			make([]byte, 32), sequence(32),
			"aee5d1d5de30398a4520b7a03bd6b9cc859844392605df664d86158cf6cd6c3a",
		},
		{
			"256-bit block, EAC's key, zero block",
			eacKey, make([]byte, 32),
			"8119e049f09e65e29a75774e9c4fa06e755ee09a66a79dec7726036c188eb346",
		},
		{
			"256-bit block, EAC's key, counting block",
			eacKey, sequence(32),
			"f02d452c822b30af0f0fc31d442428d1cee76b29d325d8e7c14c541942ee0705",
		},
		{
			"256-bit block, 128-bit key",
			sequence(16), sequence(32),
			"21c89c4a7ae37f185597362e5d20485f6144afed71bd4a798688662e6cde7dc4",
		},
		{
			"256-bit block, 192-bit key",
			sequence(24), sequence(32),
			"d4cc0b070ebebd98ffa1c28e40bffa5db8bdb8fb5bfb6ccf23af2c1608967acc",
		},
		{
			"192-bit block, 128-bit key",
			sequence(16), sequence(24),
			"54030626e366bba5827f46be060b53c75668fc25fb1a6074",
		},
		{
			"192-bit block, 256-bit key",
			sequence(32), sequence(24),
			"b5e5bb698a33a80e4daed256760f1a5f08cc6f181e67b5bc",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, encryptHex(t, tc.key, tc.block))
		})
	}
}

func TestEACCipherIsBuiltAfterTheSbox(t *testing.T) {
	// eacCipher is built when the package starts up, from a table that has to be
	// ready by then. A cipher built later, in a test, would not show it going
	// wrong: this one gives the reference answer only if it was built right.
	out := make([]byte, 32)
	eacCipher.encrypt(out, make([]byte, 32))

	assert.Equal(t, "8119e049f09e65e29a75774e9c4fa06e755ee09a66a79dec7726036c188eb346", hex.EncodeToString(out))
}

func TestRijndaelUse(t *testing.T) {
	t.Run("the block size", func(t *testing.T) {
		assert.Equal(t, 16, newRijndael(sequence(16), 16).blockSize())
		assert.Equal(t, 24, newRijndael(sequence(16), 24).blockSize())
		assert.Equal(t, 32, newRijndael(sequence(32), 32).blockSize())
	})

	t.Run("encrypting in place", func(t *testing.T) {
		block := sequence(32)
		want := encryptHex(t, eacKey, block)

		newRijndael(eacKey, 32).encrypt(block, block)

		assert.Equal(t, want, hex.EncodeToString(block))
	})

	t.Run("the input is left alone", func(t *testing.T) {
		in, before := sequence(32), sequence(32)

		newRijndael(eacKey, 32).encrypt(make([]byte, 32), in)

		assert.Equal(t, before, in)
	})

	t.Run("a key or block size it does not do", func(t *testing.T) {
		for name, f := range map[string]func(){
			"a 15-byte key":     func() { newRijndael(make([]byte, 15), 16) },
			"a 20-byte key":     func() { newRijndael(make([]byte, 20), 16) },
			"a 28-byte key":     func() { newRijndael(make([]byte, 28), 16) },
			"a 40-byte key":     func() { newRijndael(make([]byte, 40), 16) },
			"a 20-byte block":   func() { newRijndael(make([]byte, 16), 20) },
			"a 28-byte block":   func() { newRijndael(make([]byte, 16), 28) },
			"a block not whole": func() { newRijndael(make([]byte, 16), 30) },
			"a 12-byte block":   func() { newRijndael(make([]byte, 16), 12) },
			"no key at all":     func() { newRijndael(nil, 16) },
		} {
			assert.Panics(t, f, name)
		}
	})
}

func TestMustDecodeHex(t *testing.T) {
	assert.Equal(t, []byte{0xde, 0xad}, mustDecodeHex("dead"))
	assert.Panics(t, func() { mustDecodeHex("not hex") })
}
