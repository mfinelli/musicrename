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
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/musicbrainz"
	"github.com/mfinelli/musicrename/internal/pcm"
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

// A rip from an older EAC (1.5, on Windows), of a different drive, of a disc
// with audio before track 1: it starts at sector 12000, 2:40 in.
const (
	fixtureTransistor    = "transistor.log"
	fixtureTransistorCue = "transistor.cue"
)

var allFixtures = []string{fixtureBlack, fixtureBday, fixtureCentral, fixtureTransistor}

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

// signed returns text with the checksum EAC would give it, in place of any it has.
func signed(text string) string {
	if i := strings.Index(text, "\n\n==== Log checksum"); i >= 0 {
		text = text[:i]
	}
	return text + "\n\n==== Log checksum " + computeChecksum(text) + " ====\n"
}

// parse parses text as a log and fails the test if it does not parse.
//
// The text is signed first. Tests make a log with one particular fault by editing
// a real one, and the edit would otherwise also break the checksum, so that every
// such test was about two things. A test that is about the checksum calls
// ParseLog itself, with the text as it wants it.
func parse(t *testing.T, text string) *Log {
	t.Helper()
	l, err := ParseLog([]byte(signed(text)))
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

// testMD5 is the MD5 that the header of an image built by buildRip records.
var testMD5 = [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

// withMD5 returns a FLAC header with the given MD5 of its audio in it.
func withMD5(header []byte, sum [16]byte) []byte {
	out := append([]byte(nil), header...)
	copy(out[8+18:], sum[:])
	return out
}

// imageFLAC returns the FLAC header an image of the disc in log should have,
// beginning at the given sector of the disc (0 for the whole disc), with an MD5
// of its audio.
func imageFLAC(t *testing.T, log []byte, start int) []byte {
	t.Helper()
	l, err := ParseLog(log)
	require.NoError(t, err)
	if len(l.TOC) == 0 {
		return withMD5(cdFLAC(0), testMD5)
	}
	return withMD5(cdFLAC(uint64(expectedImageSamples(l, start))), testMD5)
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
	require.NoError(t, os.WriteFile(filepath.Join(r.dir, releaseFile), []byte(testReleaseURL+"\n"), 0o644))
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

// transistorStem is the name the Transistor rip's files have; the brackets are
// part of it.
const transistorStem = "[1997] Transistor"

// transistorPretrack is the length of the audio before track 1 of the
// Transistor disc, in frames (sectors).
const transistorPretrack = 12000

// transistorRip builds the real rip of a disc with audio before its first
// track, with an image that includes it, as EAC makes them.
func transistorRip(t *testing.T) rip {
	t.Helper()
	return buildRip(t, transistorStem, fixture(t, fixtureTransistor), cueText(t, fixtureTransistorCue))
}

// transistorRipLeftOut builds that rip as it would be if the audio before track
// 1 had been left out of the image: the cue sheet starts at track 1 (no INDEX
// 00, INDEX 01 at 00:00:00) and measures every other position from there, and
// the FLAC is that much shorter. No real EAC output looks like this; it is the
// real cue sheet with its positions shifted.
func transistorRipLeftOut(t *testing.T) rip {
	t.Helper()

	cue := replaced(t, cueText(t, fixtureTransistorCue),
		"    INDEX 00 00:00:00\n    INDEX 01 02:40:00", "    INDEX 01 00:00:00")
	re := regexp.MustCompile(`(?m)^(\s+INDEX \d\d )(\d\d):(\d\d):(\d\d)$`)
	cue = re.ReplaceAllStringFunc(cue, func(m string) string {
		p := re.FindStringSubmatch(m)
		mm, _ := strconv.Atoi(p[2])
		ss, _ := strconv.Atoi(p[3])
		ff, _ := strconv.Atoi(p[4])
		frames := (mm*60+ss)*framesPerSecond + ff
		if frames == 0 {
			return m // track 1, which now starts the image
		}
		return p[1] + msf(frames-transistorPretrack)
	})

	log := fixture(t, fixtureTransistor)
	r := buildRip(t, transistorStem, log, cue)
	require.NoError(t, os.WriteFile(r.path(".flac"), imageFLAC(t, log, transistorPretrack), 0o644))
	return r
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

// testReleaseID and testReleaseURL are the release in the musicbrainz.txt of a
// rip built by buildRip: the one Back in Black's own file holds.
const (
	testReleaseID  = "d1d196ec-bb2b-4636-81a0-b8f5c5774514"
	testReleaseURL = "https://musicbrainz.org/release/" + testReleaseID
)

// fakeLookup is a MusicBrainz that knows only what a test tells it.
type fakeLookup struct {
	releases   map[string]*musicbrainz.ReleaseDiscs
	releaseErr error
	discs      map[string][]musicbrainz.ReleaseRef
	discErr    error

	releaseCalls, discCalls []string
}

func (f *fakeLookup) ReleaseDiscs(_ context.Context, id string) (*musicbrainz.ReleaseDiscs, error) {
	f.releaseCalls = append(f.releaseCalls, id)
	if f.releaseErr != nil {
		return nil, f.releaseErr
	}
	r, ok := f.releases[id]
	if !ok {
		return nil, musicbrainz.ErrNotFound
	}
	return r, nil
}

func (f *fakeLookup) DiscReleases(_ context.Context, id string) ([]musicbrainz.ReleaseRef, error) {
	f.discCalls = append(f.discCalls, id)
	return f.discs[id], f.discErr
}

// knowing returns a fake MusicBrainz in which the release in testReleaseURL
// has, as its only medium's disc ID, the given one.
func knowing(discID string) *fakeLookup {
	return &fakeLookup{releases: map[string]*musicbrainz.ReleaseDiscs{
		testReleaseID: {Media: 1, Discs: []musicbrainz.Disc{{ID: discID, Medium: 1}}},
	}}
}

// fakeAudio is a decoder that gives what a test tells it.
type fakeAudio struct {
	fp    pcm.Fingerprint
	err   error
	calls [][]string
}

func (f *fakeAudio) Fingerprint(_ context.Context, paths ...string) (pcm.Fingerprint, error) {
	f.calls = append(f.calls, paths)
	return f.fp, f.err
}

// honest returns a decoder that finds in the rip's image what the rip says is
// there: the CRC the log gives as its Copy CRC, and the length and MD5 that the
// FLAC's header records. It is what a good rip decodes to, and tests change one
// thing from it. A part that is not there (a log without a Copy CRC, a FLAC that
// is not one) is left at zero.
func (r rip) honest(t *testing.T) *fakeAudio {
	t.Helper()
	f := &fakeAudio{}

	if l, err := ParseLog(mustRead(t, r.logPath())); err == nil {
		if crc, err := strconv.ParseUint(l.Range.CopyCRC, 16, 32); err == nil {
			f.fp.CRC32 = uint32(crc)
		}
	}
	if si, err := readStreamInfo(r.path(".flac")); err == nil {
		f.fp.Bytes = int64(si.TotalSamples) * 4
		f.fp.MD5 = si.MD5
	}
	return f
}

// check runs CheckFile on the rip against a MusicBrainz that has the rip's disc
// attached to its release, as it would for a rip tagged with the right release,
// and an image that decodes to what the rip says it should, and fails the test
// if it returns an error.
func (r rip) check(t *testing.T) *Result {
	t.Helper()
	l, err := ParseLog(mustRead(t, r.logPath()))
	require.NoError(t, err)
	return r.checkWith(t, knowing(l.DiscID()))
}

// checkWith runs CheckFile on the rip against the given MusicBrainz (nil for
// none) and an image that decodes to what the rip says it should, and fails the
// test if it returns an error.
func (r rip) checkWith(t *testing.T, mb Lookup) *Result {
	t.Helper()
	return r.checkOpts(t, Options{MusicBrainz: mb, Audio: r.honest(t)})
}

// checkOpts runs CheckFile on the rip with the given options and fails the test
// if it returns an error.
func (r rip) checkOpts(t *testing.T, opts Options) *Result {
	t.Helper()
	_, res, err := CheckFile(context.Background(), r.logPath(), opts)
	require.NoError(t, err)
	return res
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return b
}
