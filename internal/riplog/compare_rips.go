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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mfinelli/musicrename/internal/pcm"
)

// ripFacts is what a rip's log says of how its drive read the disc, which says what
// its image cannot be relied on for.
type ripFacts struct {
	side     string // "A" or "B"
	drive    string
	offset   int
	overread bool
}

// readFacts reads from the log how the rip was made.
func readFacts(side string, l *Log) (ripFacts, error) {
	f := ripFacts{side: side, drive: normalizeDrive(l.Drive)}

	raw, ok := l.Setting("Read offset correction")
	if !ok {
		return f, fmt.Errorf("%s: the log does not state its read offset", side)
	}
	offset, err := parseOffset(raw)
	if err != nil {
		return f, fmt.Errorf("%s: %q is not a read offset", side, raw)
	}
	f.offset = offset

	raw, ok = l.Setting(overreadLabel)
	if !ok {
		return f, fmt.Errorf("%s: the log does not state whether it read past the ends of the disc", side)
	}
	if f.overread, ok = parseYesNo(raw); !ok {
		return f, fmt.Errorf("%s: %q is neither Yes nor No", side, raw)
	}
	return f, nil
}

// unreadable is how many samples at the start and at the end of its image the
// drive could not read, which Exact Audio Copy fills in with silence. A drive
// delivers audio late or early by its offset: with a positive offset the last
// samples are past the end of what it reads, and with a negative one the first
// are before the start, unless it can read past the ends of the disc.
//
// The log's word that overread was on is all that is known of whether it did, and
// is a request and not proof, so what differs is checked against this and not
// taken to follow from it.
func (f ripFacts) unreadable() (start, end int64) {
	switch {
	case f.overread:
		return 0, 0
	case f.offset > 0:
		return 0, int64(f.offset)
	default:
		return int64(-f.offset), 0
	}
}

// cannotRead says what the drive could not read, in words.
func (f ripFacts) cannotRead() string {
	start, end := f.unreadable()
	switch {
	case start > 0:
		return fmt.Sprintf("its first %d samples", start)
	case end > 0:
		return fmt.Sprintf("its last %d samples", end)
	}
	return "nothing"
}

// settings describes the offset and overread, for saying why.
func (f ripFacts) settings() string {
	over := "overread off"
	if f.overread {
		over = "overread on"
	}
	return fmt.Sprintf("offset %+d, %s", f.offset, over)
}

// describe says what a rip was made with, and what that leaves unreadable.
func (f ripFacts) describe() string {
	drive := f.drive
	if drive == "" {
		drive = "a drive that is not named"
	}
	text := fmt.Sprintf("%s, %s", drive, f.settings())
	if what := f.cannotRead(); what != "nothing" {
		return text + ": could not read " + what
	}
	return text + ": could read all of it"
}

// CompareRips compares the audio of the images of two rips of a disc, from their
// logs, each with its cue sheet beside it and the image it names. Besides
// whether the audio is the same, it uses what the logs say about the drives to
// say whether a difference is what the drives account for: a drive that cannot
// read the end of a disc leaves silence, so two rips of one disc can differ
// in the first or last few samples and both be right.
//
// What it concludes is checked and not assumed. A difference is explained only if
// it is all within what a drive could not read, and each of its samples is silence
// in one of the two; one that is not is reported as it is. A difference that is
// the audio moved along is not a drive's: if both offsets are right the images
// line up, so one is wrong. A log that cannot be read is an error; every other
// finding is in the result.
func CompareRips(ctx context.Context, logA, logB string, dec Comparer) (*Result, error) {
	a, err := readLog(logA)
	if err != nil {
		return nil, fmt.Errorf("log A (%s): %w", filepath.Base(logA), err)
	}
	b, err := readLog(logB)
	if err != nil {
		return nil, fmt.Errorf("log B (%s): %w", filepath.Base(logB), err)
	}

	r := &Result{}
	// The logs are what the drives' settings are taken from, so they should be as
	// EAC wrote them.
	addChecksum(r, "A", a)
	addChecksum(r, "B", b)

	if !checkSameDisc(r, a, b) {
		return r, nil
	}

	facts := [2]ripFacts{}
	for i, f := range []struct {
		side string
		l    *Log
	}{{"A", a}, {"B", b}} {
		facts[i], err = readFacts(f.side, f.l)
		if err != nil {
			r.add("compare-rips", "Rips", StatusError, "%v, so what its drive could not read is not known", err)
			return r, nil
		}
		r.add("compare-rip-"+strings.ToLower(f.side), "Rip "+f.side, StatusInfo, "%s", facts[i].describe())
	}

	imgA, imgB, ok := checkRipImages(r, logA, logB)
	if !ok || !checkCompareFormat(r, imgA, imgB) {
		return r, nil
	}
	if dec == nil {
		r.add("compare-audio", "Audio", StatusError, "there is nothing to decode with, so the audio was not compared")
		return r, nil
	}

	d, err := dec.Diff(ctx, []string{imgA}, []string{imgB})
	if msg := compareError(err, imgB); msg != "" {
		r.add("compare-audio", "Audio", StatusError, "%s", msg)
		return r, nil
	}

	status, detail := interpretRips(ctx, dec, imgA, imgB, d, a, b, facts[0], facts[1])
	r.add("compare-audio", "Audio", status, "%s", detail)
	return r, nil
}

// addChecksum adds the check of a log's checksum, for the rip on one side.
func addChecksum(r *Result, side string, l *Log) {
	tmp := &Result{}
	checkChecksum(tmp, l)
	for _, c := range tmp.Checks {
		r.add(c.ID+"-"+strings.ToLower(side), c.Name+" ("+side+")", c.Status, "%s", c.Detail)
	}
}

// checkSameDisc confirms both logs are of the same disc, by their tables of
// contents: two pressings of an album, which differ in them, have audio that does
// not line up, and nothing that follows would mean anything.
func checkSameDisc(r *Result, a, b *Log) bool {
	const id, name = "compare-disc", "Disc"

	if len(a.TOC) == 0 || len(b.TOC) == 0 {
		r.add(id, name, StatusError, "a log has no table of contents, so it cannot be told whether they are the same disc")
		return false
	}
	if len(a.TOC) != len(b.TOC) {
		r.add(id, name, StatusError, "not the same disc: A has %d tracks (disc ID %s) and B has %d (disc ID %s)",
			len(a.TOC), a.DiscID(), len(b.TOC), b.DiscID())
		return false
	}
	for i := range a.TOC {
		if a.TOC[i] != b.TOC[i] {
			r.add(id, name, StatusError, "not the same disc: track %d is at sectors %d to %d in A (disc ID %s) and %d to %d in B (disc ID %s)",
				a.TOC[i].Number, a.TOC[i].Start, a.TOC[i].End, a.DiscID(), b.TOC[i].Start, b.TOC[i].End, b.DiscID())
			return false
		}
	}
	r.add(id, name, StatusPass, "the same disc: %d tracks, disc ID %s", len(a.TOC), a.DiscID())
	return true
}

// ripImage finds the image of the rip with the log at logPath: the one FLAC file
// its cue sheet names, beside it. If there is none it says why.
func ripImage(logPath string) (image, problem string) {
	dir := filepath.Dir(logPath)
	want := strings.TrimSuffix(filepath.Base(logPath), filepath.Ext(logPath)) + ".cue"

	cuePath := findFile(dir, want)
	if cuePath == "" {
		return "", fmt.Sprintf("no %q beside the log", want)
	}
	data, err := os.ReadFile(cuePath)
	if err != nil {
		return "", fmt.Sprintf("cannot read %q: %v", filepath.Base(cuePath), err)
	}
	c, err := ParseCue(data)
	if err != nil {
		return "", fmt.Sprintf("%s: %v", filepath.Base(cuePath), err)
	}
	if image = splitImage(dir, c); image == "" {
		return "", "the cue sheet does not name one FLAC image that is beside the log"
	}
	return image, ""
}

// checkRipImages finds the image of each rip.
func checkRipImages(r *Result, logA, logB string) (imgA, imgB string, ok bool) {
	const id, name = "compare-images", "Images"

	imgA, probA := ripImage(logA)
	imgB, probB := ripImage(logB)

	var problems []string
	if probA != "" {
		problems = append(problems, "A: "+probA)
	}
	if probB != "" {
		problems = append(problems, "B: "+probB)
	}
	if len(problems) > 0 {
		r.add(id, name, StatusError, "%s", strings.Join(problems, "; "))
		return "", "", false
	}
	r.add(id, name, StatusPass, "A: %s, B: %s", filepath.Base(imgA), filepath.Base(imgB))
	return imgA, imgB, true
}

// interpretRips says what the difference between the images of two rips is, and
// how bad. It is a pass for the same audio, a note for differences that the two
// drives' inability to read the ends of the disc accounts for, a warning for ones
// that are in what they could not read but are not what EAC fills in there, and an
// error for the rest.
func interpretRips(ctx context.Context, dec Comparer, imgA, imgB string, d pcm.Difference,
	la, lb *Log, fa, fb ripFacts) (Status, string) {
	if d.Identical() {
		return StatusPass, fmt.Sprintf("the same audio: %d samples", d.SamplesA)
	}

	// The two lined up would be the same audio, and an offset that is right cannot
	// move it.
	if d.Shift != nil {
		return StatusError, describeShift(ctx, dec, imgA, imgB, d) + "; " + shiftReason(la, lb, fa, fb)
	}

	startA, endA := fa.unreadable()
	startB, endB := fb.unreadable()
	start, end := max(startA, startB), max(endA, endB)
	common := min(d.SamplesA, d.SamplesB)

	if !withinEnds(d, start, end) {
		text := describeDifference(d)
		text += fmt.Sprintf("; that is not what the drives account for: A could not read %s, and B %s", fa.cannotRead(), fb.cannotRead())
		if hint := overreadHint(d, start, end, fa, fb); hint != "" {
			text += "; " + hint
		}
		return StatusError, text
	}

	where := "the start"
	atStart, atEnd := d.First < start, d.Last >= common-end
	switch {
	case atStart && atEnd:
		where = "the start and the end"
	case atEnd:
		where = "the end"
	}
	text := fmt.Sprintf("the same audio except for %d %s at %s, within what the drives could not read (A %s, B %s)",
		d.Differing, plural(d.Differing, "sample"), where, fa.cannotRead(), fb.cannotRead())

	if d.Silent == d.Differing {
		return StatusInfo, text + "; in every one of them one rip has silence, which is what EAC fills in for samples a drive could not read"
	}
	return StatusWarning, text + fmt.Sprintf("; but in %d of them both rips have sound, so they are not only what EAC fills in",
		d.Differing-d.Silent)
}

// withinEnds reports whether every difference is within the first start samples
// or the last end of what both have, and the images are the same length, since
// EAC fills in what a drive could not read and so keeps it.
func withinEnds(d pcm.Difference, start, end int64) bool {
	if d.Differing == 0 || d.RunsTruncated || d.SamplesA != d.SamplesB {
		return false
	}
	for _, run := range d.Runs {
		if run.End > start && run.Start < d.SamplesA-end {
			return false
		}
	}
	return true
}

// overreadHint says, for a difference that no drive accounts for and that reaches
// an end of the images, that a log that says overread was on does not show that
// the drive read past it. A difference that is accounted for, or is not at an end,
// has nothing to do with it.
func overreadHint(d pcm.Difference, start, end int64, fa, fb ripFacts) string {
	common := min(d.SamplesA, d.SamplesB)
	atAnEnd := false
	for _, run := range d.Runs {
		unaccounted := run.End > start && run.Start < common-end
		if unaccounted && (run.Start == 0 || run.End == common) {
			atAnEnd = true
		}
	}
	if !atAnEnd {
		return ""
	}
	var sides []string
	for _, f := range []ripFacts{fa, fb} {
		if f.overread {
			sides = append(sides, f.side)
		}
	}
	if len(sides) == 0 {
		return ""
	}
	return fmt.Sprintf("the log of %s says overread was on, which does not show that its drive read past the ends of the disc",
		strings.Join(sides, " and "))
}

// shiftReason says what it means that two rips of a disc are one the other moved
// along: with both offsets right they line up, so one is not its drive's, and by
// as much as the shift. AccurateRip's word on each, which the logs have, says which.
func shiftReason(la, lb *Log, fa, fb ripFacts) string {
	text := fmt.Sprintf("one of the offsets (A's is %+d, B's is %+d) is not its drive's, by as much as that", fa.offset, fb.offset)
	switch confirmedA, confirmedB := arConfirmsAll(la), arConfirmsAll(lb); {
	case confirmedA && !confirmedB:
		return text + "; AccurateRip confirmed every track of A and not of B, so B's is the one to doubt"
	case confirmedB && !confirmedA:
		return text + "; AccurateRip confirmed every track of B and not of A, so A's is the one to doubt"
	case confirmedA && confirmedB:
		return text + "; AccurateRip confirmed every track of both, which the images being moved along does not agree with"
	}
	return text + "; AccurateRip confirmed neither"
}
