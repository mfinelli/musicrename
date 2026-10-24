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
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/require"
)

// The three fixture logs are real EAC 1.8 image-rip logs, byte for byte as EAC
// wrote them (UTF-16LE with CRLF line endings). The tests that need a log with
// something wrong in it make one by editing a good log's text, which is why
// they all start from these.
const (
	fixtureBlack   = "back-in-black.log"       // with a CUETools DB section
	fixtureBday    = "bday.log"                // without one
	fixtureCentral = "central-park-disc-1.log" // disc 1 of a multi-disc release
)

var allFixtures = []string{fixtureBlack, fixtureBday, fixtureCentral}

// fixture returns a fixture's raw bytes.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return b
}

// fixtureText returns a fixture decoded to text, ready to be edited and passed
// back through ParseLog (which accepts UTF-8).
func fixtureText(t *testing.T, name string) string {
	t.Helper()
	s, err := decode(fixture(t, name))
	require.NoError(t, err)
	return s
}

// parse parses text as a log and fails the test if it does not parse.
func parse(t *testing.T, text string) *Log {
	t.Helper()
	l, err := ParseLog([]byte(text))
	require.NoError(t, err)
	return l
}

// verify parses and checks text.
func verify(t *testing.T, text string) *Result {
	t.Helper()
	return Verify(parse(t, text))
}

// withSetting returns text with the value of the header line "label : value"
// replaced.
func withSetting(t *testing.T, text, label, value string) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^(` + regexp.QuoteMeta(label) + `\s*: ).*$`)
	require.True(t, re.MatchString(text), "no %q line to change", label)
	return re.ReplaceAllString(text, "${1}"+value)
}

// withoutLine returns text with every line that starts (after leading space)
// with prefix removed.
func withoutLine(t *testing.T, text, prefix string) string {
	t.Helper()
	var out []string
	found := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			found = true
			continue
		}
		out = append(out, line)
	}
	require.True(t, found, "no line starting %q to remove", prefix)
	return strings.Join(out, "\n")
}

// replaced returns text with the first old replaced by new, failing the test
// if old is not there.
func replaced(t *testing.T, text, old, new string) string {
	t.Helper()
	require.Contains(t, text, old)
	return strings.Replace(text, old, new, 1)
}

// encodeUTF16 encodes text as UTF-16, optionally big-endian and optionally with
// a byte order mark.
func encodeUTF16(text string, bigEndian, bom bool) []byte {
	u := utf16.Encode([]rune(text))
	var out []byte
	put := func(v uint16) {
		if bigEndian {
			out = append(out, byte(v>>8), byte(v))
		} else {
			out = append(out, byte(v), byte(v>>8))
		}
	}
	if bom {
		put(0xFEFF)
	}
	for _, v := range u {
		put(v)
	}
	return out
}
