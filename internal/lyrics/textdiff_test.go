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
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// applyEdits rebuilds the new sequence from the old one and an edit script,
// failing the test if the script isn't consistent with either.
func applyEdits(t *testing.T, a, b []string, edits []edit) []string {
	t.Helper()
	var out []string
	nextA, nextB := 0, 0
	for _, e := range edits {
		switch e.Kind {
		case OpEqual:
			require.Equal(t, nextA, e.A)
			require.Equal(t, nextB, e.B)
			require.Equal(t, a[e.A], b[e.B])
			out = append(out, a[e.A])
			nextA++
			nextB++
		case OpDelete:
			require.Equal(t, nextA, e.A)
			require.Equal(t, -1, e.B)
			nextA++
		case OpInsert:
			require.Equal(t, nextB, e.B)
			require.Equal(t, -1, e.A)
			out = append(out, b[e.B])
			nextB++
		}
	}
	require.Equal(t, len(a), nextA, "script must consume all of the old sequence")
	require.Equal(t, len(b), nextB, "script must consume all of the new sequence")
	return out
}

func TestDiffSeq(t *testing.T) {
	kinds := func(edits []edit) string {
		var sb strings.Builder
		for _, e := range edits {
			sb.WriteString(map[OpKind]string{OpEqual: "=", OpDelete: "-", OpInsert: "+"}[e.Kind])
		}
		return sb.String()
	}

	t.Run("identical sequences are all equal", func(t *testing.T) {
		a := []string{"a", "b", "c"}
		assert.Equal(t, "===", kinds(diffSeq(a, a)))
	})

	t.Run("empty sequences", func(t *testing.T) {
		assert.Empty(t, diffSeq(nil, nil))
		assert.Equal(t, "++", kinds(diffSeq(nil, []string{"a", "b"})))
		assert.Equal(t, "--", kinds(diffSeq([]string{"a", "b"}, nil)))
	})

	t.Run("insertion, deletion and replacement", func(t *testing.T) {
		assert.Equal(t, "=+==", kinds(diffSeq([]string{"a", "c", "d"}, []string{"a", "b", "c", "d"})))
		assert.Equal(t, "=-==", kinds(diffSeq([]string{"a", "b", "c", "d"}, []string{"a", "c", "d"})))
		assert.Equal(t, "=-+=", kinds(diffSeq([]string{"a", "b", "d"}, []string{"a", "c", "d"})))
	})

	t.Run("deletes come before inserts within a change", func(t *testing.T) {
		assert.Equal(t, "--++", kinds(diffSeq([]string{"a", "b"}, []string{"c", "d"})))
	})

	t.Run("the script always turns the old sequence into the new", func(t *testing.T) {
		rng := rand.New(rand.NewPCG(1, 2))
		words := []string{"a", "b", "c", "d", "e"}
		gen := func() []string {
			n := rng.IntN(12)
			out := make([]string, n)
			for i := range out {
				out[i] = words[rng.IntN(len(words))]
			}
			return out
		}
		for range 300 {
			a, b := gen(), gen()
			got := applyEdits(t, a, b, diffSeq(a, b))
			if len(b) == 0 {
				assert.Empty(t, got)
			} else {
				assert.Equal(t, b, got)
			}
		}
	})

	t.Run("finds a longest common subsequence", func(t *testing.T) {
		// The unchanged lines in the script are a longest common
		// subsequence, so a diff never reports more change than necessary.
		a := []string{"x", "a", "b", "c", "y", "d"}
		b := []string{"a", "q", "b", "c", "d", "z"}
		var equal int
		for _, e := range diffSeq(a, b) {
			if e.Kind == OpEqual {
				equal++
			}
		}
		assert.Equal(t, 4, equal) // a, b, c, d
	})

	t.Run("gives up gracefully past the size limit", func(t *testing.T) {
		n := 2100 // n*n is over maxDiffCells
		a, b := make([]string, n), make([]string, n)
		for i := range a {
			a[i] = fmt.Sprintf("old %d", i)
			b[i] = fmt.Sprintf("new %d", i)
		}
		edits := diffSeq(a, b)
		require.Len(t, edits, 2*n)
		for i := range n {
			assert.Equal(t, OpDelete, edits[i].Kind)
			assert.Equal(t, OpInsert, edits[n+i].Kind)
		}
	})
}

func TestDiffLines(t *testing.T) {
	src := func(lines ...string) lineSource { return lineSource{Text: lines} }
	numbered := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("line %d", i+1)
		}
		return out
	}

	t.Run("identical text is Same with no hunks", func(t *testing.T) {
		d := diffLines(src("a", "b"), src("a", "b"), 3)
		assert.True(t, d.Same)
		assert.Empty(t, d.Hunks)
		assert.Zero(t, d.Added+d.Removed)
	})

	t.Run("counts additions and removals", func(t *testing.T) {
		d := diffLines(src("a", "b", "c"), src("a", "x", "y", "c"), 3)
		assert.False(t, d.Same)
		assert.Equal(t, 2, d.Added)
		assert.Equal(t, 1, d.Removed)
	})

	t.Run("a change gets its context and a numbered header", func(t *testing.T) {
		old := numbered(10)
		updated := append([]string(nil), old...)
		updated[4] = "line five, reworded"

		d := diffLines(src(old...), src(updated...), 2)
		require.Len(t, d.Hunks, 1)
		h := d.Hunks[0]

		// Lines 3-7 of both: two before, the change, two after.
		assert.Equal(t, 3, h.OldStart)
		assert.Equal(t, 5, h.OldLines)
		assert.Equal(t, 3, h.NewStart)
		assert.Equal(t, 5, h.NewLines)

		var got []string
		for _, ln := range h.Lines {
			got = append(got, map[OpKind]string{OpEqual: " ", OpDelete: "-", OpInsert: "+"}[ln.Kind]+ln.Text)
		}
		assert.Equal(t, []string{" line 3", " line 4", "-line 5", "+line five, reworded", " line 6", " line 7"}, got)
	})

	t.Run("distant changes make separate hunks, near ones merge", func(t *testing.T) {
		old := numbered(20)
		updated := append([]string(nil), old...)
		updated[2] = "changed 3"
		updated[16] = "changed 17"

		assert.Len(t, diffLines(src(old...), src(updated...), 2).Hunks, 2)
		assert.Len(t, diffLines(src(old...), src(updated...), 20).Hunks, 1)
	})

	t.Run("no context keeps only the changes", func(t *testing.T) {
		d := diffLines(src("a", "b", "c"), src("a", "x", "c"), 0)
		require.Len(t, d.Hunks, 1)
		for _, ln := range d.Hunks[0].Lines {
			assert.NotEqual(t, OpEqual, ln.Kind)
		}
	})

	t.Run("full context is one hunk holding every line", func(t *testing.T) {
		old := numbered(30)
		updated := append([]string(nil), old...)
		updated[1] = "changed 2"
		updated[28] = "changed 29"

		d := diffLines(src(old...), src(updated...), FullContext)
		require.Len(t, d.Hunks, 1)
		h := d.Hunks[0]
		assert.Equal(t, 1, h.OldStart)
		assert.Equal(t, 30, h.OldLines)
		assert.Equal(t, 30, h.NewLines)
		assert.Len(t, h.Lines, 32) // 28 equal + 2 deleted + 2 inserted
	})

	t.Run("line numbers refer to each version", func(t *testing.T) {
		d := diffLines(src("a", "b", "c"), src("a", "c", "d"), FullContext)
		require.Len(t, d.Hunks, 1)
		var del, ins *DiffLine
		for i := range d.Hunks[0].Lines {
			ln := &d.Hunks[0].Lines[i]
			switch ln.Kind {
			case OpDelete:
				del = ln
			case OpInsert:
				ins = ln
			}
		}
		require.NotNil(t, del)
		require.NotNil(t, ins)
		assert.Equal(t, "b", del.Text)
		assert.Equal(t, 2, del.OldNum)
		assert.Zero(t, del.NewNum)
		assert.Equal(t, "d", ins.Text)
		assert.Equal(t, 3, ins.NewNum)
		assert.Zero(t, ins.OldNum)
	})

	t.Run("pure insertion at the start has an empty old side", func(t *testing.T) {
		d := diffLines(src(), src("a"), 0)
		require.Len(t, d.Hunks, 1)
		assert.Zero(t, d.Hunks[0].OldLines)
		assert.Equal(t, 1, d.Hunks[0].NewLines)
	})

	t.Run("stamps travel with the line they belong to", func(t *testing.T) {
		old := lineSource{Text: []string{"a", "b"}, Stamps: []string{"[00:01.00]", "[00:02.00]"}}
		updated := lineSource{Text: []string{"a", "c"}, Stamps: []string{"[00:01.00]", "[00:03.00]"}}
		d := diffLines(old, updated, FullContext)
		require.Len(t, d.Hunks, 1)
		var stamps []string
		for _, ln := range d.Hunks[0].Lines {
			stamps = append(stamps, ln.Stamp)
		}
		// Equal line shows the new stamp, deleted line its old one, inserted
		// line its new one.
		assert.Equal(t, []string{"[00:01.00]", "[00:02.00]", "[00:03.00]"}, stamps)
	})
}

func TestHighlighting(t *testing.T) {
	changedWords := func(segs []Segment) string {
		var out []string
		for _, s := range segs {
			if s.Changed {
				out = append(out, s.Text)
			}
		}
		return strings.Join(out, "|")
	}
	joined := func(segs []Segment) string {
		var sb strings.Builder
		for _, s := range segs {
			sb.WriteString(s.Text)
		}
		return sb.String()
	}

	t.Run("an edited line has its changed words marked on both sides", func(t *testing.T) {
		oldSegs, newSegs, ok := wordSegments("I love you more than words", "I love you most than words")
		require.True(t, ok)
		assert.Equal(t, "more", changedWords(oldSegs))
		assert.Equal(t, "most", changedWords(newSegs))
		assert.Equal(t, "I love you more than words", joined(oldSegs))
		assert.Equal(t, "I love you most than words", joined(newSegs))
	})

	t.Run("a changed phrase is one run, spaces between its words included", func(t *testing.T) {
		oldSegs, newSegs, ok := wordSegments("I love you more than words", "I love you most of all")
		require.True(t, ok)
		assert.Equal(t, "more than words", changedWords(oldSegs))
		assert.Equal(t, "most of all", changedWords(newSegs))
	})

	t.Run("spaces between unchanged and changed words stay unchanged", func(t *testing.T) {
		oldSegs, _, ok := wordSegments("one two three four", "one 2 three 4")
		require.True(t, ok)
		assert.Equal(t, "two|four", changedWords(oldSegs))
	})

	t.Run("punctuation changes are marked too", func(t *testing.T) {
		oldSegs, newSegs, ok := wordSegments("Don't stop, believing", "Don't stop believing!")
		require.True(t, ok)
		assert.Equal(t, ",", changedWords(oldSegs))
		assert.Equal(t, "!", changedWords(newSegs))
	})

	t.Run("lines with little in common are not highlighted", func(t *testing.T) {
		_, _, ok := wordSegments("one two three four", "alpha beta gamma delta")
		assert.False(t, ok)
	})

	t.Run("a replaced line pairs with its replacement in the diff", func(t *testing.T) {
		d := diffLines(
			lineSource{Text: []string{"keep", "I love you more", "keep too"}},
			lineSource{Text: []string{"keep", "I love you most", "keep too"}},
			FullContext)
		require.Len(t, d.Hunks, 1)
		var withSegments int
		for _, ln := range d.Hunks[0].Lines {
			if ln.Segments != nil {
				withSegments++
				assert.NotEqual(t, OpEqual, ln.Kind)
			}
		}
		assert.Equal(t, 2, withSegments)
	})

	t.Run("the k-th deleted line pairs with the k-th inserted line", func(t *testing.T) {
		d := diffLines(
			lineSource{Text: []string{"the first old line here", "the second old line here"}},
			lineSource{Text: []string{"the first new line here", "the second new line here", "an extra unrelated thing"}},
			FullContext)
		require.Len(t, d.Hunks, 1)
		var highlighted int
		for _, ln := range d.Hunks[0].Lines {
			if ln.Segments != nil {
				highlighted++
			}
		}
		assert.Equal(t, 4, highlighted) // two pairs; the extra line is plain
	})
}

func TestPlainLines(t *testing.T) {
	t.Run("ignores line endings, trailing space, and blank lines at the ends", func(t *testing.T) {
		assert.Equal(t, []string{"a", "b"}, plainLines("\n\na  \r\nb\t\r\n\n\n"))
	})

	t.Run("keeps blank lines between stanzas", func(t *testing.T) {
		assert.Equal(t, []string{"a", "", "b"}, plainLines("a\n\nb\n"))
	})

	t.Run("empty text has no lines", func(t *testing.T) {
		assert.Empty(t, plainLines(""))
		assert.Empty(t, plainLines("\n \n"))
	})
}

func TestDiffPlain(t *testing.T) {
	t.Run("formatting-only differences are not differences", func(t *testing.T) {
		assert.True(t, diffPlain("Hello\nWorld\n", "Hello  \r\nWorld\r\n\r\n", 3).Same)
	})

	t.Run("a changed word shows up", func(t *testing.T) {
		d := diffPlain("Hello\nWorld", "Hello\nWorlds", 3)
		assert.False(t, d.Same)
		assert.Equal(t, 1, d.Added)
		assert.Equal(t, 1, d.Removed)
	})

	t.Run("a stanza break is a difference", func(t *testing.T) {
		assert.False(t, diffPlain("a\nb", "a\n\nb", 3).Same)
	})

	t.Run("lyrics against nothing are all removed", func(t *testing.T) {
		d := diffPlain("a\nb", "", 3)
		assert.Equal(t, 2, d.Removed)
		assert.Zero(t, d.Added)
	})
}
