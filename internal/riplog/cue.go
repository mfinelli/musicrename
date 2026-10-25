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
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// CueIndex is one INDEX line of a cue sheet: a numbered position, in frames
// (75 to a second) from the start of the audio file.
type CueIndex struct {
	Number int
	Frames int
}

// CueTrack is one TRACK of a cue sheet.
type CueTrack struct {
	Number int
	// Type is the track's mode as the cue sheet states it, "AUDIO" for an
	// audio track.
	Type    string
	Indexes []CueIndex
	// Title, Performer and ISRC are the track's, from its TITLE, PERFORMER and
	// ISRC lines, or "" if it has none.
	Title, Performer, ISRC string
}

// Index returns the position of the track's INDEX n, in frames, and whether the
// track has one. INDEX 01 is where the track starts; INDEX 00, when there is
// one, is where its pregap (the silence or lead-in before it, which belongs to
// the track before) starts.
func (t CueTrack) Index(n int) (frames int, ok bool) {
	for _, i := range t.Indexes {
		if i.Number == n {
			return i.Frames, true
		}
	}
	return 0, false
}

// Cue is a parsed cue sheet. Only what is checked is kept: the audio files it
// names, its tracks and their indexes, and the disc ID EAC records in it.
type Cue struct {
	// Files are the names given on the FILE lines, in order, as written. A cue
	// sheet for an image rip names one.
	Files  []string
	Tracks []CueTrack
	// DiscID is the freedb disc ID from the "REM DISCID" line, or "" if there
	// is none.
	DiscID string
	// Title and Performer are the album's, from the TITLE and PERFORMER lines
	// before the first TRACK, or "" if there are none. They are in UTF-8, whatever
	// the cue sheet's encoding was.
	Title, Performer string
}

var (
	reCueFile   = regexp.MustCompile(`(?i)^FILE\s+(?:"(.*)"|(\S+))\s+\S+$`)
	reCueTrack  = regexp.MustCompile(`(?i)^TRACK\s+(\d+)\s+(\S+)$`)
	reCueIndex  = regexp.MustCompile(`(?i)^INDEX\s+(\d+)\s+(\d+):(\d+):(\d+)$`)
	reCueDiscID = regexp.MustCompile(`(?i)^REM\s+DISCID\s+(\S+)`)
	reCueText   = regexp.MustCompile(`(?i)^(TITLE|PERFORMER|ISRC)\s+(.*)$`)
)

// ParseCue reads a cue sheet from its raw bytes. EAC writes them in the system's
// ANSI code page by default, so text that is not valid UTF-8 is read as
// Windows-1252.
func ParseCue(data []byte) (*Cue, error) {
	text := decodeLegacy(data)

	c := &Cue{}
	for n, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		where := func(format string, args ...any) error {
			return fmt.Errorf("line %d: %s", n+1, fmt.Sprintf(format, args...))
		}

		switch {
		case reCueFile.MatchString(line):
			m := reCueFile.FindStringSubmatch(line)
			name := m[1]
			if m[2] != "" {
				name = m[2]
			}
			c.Files = append(c.Files, name)

		case reCueTrack.MatchString(line):
			m := reCueTrack.FindStringSubmatch(line)
			num, _ := strconv.Atoi(m[1])
			c.Tracks = append(c.Tracks, CueTrack{Number: num, Type: strings.ToUpper(m[2])})

		case reCueIndex.MatchString(line):
			if len(c.Tracks) == 0 {
				return nil, where("INDEX before any TRACK")
			}
			m := reCueIndex.FindStringSubmatch(line)
			num, _ := strconv.Atoi(m[1])
			mm, _ := strconv.Atoi(m[2])
			ss, _ := strconv.Atoi(m[3])
			ff, _ := strconv.Atoi(m[4])
			if ss >= 60 || ff >= framesPerSecond {
				return nil, where("%q is not a valid position", m[2]+":"+m[3]+":"+m[4])
			}
			t := &c.Tracks[len(c.Tracks)-1]
			t.Indexes = append(t.Indexes, CueIndex{
				Number: num,
				Frames: (mm*60+ss)*framesPerSecond + ff,
			})

		case reCueDiscID.MatchString(line):
			c.DiscID = reCueDiscID.FindStringSubmatch(line)[1]

		case reCueText.MatchString(line):
			m := reCueText.FindStringSubmatch(line)
			value := cueUnquote(m[2])
			switch key := strings.ToUpper(m[1]); {
			case len(c.Tracks) == 0 && key == "TITLE":
				c.Title = value
			case len(c.Tracks) == 0 && key == "PERFORMER":
				c.Performer = value
			case len(c.Tracks) > 0 && key == "TITLE":
				c.Tracks[len(c.Tracks)-1].Title = value
			case len(c.Tracks) > 0 && key == "PERFORMER":
				c.Tracks[len(c.Tracks)-1].Performer = value
			case len(c.Tracks) > 0 && key == "ISRC":
				c.Tracks[len(c.Tracks)-1].ISRC = value
			}
		}
	}
	return c, nil
}

// cueUnquote returns the text of a cue sheet value, which has no space around it
// as it is from a line that was trimmed: what is between its quotes if it has
// them, which is what EAC writes, and what there is if it does not. A quote
// inside it is part of it.
func cueUnquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	return s
}
