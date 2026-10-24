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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecode(t *testing.T) {
	const sample = "Exact Audio Copy V1.8\r\n\r\nBeyoncé / B'day\r\n"
	want := "Exact Audio Copy V1.8\n\nBeyoncé / B'day\n"

	tests := []struct {
		name string
		data []byte
	}{
		{"UTF-16LE with a byte order mark, as EAC writes it", encodeUTF16(sample, false, true)},
		{"UTF-16BE with a byte order mark", encodeUTF16(sample, true, true)},
		{"UTF-16LE with no byte order mark", encodeUTF16(sample, false, false)},
		{"UTF-8 with a byte order mark", append([]byte{0xEF, 0xBB, 0xBF}, sample...)},
		{"UTF-8 without one", []byte(sample)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decode(tc.data)

			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}

	t.Run("a real log", func(t *testing.T) {
		for _, name := range allFixtures {
			got, err := decode(fixture(t, name))

			require.NoError(t, err, name)
			assert.True(t, strings.HasPrefix(got, "Exact Audio Copy V1."), name)
			assert.NotContains(t, got, "\r", name)
			assert.NotContains(t, got, "\ufeff", name)
		}
	})

	t.Run("lone carriage returns become newlines", func(t *testing.T) {
		got, err := decode([]byte("a\rb\r\nc"))
		require.NoError(t, err)
		assert.Equal(t, "a\nb\nc", got)
	})

	t.Run("the text ends at the first NUL", func(t *testing.T) {
		got, err := decode([]byte("log text\n\x00\x00junk after the end"))
		require.NoError(t, err)
		assert.Equal(t, "log text\n", got)
	})

	t.Run("bytes that are not text are rejected", func(t *testing.T) {
		_, err := decode([]byte{0x80, 0x81, 0x82, 0x83})
		assert.ErrorIs(t, err, errNotLog)
	})

	t.Run("an empty file decodes to nothing", func(t *testing.T) {
		got, err := decode(nil)
		require.NoError(t, err)
		assert.Equal(t, "", got)
	})
}

func TestLooksLikeUTF16LE(t *testing.T) {
	assert.True(t, looksLikeUTF16LE(encodeUTF16("Exact Audio Copy", false, false)))
	assert.False(t, looksLikeUTF16LE([]byte("Exact Audio Copy")), "plain ASCII has no zero bytes")
	assert.False(t, looksLikeUTF16LE([]byte("abc")), "too short to tell")
	assert.False(t, looksLikeUTF16LE([]byte("abcde")), "odd length cannot be UTF-16")
}
