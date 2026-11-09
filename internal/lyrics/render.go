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

package lyrics

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

// Palette is how the renderer styles what it writes. The functions take text
// and return it styled (with terminal colors, say); the command layer builds
// one from internal/style, and tests use ones that only mark the text, so that
// what is written can be checked without any terminal handling here.
type Palette struct {
	// Add and Del style added and removed lines, and AddWord and DelWord the
	// words that changed within those lines.
	Add, Del         func(string) string
	AddWord, DelWord func(string) string

	// OK, Warn, Err, Name, Faint and Bold style status markers and labels.
	OK, Warn, Err, Name, Faint, Bold func(string) string
}

// PlainPalette returns a Palette that leaves all text unstyled.
func PlainPalette() Palette {
	id := func(s string) string { return s }
	return Palette{
		Add: id, Del: id, AddWord: id, DelWord: id,
		OK: id, Warn: id, Err: id, Name: id, Faint: id, Bold: id,
	}
}

// RenderOptions controls how much RenderDetails writes.
type RenderOptions struct {
	// ListRetimed also lists each line whose time changed, with the time
	// before and after, instead of only summarizing how many there are.
	ListRetimed bool
}

// detailLabelWidth is the column at which the values of RenderDetails begin,
// wide enough for "Word-synced:" and a space.
const detailLabelWidth = 14

// StatusMarker returns the one-character marker for d's status, styled by pal.
func StatusMarker(d TrackDiff, pal Palette) string {
	switch d.Status {
	case DiffSame:
		return pal.OK("✓")
	case DiffChanged:
		return pal.Warn("~")
	case DiffWordSync, DiffNew:
		return pal.Name("+")
	case DiffNotFound:
		return pal.Faint("✗")
	default:
		return pal.Err("!")
	}
}

// Headline is a short phrase saying what a diff found, for the line that
// stands for a track in a listing.
func Headline(d TrackDiff) string {
	switch d.Status {
	case DiffSame:
		if d.Synced != nil && d.Synced.FormatOnly {
			return "identical (the LRC is formatted differently)"
		}
		return "identical"
	case DiffNew:
		if d.Word != nil && d.Word.State == WordAvailable {
			return "no lyrics yet; LRCLIB has some, with word sync"
		}
		return "no lyrics yet; LRCLIB has some"
	case DiffWordSync:
		return "word-sync available"
	case DiffNotFound, DiffFailed:
		return d.Reason
	}

	var parts []string
	if d.Plain != nil && !d.Plain.Same {
		parts = append(parts, "plain lyrics ("+changeCounts(d.Plain.Added, d.Plain.Removed)+")")
	}
	if d.Synced != nil && !d.Synced.Same() {
		parts = append(parts, "synced lyrics ("+syncedSummary(*d.Synced)+")")
	}
	if d.Word != nil {
		switch d.Word.State {
		case WordAvailable:
			parts = append(parts, "word-sync available")
		case WordRemoved:
			parts = append(parts, "word-sync would be removed")
		case WordChanged:
			parts = append(parts, "word-sync changed")
		}
	}
	return strings.Join(parts, "; ")
}

// plural formats a count with its noun, "1 line" or "2 lines".
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// changed reports whether a diff has any added or removed lines. It is not the
// same as !Same, which is also false for a TextDiff that was never computed.
func (d TextDiff) changed() bool { return d.Added+d.Removed > 0 }

// changeCounts formats a diff's size, "+2 −1 lines".
func changeCounts(added, removed int) string {
	return fmt.Sprintf("+%d −%d lines", added, removed)
}

// syncedSummary says what differs in synced lyrics: changed text, retimed
// lines, or both.
func syncedSummary(d SyncedDiff) string {
	var parts []string
	if !d.Text.Same {
		parts = append(parts, changeCounts(d.Text.Added, d.Text.Removed))
	}
	if d.Timing.Retimed > 0 {
		parts = append(parts, timingSummary(d.Timing, "line"))
	}
	return strings.Join(parts, ", ")
}

// timingSummary says how many of the compared items were retimed and by how
// much: "all by +300 ms" if they moved together, or the median and largest
// change if not. unit names what is counted ("line" or "word").
func timingSummary(t Timing, unit string) string {
	if t.Uniform {
		return fmt.Sprintf("%s retimed, all by %s", plural(t.Retimed, unit), fmtShift(t.Shift))
	}
	return fmt.Sprintf("%d of %s retimed, median %s, up to %s",
		t.Retimed, plural(t.Compared, unit), fmtShift(t.Median), fmtShift(t.MaxAbs))
}

// fmtShift formats a change in time with its sign: milliseconds under a
// second, seconds beyond.
func fmtShift(d time.Duration) string {
	if d > -time.Second && d < time.Second {
		return fmt.Sprintf("%+d ms", d.Milliseconds())
	}
	return fmt.Sprintf("%+.2f s", d.Seconds())
}

// RenderDetails writes what a diff found, part by part, with the diffs of the
// lyrics themselves, each line starting with indent. A part that wasn't
// compared (see TrackDiff) isn't written, so a track that wasn't found or
// couldn't be compared has no details; see Headline for why.
func RenderDetails(w io.Writer, d TrackDiff, pal Palette, indent string, opts RenderOptions) {
	row := func(label, text string) {
		pad := strings.Repeat(" ", max(detailLabelWidth-len(label)-1, 1))
		fmt.Fprintf(w, "%s%s%s%s\n", indent, pal.Bold(label+":"), pad, text)
	}
	cont := indent + strings.Repeat(" ", detailLabelWidth)

	// Nothing was compared for these, and the reason is the headline already.
	if d.Status == DiffNotFound || d.Status == DiffFailed {
		return
	}

	if strings.ToLower(filepath.Ext(d.Path)) == ".flac" && d.CandidateURL != "" {
		switch d.Record {
		case RecordSame:
			row("Record", "the one the file came from, "+d.CandidateURL)
		case RecordDifferent:
			row("Record", "the file came from "+d.EmbeddedURL+"; LRCLIB now matches "+d.CandidateURL)
		default:
			row("Record", "not recorded in the file; LRCLIB matches "+d.CandidateURL)
		}
	}

	if d.Plain != nil {
		switch {
		case !d.HadPlain:
			row("Plain", "none in the file; LRCLIB has "+plural(d.Plain.Added, "line"))
		case d.Plain.Same:
			row("Plain", "identical")
		default:
			row("Plain", "changed: "+changeCounts(d.Plain.Added, d.Plain.Removed))
			renderHunks(w, d.Plain.Hunks, pal, cont)
		}
	}

	if d.Synced != nil {
		switch {
		case !d.HadSynced:
			row("Synced", "none in the file; LRCLIB has "+plural(d.Synced.Text.Added, "line"))
		case d.Synced.Same() && d.Synced.FormatOnly:
			row("Synced", "identical (the file's LRC is formatted differently)")
		case d.Synced.Same():
			row("Synced", "identical")
		default:
			row("Synced", "changed: "+syncedSummary(*d.Synced))
			renderHunks(w, d.Synced.Text.Hunks, pal, cont)
			if opts.ListRetimed {
				renderRetimed(w, d.Synced.Retimed, pal, cont)
			}
		}
	}

	if d.Word != nil {
		renderWord(w, *d.Word, pal, row, cont, opts)
	}
}

// renderWord writes the Word-synced part of RenderDetails.
func renderWord(w io.Writer, wd WordDiff, pal Palette, row func(label, text string), cont string, opts RenderOptions) {
	switch wd.State {
	case WordNone:
		return
	case WordAvailable:
		row("Word-synced", "available from LRCLIB; the file has none")
		return
	case WordRemoved:
		row("Word-synced", "the file has one; LRCLIB's record is not word-synced, so it would be removed")
		return
	case WordIdentical:
		row("Word-synced", "identical")
		return
	case WordFormatOnly:
		row("Word-synced", "identical (the documents are written differently)")
		return
	}

	row("Word-synced", "changed")
	note := func(label, text string) {
		fmt.Fprintf(w, "%s%s %s\n", cont, pal.Faint(label+":"), text)
	}

	if wd.ParseErr != nil {
		note("error", wd.ParseErr.Error())
	}
	if wd.VersionOld != wd.VersionNew {
		note("version", fmt.Sprintf("%q → %q", wd.VersionOld, wd.VersionNew))
	}
	for _, m := range wd.Metadata {
		switch {
		case !m.HasOld:
			note("metadata", fmt.Sprintf("%s added: %s", m.Key, m.New))
		case !m.HasNew:
			note("metadata", fmt.Sprintf("%s removed (was %s)", m.Key, m.Old))
		default:
			note("metadata", fmt.Sprintf("%s: %s → %s", m.Key, m.Old, m.New))
		}
	}
	if wd.Lines.changed() {
		note("lines", changeCounts(wd.Lines.Added, wd.Lines.Removed))
		renderHunks(w, wd.Lines.Hunks, pal, cont+"  ")
	}
	if wd.LineTiming.Retimed > 0 {
		note("line timing", timingSummary(wd.LineTiming, "line"))
		if opts.ListRetimed {
			renderRetimed(w, wd.LineRetimed, pal, cont+"  ")
		}
	}
	if wd.LineEndChanged > 0 {
		if wd.LineEndChanged == 1 {
			note("line ends", "1 line ends at a different time")
		} else {
			note("line ends", fmt.Sprintf("%d lines end at a different time", wd.LineEndChanged))
		}
	}
	if wd.Words.Retimed > 0 {
		note("word timing", timingSummary(Timing{
			Compared: wd.Words.Compared, Retimed: wd.Words.Retimed,
			Median: wd.Words.Median, MaxAbs: wd.Words.MaxAbs,
		}, "word"))
	}
	if wd.SegmentedDiffer > 0 {
		if wd.SegmentedDiffer == 1 {
			note("segmentation", "1 line is split into words differently")
		} else {
			note("segmentation", fmt.Sprintf("%d lines are split into words differently", wd.SegmentedDiffer))
		}
	}
	if wd.WordsAdded > 0 {
		if wd.WordsAdded == 1 {
			note("words", "1 line gains word timings")
		} else {
			note("words", fmt.Sprintf("%d lines gain word timings", wd.WordsAdded))
		}
	}
	if wd.WordsRemoved > 0 {
		if wd.WordsRemoved == 1 {
			note("words", "1 line loses its word timings")
		} else {
			note("words", fmt.Sprintf("%d lines lose their word timings", wd.WordsRemoved))
		}
	}
	if wd.Plain.changed() {
		note("plain", changeCounts(wd.Plain.Added, wd.Plain.Removed))
		renderHunks(w, wd.Plain.Hunks, pal, cont+"  ")
	}
	if wd.Uninterpreted {
		note("other", "the documents differ in fields this command doesn't read; --raw shows them")
	}
	if wd.Raw.changed() {
		note("raw", changeCounts(wd.Raw.Added, wd.Raw.Removed))
		renderHunks(w, wd.Raw.Hunks, pal, cont+"  ")
	}
}

// renderHunks writes the hunks of a diff, each line starting with prefix.
func renderHunks(w io.Writer, hunks []Hunk, pal Palette, prefix string) {
	for _, h := range hunks {
		fmt.Fprintf(w, "%s%s\n", prefix, pal.Faint(fmt.Sprintf("@@ -%d,%d +%d,%d @@", h.OldStart, h.OldLines, h.NewStart, h.NewLines)))
		for _, ln := range h.Lines {
			fmt.Fprintf(w, "%s%s\n", prefix, renderDiffLine(ln, pal))
		}
	}
}

// renderDiffLine styles one line of a hunk: a two-character marker, the
// timestamp if there is one, and the text, with changed words highlighted when
// the line has them.
func renderDiffLine(ln DiffLine, pal Palette) string {
	stamp := ""
	if ln.Stamp != "" {
		stamp = pal.Faint(ln.Stamp) + " "
	}

	switch ln.Kind {
	case OpDelete:
		return pal.Del("- ") + stamp + styleSegments(ln, pal.Del, pal.DelWord)
	case OpInsert:
		return pal.Add("+ ") + stamp + styleSegments(ln, pal.Add, pal.AddWord)
	default:
		return "  " + stamp + ln.Text
	}
}

// styleSegments styles a changed line: its changed words with word, and the
// rest with line. A line with no Segments is styled whole with line.
func styleSegments(ln DiffLine, line, word func(string) string) string {
	if ln.Segments == nil {
		return line(ln.Text)
	}
	var sb strings.Builder
	for _, seg := range ln.Segments {
		if seg.Changed {
			sb.WriteString(word(seg.Text))
		} else {
			sb.WriteString(line(seg.Text))
		}
	}
	return sb.String()
}

// renderRetimed lists lines that are at a different time, with the time before
// and after.
func renderRetimed(w io.Writer, retimed []Retime, pal Palette, prefix string) {
	for _, r := range retimed {
		fmt.Fprintf(w, "%s%s %s\n", prefix,
			pal.Faint(formatStamp(r.Old)+" → "+formatStamp(r.New)+" ("+fmtShift(r.New-r.Old)+")"), r.Text)
	}
}
