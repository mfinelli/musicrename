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

// Package riplog checks an Exact Audio Copy (EAC) rip log against the settings
// a rip is expected to have been made with.
//
// An EAC log records, besides the result of the rip, every setting that
// affects its accuracy: the read mode, whether the audio cache was defeated,
// the drive's offset correction, and so on. EAC will go ahead with a wrong
// setting and the audio often sounds fine, so nothing at the time points to the
// mistake; what the log offers is a record of how the rip was really made, to
// check against how it should have been.
//
// The package reads the log (ParseLog) and runs it against a fixed profile
// (Verify), producing a list of checks, each of which passed or did not. It is
// not a score: a check either holds or it does not, and the report says which
// and why.
//
// Scope, deliberately narrow:
//
//   - Image rips only: one disc, one file, cue sheet. EAC records these under
//     "Range status and errors" rather than as one "Track N" section per
//     track. A per-track log is reported as unsupported rather than judged
//     against rules that were written for something else.
//   - English logs from EAC 1.x only. A log in another language, or from an
//     older EAC whose layout differs, is reported as unsupported.
//   - The profile is the author's own, hardcoded, not configurable. It lives
//     in profile.go and drives.go.
//
// The log's checksum is checked for presence only, not verified. Verifying it
// would show that a log has not been edited since EAC wrote it, which matters
// for logs from other people and not for one's own.
package riplog

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// UnsupportedError reports a log this package does not handle, with the reason.
// It is not a statement that the rip was bad: only that the log is not in a
// form this package can judge.
type UnsupportedError struct {
	Reason string
}

func (e *UnsupportedError) Error() string {
	return "unsupported log: " + e.Reason
}

func unsupported(format string, args ...any) error {
	return &UnsupportedError{Reason: fmt.Sprintf(format, args...)}
}

// TOCTrack is one track of the disc's table of contents as EAC lists it.
type TOCTrack struct {
	Number int
	// Start and End are sectors (LBA: sector 0 is the start of the audio area,
	// two seconds after the physical start of the disc). End is the last
	// sector of the track, inclusive.
	Start, End int
}

// Range is the "Range status and errors" section: the result of ripping the
// whole selected range (for an image rip, the whole disc) as one piece.
type Range struct {
	// Filename is the image file as EAC wrote it, a Windows path to the
	// uncompressed .wav it encoded from.
	Filename string
	// PeakLevel, RangeQuality are percentages as printed.
	PeakLevel, RangeQuality string
	// TestCRC and CopyCRC are the CRCs of the test pass and the copy pass;
	// empty if that pass was not made.
	TestCRC, CopyCRC string
	// CopyOK is whether the log says "Copy OK".
	CopyOK bool
	// NoErrors is whether the log says "No errors occurred".
	NoErrors bool
	// Problems holds any other status lines in the section, which EAC uses to
	// describe what went wrong when NoErrors is false.
	Problems []string
}

// ARStatus is the AccurateRip verdict on one track.
type ARStatus int

const (
	// ARUnknown is a track line this package does not recognize.
	ARUnknown ARStatus = iota
	// ARAccurate means the track matched the AccurateRip database.
	ARAccurate
	// ARMismatch means the track was found in the database but did not match.
	ARMismatch
	// ARNotPresent means the track is not in the database.
	ARNotPresent
	// ARNotVerifiable means EAC could not look the track up (for example, it
	// was not ripped completely).
	ARNotVerifiable
)

// ARTrack is one line of the AccurateRip summary.
type ARTrack struct {
	Number     int
	Status     ARStatus
	Confidence int    // zero unless Status is ARAccurate or ARMismatch
	Text       string // the line, after the track number
}

// CTDB is the CUETools Database plugin's result, which EAC appends to the log
// when the plugin is installed and running.
type CTDB struct {
	// TOCID is CUETools' identifier for the disc. It is not a MusicBrainz disc
	// ID, though it looks like one.
	TOCID string
	// Found is whether the disc was already in the database.
	Found bool
	// Tracks is each track's status text, indexed from zero.
	Tracks []string
}

// Log is a parsed EAC log.
type Log struct {
	// Version is EAC's version as the log states it, such as "1.8".
	Version string
	// Title is the artist and album line.
	Title string
	// Drive is the drive as the log states it, with its adapter and ID.
	Drive string
	// Settings holds the "label : value" lines between the title and the table
	// of contents, keyed by lower-cased label. Use Setting to look one up.
	Settings map[string]string
	TOC      []TOCTrack
	Range    Range
	// AR is the AccurateRip summary, in track order. Empty if the log has none.
	AR []ARTrack
	// CTDB is nil if the log has no CUETools DB section.
	CTDB *CTDB
	// Checksum is the hex string on the final "==== Log checksum" line, or
	// empty if there is none.
	Checksum string
	// Normalization is whether any line says audio was normalized.
	Normalization bool
}

// Setting returns the value of the header setting with the given label (such
// as "Read mode") and whether the log has one. Labels are matched without
// regard to case.
func (l *Log) Setting(label string) (string, bool) {
	v, ok := l.Settings[strings.ToLower(label)]
	return v, ok
}

var (
	reVersion  = regexp.MustCompile(`^Exact Audio Copy V(\d+)\.(\d+)(.*?) from (.+)$`)
	reSetting  = regexp.MustCompile(`^(\S[^:]*?)\s*:\s+(.*?)\s*$`)
	reTOCRow   = regexp.MustCompile(`^\s*(\d+)\s*\|\s*[\d:.]+\s*\|\s*[\d:.]+\s*\|\s*(\d+)\s*\|\s*(\d+)\s*$`)
	reTrackHdr = regexp.MustCompile(`^Track\s+\d+$`)
	reARTrack  = regexp.MustCompile(`^Track\s+(\d+)\s+(.*)$`)
	reConf     = regexp.MustCompile(`(?i)confidence\s+(\d+)`)
	reCTDBID   = regexp.MustCompile(`^\[CTDB TOCID:\s*(\S+?)\]\s*(.*)$`)
	reCTDBRow  = regexp.MustCompile(`^(\d+)\s*\|\s*(.+)$`)
	reChecksum = regexp.MustCompile(`^==== Log checksum\s+(\S+)\s+====$`)
	reNormal   = regexp.MustCompile(`(?i)^Normalize to\s*:`)
)

// section is which part of the log the parser is in.
type section int

const (
	secHeader section = iota
	secTOC
	secRange
	secAR
	secPlugin
	secCTDB
)

// ParseLog reads an EAC log from its raw bytes (UTF-16 or UTF-8, as EAC writes
// them). A log that is not in the supported form (see the package comment)
// yields an *UnsupportedError.
func ParseLog(data []byte) (*Log, error) {
	text, err := decode(data)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(text, "\n")

	l := &Log{Settings: map[string]string{}}

	// The first non-empty line must be EAC's own version line.
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i == len(lines) {
		return nil, unsupported("the log is empty")
	}
	m := reVersion.FindStringSubmatch(strings.TrimSpace(lines[i]))
	if m == nil {
		return nil, unsupported("not an EAC 1.x log (first line is %q)", truncate(strings.TrimSpace(lines[i]), 60))
	}
	major, _ := strconv.Atoi(m[1])
	if major < 1 {
		return nil, unsupported("EAC %s.%s predates the 1.x log format", m[1], m[2])
	}
	l.Version = m[1] + "." + m[2] + strings.TrimRight(m[3], " ")

	sec := secHeader
	sawExtraction, sawRange := false, false
	titleNext := false
	var ctdbTracks map[int]string

	for _, raw := range lines[i+1:] {
		line := strings.TrimSpace(raw)

		// Lines that mark the start of a section, wherever they occur.
		switch {
		case strings.HasPrefix(line, "EAC extraction logfile from"):
			sawExtraction = true
			titleNext = true
			continue
		case line == "TOC of the extracted CD":
			sec = secTOC
			continue
		case line == "Range status and errors":
			sec, sawRange = secRange, true
			continue
		case line == "AccurateRip summary":
			sec = secAR
			continue
		case line == "End of status report":
			sec = secHeader
			continue
		case strings.HasPrefix(line, "---- CUETools DB Plugin"):
			sec = secCTDB
			l.CTDB = &CTDB{}
			ctdbTracks = map[int]string{}
			continue
		case strings.HasPrefix(line, "---- "):
			sec = secPlugin
			continue
		}
		if cm := reChecksum.FindStringSubmatch(line); cm != nil {
			l.Checksum = cm[1]
			continue
		}
		if reNormal.MatchString(line) {
			l.Normalization = true
		}
		if reTrackHdr.MatchString(line) {
			return nil, unsupported("per-track rip log (this checks image rips only)")
		}

		if line == "" {
			continue
		}

		switch sec {
		case secHeader:
			switch {
			case titleNext:
				l.Title = line
				titleNext = false
			case sawExtraction:
				if sm := reSetting.FindStringSubmatch(line); sm != nil {
					label := strings.ToLower(sm[1])
					l.Settings[label] = sm[2]
					if label == "used drive" {
						l.Drive = sm[2]
					}
				}
			}

		case secTOC:
			if tm := reTOCRow.FindStringSubmatch(line); tm != nil {
				n, _ := strconv.Atoi(tm[1])
				s, _ := strconv.Atoi(tm[2])
				e, _ := strconv.Atoi(tm[3])
				l.TOC = append(l.TOC, TOCTrack{Number: n, Start: s, End: e})
			}

		case secRange:
			parseRangeLine(&l.Range, line)

		case secAR:
			if am := reARTrack.FindStringSubmatch(line); am != nil {
				n, _ := strconv.Atoi(am[1])
				l.AR = append(l.AR, classifyAR(n, am[2]))
			}

		case secCTDB:
			if cm := reCTDBID.FindStringSubmatch(line); cm != nil {
				l.CTDB.TOCID = cm[1]
				l.CTDB.Found = strings.Contains(strings.ToLower(cm[2]), "found") &&
					!strings.Contains(strings.ToLower(cm[2]), "not present")
			} else if rm := reCTDBRow.FindStringSubmatch(line); rm != nil {
				n, _ := strconv.Atoi(rm[1])
				ctdbTracks[n] = rm[2]
			}
		}
	}

	if !sawExtraction {
		return nil, unsupported("no \"EAC extraction logfile\" line: not an English EAC log")
	}
	if !sawRange {
		return nil, unsupported("no \"Range status and errors\" section")
	}

	if l.CTDB != nil {
		for n := 1; n <= len(ctdbTracks); n++ {
			l.CTDB.Tracks = append(l.CTDB.Tracks, ctdbTracks[n])
		}
	}
	return l, nil
}

// parseRangeLine interprets one line of the "Range status and errors" section.
func parseRangeLine(r *Range, line string) {
	switch {
	case line == "Selected range":
	case strings.HasPrefix(line, "Filename "):
		r.Filename = strings.TrimSpace(strings.TrimPrefix(line, "Filename "))
	case strings.HasPrefix(line, "Peak level "):
		r.PeakLevel = strings.TrimSpace(strings.TrimPrefix(line, "Peak level "))
	case strings.HasPrefix(line, "Extraction speed "):
	case strings.HasPrefix(line, "Range quality "):
		r.RangeQuality = strings.TrimSpace(strings.TrimPrefix(line, "Range quality "))
	case strings.HasPrefix(line, "Test CRC "):
		r.TestCRC = strings.TrimSpace(strings.TrimPrefix(line, "Test CRC "))
	case strings.HasPrefix(line, "Copy CRC "):
		r.CopyCRC = strings.TrimSpace(strings.TrimPrefix(line, "Copy CRC "))
	case line == "Copy OK":
		r.CopyOK = true
	case line == "No errors occurred":
		r.NoErrors = true
	default:
		r.Problems = append(r.Problems, line)
	}
}

// classifyAR interprets the text after "Track N" on an AccurateRip summary
// line. EAC words these in several ways; they are told apart by their key
// phrases.
func classifyAR(n int, text string) ARTrack {
	t := ARTrack{Number: n, Text: text}
	lower := strings.ToLower(text)

	switch {
	case strings.Contains(lower, "cannot be verified"):
		t.Status = ARMismatch
	case strings.Contains(lower, "not fully ripped"):
		t.Status = ARNotVerifiable
	case strings.Contains(lower, "not present in"):
		t.Status = ARNotPresent
	case strings.Contains(lower, "accurately ripped"):
		t.Status = ARAccurate
	}
	if cm := reConf.FindStringSubmatch(text); cm != nil && (t.Status == ARAccurate || t.Status == ARMismatch) {
		t.Confidence, _ = strconv.Atoi(cm[1])
	}
	return t
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
