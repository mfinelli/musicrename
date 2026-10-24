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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRealLogs(t *testing.T) {
	t.Run("Back in Black", func(t *testing.T) {
		l, err := ParseLog(fixture(t, fixtureBlack))

		require.NoError(t, err)
		assert.Equal(t, "1.8", l.Version)
		assert.Equal(t, "AC/DC / Back in Black", l.Title)
		assert.Equal(t, "ASUS    SDRW-08V1M-U   Adapter: 1  ID: 0", l.Drive)

		require.Len(t, l.TOC, 10)
		assert.Equal(t, TOCTrack{Number: 1, Start: 0, End: 23429}, l.TOC[0])
		assert.Equal(t, TOCTrack{Number: 10, Start: 169682, End: 189634}, l.TOC[9])

		assert.Equal(t, Range{
			Filename:     `C:\users\mario\Music\Back in Black.wav`,
			PeakLevel:    "100.0 %",
			RangeQuality: "100.0 %",
			TestCRC:      "9FB18E2F",
			CopyCRC:      "9FB18E2F",
			CopyOK:       true,
			NoErrors:     true,
		}, l.Range)

		require.Len(t, l.AR, 10)
		assert.Equal(t, ARAccurate, l.AR[0].Status)
		assert.Equal(t, 200, l.AR[0].Confidence)
		assert.Equal(t, 10, l.AR[9].Number)

		require.NotNil(t, l.CTDB)
		assert.Equal(t, "65IDfJEiUdejkAyazx.i9JS5hOc-", l.CTDB.TOCID)
		assert.True(t, l.CTDB.Found)
		assert.Len(t, l.CTDB.Tracks, 10)
		assert.Contains(t, l.CTDB.Tracks[5], "differs in 43 samples", "a track with alternative results keeps its text")

		assert.Equal(t, "66FF03DD933AE06F39B8636454D88AD8E571A1ADB70B19A586756787DB5D72DB", l.Checksum)
		assert.False(t, l.Normalization)
	})

	t.Run("B'day has no CUETools DB section", func(t *testing.T) {
		l, err := ParseLog(fixture(t, fixtureBday))

		require.NoError(t, err)
		assert.Equal(t, "Beyoncé / B'day [Deluxe Edition]", l.Title, "accents and brackets survive")
		assert.Equal(t, `C:\users\mario\Music\B'day.wav`, l.Range.Filename)
		assert.Len(t, l.TOC, 19)
		assert.Len(t, l.AR, 19)
		assert.Nil(t, l.CTDB)
	})

	t.Run("a disc of a multi-disc release", func(t *testing.T) {
		l, err := ParseLog(fixture(t, fixtureCentral))

		require.NoError(t, err)
		assert.Equal(t, "Dave Matthews Band / The Central Park Concert", l.Title)
		assert.Equal(t, `C:\users\mario\Music\The Central Park Concert (disc 1).wav`, l.Range.Filename)
		assert.Len(t, l.TOC, 6)
		require.NotNil(t, l.CTDB)
		assert.Len(t, l.CTDB.Tracks, 6)
	})
}

func TestParseSettings(t *testing.T) {
	l, err := ParseLog(fixture(t, fixtureBlack))
	require.NoError(t, err)

	tests := map[string]string{
		"Read mode":                                   "Secure",
		"Utilize accurate stream":                     "Yes",
		"Defeat audio cache":                          "Yes",
		"Make use of C2 pointers":                     "No",
		"Read offset correction":                      "6",
		"Fill up missing offset samples with silence": "Yes",
		"Delete leading and trailing silent blocks":   "No",
		"Null samples used in CRC calculations":       "Yes",
		"Add ID3 tag":                                 "No",
		"Used output format":                          "User Defined Encoder",
	}
	for label, want := range tests {
		got, ok := l.Setting(label)
		assert.True(t, ok, label)
		assert.Equal(t, want, got, label)
	}

	t.Run("labels match without regard to case", func(t *testing.T) {
		got, ok := l.Setting("READ MODE")
		assert.True(t, ok)
		assert.Equal(t, "Secure", got)
	})

	t.Run("a value that contains a colon is kept whole", func(t *testing.T) {
		got, ok := l.Setting("Command line compressor")
		assert.True(t, ok)
		assert.Equal(t, `Z:\home\mario\.exact-audio-copy\eac\FLAC\FLAC.EXE`, got)
	})

	t.Run("a long value is kept whole", func(t *testing.T) {
		got, ok := l.Setting("Additional command line options")
		assert.True(t, ok)
		assert.True(t, strings.HasPrefix(got, "-6 -V -T "))
		assert.True(t, strings.HasSuffix(got, "%source% -o %dest%"))
	})

	t.Run("an absent setting is reported as absent", func(t *testing.T) {
		_, ok := l.Setting("No such setting")
		assert.False(t, ok)
	})

	t.Run("the title is not mistaken for a setting even if it contains a colon", func(t *testing.T) {
		text := replaced(t, fixtureText(t, fixtureBlack), "AC/DC / Back in Black", "AC/DC / Live: Back in Black")

		l := parse(t, text)

		assert.Equal(t, "AC/DC / Live: Back in Black", l.Title)
		_, ok := l.Setting("AC/DC / Live")
		assert.False(t, ok)
	})
}

func TestParseUnsupported(t *testing.T) {
	good := fixtureText(t, fixtureBlack)

	tests := []struct {
		name   string
		data   []byte
		reason string
	}{
		{"empty", nil, "empty"},
		{"only whitespace", []byte(" \n\n \n"), "empty"},
		{"not an EAC log", []byte("hello\nworld\n"), "not an EAC 1.x log"},
		{"an XLD log", []byte("X Lossless Decoder version 20121222 (153.1)\n"), "not an EAC 1.x log"},
		{"EAC before 1.0", []byte(strings.Replace(good, "V1.8 from", "V0.99 from", 1)), "predates"},
		{"an old-style version line", []byte(strings.Replace(good, "Exact Audio Copy V1.8 from", "Exact Audio Copy 0.99 prebeta 5 from", 1)), "not an EAC 1.x log"},
		{"a log in Italian", []byte(strings.Replace(good, "EAC extraction logfile from", "File di log di estrazione EAC del", 1)), "not an English EAC log"},
		{"a per-track log", []byte(strings.Replace(good, "Range status and errors\n\nSelected range\n", "Track  1\n     Filename x.wav\n", 1)), "per-track"},
		{"no range section", []byte(strings.Replace(good, "Range status and errors", "Something else", 1)), "Range status and errors"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, err := ParseLog(tc.data)

			assert.Nil(t, l)
			var ue *UnsupportedError
			require.True(t, errors.As(err, &ue), "got %v", err)
			assert.Contains(t, ue.Error(), tc.reason)
			assert.Contains(t, ue.Error(), "unsupported log")
		})
	}

	t.Run("bytes that are not text are an error", func(t *testing.T) {
		l, err := ParseLog([]byte{0x80, 0x81, 0x82})

		assert.Nil(t, l)
		assert.Error(t, err)
	})
}

func TestClassifyAR(t *testing.T) {
	tests := []struct {
		text       string
		want       ARStatus
		confidence int
	}{
		{"accurately ripped (confidence 103)  [5229458F]  (AR v2)", ARAccurate, 103},
		{"Accurately ripped (confidence 1)  [5229458F]", ARAccurate, 1},
		{"cannot be verified as accurate (confidence 2)  [AAAA], AccurateRip returned [BBBB]  (AR v2)", ARMismatch, 2},
		{"Cannot be verified as accurate", ARMismatch, 0},
		{"not present in database", ARNotPresent, 0},
		{"not fully ripped for AccurateRip lookup", ARNotVerifiable, 0},
		{"something EAC has not said before", ARUnknown, 0},
	}
	for _, tc := range tests {
		t.Run(tc.text, func(t *testing.T) {
			got := classifyAR(4, tc.text)

			assert.Equal(t, tc.want, got.Status)
			assert.Equal(t, tc.confidence, got.Confidence)
			assert.Equal(t, 4, got.Number)
			assert.Equal(t, tc.text, got.Text)
		})
	}
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "abc", truncate("abc", 5))
	assert.Equal(t, "abcde…", truncate("abcdefgh", 5))
}
