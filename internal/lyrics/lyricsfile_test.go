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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lfBase is a word-synced document to derive the others in these tests from.
const lfBase = `version: '1.0'

metadata:
  title: 'Test Track'
  artist: 'Test Artist'
  language: 'en'

lines:
  - text: 'Hello world'
    start_ms: 1000
    end_ms: 3000
    words:
      - text: 'Hello '
        start_ms: 1000
        end_ms: 2000
      - text: 'world'
        start_ms: 2000
        end_ms: 3000
  - text: 'Second line'
    start_ms: 5000
    end_ms: 7000
    words:
      - text: 'Second '
        start_ms: 5000
        end_ms: 6000
      - text: 'line'
        start_ms: 6000
        end_ms: 7000

plain: |
  Hello world
  Second line
`

// lfReplace returns lfBase with old replaced by new, failing if old isn't in
// it, so a test can't silently compare a document with itself.
func lfReplace(t *testing.T, old, replacement string) string {
	t.Helper()
	require.Contains(t, lfBase, old)
	return strings.Replace(lfBase, old, replacement, 1)
}

func TestParseLyricsfile(t *testing.T) {
	t.Run("reads the fields the diff uses", func(t *testing.T) {
		doc, generic, err := parseLyricsfile(lfBase)
		require.NoError(t, err)
		assert.Equal(t, "1.0", doc.Version)
		assert.Equal(t, "Test Track", doc.Metadata["title"])
		require.Len(t, doc.Lines, 2)
		assert.Equal(t, "Hello world", doc.Lines[0].Text)
		require.NotNil(t, doc.Lines[0].StartMS)
		assert.Equal(t, int64(1000), *doc.Lines[0].StartMS)
		require.Len(t, doc.Lines[0].Words, 2)
		assert.Equal(t, "Hello ", doc.Lines[0].Words[0].Text)
		assert.Equal(t, "Hello world\nSecond line\n", doc.Plain)
		assert.Contains(t, generic, "lines")
	})

	t.Run("an unquoted version is still a version", func(t *testing.T) {
		doc, _, err := parseLyricsfile("version: 1.0\nmetadata:\n  title: t\n  artist: a\n")
		require.NoError(t, err)
		assert.Equal(t, "1.0", doc.Version)
	})

	t.Run("fields it doesn't know are ignored", func(t *testing.T) {
		_, _, err := parseLyricsfile("version: '1.0'\nfuture_field: {a: 1}\nmetadata: {title: t, artist: a}\n")
		assert.NoError(t, err)
	})

	t.Run("a field of the wrong type is an error", func(t *testing.T) {
		_, _, err := parseLyricsfile("version: '1.0'\nlines:\n  - text: x\n    start_ms: soon\n")
		assert.Error(t, err)
	})

	t.Run("malformed YAML is an error", func(t *testing.T) {
		_, _, err := parseLyricsfile("lines: [unclosed\n")
		assert.Error(t, err)
	})
}

func TestDiffLyricsfile_State(t *testing.T) {
	t.Run("neither side has one", func(t *testing.T) {
		w := diffLyricsfile("", "", 3, false)
		assert.Equal(t, WordNone, w.State)
		assert.False(t, w.Differs())
	})

	t.Run("only the new version has one", func(t *testing.T) {
		w := diffLyricsfile("", lfBase, 3, false)
		assert.Equal(t, WordAvailable, w.State)
		assert.True(t, w.Differs())
	})

	t.Run("only the file has one", func(t *testing.T) {
		w := diffLyricsfile(lfBase, "", 3, false)
		assert.Equal(t, WordRemoved, w.State)
		assert.True(t, w.Differs())
	})

	t.Run("the same text is identical", func(t *testing.T) {
		w := diffLyricsfile(lfBase, lfBase, 3, false)
		assert.Equal(t, WordIdentical, w.State)
		assert.False(t, w.Differs())
	})

	t.Run("the same content written differently is format-only", func(t *testing.T) {
		// Double quotes, different key order, and different indentation.
		reformatted := `version: "1.0"
plain: "Hello world\nSecond line\n"
metadata: {language: en, artist: Test Artist, title: Test Track}
lines:
    - text: Hello world
      start_ms: 1000
      end_ms: 3000
      words:
          - {text: "Hello ", start_ms: 1000, end_ms: 2000}
          - {text: world, start_ms: 2000, end_ms: 3000}
    - text: Second line
      start_ms: 5000
      end_ms: 7000
      words:
          - {text: "Second ", start_ms: 5000, end_ms: 6000}
          - {text: line, start_ms: 6000, end_ms: 7000}
`
		w := diffLyricsfile(lfBase, reformatted, 3, false)
		assert.Equal(t, WordFormatOnly, w.State)
		assert.False(t, w.Differs())
	})
}

func TestDiffLyricsfile_Changes(t *testing.T) {
	t.Run("a new version", func(t *testing.T) {
		w := diffLyricsfile(lfBase, lfReplace(t, "version: '1.0'", "version: '1.1'"), 3, false)
		assert.Equal(t, WordChanged, w.State)
		assert.Equal(t, "1.0", w.VersionOld)
		assert.Equal(t, "1.1", w.VersionNew)
		assert.False(t, w.Uninterpreted)
	})

	t.Run("metadata added, removed and changed", func(t *testing.T) {
		updated := lfReplace(t, "  language: 'en'\n", "  title: 'Other Title'\n  duration_ms: 245000\n")
		// title appears twice now; remove the original so the key is unique.
		updated = strings.Replace(updated, "  title: 'Test Track'\n", "", 1)

		w := diffLyricsfile(lfBase, updated, 3, false)
		assert.Equal(t, WordChanged, w.State)
		assert.Equal(t, []MetaChange{
			{Key: "duration_ms", New: "245000", HasNew: true},
			{Key: "language", Old: "en", HasOld: true},
			{Key: "title", Old: "Test Track", New: "Other Title", HasOld: true, HasNew: true},
		}, w.Metadata)
	})

	t.Run("a line that was reworded", func(t *testing.T) {
		updated := strings.NewReplacer("'Second line'", "'Second lines'", "'line'", "'lines'").Replace(lfBase)
		w := diffLyricsfile(lfBase, updated, 3, false)
		assert.False(t, w.Lines.Same)
		assert.Equal(t, 1, w.Lines.Added)
		assert.Equal(t, 1, w.Lines.Removed)
		assert.False(t, w.Uninterpreted)
	})

	t.Run("lines that all moved by the same amount", func(t *testing.T) {
		updated := strings.NewReplacer(
			"start_ms: 1000", "start_ms: 1200", "start_ms: 5000", "start_ms: 5200",
		).Replace(lfBase)
		w := diffLyricsfile(lfBase, updated, 3, false)

		assert.True(t, w.Lines.Same)
		assert.Equal(t, 2, w.LineTiming.Compared)
		assert.Equal(t, 2, w.LineTiming.Retimed)
		assert.True(t, w.LineTiming.Uniform)
		assert.Equal(t, 200*time.Millisecond, w.LineTiming.Shift)
		require.Len(t, w.LineRetimed, 2)
		assert.Equal(t, Retime{Text: "Hello world", Old: time.Second, New: 1200 * time.Millisecond}, w.LineRetimed[0])
	})

	t.Run("a line whose end changed", func(t *testing.T) {
		w := diffLyricsfile(lfBase, lfReplace(t, "end_ms: 3000\n    words", "end_ms: 3500\n    words"), 3, false)
		assert.Equal(t, 1, w.LineEndChanged)
		assert.Zero(t, w.LineTiming.Retimed)
		assert.False(t, w.Uninterpreted)
	})

	t.Run("words that were retimed", func(t *testing.T) {
		updated := strings.NewReplacer(
			"text: 'world'\n        start_ms: 2000", "text: 'world'\n        start_ms: 2100",
			"text: 'line'\n        start_ms: 6000", "text: 'line'\n        start_ms: 5900",
		).Replace(lfBase)
		w := diffLyricsfile(lfBase, updated, 3, false)

		assert.Equal(t, 4, w.Words.Compared)
		assert.Equal(t, 2, w.Words.Retimed)
		assert.Equal(t, 100*time.Millisecond, w.Words.MaxAbs)
		assert.Zero(t, w.LineTiming.Retimed, "the lines themselves didn't move")
		assert.False(t, w.Uninterpreted)
	})

	t.Run("a line split into words differently", func(t *testing.T) {
		updated := strings.NewReplacer("text: 'Hello '", "text: 'Hel'", "text: 'world'\n        start_ms: 2000", "text: 'lo world'\n        start_ms: 2000").Replace(lfBase)
		w := diffLyricsfile(lfBase, updated, 3, false)
		assert.Equal(t, 1, w.SegmentedDiffer)
		assert.False(t, w.Uninterpreted)
	})

	t.Run("a line that gained or lost its word timings", func(t *testing.T) {
		noWords := `version: '1.0'
metadata: {title: 'Test Track', artist: 'Test Artist', language: 'en'}
lines:
  - text: 'Hello world'
    start_ms: 1000
    end_ms: 3000
  - text: 'Second line'
    start_ms: 5000
    end_ms: 7000
    words:
      - {text: 'Second ', start_ms: 5000, end_ms: 6000}
      - {text: 'line', start_ms: 6000, end_ms: 7000}
plain: |
  Hello world
  Second line
`
		lost := diffLyricsfile(lfBase, noWords, 3, false)
		assert.Equal(t, 1, lost.WordsRemoved)
		assert.Zero(t, lost.WordsAdded)

		gained := diffLyricsfile(noWords, lfBase, 3, false)
		assert.Equal(t, 1, gained.WordsAdded)
		assert.Zero(t, gained.WordsRemoved)
	})

	t.Run("the plain lyrics changed", func(t *testing.T) {
		w := diffLyricsfile(lfBase, strings.Replace(lfBase, "  Second line\n", "  Second lines\n", 1), 3, false)
		assert.False(t, w.Plain.Same)
		assert.True(t, w.Lines.Same)
		assert.False(t, w.Uninterpreted)
	})
}

func TestDiffLyricsfile_Uninterpreted(t *testing.T) {
	t.Run("a difference in a field the diff doesn't read says so", func(t *testing.T) {
		w := diffLyricsfile(lfBase, lfBase+"future_field: 42\n", 3, false)
		assert.Equal(t, WordChanged, w.State)
		assert.True(t, w.Uninterpreted)
		assert.True(t, w.Differs())
	})

	t.Run("in a line", func(t *testing.T) {
		w := diffLyricsfile(lfBase, lfReplace(t, "    end_ms: 3000\n", "    end_ms: 3000\n    singer: 'B'\n"), 3, false)
		assert.Equal(t, WordChanged, w.State)
		assert.True(t, w.Uninterpreted)
	})

	t.Run("an interpreted difference is not reported as uninterpreted", func(t *testing.T) {
		w := diffLyricsfile(lfBase, lfReplace(t, "'en'", "'fr'"), 3, false)
		assert.False(t, w.Uninterpreted)
		assert.Len(t, w.Metadata, 1)
	})
}

func TestDiffLyricsfile_Errors(t *testing.T) {
	t.Run("an unreadable stored document is reported, not hidden", func(t *testing.T) {
		w := diffLyricsfile("lines: [unclosed\n", lfBase, 3, false)
		assert.Equal(t, WordChanged, w.State)
		require.Error(t, w.ParseErr)
		assert.Contains(t, w.ParseErr.Error(), "stored")
		assert.True(t, w.Uninterpreted)
	})

	t.Run("an unreadable new document is reported too", func(t *testing.T) {
		w := diffLyricsfile(lfBase, "lines: [unclosed\n", 3, false)
		require.Error(t, w.ParseErr)
		assert.Contains(t, w.ParseErr.Error(), "new")
	})
}

func TestDiffLyricsfile_Raw(t *testing.T) {
	updated := lfReplace(t, "'en'", "'fr'")

	t.Run("the text diff is only produced when asked for", func(t *testing.T) {
		assert.Empty(t, diffLyricsfile(lfBase, updated, 3, false).Raw.Hunks)

		raw := diffLyricsfile(lfBase, updated, 3, true).Raw
		assert.False(t, raw.Same)
		assert.Equal(t, 1, raw.Added)
		assert.Equal(t, 1, raw.Removed)
	})

	t.Run("it is available when the documents can't be read", func(t *testing.T) {
		w := diffLyricsfile("lines: [unclosed\n", lfBase, FullContext, true)
		require.Error(t, w.ParseErr)
		assert.False(t, w.Raw.Same)
	})
}
