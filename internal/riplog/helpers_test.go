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

// The cue sheets that go with two of the logs, as EAC wrote them. The B'day one
// is in Windows-1252, not UTF-8, because of the accent in "Beyoncé".
const (
	fixtureBlackCue   = "back-in-black.cue"
	fixtureBdayCue    = "bday.cue"
	fixtureCentralCue = "central-park-disc-1.cue" // no INDEX 00 lines: no pregaps
)

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

// cueText returns a cue fixture decoded to text, ready to be edited.
func cueText(t *testing.T, name string) string {
	t.Helper()
	return decodeLegacy(fixture(t, name))
}

// flacHeader returns the start of a FLAC file: its signature and STREAMINFO
// block, which is all the checks read, with the given properties. It packs the
// fields as the FLAC format does (see readStreamInfo). Real encoder output is
// what the packing is checked against, in flac_test.go.
func flacHeader(rate, channels, bits int, total uint64) []byte {
	b := make([]byte, 8+streamInfoLen)
	copy(b, "fLaC")
	b[4] = 0x80 // the last metadata block, of type STREAMINFO
	b[7] = streamInfoLen

	p := b[8+10:]
	p[0] = byte(rate >> 12)
	p[1] = byte(rate >> 4)
	p[2] = byte(rate&0x0F)<<4 | byte(channels-1)<<1 | byte((bits-1)>>4)
	p[3] = byte((bits-1)&0x0F)<<4 | byte(total>>32)
	p[4] = byte(total >> 24)
	p[5] = byte(total >> 16)
	p[6] = byte(total >> 8)
	p[7] = byte(total)
	return b
}

// cdFLAC returns a FLAC header for CD-format audio of the given length.
func cdFLAC(total uint64) []byte { return flacHeader(44100, 2, 16, total) }

// imageFLAC returns the FLAC header an image of the disc in log should have,
// beginning at the given sector of the disc (0 for the whole disc).
func imageFLAC(t *testing.T, log []byte, start int) []byte {
	t.Helper()
	l, err := ParseLog(log)
	require.NoError(t, err)
	if len(l.TOC) == 0 {
		return cdFLAC(0)
	}
	return cdFLAC(uint64(expectedImageSamples(l, start)))
}

// rip is the files of one rip in a temporary directory.
type rip struct {
	dir  string
	stem string
}

func (r rip) path(ext string) string { return filepath.Join(r.dir, r.stem+ext) }

// logPath returns the path of the rip's log.
func (r rip) logPath() string { return r.path(".log") }

// buildRip writes a complete rip into a new temporary directory: the log, the
// cue sheet (as UTF-8 text, which ParseCue accepts), and a FLAC image named as
// the cue sheet names it, with the length the log implies. Tests then break it
// by changing or removing files.
func buildRip(t *testing.T, stem string, log []byte, cue string) rip {
	t.Helper()
	r := rip{dir: t.TempDir(), stem: stem}
	require.NoError(t, os.WriteFile(r.path(".log"), log, 0o644))
	require.NoError(t, os.WriteFile(r.path(".cue"), []byte(cue), 0o644))
	require.NoError(t, os.WriteFile(r.path(".flac"), imageFLAC(t, log, 0), 0o644))
	return r
}

// blackRip and bdayRip build the two rips that have real cue sheets.
func blackRip(t *testing.T) rip {
	t.Helper()
	return buildRip(t, "Back in Black", fixture(t, fixtureBlack), cueText(t, fixtureBlackCue))
}

func bdayRip(t *testing.T) rip {
	t.Helper()
	return buildRip(t, "B'day", fixture(t, fixtureBday), cueText(t, fixtureBdayCue))
}

// centralRip builds disc 1 of a multi-disc release, whose names have "(disc 1)"
// in them and whose cue sheet records no pregaps.
func centralRip(t *testing.T) rip {
	t.Helper()
	return buildRip(t, "The Central Park Concert (disc 1)", fixture(t, fixtureCentral), cueText(t, fixtureCentralCue))
}

// rewriteCue changes the rip's cue sheet by replacing old with new (which must
// be there) in its text.
func rewriteCue(t *testing.T, r rip, old, new string) {
	t.Helper()
	b, err := os.ReadFile(r.path(".cue"))
	require.NoError(t, err)
	text := replaced(t, string(b), old, new)
	require.NoError(t, os.WriteFile(r.path(".cue"), []byte(text), 0o644))
}

// check runs CheckFile on the rip and fails the test if it returns an error.
func (r rip) check(t *testing.T) *Result {
	t.Helper()
	_, res, err := CheckFile(r.logPath())
	require.NoError(t, err)
	return res
}
