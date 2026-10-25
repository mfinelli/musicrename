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
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCueRealCues(t *testing.T) {
	t.Run("Back in Black", func(t *testing.T) {
		c, err := ParseCue(fixture(t, fixtureBlackCue))

		require.NoError(t, err)
		assert.Equal(t, []string{"Back in Black.flac"}, c.Files)
		assert.Equal(t, "7509E00A", c.DiscID)
		require.Len(t, c.Tracks, 10)

		first := c.Tracks[0]
		assert.Equal(t, 1, first.Number)
		assert.Equal(t, "AUDIO", first.Type)
		i1, ok := first.Index(1)
		assert.True(t, ok)
		assert.Equal(t, 0, i1)
		_, ok = first.Index(0)
		assert.False(t, ok, "track 1 has no pregap")

		second := c.Tracks[1]
		i0, ok := second.Index(0)
		assert.True(t, ok)
		assert.Equal(t, (5*60+12)*75+22, i0, "INDEX 00 05:12:22")
		i1, _ = second.Index(1)
		assert.Equal(t, 23430, i1, "INDEX 01 05:12:30 is the log's start sector")

		var pregaps []int
		for _, tr := range c.Tracks {
			if _, ok := tr.Index(0); ok {
				pregaps = append(pregaps, tr.Number)
			}
		}
		assert.Equal(t, []int{2, 4, 6, 7, 8}, pregaps)
	})

	t.Run("B'day, which is not UTF-8", func(t *testing.T) {
		c, err := ParseCue(fixture(t, fixtureBdayCue))

		require.NoError(t, err)
		assert.Equal(t, []string{"B'day.flac"}, c.Files)
		assert.Equal(t, "13111213", c.DiscID)
		assert.Len(t, c.Tracks, 19)
	})
}

func TestDecodeLegacy(t *testing.T) {
	t.Run("the B'day cue sheet's accent survives", func(t *testing.T) {
		got := decodeLegacy(fixture(t, fixtureBdayCue))

		assert.Contains(t, got, `PERFORMER "Beyoncé"`)
		assert.NotContains(t, got, "\r")
	})

	t.Run("valid UTF-8 is left alone", func(t *testing.T) {
		got := decodeLegacy([]byte("TITLE \"Beyoncé\"\r\n"))

		assert.Equal(t, "TITLE \"Beyoncé\"\n", got)
	})

	t.Run("UTF-16 is still recognized", func(t *testing.T) {
		got := decodeLegacy(encodeUTF16("TITLE \"x\"\r\n", false, true))

		assert.Equal(t, "TITLE \"x\"\n", got)
	})

	t.Run("the characters Windows-1252 has beyond Latin-1", func(t *testing.T) {
		// 0x80 euro, 0x93 and 0x94 curly quotes, 0x96 en dash; 0xE9 as Latin-1.
		got := decodeLegacy([]byte{0x80, 0x20, 0x93, 'a', 0x94, 0x20, 0x96, 0x20, 0xE9})

		assert.Equal(t, "€ “a” – é", got)
	})

	t.Run("an undefined byte does not stop the decode", func(t *testing.T) {
		got := decodeLegacy([]byte{'a', 0x81, 'b'})

		assert.Equal(t, "a\u0081b", got)
	})
}

func TestCleanText(t *testing.T) {
	assert.Equal(t, "a\nb\nc", cleanText("a\r\nb\rc"))
	assert.Equal(t, "text\n", cleanText("text\n\x00junk"))
}

func TestParseCueSyntax(t *testing.T) {
	t.Run("keywords in any case, and unquoted file names", func(t *testing.T) {
		c, err := ParseCue([]byte("file album.flac wave\ntrack 01 audio\n  index 01 00:00:00\n"))

		require.NoError(t, err)
		assert.Equal(t, []string{"album.flac"}, c.Files)
		require.Len(t, c.Tracks, 1)
		assert.Equal(t, "AUDIO", c.Tracks[0].Type)
	})

	t.Run("a quoted file name may contain spaces and quotes of another kind", func(t *testing.T) {
		c, err := ParseCue([]byte(`FILE "Rock and Roll Ain't Noise (disc 1).flac" WAVE` + "\n"))

		require.NoError(t, err)
		assert.Equal(t, []string{"Rock and Roll Ain't Noise (disc 1).flac"}, c.Files)
	})

	t.Run("several files are all kept", func(t *testing.T) {
		c, err := ParseCue([]byte("FILE \"a.flac\" WAVE\nTRACK 01 AUDIO\nINDEX 01 00:00:00\nFILE \"b.flac\" WAVE\n"))

		require.NoError(t, err)
		assert.Equal(t, []string{"a.flac", "b.flac"}, c.Files)
	})

	t.Run("indexes beyond 01 are kept", func(t *testing.T) {
		c, err := ParseCue([]byte("TRACK 01 AUDIO\nINDEX 01 00:00:00\nINDEX 02 01:00:00\n"))

		require.NoError(t, err)
		i2, ok := c.Tracks[0].Index(2)
		assert.True(t, ok)
		assert.Equal(t, 60*75, i2)
	})

	t.Run("a data track keeps its type", func(t *testing.T) {
		c, err := ParseCue([]byte("TRACK 01 AUDIO\nINDEX 01 00:00:00\nTRACK 02 MODE1/2352\nINDEX 01 50:00:00\n"))

		require.NoError(t, err)
		assert.Equal(t, "MODE1/2352", c.Tracks[1].Type)
	})

	t.Run("everything else is ignored", func(t *testing.T) {
		c, err := ParseCue([]byte("REM GENRE Rock\nPERFORMER \"x\"\nTITLE \"y\"\nCATALOG 123\n\n  ISRC AAAA\nFLAGS DCP\n"))

		require.NoError(t, err)
		assert.Empty(t, c.Tracks)
		assert.Empty(t, c.Files)
		assert.Equal(t, "", c.DiscID)
	})

	t.Run("the disc ID", func(t *testing.T) {
		c, err := ParseCue([]byte("REM DISCID 7509E00A\n"))

		require.NoError(t, err)
		assert.Equal(t, "7509E00A", c.DiscID)
	})

	t.Run("an index before any track", func(t *testing.T) {
		_, err := ParseCue([]byte("REM x\nINDEX 01 00:00:00\n"))

		assert.ErrorContains(t, err, "line 2")
		assert.ErrorContains(t, err, "INDEX before any TRACK")
	})

	t.Run("positions that cannot exist", func(t *testing.T) {
		for _, bad := range []string{"00:60:00", "00:00:75"} {
			_, err := ParseCue([]byte("TRACK 01 AUDIO\nINDEX 01 " + bad + "\n"))

			assert.ErrorContains(t, err, "not a valid position", bad)
			assert.ErrorContains(t, err, "line 2", bad)
		}
	})

	t.Run("a cue sheet may be empty", func(t *testing.T) {
		c, err := ParseCue(nil)

		require.NoError(t, err)
		assert.Empty(t, c.Tracks)
	})
}

func TestCueTrackIndex(t *testing.T) {
	track := CueTrack{Indexes: []CueIndex{{Number: 0, Frames: 10}, {Number: 1, Frames: 20}}}

	got, ok := track.Index(0)
	assert.True(t, ok)
	assert.Equal(t, 10, got)

	got, ok = track.Index(1)
	assert.True(t, ok)
	assert.Equal(t, 20, got)

	_, ok = track.Index(2)
	assert.False(t, ok)
}

func TestParseCueMetadata(t *testing.T) {
	t.Run("a cue sheet of EAC's, in UTF-8", func(t *testing.T) {
		c, err := ParseCue(fixture(t, "back-in-black.cue"))
		require.NoError(t, err)

		assert.Equal(t, "Back in Black", c.Title)
		assert.Equal(t, "AC/DC", c.Performer)
		require.Len(t, c.Tracks, 10)
		assert.Equal(t, "Hells Bells", c.Tracks[0].Title)
		assert.Equal(t, "AC/DC", c.Tracks[0].Performer)
		assert.Equal(t, "AUAP08000041", c.Tracks[0].ISRC)
		assert.Equal(t, "Rock and Roll Ain't Noise Pollution", c.Tracks[9].Title, "an apostrophe is part of it")
		assert.Equal(t, "AUAP08000050", c.Tracks[9].ISRC)
	})

	t.Run("one in the Windows code page, read as text and not as bytes", func(t *testing.T) {
		raw := fixture(t, "bday.cue")
		require.False(t, utf8.Valid(raw), "it is not UTF-8, which is what makes this a test")

		c, err := ParseCue(raw)
		require.NoError(t, err)

		assert.Equal(t, "B'day [Deluxe Edition]", c.Title)
		assert.Equal(t, "Beyoncé", c.Performer, "the album's artist")
		require.Len(t, c.Tracks, 19)
		assert.Equal(t, "Beyoncé & Shakira", c.Tracks[0].Performer, "a track's can differ from the album's")
		assert.Equal(t, "Beautiful Liar", c.Tracks[0].Title)
		assert.Equal(t, "USSM10700448", c.Tracks[0].ISRC)
		assert.True(t, utf8.ValidString(c.Performer+c.Tracks[0].Performer))
	})

	t.Run("an older EAC's, which has no ISRC", func(t *testing.T) {
		c, err := ParseCue(fixture(t, "transistor.cue"))
		require.NoError(t, err)

		assert.Equal(t, "Transistor", c.Title)
		assert.Equal(t, "311", c.Performer)
		require.Len(t, c.Tracks, 21)
		assert.Equal(t, "Stealing Happy Hours", c.Tracks[20].Title)
		for _, tr := range c.Tracks {
			assert.Equal(t, "", tr.ISRC, "track %d", tr.Number)
		}
	})

	t.Run("lines are the album's before the first track and the track's after", func(t *testing.T) {
		c, err := ParseCue([]byte("PERFORMER \"Album Artist\"\r\nTITLE \"The Album\"\r\n" +
			"FILE \"a.flac\" WAVE\r\n" +
			"  TRACK 01 AUDIO\r\n    TITLE \"One\"\r\n    INDEX 01 00:00:00\r\n" +
			"  TRACK 02 AUDIO\r\n    PERFORMER \"Guest\"\r\n    ISRC XX1234567890\r\n    INDEX 01 03:00:00\r\n"))
		require.NoError(t, err)

		assert.Equal(t, "The Album", c.Title)
		assert.Equal(t, "Album Artist", c.Performer)
		assert.Equal(t, CueTrack{Number: 1, Type: "AUDIO", Title: "One", Indexes: []CueIndex{{1, 0}}}, c.Tracks[0], "none of the album's")
		assert.Equal(t, "Guest", c.Tracks[1].Performer)
		assert.Equal(t, "", c.Tracks[1].Title)
		assert.Equal(t, "XX1234567890", c.Tracks[1].ISRC)
	})

	t.Run("values as people write them", func(t *testing.T) {
		for name, tc := range map[string]struct{ line, want string }{
			"quoted":                         {`TITLE "Hello"`, "Hello"},
			"not quoted":                     {`TITLE Hello`, "Hello"},
			"quotes inside":                  {`TITLE "Say "hi""`, `Say "hi"`},
			"spaces around":                  {`TITLE    "  Hello  "   `, "  Hello  "},
			"lower case keyword":             {`title "Hello"`, "Hello"},
			"an empty one":                   {`TITLE ""`, ""},
			"one quote":                      {`TITLE "`, `"`},
			"text that looks like a keyword": {`TITLE "PERFORMER x"`, "PERFORMER x"},
		} {
			c, err := ParseCue([]byte(tc.line + "\r\n"))
			require.NoError(t, err, name)
			assert.Equal(t, tc.want, c.Title, name)
		}
	})

	t.Run("a byte order mark does not hide the first line", func(t *testing.T) {
		c, err := ParseCue([]byte("\xef\xbb\xbfTITLE \"First\"\r\nPERFORMER \"Someone\"\r\n"))
		require.NoError(t, err)

		assert.Equal(t, "First", c.Title)
		assert.Equal(t, "Someone", c.Performer)
	})

	t.Run("a longer word that begins with a keyword is not one", func(t *testing.T) {
		c, err := ParseCue([]byte("TITLEX \"no\"\r\nPERFORMERS \"no\"\r\n  TRACK 01 AUDIO\r\n    ISRCX 1\r\n    INDEX 01 00:00:00\r\n"))
		require.NoError(t, err)

		assert.Equal(t, "", c.Title)
		assert.Equal(t, "", c.Performer)
		assert.Equal(t, "", c.Tracks[0].ISRC)
	})

	t.Run("a keyword with nothing after it", func(t *testing.T) {
		c, err := ParseCue([]byte("TITLE\r\nPERFORMER\r\n  TRACK 01 AUDIO\r\n    ISRC\r\n    INDEX 01 00:00:00\r\n"))
		require.NoError(t, err)

		assert.Equal(t, "", c.Title)
		assert.Equal(t, "", c.Performer)
		assert.Equal(t, "", c.Tracks[0].ISRC)
	})

	t.Run("what was parsed before is still parsed", func(t *testing.T) {
		c, err := ParseCue(fixture(t, "transistor.cue"))
		require.NoError(t, err)

		assert.Equal(t, "310F4D15", c.DiscID)
		assert.Equal(t, []string{"[1997] Transistor.flac"}, c.Files)
		frames, ok := c.Tracks[0].Index(1)
		assert.True(t, ok)
		assert.Equal(t, 12000, frames)
	})
}
