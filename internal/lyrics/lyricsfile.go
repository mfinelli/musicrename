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
	"reflect"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// lfWord is one word or segment of a Lyricsfile line. Only the fields the
// lyrics diff interprets are read; the format is a draft, and anything else in
// a document is left alone.
type lfWord struct {
	Text    string `yaml:"text"`
	StartMS *int64 `yaml:"start_ms"`
	EndMS   *int64 `yaml:"end_ms"`
}

// lfLine is one synchronized line of a Lyricsfile.
type lfLine struct {
	Text    string   `yaml:"text"`
	StartMS *int64   `yaml:"start_ms"`
	EndMS   *int64   `yaml:"end_ms"`
	Words   []lfWord `yaml:"words"`
}

// lfDoc is a Lyricsfile document as far as the lyrics diff reads it.
type lfDoc struct {
	Version  string         `yaml:"version"`
	Metadata map[string]any `yaml:"metadata"`
	Lines    []lfLine       `yaml:"lines"`
	Plain    string         `yaml:"plain"`
}

// parseLyricsfile reads doc twice: into an lfDoc for the fields the diff
// interprets, and into a generic structure for telling whether two documents
// mean the same thing whatever their formatting.
func parseLyricsfile(doc string) (lfDoc, map[string]any, error) {
	var typed lfDoc
	if err := yaml.Unmarshal([]byte(doc), &typed); err != nil {
		return lfDoc{}, nil, err
	}
	var generic map[string]any
	if err := yaml.Unmarshal([]byte(doc), &generic); err != nil {
		return lfDoc{}, nil, err
	}
	return typed, generic, nil
}

// WordState says how a file's stored Lyricsfile relates to the one that would
// replace it.
type WordState uint8

const (
	// WordNone means neither the file nor the other version has a Lyricsfile.
	WordNone WordState = iota
	// WordAvailable means only the other version has one: the file would gain
	// word-synced data.
	WordAvailable
	// WordRemoved means only the file has one: the other version isn't
	// word-synced, so the stored document would be removed.
	WordRemoved
	// WordIdentical means the two documents are exactly the same text.
	WordIdentical
	// WordFormatOnly means the documents differ as text but not in what they
	// say, as when only quoting or indentation differs.
	WordFormatOnly
	// WordChanged means the documents say different things.
	WordChanged
)

// MetaChange is a metadata field that differs between two Lyricsfiles. A side
// that doesn't have the field has its Has flag false.
type MetaChange struct {
	Key      string
	Old, New string
	HasOld   bool
	HasNew   bool
}

// WordTiming summarizes how the times of words differ in lines that are the
// same in both versions. It counts words, where Timing counts lines.
type WordTiming struct {
	// Compared is how many words were compared and Retimed how many have a
	// different start time.
	Compared, Retimed int
	// Median is the median change of those, positive for later, and MaxAbs
	// the largest change in either direction.
	Median, MaxAbs time.Duration
}

// WordDiff compares a file's stored Lyricsfile with another version. It reads
// the documents' content instead of diffing their text, because a word-synced
// document is mostly millisecond timings, and a line diff of those buries
// what actually changed.
type WordDiff struct {
	State WordState

	// VersionOld and VersionNew are the documents' version fields, set when
	// they differ.
	VersionOld, VersionNew string

	// Metadata lists the metadata fields that differ, by name.
	Metadata []MetaChange

	// Lines is a diff of the text of the lines, LineTiming is how the start
	// times of lines that match differ, LineRetimed lists those lines, and
	// LineEndChanged counts the matching lines whose end time differs.
	Lines          TextDiff
	LineTiming     Timing
	LineRetimed    []Retime
	LineEndChanged int

	// Words summarizes the word timings of matching lines. SegmentedDiffer
	// counts matching lines whose words are split differently, WordsAdded
	// and WordsRemoved the matching lines that gained or lost word timings
	// altogether.
	Words           WordTiming
	SegmentedDiffer int
	WordsAdded      int
	WordsRemoved    int

	// Plain is a diff of the documents' plain lyrics.
	Plain TextDiff

	// Uninterpreted is true when the documents say different things but
	// nothing above shows it: the difference is in fields the diff doesn't
	// read. Raw can show it.
	Uninterpreted bool

	// ParseErr is set when a document couldn't be read at all, in which case
	// only Raw says anything about the difference.
	ParseErr error

	// Raw is a line diff of the two documents as text, filled in only when
	// asked for.
	Raw TextDiff
}

// Differs reports whether the two documents say different things (or one is
// missing).
func (w WordDiff) Differs() bool {
	switch w.State {
	case WordAvailable, WordRemoved, WordChanged:
		return true
	}
	return false
}

// diffLyricsfile compares the Lyricsfile stored in a file (stored, empty if it
// has none) with the one of the version that would replace it (candidate,
// empty if that version isn't word-synced). context is as for diffLines; raw
// asks for the text diff in WordDiff.Raw.
func diffLyricsfile(stored, candidate string, context int, raw bool) WordDiff {
	switch {
	case stored == "" && candidate == "":
		return WordDiff{State: WordNone}
	case stored == "":
		return WordDiff{State: WordAvailable}
	case candidate == "":
		return WordDiff{State: WordRemoved}
	case stored == candidate:
		return WordDiff{State: WordIdentical}
	}

	var w WordDiff
	if raw {
		w.Raw = diffLines(lineSource{Text: strings.Split(stored, "\n")}, lineSource{Text: strings.Split(candidate, "\n")}, context)
	}

	oldDoc, oldGeneric, err := parseLyricsfile(stored)
	if err != nil {
		w.State, w.ParseErr, w.Uninterpreted = WordChanged, fmt.Errorf("stored lyricsfile: %w", err), true
		return w
	}
	newDoc, newGeneric, err := parseLyricsfile(candidate)
	if err != nil {
		w.State, w.ParseErr, w.Uninterpreted = WordChanged, fmt.Errorf("new lyricsfile: %w", err), true
		return w
	}

	if reflect.DeepEqual(oldGeneric, newGeneric) {
		w.State = WordFormatOnly
		return w
	}
	w.State = WordChanged

	if oldDoc.Version != newDoc.Version {
		w.VersionOld, w.VersionNew = oldDoc.Version, newDoc.Version
	}
	w.Metadata = diffMetadata(oldDoc.Metadata, newDoc.Metadata)
	w.Plain = diffPlain(oldDoc.Plain, newDoc.Plain, context)

	// Lines, matched up by their text.
	lineText := func(lines []lfLine) []string {
		out := make([]string, len(lines))
		for i, ln := range lines {
			out[i] = ln.Text
		}
		return out
	}
	oldText, newText := lineText(oldDoc.Lines), lineText(newDoc.Lines)
	w.Lines = diffLines(lineSource{Text: oldText}, lineSource{Text: newText}, context)

	var lineDeltas, wordDeltas []time.Duration
	var wordsCompared int
	for _, e := range diffSeq(oldText, newText) {
		if e.Kind != OpEqual {
			continue
		}
		oldLine, newLine := oldDoc.Lines[e.A], newDoc.Lines[e.B]

		if oldLine.StartMS != nil && newLine.StartMS != nil {
			delta := ms(*newLine.StartMS - *oldLine.StartMS)
			lineDeltas = append(lineDeltas, delta)
			if delta != 0 {
				w.LineRetimed = append(w.LineRetimed, Retime{
					Text: newLine.Text,
					Old:  ms(*oldLine.StartMS),
					New:  ms(*newLine.StartMS),
				})
			}
		}
		if differsPtr(oldLine.EndMS, newLine.EndMS) {
			w.LineEndChanged++
		}

		switch {
		case len(oldLine.Words) == 0 && len(newLine.Words) == 0:
		case len(oldLine.Words) == 0:
			w.WordsAdded++
		case len(newLine.Words) == 0:
			w.WordsRemoved++
		default:
			if !slices.EqualFunc(oldLine.Words, newLine.Words, func(a, b lfWord) bool { return a.Text == b.Text }) {
				w.SegmentedDiffer++
				continue
			}
			for k := range oldLine.Words {
				a, b := oldLine.Words[k], newLine.Words[k]
				if a.StartMS == nil || b.StartMS == nil {
					continue
				}
				wordsCompared++
				if delta := ms(*b.StartMS - *a.StartMS); delta != 0 {
					wordDeltas = append(wordDeltas, delta)
				}
			}
		}
	}
	w.LineTiming = summarizeTiming(lineDeltas)

	wt := timingOf(wordsCompared, wordDeltas)
	w.Words = WordTiming{Compared: wt.Compared, Retimed: wt.Retimed, Median: wt.Median, MaxAbs: wt.MaxAbs}

	if len(w.Metadata) == 0 && w.VersionOld == w.VersionNew &&
		w.Lines.Same && w.Plain.Same && w.LineTiming.Retimed == 0 && w.LineEndChanged == 0 &&
		w.Words.Retimed == 0 && w.SegmentedDiffer == 0 && w.WordsAdded == 0 && w.WordsRemoved == 0 {
		w.Uninterpreted = true
	}
	return w
}

// ms converts a number of milliseconds to a Duration.
func ms(n int64) time.Duration { return time.Duration(n) * time.Millisecond }

// differsPtr reports whether two optional values differ, treating a value
// against no value as a difference.
func differsPtr(a, b *int64) bool {
	if a == nil || b == nil {
		return a != b
	}
	return *a != *b
}

// diffMetadata lists the fields that differ between two metadata mappings,
// sorted by name.
func diffMetadata(oldMeta, newMeta map[string]any) []MetaChange {
	seen := make(map[string]bool)
	var keys []string
	for _, meta := range []map[string]any{oldMeta, newMeta} {
		for k := range meta {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	slices.Sort(keys)

	var out []MetaChange
	for _, k := range keys {
		oldVal, hadOld := oldMeta[k]
		newVal, hasNew := newMeta[k]
		if hadOld && hasNew && reflect.DeepEqual(oldVal, newVal) {
			continue
		}
		c := MetaChange{Key: k, HasOld: hadOld, HasNew: hasNew}
		if hadOld {
			c.Old = fmt.Sprint(oldVal)
		}
		if hasNew {
			c.New = fmt.Sprint(newVal)
		}
		out = append(out, c)
	}
	return out
}
