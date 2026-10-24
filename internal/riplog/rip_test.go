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
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cueCheckIDs are the checks CheckFile adds to Verify's when the cue sheet is
// there and reads.
var cueCheckIDs = []string{
	"cue-sheet", "cue-image", "image-format", "image-length",
	"cue-tracks", "cue-positions", "cue-disc-id", "cue-pregaps",
}

// ids returns the IDs of a result's checks, in order.
func ids(r *Result) []string {
	var out []string
	for _, c := range r.Checks {
		out = append(out, c.ID)
	}
	return out
}

// setCue replaces the rip's cue sheet text.
func setCue(t *testing.T, r rip, text string) {
	t.Helper()
	require.NoError(t, os.WriteFile(r.path(".cue"), []byte(text), 0o644))
}

// readCue returns the rip's cue sheet text.
func readCue(t *testing.T, r rip) string {
	t.Helper()
	b, err := os.ReadFile(r.path(".cue"))
	require.NoError(t, err)
	return string(b)
}

func TestCheckFileRealRips(t *testing.T) {
	t.Run("Back in Black passes everything", func(t *testing.T) {
		r := blackRip(t).check(t)

		assert.Equal(t, append(append([]string{}, allCheckIDs...), cueCheckIDs...), ids(r))
		assert.Equal(t, StatusPass, r.Worst())
		assert.Equal(t, len(allCheckIDs)+len(cueCheckIDs), r.Count(StatusPass))
		assert.Equal(t, "Back in Black.cue", want(t, r, "cue-sheet", StatusPass).Detail)
		assert.Equal(t, "Back in Black.flac", want(t, r, "cue-image", StatusPass).Detail)
		assert.Equal(t, "10 tracks", want(t, r, "cue-tracks", StatusPass).Detail)
		assert.Contains(t, want(t, r, "cue-positions", StatusPass).Detail, "all 10 tracks")
		assert.Equal(t, "7509E00A", want(t, r, "cue-disc-id", StatusPass).Detail)
		assert.Equal(t, "5 pregaps recorded (tracks 2, 4, 6, 7, 8)", want(t, r, "cue-pregaps", StatusPass).Detail)
	})

	t.Run("B'day, whose cue sheet is not UTF-8, passes everything but the missing CUETools result", func(t *testing.T) {
		rip := bdayRip(t)
		// The real fixture, byte for byte, not the UTF-8 copy the other tests
		// use: this is the Windows-1252 decode being exercised end to end.
		require.NoError(t, os.WriteFile(rip.path(".cue"), fixture(t, fixtureBdayCue), 0o644))

		r := rip.check(t)

		assert.Equal(t, StatusWarning, r.Worst())
		assert.Equal(t, 1, r.Count(StatusWarning))
		want(t, r, "ctdb", StatusWarning)
		assert.Equal(t, "13111213", want(t, r, "cue-disc-id", StatusPass).Detail)
		assert.Equal(t, "4 pregaps recorded (tracks 8, 15, 16, 18)", want(t, r, "cue-pregaps", StatusPass).Detail)
		assert.Equal(t, "B'day.flac", want(t, r, "cue-image", StatusPass).Detail)
	})

	t.Run("disc 1 of a multi-disc release, with no pregaps recorded", func(t *testing.T) {
		r := centralRip(t).check(t)

		assert.Equal(t, StatusInfo, r.Worst(), "only the pregap note")
		assert.Equal(t, 0, r.Count(StatusWarning)+r.Count(StatusError), "a note is not a finding")
		assert.False(t, r.Fails(true), "not even with --strict")
		assert.Equal(t, "The Central Park Concert (disc 1).flac", want(t, r, "cue-image", StatusPass).Detail)
		assert.Equal(t, "3E099506", want(t, r, "cue-disc-id", StatusPass).Detail)
		assert.Contains(t, want(t, r, "cue-pregaps", StatusInfo).Detail, "none recorded")
	})

	t.Run("the image checks on a real rip", func(t *testing.T) {
		r := blackRip(t).check(t)

		assert.Equal(t, "44.1 kHz, 16-bit, stereo", want(t, r, "image-format", StatusPass).Detail)
		assert.Equal(t, "111505380 samples, as the log's table of contents gives",
			want(t, r, "image-length", StatusPass).Detail, "the figure metaflac reports for the real file")
	})

	t.Run("the log is returned too", func(t *testing.T) {
		l, _, err := CheckFile(blackRip(t).logPath())

		require.NoError(t, err)
		assert.Equal(t, "AC/DC / Back in Black", l.Title)
	})
}

func TestCheckFileErrors(t *testing.T) {
	t.Run("a log that does not exist", func(t *testing.T) {
		l, res, err := CheckFile(filepath.Join(t.TempDir(), "missing.log"))

		assert.Nil(t, l)
		assert.Nil(t, res)
		assert.ErrorContains(t, err, "could not read")
	})

	t.Run("a log that is not supported", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "x.log")
		require.NoError(t, os.WriteFile(path, []byte("hello\n"), 0o644))

		_, _, err := CheckFile(path)

		var ue *UnsupportedError
		assert.True(t, errors.As(err, &ue), "got %v", err)
	})

	t.Run("a log that is not text", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "x.log")
		require.NoError(t, os.WriteFile(path, []byte{0x80, 0x81, 0x82, 0x83}, 0o644))

		_, _, err := CheckFile(path)

		assert.ErrorContains(t, err, "could not read the log")
	})
}

func TestCheckFileCueSheet(t *testing.T) {
	t.Run("no cue sheet", func(t *testing.T) {
		rip := blackRip(t)
		require.NoError(t, os.Remove(rip.path(".cue")))

		r := rip.check(t)

		c := want(t, r, "cue-sheet", StatusError)
		assert.Contains(t, c.Detail, `"Back in Black.cue"`)
		assert.True(t, r.Failed())
		assert.Equal(t, append(append([]string{}, allCheckIDs...), "cue-sheet"), ids(r),
			"the checks that need the cue sheet are not made without it")
	})

	t.Run("the extension's case does not matter", func(t *testing.T) {
		rip := blackRip(t)
		require.NoError(t, os.Rename(rip.path(".cue"), rip.path(".CUE")))

		r := rip.check(t)

		want(t, r, "cue-sheet", StatusPass)
	})

	t.Run("a cue sheet that cannot be read", func(t *testing.T) {
		rip := blackRip(t)
		require.NoError(t, os.Remove(rip.path(".cue")))
		require.NoError(t, os.Mkdir(rip.path(".cue"), 0o755))

		r := rip.check(t)

		c := want(t, r, "cue-sheet", StatusError)
		assert.Contains(t, c.Detail, "could not read")
	})

	t.Run("a cue sheet that does not parse", func(t *testing.T) {
		rip := blackRip(t)
		setCue(t, rip, "INDEX 01 00:00:00\n"+readCue(t, rip))

		r := rip.check(t)

		c := want(t, r, "cue-sheet", StatusError)
		assert.Contains(t, c.Detail, "line 1")
		assert.Contains(t, c.Detail, "INDEX before any TRACK")
	})
}

func TestFindFile(t *testing.T) {
	t.Run("the exact name is preferred", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "a.cue"), nil, 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "A.CUE"), nil, 0o644))

		assert.Equal(t, filepath.Join(dir, "a.cue"), findFile(dir, "a.cue"))
	})

	t.Run("a name differing in case", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "A.CUE"), nil, 0o644))

		assert.Equal(t, filepath.Join(dir, "A.CUE"), findFile(dir, "a.cue"))
	})

	t.Run("nothing", func(t *testing.T) {
		assert.Equal(t, "", findFile(t.TempDir(), "a.cue"))
		assert.Equal(t, "", findFile(filepath.Join(t.TempDir(), "no such dir"), "a.cue"))
	})
}

func TestCheckFileImage(t *testing.T) {
	t.Run("the FLAC is missing", func(t *testing.T) {
		rip := blackRip(t)
		require.NoError(t, os.Remove(rip.path(".flac")))

		c := want(t, rip.check(t), "cue-image", StatusError)

		assert.Contains(t, c.Detail, `"Back in Black.flac" is not in the directory`)
	})

	t.Run("the name differs only in case", func(t *testing.T) {
		rip := blackRip(t)
		require.NoError(t, os.Rename(rip.path(".flac"), filepath.Join(rip.dir, "BACK IN BLACK.FLAC")))

		c := want(t, rip.check(t), "cue-image", StatusError)

		assert.Contains(t, c.Detail, "differs only in case")
		assert.Contains(t, c.Detail, "BACK IN BLACK.FLAC")
	})

	t.Run("the cue sheet names a WAV file", func(t *testing.T) {
		rip := blackRip(t)
		rewriteCue(t, rip, "Back in Black.flac", "Back in Black.wav")

		c := want(t, rip.check(t), "cue-image", StatusError)

		assert.Contains(t, c.Detail, "is not a .flac file")
	})

	t.Run("the extension's case does not matter", func(t *testing.T) {
		rip := blackRip(t)
		rewriteCue(t, rip, "Back in Black.flac", "Back in Black.FLAC")
		require.NoError(t, os.Rename(rip.path(".flac"), filepath.Join(rip.dir, "Back in Black.FLAC")))

		want(t, rip.check(t), "cue-image", StatusPass)
	})

	t.Run("the cue sheet names a path", func(t *testing.T) {
		for _, name := range []string{`C:\Music\Back in Black.flac`, "../Back in Black.flac"} {
			rip := blackRip(t)
			rewriteCue(t, rip, `"Back in Black.flac"`, `"`+name+`"`)

			c := want(t, rip.check(t), "cue-image", StatusError)

			assert.Contains(t, c.Detail, "names a path", name)
		}
	})

	t.Run("the cue sheet names two files", func(t *testing.T) {
		rip := blackRip(t)
		rewriteCue(t, rip, "  TRACK 02 AUDIO", "FILE \"Back in Black 2.flac\" WAVE\n  TRACK 02 AUDIO")

		c := want(t, rip.check(t), "cue-image", StatusError)

		assert.Contains(t, c.Detail, "2 audio files")
	})

	t.Run("the cue sheet names none", func(t *testing.T) {
		rip := blackRip(t)
		rewriteCue(t, rip, "FILE \"Back in Black.flac\" WAVE\n", "")

		c := want(t, rip.check(t), "cue-image", StatusError)

		assert.Contains(t, c.Detail, "names no audio file")
	})

	t.Run("an empty file", func(t *testing.T) {
		rip := blackRip(t)
		require.NoError(t, os.WriteFile(rip.path(".flac"), nil, 0o644))

		c := want(t, rip.check(t), "cue-image", StatusError)

		assert.Contains(t, c.Detail, "is empty")
	})

	t.Run("a file that is not FLAC", func(t *testing.T) {
		rip := blackRip(t)
		require.NoError(t, os.WriteFile(rip.path(".flac"), []byte("RIFF....WAVEfmt "), 0o644))

		c := want(t, rip.check(t), "cue-image", StatusError)

		assert.Contains(t, c.Detail, "not a FLAC file")
	})

	t.Run("a file shorter than the signature", func(t *testing.T) {
		rip := blackRip(t)
		require.NoError(t, os.WriteFile(rip.path(".flac"), []byte("fL"), 0o644))

		want(t, rip.check(t), "cue-image", StatusError)
	})

	t.Run("a directory in its place", func(t *testing.T) {
		rip := blackRip(t)
		require.NoError(t, os.Remove(rip.path(".flac")))
		require.NoError(t, os.Mkdir(rip.path(".flac"), 0o755))

		c := want(t, rip.check(t), "cue-image", StatusError)

		assert.Contains(t, c.Detail, "not a regular file")
	})

	t.Run("a cue sheet from a different rip", func(t *testing.T) {
		rip := blackRip(t)
		rewriteCue(t, rip, "Back in Black.flac", "Some Other Album.flac")
		require.NoError(t, os.WriteFile(filepath.Join(rip.dir, "Some Other Album.flac"), imageFLAC(t, fixture(t, fixtureBlack), 0), 0o644))

		c := want(t, rip.check(t), "cue-image", StatusWarning)

		assert.Contains(t, c.Detail, "Some Other Album.flac")
		assert.Contains(t, c.Detail, "Back in Black.wav")
		assert.Contains(t, c.Detail, "same rip")
	})

	t.Run("a name the file system rejects", func(t *testing.T) {
		rip := blackRip(t)
		rewriteCue(t, rip, "Back in Black.flac", strings.Repeat("a", 300)+".flac")

		c := want(t, rip.check(t), "cue-image", StatusError)

		assert.Contains(t, c.Detail, "cannot read")
	})

	t.Run("a file that cannot be read", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("file permissions do not apply to root")
		}
		rip := blackRip(t)
		require.NoError(t, os.Chmod(rip.path(".flac"), 0o000))

		c := want(t, rip.check(t), "cue-image", StatusError)

		assert.Contains(t, c.Detail, "cannot read")
	})

	t.Run("windowsBase", func(t *testing.T) {
		assert.Equal(t, "Back in Black.wav", windowsBase(`C:\users\mario\Music\Back in Black.wav`))
		assert.Equal(t, "x.wav", windowsBase("/home/mario/x.wav"))
		assert.Equal(t, "x.wav", windowsBase("x.wav"))
		assert.Equal(t, "", windowsBase(""))
	})
}

func TestCheckFileTracks(t *testing.T) {
	t.Run("a track fewer", func(t *testing.T) {
		rip := blackRip(t)
		text := readCue(t, rip)
		setCue(t, rip, text[:strings.Index(text, "  TRACK 10 AUDIO")])

		r := rip.check(t)

		c := want(t, r, "cue-tracks", StatusError)
		assert.Contains(t, c.Detail, "9 tracks and the log 10")
		c = want(t, r, "cue-positions", StatusError)
		assert.Contains(t, c.Detail, "track 10 is not in the cue sheet")
	})

	t.Run("a track more", func(t *testing.T) {
		rip := blackRip(t)
		setCue(t, rip, readCue(t, rip)+"  TRACK 11 AUDIO\n    INDEX 01 99:00:00\n")

		c := want(t, rip.check(t), "cue-tracks", StatusError)

		assert.Contains(t, c.Detail, "11 tracks and the log 10")
	})

	t.Run("a track numbered wrongly", func(t *testing.T) {
		rip := blackRip(t)
		rewriteCue(t, rip, "TRACK 05 AUDIO", "TRACK 06 AUDIO")

		c := want(t, rip.check(t), "cue-tracks", StatusError)

		assert.Contains(t, c.Detail, "track 5 is numbered 6")
	})

	t.Run("a data track", func(t *testing.T) {
		rip := blackRip(t)
		rewriteCue(t, rip, "TRACK 10 AUDIO", "TRACK 10 MODE1/2352")

		c := want(t, rip.check(t), "cue-tracks", StatusError)

		assert.Contains(t, c.Detail, "track 10 is MODE1/2352")
		assert.Contains(t, c.Detail, "not audio")
	})

	t.Run("a log with no table of contents", func(t *testing.T) {
		text := regexp.MustCompile(`(?m)^\s+\d+\s+\|\s+[\d:.]+\s+\|.*$`).ReplaceAllString(fixtureText(t, fixtureBlack), "")
		rip := buildRip(t, "Back in Black", []byte(text), cueText(t, fixtureBlackCue))

		r := rip.check(t)

		assert.Contains(t, want(t, r, "cue-tracks", StatusError).Detail, "no table of contents")
		assert.Contains(t, want(t, r, "cue-positions", StatusError).Detail, "no table of contents")
		assert.Contains(t, want(t, r, "cue-disc-id", StatusInfo).Detail, "no table of contents")
	})
}

func TestCheckFilePositions(t *testing.T) {
	t.Run("a track one frame out", func(t *testing.T) {
		rip := blackRip(t)
		rewriteCue(t, rip, "INDEX 01 17:37:12", "INDEX 01 17:37:13")

		c := want(t, rip.check(t), "cue-positions", StatusError)

		assert.Equal(t, "track 5: the cue sheet has 17:37:13, the log 17:37:12", c.Detail)
	})

	t.Run("a track with no INDEX 01", func(t *testing.T) {
		rip := blackRip(t)
		rewriteCue(t, rip, "    INDEX 01 17:37:12\n", "")

		c := want(t, rip.check(t), "cue-positions", StatusError)

		assert.Equal(t, "track 5 has no INDEX 01", c.Detail)
	})

	t.Run("many problems are summarized", func(t *testing.T) {
		rip := blackRip(t)
		n := 0
		text := regexp.MustCompile(`(?m)^(\s+INDEX 01 \d\d:\d\d:)(\d\d)$`).ReplaceAllStringFunc(readCue(t, rip), func(m string) string {
			n++
			if n > 5 {
				return m
			}
			return regexp.MustCompile(`\d\d$`).ReplaceAllString(m, "74") // none of these tracks starts on frame 74
		})
		setCue(t, rip, text)

		c := want(t, rip.check(t), "cue-positions", StatusError)

		assert.Equal(t, maxListed, strings.Count(c.Detail, "the cue sheet has"), "only some are named: %s", c.Detail)
		assert.Contains(t, c.Detail, "and 2 more")
	})
}

func TestCheckFileDiscID(t *testing.T) {
	t.Run("a different disc ID", func(t *testing.T) {
		rip := blackRip(t)
		rewriteCue(t, rip, "REM DISCID 7509E00A", "REM DISCID 7509E00B")

		c := want(t, rip.check(t), "cue-disc-id", StatusError)

		assert.Contains(t, c.Detail, "7509E00B")
		assert.Contains(t, c.Detail, "7509E00A")
		assert.Contains(t, c.Detail, "different discs")
	})

	t.Run("no disc ID in the cue sheet", func(t *testing.T) {
		rip := blackRip(t)
		rewriteCue(t, rip, "REM DISCID 7509E00A\n", "")

		c := want(t, rip.check(t), "cue-disc-id", StatusInfo)

		assert.Contains(t, c.Detail, "no REM DISCID")
	})

	t.Run("the case of the digits does not matter", func(t *testing.T) {
		rip := blackRip(t)
		rewriteCue(t, rip, "REM DISCID 7509E00A", "REM DISCID 7509e00a")

		want(t, rip.check(t), "cue-disc-id", StatusPass)
	})
}

func TestCheckFilePregaps(t *testing.T) {
	t.Run("no pregaps recorded is only a note", func(t *testing.T) {
		rip := blackRip(t)
		setCue(t, rip, regexp.MustCompile(`(?m)^\s+INDEX 00 .*\n`).ReplaceAllString(readCue(t, rip), ""))

		r := rip.check(t)

		c := want(t, r, "cue-pregaps", StatusInfo)
		assert.Contains(t, c.Detail, "none recorded")
		assert.Contains(t, c.Detail, "Detect Gaps")
		assert.False(t, r.Failed())
	})

	t.Run("a single pregap", func(t *testing.T) {
		rip := blackRip(t)
		text := readCue(t, rip)
		for _, line := range []string{"    INDEX 00 14:05:27\n", "    INDEX 00 21:52:32\n", "    INDEX 00 26:07:72\n", "    INDEX 00 29:38:20\n"} {
			text = replaced(t, text, line, "")
		}
		setCue(t, rip, text)

		c := want(t, rip.check(t), "cue-pregaps", StatusPass)

		assert.Equal(t, "1 pregap recorded (track 2)", c.Detail)
	})

	t.Run("a pregap on the first track", func(t *testing.T) {
		rip := blackRip(t)
		rewriteCue(t, rip, "    INDEX 01 00:00:00", "    INDEX 00 00:00:00\n    INDEX 01 00:02:00")

		c := want(t, rip.check(t), "cue-pregaps", StatusPass)

		assert.Equal(t, "6 pregaps recorded (tracks 1, 2, 4, 6, 7, 8)", c.Detail)
	})

	t.Run("a pregap that starts after its track does", func(t *testing.T) {
		rip := blackRip(t)
		rewriteCue(t, rip, "INDEX 00 05:12:22", "INDEX 00 05:12:40")

		c := want(t, rip.check(t), "cue-pregaps", StatusError)

		assert.Contains(t, c.Detail, "track 2: INDEX 00 at 05:12:40")
	})

	t.Run("a pregap that starts before the track before it", func(t *testing.T) {
		rip := blackRip(t)
		rewriteCue(t, rip, "INDEX 00 14:05:27", "INDEX 00 05:00:00")

		c := want(t, rip.check(t), "cue-pregaps", StatusError)

		assert.Contains(t, c.Detail, "track 4: INDEX 00 at 05:00:00")
	})

	t.Run("a pregap with no INDEX 01 to belong to", func(t *testing.T) {
		rip := blackRip(t)
		rewriteCue(t, rip, "    INDEX 01 05:12:30\n", "")

		c := want(t, rip.check(t), "cue-pregaps", StatusError)

		assert.Contains(t, c.Detail, "track 2")
	})

	t.Run("many problems are summarized", func(t *testing.T) {
		rip := blackRip(t)
		setCue(t, rip, regexp.MustCompile(`INDEX 00 \d\d:\d\d:\d\d`).ReplaceAllString(readCue(t, rip), "INDEX 00 99:59:74"))

		c := want(t, rip.check(t), "cue-pregaps", StatusError)

		assert.Contains(t, c.Detail, "and 2 more")
	})
}

func TestCheckFileImageFormat(t *testing.T) {
	tests := []struct {
		name           string
		rate, ch, bits int
		detail         string
	}{
		{"48 kHz", 48000, 2, 16, "48 kHz, 16-bit, stereo"},
		{"24-bit", 44100, 2, 24, "44.1 kHz, 24-bit, stereo"},
		{"mono", 44100, 1, 16, "44.1 kHz, 16-bit, mono"},
		{"surround", 44100, 6, 16, "44.1 kHz, 16-bit, 6 channels"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rip := blackRip(t)
			l, err := ParseLog(fixture(t, fixtureBlack))
			require.NoError(t, err)
			total := uint64(expectedImageSamples(l, 0))
			require.NoError(t, os.WriteFile(rip.path(".flac"), flacHeader(tc.rate, tc.ch, tc.bits, total), 0o644))

			c := want(t, rip.check(t), "image-format", StatusError)

			assert.Contains(t, c.Detail, tc.detail)
			assert.Contains(t, c.Detail, "a CD is 44.1 kHz, 16-bit, stereo")
		})
	}

	t.Run("the checks that need the image are not made without it", func(t *testing.T) {
		rip := blackRip(t)
		require.NoError(t, os.Remove(rip.path(".flac")))

		r := rip.check(t)

		want(t, r, "cue-image", StatusError)
		_, ok := r.Check("image-format")
		assert.False(t, ok)
		_, ok = r.Check("image-length")
		assert.False(t, ok)
	})
}

func TestCheckFileImageLength(t *testing.T) {
	const total = 111505380 // Back in Black, as metaflac reports it

	setLength := func(t *testing.T, rip rip, n uint64) {
		t.Helper()
		require.NoError(t, os.WriteFile(rip.path(".flac"), cdFLAC(n), 0o644))
	}

	t.Run("a whole sector short", func(t *testing.T) {
		rip := blackRip(t)
		setLength(t, rip, total-588)

		c := want(t, rip.check(t), "image-length", StatusError)

		assert.Contains(t, c.Detail, "holds 111504792 samples")
		assert.Contains(t, c.Detail, "gives 111505380")
		assert.True(t, strings.HasSuffix(c.Detail, "1 sector short"), c.Detail)
	})

	t.Run("several sectors long", func(t *testing.T) {
		rip := blackRip(t)
		setLength(t, rip, total+3*588)

		c := want(t, rip.check(t), "image-length", StatusError)

		assert.True(t, strings.HasSuffix(c.Detail, "3 sectors long"), c.Detail)
	})

	t.Run("a single sample out", func(t *testing.T) {
		for _, tc := range []struct {
			n   uint64
			end string
		}{{total - 1, "1 sample short"}, {total + 1, "1 sample long"}, {total - 100, "100 samples short"}} {
			rip := blackRip(t)
			setLength(t, rip, tc.n)

			c := want(t, rip.check(t), "image-length", StatusError)

			assert.True(t, strings.HasSuffix(c.Detail, tc.end), c.Detail)
		}
	})

	t.Run("a FLAC that does not record its length", func(t *testing.T) {
		rip := blackRip(t)
		setLength(t, rip, 0)

		c := want(t, rip.check(t), "image-length", StatusWarning)

		assert.Contains(t, c.Detail, "does not record its length")
	})

	t.Run("a cue sheet with no INDEX 01 for the first track", func(t *testing.T) {
		rip := blackRip(t)
		rewriteCue(t, rip, "    INDEX 01 00:00:00\n", "")

		c := want(t, rip.check(t), "image-length", StatusError)

		assert.Contains(t, c.Detail, "cannot tell where the image starts")
	})

	t.Run("a cue sheet that puts the first track later than the log does", func(t *testing.T) {
		rip := blackRip(t)
		rewriteCue(t, rip, "    INDEX 01 00:00:00", "    INDEX 01 00:01:00")

		c := want(t, rip.check(t), "image-length", StatusError)

		assert.Contains(t, c.Detail, "00:01:00")
		assert.Contains(t, c.Detail, "before the disc")
	})

	t.Run("a log with no table of contents", func(t *testing.T) {
		text := regexp.MustCompile(`(?m)^\s+\d+\s+\|\s+[\d:.]+\s+\|.*$`).ReplaceAllString(fixtureText(t, fixtureBlack), "")
		rip := buildRip(t, "Back in Black", []byte(text), cueText(t, fixtureBlackCue))

		c := want(t, rip.check(t), "image-length", StatusError)

		assert.Contains(t, c.Detail, "no table of contents")
	})

	t.Run("plural", func(t *testing.T) {
		assert.Equal(t, "sample", plural(1, "sample"))
		assert.Equal(t, "samples", plural(2, "sample"))
		assert.Equal(t, "samples", plural(0, "sample"))
	})
}

// hiddenTrackRip builds a rip of Back in Black as if the disc had audio before
// track 1: the log says track 1 starts at sector pregap, not 0. With included
// set, the cue sheet and image keep that audio, as EAC is understood to write
// them (INDEX 00 at 00:00:00, INDEX 01 at the log's start sector, an image that
// holds the whole disc); without it, the cue sheet and image begin at track 1.
func hiddenTrackRip(t *testing.T, pregap int, included bool) rip {
	t.Helper()

	// The log: track 1 begins at sector pregap instead of 0.
	logText := regexp.MustCompile(`(?m)^(\s+1\s+\|[^|]*\|[^|]*\|\s+)0(\s+\|\s+23429\s*)$`).
		ReplaceAllString(fixtureText(t, fixtureBlack), "${1}"+strconv.Itoa(pregap)+"${2}")
	require.NotEqual(t, fixtureText(t, fixtureBlack), logText, "the log's first track should have moved")
	l, err := ParseLog([]byte(logText))
	require.NoError(t, err)
	require.Equal(t, pregap, l.TOC[0].Start)

	// The cue sheet: its REM DISCID is the one EAC would have written for this
	// layout.
	cue := replaced(t, cueText(t, fixtureBlackCue), "REM DISCID 7509E00A", "REM DISCID "+l.FreeDBID())
	start := 0
	if included {
		cue = replaced(t, cue, "    INDEX 01 00:00:00", "    INDEX 00 00:00:00\n    INDEX 01 "+msf(pregap))
	} else {
		// Every position after track 1 is measured from where the image starts.
		start = pregap
		cue = regexp.MustCompile(`(?m)^(\s+)INDEX (0[01]) (\d\d):(\d\d):(\d\d)$`).ReplaceAllStringFunc(cue, func(m string) string {
			parts := regexp.MustCompile(`^(\s+)INDEX (0[01]) (\d\d):(\d\d):(\d\d)$`).FindStringSubmatch(m)
			mm, _ := strconv.Atoi(parts[3])
			ss, _ := strconv.Atoi(parts[4])
			ff, _ := strconv.Atoi(parts[5])
			frames := (mm*60+ss)*75 + ff
			if frames == 0 {
				return m // track 1, which starts the image
			}
			return parts[1] + "INDEX " + parts[2] + " " + msf(frames-pregap)
		})
	}

	r := buildRip(t, "Back in Black", []byte(logText), cue)
	require.NoError(t, os.WriteFile(r.path(".flac"), imageFLAC(t, []byte(logText), start), 0o644))
	return r
}

func TestCheckFileHiddenTrack(t *testing.T) {
	const pregap = 3000 // 40 seconds of audio before track 1

	t.Run("the audio before track 1 is in the image", func(t *testing.T) {
		r := hiddenTrackRip(t, pregap, true).check(t)

		assert.Equal(t, StatusPass, r.Worst(), "the whole image is consistent")
		assert.Equal(t, "6 pregaps recorded (tracks 1, 2, 4, 6, 7, 8)", want(t, r, "cue-pregaps", StatusPass).Detail)
		assert.Equal(t, "111505380 samples, as the log's table of contents gives",
			want(t, r, "image-length", StatusPass).Detail, "the image holds the whole disc")
	})

	t.Run("the audio before track 1 was left out", func(t *testing.T) {
		r := hiddenTrackRip(t, pregap, false).check(t)

		c := want(t, r, "cue-positions", StatusError)
		assert.Contains(t, c.Detail, "image starts at track 1")
		assert.Contains(t, c.Detail, "00:40:00")
		assert.Contains(t, c.Detail, "3000 frames")
		assert.Contains(t, c.Detail, "hidden track or pregap")

		// The image is consistent with its own cue sheet, so its length is
		// not the problem, and only the one cause is reported.
		want(t, r, "image-length", StatusPass)
		assert.Equal(t, 1, r.Count(StatusError))
	})

	t.Run("an image that left the audio out is shorter than the whole disc", func(t *testing.T) {
		r := hiddenTrackRip(t, pregap, false).check(t)

		c := want(t, r, "image-length", StatusPass)

		assert.Equal(t, strconv.FormatInt(int64(189635-pregap)*588, 10)+" samples, as the log's table of contents gives", c.Detail)
	})

	t.Run("a whole-disc cue sheet with an image that is too short", func(t *testing.T) {
		rip := hiddenTrackRip(t, pregap, true)
		l, err := ParseLog(fixture(t, fixtureBlack))
		require.NoError(t, err)
		// The length a leaves-it-out image would have.
		require.NoError(t, os.WriteFile(rip.path(".flac"), cdFLAC(uint64(expectedImageSamples(l, pregap))), 0o644))

		c := want(t, rip.check(t), "image-length", StatusError)

		assert.True(t, strings.HasSuffix(c.Detail, "3000 sectors short"), c.Detail)
	})

	t.Run("positions that differ in some other way are reported as they are", func(t *testing.T) {
		rip := hiddenTrackRip(t, pregap, false)
		rewriteCue(t, rip, "INDEX 01 16:57:12", "INDEX 01 16:57:13") // track 5, 3000 frames earlier

		c := want(t, rip.check(t), "cue-positions", StatusError)

		assert.NotContains(t, c.Detail, "image starts at track 1")
		assert.Contains(t, c.Detail, "the cue sheet has")
	})

	t.Run("an ordinary small difference is not mistaken for it", func(t *testing.T) {
		rip := blackRip(t) // track 1 starts at sector 0
		rewriteCue(t, rip, "INDEX 01 17:37:12", "INDEX 01 17:37:13")

		c := want(t, rip.check(t), "cue-positions", StatusError)

		assert.NotContains(t, c.Detail, "image starts at track 1")
	})
}

func TestImageStartsAtFirstTrack(t *testing.T) {
	cue := func(frames ...int) map[int]CueTrack {
		m := map[int]CueTrack{}
		for i, f := range frames {
			m[i+1] = CueTrack{Number: i + 1, Indexes: []CueIndex{{Number: 1, Frames: f}}}
		}
		return m
	}

	t.Run("every track earlier by the length of the audio before track 1", func(t *testing.T) {
		l := &Log{TOC: []TOCTrack{{1, 3000, 9999}, {2, 10000, 19999}}}

		before, ok := imageStartsAtFirstTrack(l, cue(0, 7000))

		assert.True(t, ok)
		assert.Equal(t, 3000, before)
	})

	t.Run("a disc whose first track starts at sector 0 has nothing before it", func(t *testing.T) {
		l := &Log{TOC: []TOCTrack{{1, 0, 9999}, {2, 10000, 19999}}}

		_, ok := imageStartsAtFirstTrack(l, cue(0, 10000))

		assert.False(t, ok, "positions that already match are not a shift")
	})

	t.Run("positions that are off by something else", func(t *testing.T) {
		l := &Log{TOC: []TOCTrack{{1, 3000, 9999}, {2, 10000, 19999}}}

		_, ok := imageStartsAtFirstTrack(l, cue(0, 7001))

		assert.False(t, ok)
	})

	t.Run("a track missing from the cue sheet", func(t *testing.T) {
		l := &Log{TOC: []TOCTrack{{1, 3000, 9999}, {2, 10000, 19999}}}

		_, ok := imageStartsAtFirstTrack(l, cue(0))

		assert.False(t, ok)
	})
}
