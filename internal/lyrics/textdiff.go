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
	"regexp"
	"strings"
	"unicode"
)

// OpKind says what an edit or a diff line does to the old text to get the new.
type OpKind uint8

const (
	// OpEqual is a line present, unchanged, in both versions.
	OpEqual OpKind = iota
	// OpDelete is a line only the old version has.
	OpDelete
	// OpInsert is a line only the new version has.
	OpInsert
)

// edit is one step of the edit script that turns one sequence into another.
// A indexes the old sequence and B the new; whichever side a step doesn't
// involve (B for a delete, A for an insert) is -1.
type edit struct {
	Kind OpKind
	A, B int
}

// maxDiffCells bounds the work and memory diffSeq spends on one comparison
// (the old length times the new length). Lyrics are a few hundred lines at
// most, so this is never reached by real input; it only keeps a pathological
// one, such as a Lyricsfile with a huge number of lines, from using unbounded
// memory. Past it, the two sequences are reported as entirely replaced.
const maxDiffCells = 4_000_000

// diffSeq returns the shortest edit script from a to b (a longest common
// subsequence diff). Within a run of changes the deletes come before the
// inserts, as in a unified diff.
func diffSeq(a, b []string) []edit {
	var out []edit

	// Most of a lyrics edit is usually untouched, so peel off the common
	// start and end and only run the quadratic part on what's left.
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		out = append(out, edit{OpEqual, pre, pre})
		pre++
	}
	endA, endB := len(a), len(b)
	for endA > pre && endB > pre && a[endA-1] == b[endB-1] {
		endA--
		endB--
	}

	out = append(out, diffMiddle(a[pre:endA], b[pre:endB], pre, pre)...)

	for i := 0; endA+i < len(a); i++ {
		out = append(out, edit{OpEqual, endA + i, endB + i})
	}
	return out
}

// diffMiddle is diffSeq's longest-common-subsequence core, for sequences with
// no common start or end. offA and offB are added to the indexes in the edits
// it returns.
func diffMiddle(a, b []string, offA, offB int) []edit {
	n, m := len(a), len(b)

	var out []edit
	if n == 0 || m == 0 || n*m > maxDiffCells {
		for i := range a {
			out = append(out, edit{OpDelete, offA + i, -1})
		}
		for j := range b {
			out = append(out, edit{OpInsert, -1, offB + j})
		}
		return out
	}

	// lcs[i][j] is the length of the longest common subsequence of a[i:]
	// and b[j:].
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}

	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			out = append(out, edit{OpEqual, offA + i, offB + j})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, edit{OpDelete, offA + i, -1})
			i++
		default:
			out = append(out, edit{OpInsert, -1, offB + j})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, edit{OpDelete, offA + i, -1})
	}
	for ; j < m; j++ {
		out = append(out, edit{OpInsert, -1, offB + j})
	}
	return out
}

// Segment is a run of a changed line's text. In a line that has been edited in
// place, the Changed segments are the words that differ from the other version,
// for the renderer to highlight.
type Segment struct {
	Text    string
	Changed bool
}

// DiffLine is one line of a Hunk.
type DiffLine struct {
	Kind OpKind
	Text string

	// Stamp is the line's timestamp, as it appears in LRC ("[01:23.45]"), for
	// the lines of a synced-lyrics diff; empty otherwise. It isn't part of
	// what the diff compares.
	Stamp string

	// Segments, when not nil, split Text into runs of unchanged and changed
	// words. It is only set on a deleted line and the inserted line that
	// replaced it, when the two are similar enough for highlighting to help.
	Segments []Segment

	// OldNum and NewNum are the line's 1-based numbers in the old and new
	// versions, or 0 when it isn't in that version.
	OldNum, NewNum int
}

// Hunk is a run of changed lines with the unchanged lines around them, like a
// unified diff's.
type Hunk struct {
	OldStart, OldLines int
	NewStart, NewLines int
	Lines              []DiffLine
}

// TextDiff is a line diff of two texts.
type TextDiff struct {
	// Same is true when the two texts have no differing lines; Hunks is
	// empty then.
	Same           bool
	Added, Removed int
	Hunks          []Hunk
}

// FullContext is the context value that makes a diff keep every line, so
// that it is a single hunk of the whole text, not just the changes.
const FullContext = -1

// lineSource is a sequence of lines to diff, with an optional timestamp shown
// beside each.
type lineSource struct {
	Text   []string
	Stamps []string // nil, or one per line
}

func (s lineSource) stamp(i int) string {
	if s.Stamps == nil {
		return ""
	}
	return s.Stamps[i]
}

// diffLines diffs two line sequences. context is how many unchanged lines to
// keep around each change, or FullContext to keep them all.
func diffLines(oldSrc, newSrc lineSource, context int) TextDiff {
	edits := diffSeq(oldSrc.Text, newSrc.Text)

	var d TextDiff
	for _, e := range edits {
		switch e.Kind {
		case OpDelete:
			d.Removed++
		case OpInsert:
			d.Added++
		}
	}
	d.Same = d.Added == 0 && d.Removed == 0
	if d.Same {
		return d
	}

	lines := make([]DiffLine, len(edits))
	for k, e := range edits {
		switch e.Kind {
		case OpEqual:
			lines[k] = DiffLine{Kind: OpEqual, Text: newSrc.Text[e.B], Stamp: newSrc.stamp(e.B), OldNum: e.A + 1, NewNum: e.B + 1}
		case OpDelete:
			lines[k] = DiffLine{Kind: OpDelete, Text: oldSrc.Text[e.A], Stamp: oldSrc.stamp(e.A), OldNum: e.A + 1}
		case OpInsert:
			lines[k] = DiffLine{Kind: OpInsert, Text: newSrc.Text[e.B], Stamp: newSrc.stamp(e.B), NewNum: e.B + 1}
		}
	}
	highlightEdits(lines)

	// Choose the lines to keep: every change, and context lines either side.
	keep := make([]bool, len(lines))
	for k, ln := range lines {
		if ln.Kind == OpEqual {
			if context == FullContext {
				keep[k] = true
			}
			continue
		}
		keep[k] = true
		for c := 1; c <= context; c++ {
			if k-c >= 0 {
				keep[k-c] = true
			}
			if k+c < len(lines) {
				keep[k+c] = true
			}
		}
	}

	var cur *Hunk
	flush := func() {
		if cur != nil {
			d.Hunks = append(d.Hunks, *cur)
			cur = nil
		}
	}
	for k, ln := range lines {
		if !keep[k] {
			flush()
			continue
		}
		if cur == nil {
			cur = &Hunk{}
		}
		cur.Lines = append(cur.Lines, ln)
	}
	flush()

	for h := range d.Hunks {
		fillHunkHeader(&d.Hunks[h])
	}
	return d
}

// fillHunkHeader sets a hunk's start and length fields from its lines.
func fillHunkHeader(h *Hunk) {
	for _, ln := range h.Lines {
		if ln.OldNum > 0 {
			if h.OldLines == 0 {
				h.OldStart = ln.OldNum
			}
			h.OldLines++
		}
		if ln.NewNum > 0 {
			if h.NewLines == 0 {
				h.NewStart = ln.NewNum
			}
			h.NewLines++
		}
	}
}

// highlightEdits finds the lines that were edited in place, pairing each
// deleted line with the inserted line that replaced it, and gives both a
// Segments split showing which words changed. Within a run of changes the
// k-th deleted line is paired with the k-th inserted line, which is right
// for the common case of a few lines each reworded, and a pair is only
// highlighted when the two are alike enough for that to be meaningful.
func highlightEdits(lines []DiffLine) {
	for start := 0; start < len(lines); {
		if lines[start].Kind == OpEqual {
			start++
			continue
		}
		end := start
		for end < len(lines) && lines[end].Kind != OpEqual {
			end++
		}

		var dels, ins []int
		for k := start; k < end; k++ {
			if lines[k].Kind == OpDelete {
				dels = append(dels, k)
			} else {
				ins = append(ins, k)
			}
		}
		for p := 0; p < len(dels) && p < len(ins); p++ {
			oldSegs, newSegs, ok := wordSegments(lines[dels[p]].Text, lines[ins[p]].Text)
			if ok {
				lines[dels[p]].Segments = oldSegs
				lines[ins[p]].Segments = newSegs
			}
		}
		start = end
	}
}

// tokenRe splits a line into words, runs of whitespace, and single
// punctuation marks, so that a diff of the tokens can say which words of a
// line changed. An apostrophe is part of a word ("don't").
var tokenRe = regexp.MustCompile(`[\p{L}\p{N}']+|\s+|[^\p{L}\p{N}'\s]`)

// wordSegments diffs two lines word by word and returns each line split into
// unchanged and changed runs. ok is false when the lines share fewer than half
// their words, where highlighting would just mark everything.
func wordSegments(oldLine, newLine string) (oldSegs, newSegs []Segment, ok bool) {
	a := tokenRe.FindAllString(oldLine, -1)
	b := tokenRe.FindAllString(newLine, -1)

	edits := diffSeq(a, b)

	// A token counts as a word if it has a letter or digit in it, which
	// leaves out whitespace and punctuation.
	isWord := func(tok string) bool {
		return strings.ContainsFunc(tok, func(r rune) bool {
			return unicode.IsLetter(r) || unicode.IsDigit(r)
		})
	}
	var wordsA, wordsB, wordsEqual int
	for _, tok := range a {
		if isWord(tok) {
			wordsA++
		}
	}
	for _, tok := range b {
		if isWord(tok) {
			wordsB++
		}
	}
	for _, e := range edits {
		if e.Kind == OpEqual && isWord(a[e.A]) {
			wordsEqual++
		}
	}
	// Equal words must make up at least half of all the words of both lines.
	if wordsA+wordsB == 0 || 4*wordsEqual < wordsA+wordsB {
		return nil, nil, false
	}

	var oldToks, newToks []segToken
	for _, e := range edits {
		switch e.Kind {
		case OpEqual:
			oldToks = append(oldToks, segToken{a[e.A], false})
			newToks = append(newToks, segToken{b[e.B], false})
		case OpDelete:
			oldToks = append(oldToks, segToken{a[e.A], true})
		case OpInsert:
			newToks = append(newToks, segToken{b[e.B], true})
		}
	}
	return joinSegments(oldToks), joinSegments(newToks), true
}

// segToken is one token of a line with whether it changed.
type segToken struct {
	text    string
	changed bool
}

// joinSegments turns tokens into Segments. Whitespace between two changed
// tokens counts as changed too, so that a changed phrase is highlighted as one
// run and not word by word with gaps between.
func joinSegments(toks []segToken) []Segment {
	for i := 1; i < len(toks)-1; i++ {
		if !toks[i].changed && strings.TrimSpace(toks[i].text) == "" && toks[i-1].changed && toks[i+1].changed {
			toks[i].changed = true
		}
	}

	var sb segmentBuilder
	for _, t := range toks {
		sb.add(t.text, t.changed)
	}
	return sb.segs
}

// segmentBuilder accumulates Segments, merging neighbors with the same
// Changed value.
type segmentBuilder struct{ segs []Segment }

func (sb *segmentBuilder) add(text string, changed bool) {
	if n := len(sb.segs); n > 0 && sb.segs[n-1].Changed == changed {
		sb.segs[n-1].Text += text
		return
	}
	sb.segs = append(sb.segs, Segment{Text: text, Changed: changed})
}

// plainLines normalizes plain lyrics into lines for comparison: line endings
// and trailing whitespace don't count, nor do blank lines at either end.
// Blank lines inside the text are kept, since they separate stanzas.
func plainLines(s string) []string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimRight(ln, " \t\r")
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// diffPlain diffs two plain-lyrics texts, as normalized by plainLines.
func diffPlain(oldText, newText string, context int) TextDiff {
	return diffLines(lineSource{Text: plainLines(oldText)}, lineSource{Text: plainLines(newText)}, context)
}
