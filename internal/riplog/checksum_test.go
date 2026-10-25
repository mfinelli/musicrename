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

func TestComputeChecksum(t *testing.T) {
	t.Run("known answers", func(t *testing.T) {
		// Computed with eac_logsigner's algorithm in Python, which also
		// confirms the checksum of each real log. They cover each way the
		// text can fall against the 32-byte block: none, part of one, exactly
		// one or two, and more.
		tests := []struct {
			name string
			text string
			want string
		}{
			{"nothing", "", "0000000000000000000000000000000000000000000000000000000000000000"},
			{"one character", "a", "E63B5E1F5DDA2A98767CCED35D5E61034AFAAE4ACE00C8198994AD422C2688C5"},
			{"just under a block", strings.Repeat("a", 15), "6E59305A8D0D64C09BE57D72836A3ED10E7EBEDD449BD420008B0696BCD86ECD"},
			{"exactly one block", strings.Repeat("a", 16), "2822832CECC6D90F0A44306826154B2C1E641A2EEF0B5DFB90B8455CF01B58F4"},
			{"just over one block", strings.Repeat("a", 17), "A808C40A3B254573C48ADD683ABB962BFFCFC03A3B52E17C6C1BD54B0B46BEFE"},
			{"exactly two blocks", strings.Repeat("b", 32), "19A1F47AAC6D4A26191D16AC98E877177356C8B8A45AD63D41A735862271DD1B"},
			{"over two blocks", strings.Repeat("c", 33), "2A080525058B8C1B08D223A4AE29C94F901BE12DEAE5D89C46042A1D93974AA4"},
			{"line endings in it", "Exact Audio Copy V1.8\r\nline two\r\n\r\nline four", "9BA347BF55D496B126E91C6F9FCA89C5B2A94447A44833B3CFDA85B6F2811F4F"},
			{"a character outside the BMP", "clef \U0001D11E here", "3EE31FF58A713572D8C89F6885DA06A1B3FCBD123F8B36268F356AA48D0A89FA"},
			{"accented characters", "Beyoncé / B'day", "5F29744C135622D22CE33F2D55DF30EE3D9150BC448B45C5A805C396EE60CE91"},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				assert.Equal(t, tc.want, computeChecksum(tc.text))
			})
		}
	})

	t.Run("what it ignores", func(t *testing.T) {
		base := computeChecksum("line one" + "line two")

		assert.Equal(t, base, computeChecksum("line one\nline two"), "a Unix line ending")
		assert.Equal(t, base, computeChecksum("line one\r\nline two"), "a Windows line ending")
		assert.Equal(t, base, computeChecksum("line one\r\n\r\n\r\nline two"), "blank lines")
		assert.Equal(t, base, computeChecksum("line one\rline two"), "an old Mac line ending")
		assert.Equal(t, base, computeChecksum("\ufeffline one\ufefflin\ufffee two"), "byte order marks")
	})

	t.Run("a NUL at the end", func(t *testing.T) {
		// The last block is padded with zeros, and a NUL is a zero: so in a
		// block that is not full, a trailing NUL cannot be told from padding. In a
		// full one it starts a block of its own, and does change the result.
		// Reading a log stops at its first NUL, so one never reaches here.
		partial := "line one line two" // 17 characters, 34 bytes: two blocks, the second mostly padding
		assert.Equal(t, computeChecksum(partial), computeChecksum(partial+"\x00"), "in a partial block")

		full := strings.Repeat("a", 16) // 32 bytes: exactly one block
		assert.NotEqual(t, computeChecksum(full), computeChecksum(full+"\x00"), "after a full block")
	})

	t.Run("what it does not", func(t *testing.T) {
		base := computeChecksum("line one line two")

		assert.NotEqual(t, base, computeChecksum("line one  line two"), "an added space")
		assert.NotEqual(t, base, computeChecksum("line one line twO"), "a changed letter")
		assert.NotEqual(t, base, computeChecksum("Line one line two"), "a changed case")
		assert.NotEqual(t, base, computeChecksum("line one line two "), "a trailing space")
		assert.NotEqual(t, base, computeChecksum("line one line tw"), "a missing letter")
	})

	t.Run("always 64 upper-case hex digits", func(t *testing.T) {
		for _, text := range []string{"", "x", strings.Repeat("long text ", 500)} {
			assert.Regexp(t, `^[0-9A-F]{64}$`, computeChecksum(text))
		}
	})
}

// The real logs are the test that matters: each carries the checksum EAC computed
// when it wrote it, which this must reproduce from the log's text alone. They
// come from EAC 1.8 (under Wine and on a Linux system) and EAC 1.5 (on Windows).
func TestChecksumOfRealLogs(t *testing.T) {
	for _, name := range allFixtures {
		t.Run(name, func(t *testing.T) {
			l, err := ParseLog(fixture(t, name))
			require.NoError(t, err)

			assert.Regexp(t, `^[0-9A-F]{64}$`, l.Checksum)
			assert.Equal(t, l.Checksum, l.ComputedChecksum)
			assert.False(t, l.TextAfterChecksum)
		})
	}
}

func TestParseChecksum(t *testing.T) {
	good := fixtureText(t, fixtureBlack)

	// Checksum names the parts of the same log, as they are in the real one.
	require.Contains(t, good, "==== Log checksum 66FF03DD933AE06F")

	parse := func(t *testing.T, text string) *Log {
		t.Helper()
		l, err := ParseLog([]byte(text))
		require.NoError(t, err)
		return l
	}

	t.Run("the same log in another encoding or with other line endings", func(t *testing.T) {
		// The checksum is of the text, so how it is saved does not matter.
		unix := parse(t, good) // fixtureText already has "\n" line endings
		windows := parse(t, strings.ReplaceAll(good, "\n", "\r\n"))
		utf8bom := parse(t, "\ufeff"+good)

		for name, l := range map[string]*Log{"LF": unix, "CRLF": windows, "UTF-8 with a BOM": utf8bom} {
			assert.Equal(t, l.Checksum, l.ComputedChecksum, name)
		}
	})

	t.Run("a change anywhere in the log", func(t *testing.T) {
		for name, edited := range map[string]string{
			"a setting":        strings.Replace(good, "Read mode               : Secure", "Read mode               : Burst", 1),
			"a title":          strings.Replace(good, "AC/DC / Back in Black", "AC/DC / Back in Blue", 1),
			"a track's offset": strings.Replace(good, "Read offset correction                      : 6", "Read offset correction                      : 7", 1),
			"a result":         strings.Replace(good, "Copy OK", "Copy Ok", 1),
			"a checksum digit": strings.Replace(good, "66FF03DD933AE06F", "66FF03DD933AE06E", 1),
		} {
			require.NotEqual(t, good, edited, name)

			l := parse(t, edited)

			assert.NotEqual(t, l.Checksum, l.ComputedChecksum, name)
		}
	})

	t.Run("a log signed again after a change", func(t *testing.T) {
		l := parse(t, signed(strings.Replace(good, "Read mode               : Secure", "Read mode               : Burst", 1)))

		assert.Equal(t, l.Checksum, l.ComputedChecksum)
	})

	t.Run("a log with no checksum line", func(t *testing.T) {
		l := parse(t, good[:strings.Index(good, "\n\n==== Log checksum")])

		assert.Empty(t, l.Checksum)
		assert.Empty(t, l.ComputedChecksum)
		assert.False(t, l.TextAfterChecksum)
	})

	t.Run("nothing but blank lines after the checksum", func(t *testing.T) {
		l := parse(t, good+"\n\n   \n\n")

		assert.Equal(t, l.Checksum, l.ComputedChecksum)
		assert.False(t, l.TextAfterChecksum)
	})

	t.Run("a separator after the checksum", func(t *testing.T) {
		// EAC writes a row of 60 hyphens when it appends to a log.
		l := parse(t, good+"\n"+strings.Repeat("-", 60)+"\n")

		assert.False(t, l.TextAfterChecksum)
	})

	t.Run("anything else after the checksum", func(t *testing.T) {
		for name, extra := range map[string]string{
			"a line of text":        "Added after the fact\n",
			"a shorter row":         strings.Repeat("-", 59) + "\n",
			"a longer row":          strings.Repeat("-", 61) + "\n",
			"another section":       "AccurateRip summary\nTrack 1 accurately ripped (confidence 99)\n",
			"a second log's header": "Exact Audio Copy V1.8 from 15. July 2024\n",
			"a second checksum":     "==== Log checksum " + strings.Repeat("A", 64) + " ====\n",
		} {
			l := parse(t, good+"\n"+extra)

			assert.True(t, l.TextAfterChecksum, name)
		}
	})

	t.Run("the checksum is the first one", func(t *testing.T) {
		l := parse(t, good+"\n==== Log checksum "+strings.Repeat("B", 64)+" ====\n")

		assert.Equal(t, "66FF03DD933AE06F", l.Checksum[:16], "a second checksum does not replace it")
		assert.True(t, l.TextAfterChecksum)
	})
}
