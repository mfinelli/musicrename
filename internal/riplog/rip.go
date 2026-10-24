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
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// CheckFile reads the rip log at logPath and runs every check on it: those that
// need only the log (Verify), and those that need the files beside it, namely
// the cue sheet with the same name and the audio image the cue sheet names. For
// a log named "Album.log" those are "Album.cue" and the file it points to.
//
// The error is non-nil only when the log itself cannot be read or is not one
// this package supports (an *UnsupportedError), so there is nothing to check.
// A missing or wrong cue sheet or image is not an error here but a failed check
// in the result.
func CheckFile(logPath string) (*Log, *Result, error) {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return nil, nil, fmt.Errorf("could not read: %w", err)
	}

	l, err := ParseLog(data)
	if err != nil {
		var unsupported *UnsupportedError
		if errors.As(err, &unsupported) {
			return nil, nil, err
		}
		return nil, nil, fmt.Errorf("could not read the log: %w", err)
	}

	r := Verify(l)
	verifyCue(r, l, logPath)
	return l, r, nil
}

// findFile returns the path of the file in dir named name, or one whose name
// differs from it only in case, or "" if there is neither. The exact name is
// preferred.
func findFile(dir, name string) string {
	exact := filepath.Join(dir, name)
	if _, err := os.Lstat(exact); err == nil {
		return exact
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if strings.EqualFold(e.Name(), name) {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

// verifyCue adds the checks on the cue sheet beside the log, and on the audio
// image it names.
func verifyCue(r *Result, l *Log, logPath string) {
	dir := filepath.Dir(logPath)
	stem := strings.TrimSuffix(filepath.Base(logPath), filepath.Ext(logPath))
	want := stem + ".cue"

	cuePath := findFile(dir, want)
	if cuePath == "" {
		r.add("cue-sheet", "Cue sheet", StatusError,
			"no %q next to the log: an image rip needs its cue sheet", want)
		return
	}

	data, err := os.ReadFile(cuePath)
	if err != nil {
		r.add("cue-sheet", "Cue sheet", StatusError, "could not read %q: %v", filepath.Base(cuePath), err)
		return
	}
	c, err := ParseCue(data)
	if err != nil {
		r.add("cue-sheet", "Cue sheet", StatusError, "%s: %v", filepath.Base(cuePath), err)
		return
	}
	r.add("cue-sheet", "Cue sheet", StatusPass, "%s", filepath.Base(cuePath))

	if info := checkCueImage(r, l, c, dir); info != nil {
		checkImageFormat(r, *info)
		checkImageLength(r, l, c, *info)
	}
	checkCueTracks(r, l, c)
	checkCuePositions(r, l, c)
	checkCueDiscID(r, l, c)
	checkCuePregaps(r, c)
}

// windowsBase returns the last element of a path written with either kind of
// separator. The log records the image's name as the Windows path EAC wrote it
// to, which means backslashes even when EAC ran under WINE on Linux.
func windowsBase(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

func stemOf(name string) string {
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// checkCueImage confirms the cue sheet points at one FLAC file that is there,
// and that it is the image the log says was ripped. It returns the file's
// properties if it is a FLAC file that can be examined further, and nil if not.
func checkCueImage(r *Result, l *Log, c *Cue, dir string) *streamInfo {
	const id, name = "cue-image", "Audio image"

	switch len(c.Files) {
	case 0:
		r.add(id, name, StatusError, "the cue sheet names no audio file")
		return nil
	case 1:
	default:
		r.add(id, name, StatusError,
			"the cue sheet names %d audio files; an image rip has one", len(c.Files))
		return nil
	}

	file := c.Files[0]
	switch {
	case strings.ContainsAny(file, `/\`):
		r.add(id, name, StatusError,
			"the cue sheet names a path (%q), not a file beside it", file)
		return nil
	case !strings.EqualFold(filepath.Ext(file), ".flac"):
		r.add(id, name, StatusError, "%q is not a .flac file", file)
		return nil
	}

	path := filepath.Join(dir, file)
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if alt := findFile(dir, file); alt != "" {
			r.add(id, name, StatusError,
				"%q is not in the directory; there is a %q, which differs only in case",
				file, filepath.Base(alt))
		} else {
			r.add(id, name, StatusError, "%q is not in the directory", file)
		}
		return nil
	case err != nil:
		r.add(id, name, StatusError, "cannot read %q: %v", file, err)
		return nil
	case !info.Mode().IsRegular():
		r.add(id, name, StatusError, "%q is not a regular file", file)
		return nil
	case info.Size() == 0:
		r.add(id, name, StatusError, "%q is empty", file)
		return nil
	}

	si, err := readStreamInfo(path)
	switch {
	case errors.Is(err, errNotFLAC):
		r.add(id, name, StatusError, "%q: %v", file, err)
		return nil
	case err != nil:
		r.add(id, name, StatusError, "cannot read %q: %v", file, err)
		return nil
	}

	if ripped := windowsBase(l.Range.Filename); ripped != "" && stemOf(ripped) != stemOf(file) {
		r.add(id, name, StatusWarning,
			"the cue sheet names %q but the log ripped %q: are they from the same rip?", file, ripped)
		return &si
	}
	r.add(id, name, StatusPass, "%s", file)
	return &si
}

// A CD holds 44.1 kHz, 16-bit, stereo audio.
const (
	cdSampleRate = 44100
	cdChannels   = 2
	cdBits       = 16
)

// samplesPerSector is how many samples a CD sector holds in each channel:
// 44,100 a second, in 75 sectors a second.
const samplesPerSector = cdSampleRate / framesPerSecond

// rateName formats a sample rate in Hz as kilohertz: "44.1 kHz".
func rateName(rate int) string {
	return fmt.Sprintf("%g kHz", float64(rate)/1000)
}

func channelsName(n int) string {
	switch n {
	case 1:
		return "mono"
	case 2:
		return "stereo"
	default:
		return fmt.Sprintf("%d channels", n)
	}
}

// checkImageFormat confirms the image is CD audio. A rip of a CD cannot be
// anything else, so another format means something has re-processed it.
func checkImageFormat(r *Result, info streamInfo) {
	const id, name = "image-format", "Image format"

	format := fmt.Sprintf("%s, %d-bit, %s", rateName(info.SampleRate), info.BitsPerSample, channelsName(info.Channels))
	if info.SampleRate == cdSampleRate && info.Channels == cdChannels && info.BitsPerSample == cdBits {
		r.add(id, name, StatusPass, "%s", format)
		return
	}
	r.add(id, name, StatusError, "%s: a CD is %s, %d-bit, %s",
		format, rateName(cdSampleRate), cdBits, channelsName(cdChannels))
}

// expectedImageSamples is how many samples an image should hold if it begins at
// sector start of the disc and runs to the end of the last track.
func expectedImageSamples(l *Log, start int) int64 {
	last := l.TOC[len(l.TOC)-1]
	return int64(last.End+1-start) * samplesPerSector
}

// checkImageLength confirms the image holds exactly the audio the log's table
// of contents says the disc has. That catches an image that was cut short or
// padded, which still decodes and so would pass any check on its contents.
//
// The table of contents gives where the last track ends. Where the image
// begins comes from the cue sheet: its time 00:00:00 is the image's first
// sample, so track 1's INDEX 01 in the cue sheet, set against where the log
// says track 1 starts, gives the disc sector the image begins at. For a disc
// whose audio starts at sector 0 that is sector 0, and the image holds the
// whole disc. Working it out this way keeps the check right whether or not the
// audio before track 1 (see checkCuePositions) is in the image.
func checkImageLength(r *Result, l *Log, c *Cue, info streamInfo) {
	const id, name = "image-length", "Image length"

	if len(l.TOC) == 0 {
		r.add(id, name, StatusError, "cannot compare: the log has no table of contents")
		return
	}

	var track1 CueTrack
	for _, t := range c.Tracks {
		if t.Number == l.TOC[0].Number {
			track1 = t
		}
	}
	idx, ok := track1.Index(1)
	if !ok {
		r.add(id, name, StatusError,
			"cannot tell where the image starts: track %d has no INDEX 01 in the cue sheet", l.TOC[0].Number)
		return
	}
	start := l.TOC[0].Start - idx
	if start < 0 {
		r.add(id, name, StatusError,
			"the cue sheet puts track %d at %s, later than the log's %s: the image would start before the disc",
			l.TOC[0].Number, msf(idx), msf(l.TOC[0].Start))
		return
	}

	if info.TotalSamples == 0 {
		r.add(id, name, StatusWarning, "the FLAC does not record its length, so it cannot be checked")
		return
	}

	got, want := int64(info.TotalSamples), expectedImageSamples(l, start)
	if got == want {
		r.add(id, name, StatusPass, "%d samples, as the log's table of contents gives", got)
		return
	}

	diff, how := want-got, "short"
	if diff < 0 {
		diff, how = -diff, "long"
	}
	size := fmt.Sprintf("%d %s", diff, plural(diff, "sample"))
	if diff%samplesPerSector == 0 {
		size = fmt.Sprintf("%d %s", diff/samplesPerSector, plural(diff/samplesPerSector, "sector"))
	}
	r.add(id, name, StatusError,
		"the FLAC holds %d samples and the log's table of contents gives %d: %s %s",
		got, want, size, how)
}

// plural returns noun, with an "s" unless n is exactly one.
func plural(n int64, noun string) string {
	if n == 1 {
		return noun
	}
	return noun + "s"
}

// checkCueTracks confirms the cue sheet has the same tracks as the log.
func checkCueTracks(r *Result, l *Log, c *Cue) {
	const id, name = "cue-tracks", "Cue tracks"

	for _, t := range c.Tracks {
		if t.Type != "AUDIO" {
			r.add(id, name, StatusError,
				"track %d is %s, not audio: data tracks are not supported", t.Number, t.Type)
			return
		}
	}
	if len(l.TOC) == 0 {
		r.add(id, name, StatusError, "cannot compare: the log has no table of contents")
		return
	}
	if len(c.Tracks) != len(l.TOC) {
		r.add(id, name, StatusError,
			"the cue sheet has %d tracks and the log %d", len(c.Tracks), len(l.TOC))
		return
	}
	for i, t := range c.Tracks {
		if t.Number != l.TOC[i].Number {
			r.add(id, name, StatusError,
				"the cue sheet's track %d is numbered %d, where the log has %d", i+1, t.Number, l.TOC[i].Number)
			return
		}
	}
	r.add(id, name, StatusPass, "%d tracks", len(c.Tracks))
}

// maxListed is how many individual problems a check names before summarizing
// the rest.
const maxListed = 3

// checkCuePositions confirms every track starts, in the cue sheet, where the
// log's table of contents says it does.
func checkCuePositions(r *Result, l *Log, c *Cue) {
	const id, name = "cue-positions", "Track positions"

	if len(l.TOC) == 0 {
		r.add(id, name, StatusError, "cannot compare: the log has no table of contents")
		return
	}

	byNumber := make(map[int]CueTrack, len(c.Tracks))
	for _, t := range c.Tracks {
		byNumber[t.Number] = t
	}

	var bad []string
	for _, tt := range l.TOC {
		ct, ok := byNumber[tt.Number]
		if !ok {
			bad = append(bad, fmt.Sprintf("track %d is not in the cue sheet", tt.Number))
			continue
		}
		got, ok := ct.Index(1)
		switch {
		case !ok:
			bad = append(bad, fmt.Sprintf("track %d has no INDEX 01", tt.Number))
		case got != tt.Start:
			bad = append(bad, fmt.Sprintf("track %d: the cue sheet has %s, the log %s",
				tt.Number, msf(got), msf(tt.Start)))
		}
	}

	if len(bad) == 0 {
		r.add(id, name, StatusPass, "all %d tracks start where the log's table of contents puts them", len(l.TOC))
		return
	}
	if before, ok := imageStartsAtFirstTrack(l, byNumber); ok {
		r.add(id, name, StatusError,
			"the cue sheet's image starts at track 1, but the log has %s (%d frames) of audio before it: a hidden track or pregap was left out of the image",
			msf(before), before)
		return
	}
	detail := strings.Join(bad[:min(len(bad), maxListed)], "; ")
	if len(bad) > maxListed {
		detail += fmt.Sprintf("; and %d more", len(bad)-maxListed)
	}
	r.add(id, name, StatusError, "%s", detail)
}

// imageStartsAtFirstTrack reports whether every track in the cue sheet is
// earlier than in the log by exactly the length of the audio before track 1,
// which is what the cue sheet looks like when the image begins at track 1 and
// leaves that audio out. It returns that length, in frames.
//
// A disc can have audio before its first track: a hidden track in the pregap,
// or a short silence. EAC includes it in the image: the cue sheet gives track 1
// an INDEX 00 at 00:00:00 and an INDEX 01 at the log's start sector, and every
// position is measured from the first sector of the disc. A cue sheet whose
// positions are all that much smaller is what an image without it would have.
//
// NOTE: The first half of that is how EAC is known to write such a disc, from a
// real rip (testdata/transistor.cue: track 1 starts at sector 12000, with an
// INDEX 00 at 00:00:00). The second half, what a cue sheet looks like when the
// audio is left out, has not been seen from EAC and is inferred; it is
// exercised only with a cue sheet made by shifting the real one.
func imageStartsAtFirstTrack(l *Log, byNumber map[int]CueTrack) (before int, ok bool) {
	before = l.TOC[0].Start
	if before <= 0 {
		return 0, false
	}
	for _, tt := range l.TOC {
		got, has := byNumber[tt.Number].Index(1)
		if !has || got != tt.Start-before {
			return 0, false
		}
	}
	return before, true
}

// checkCueDiscID compares the freedb disc ID in the cue sheet with the one the
// log's table of contents gives. EAC computes the one in the cue sheet from the
// same table, so a difference means the cue sheet and the log are not from the
// same disc.
func checkCueDiscID(r *Result, l *Log, c *Cue) {
	const id, name = "cue-disc-id", "Disc ID"

	want := l.FreeDBID()
	switch {
	case want == "":
		r.add(id, name, StatusInfo, "no table of contents in the log to compute it from")
	case c.DiscID == "":
		r.add(id, name, StatusInfo, "the cue sheet has no REM DISCID")
	case strings.EqualFold(c.DiscID, want):
		r.add(id, name, StatusPass, "%s", strings.ToUpper(c.DiscID))
	default:
		r.add(id, name, StatusError,
			"the cue sheet says %s and the log's table of contents gives %s: they may be from different discs",
			c.DiscID, want)
	}
}

// checkCuePregaps looks at the INDEX 00 lines, which mark where a track's
// pregap begins. They are what EAC's "Detect Gaps" leaves in the cue sheet. It
// confirms those there are make sense; it cannot confirm that none are missing,
// because a disc with no pregaps and a rip made without detecting them look the
// same.
func checkCuePregaps(r *Result, c *Cue) {
	const id, name = "cue-pregaps", "Pregaps"

	var pregaps []string
	var bad []string
	for i, t := range c.Tracks {
		i0, has := t.Index(0)
		if !has {
			continue
		}

		// A pregap lies in the track before: after that track starts, and
		// before this one does.
		lo := 0
		if i > 0 {
			lo, _ = c.Tracks[i-1].Index(1)
		}
		i1, ok := t.Index(1)
		if !ok || i0 >= i1 || i0 < lo {
			bad = append(bad, fmt.Sprintf("track %d: INDEX 00 at %s is not between the previous track's start and its own",
				t.Number, msf(i0)))
			continue
		}
		pregaps = append(pregaps, fmt.Sprint(t.Number))
	}

	switch {
	case len(bad) > 0:
		detail := strings.Join(bad[:min(len(bad), maxListed)], "; ")
		if len(bad) > maxListed {
			detail += fmt.Sprintf("; and %d more", len(bad)-maxListed)
		}
		r.add(id, name, StatusError, "%s", detail)
	case len(pregaps) == 0:
		r.add(id, name, StatusInfo,
			"none recorded: this reads the same whether the disc has none or Detect Gaps was not run")
	default:
		pregapNoun, trackNoun := "pregaps", "tracks"
		if len(pregaps) == 1 {
			pregapNoun, trackNoun = "pregap", "track"
		}
		r.add(id, name, StatusPass, "%d %s recorded (%s %s)",
			len(pregaps), pregapNoun, trackNoun, strings.Join(pregaps, ", "))
	}
}
