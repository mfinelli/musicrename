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
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// markerPalette wraps text in tags naming the style, so tests can see exactly
// what the renderer styled and how.
func markerPalette() Palette {
	tag := func(name string) func(string) string {
		return func(s string) string { return "<" + name + ">" + s + "</" + name + ">" }
	}
	return Palette{
		Add: tag("add"), Del: tag("del"), AddWord: tag("ADD"), DelWord: tag("DEL"),
		OK: tag("ok"), Warn: tag("warn"), Err: tag("err"), Name: tag("name"),
		Faint: tag("faint"), Bold: tag("bold"),
	}
}

// diffAgainst compares a file holding emb with what writing r to it would
// leave, without any files: only the path's extension is used.
func diffAgainst(path string, emb embeddedLyrics, r lrclibResponse, opts DiffOptions) TrackDiff {
	return compareTrack(path, emb, asTrack(r), opts)
}

// embFrom is what a FLAC file embedded from r holds.
func embFrom(r lrclibResponse) embeddedLyrics {
	return embeddedLyrics{
		Plain:      r.PlainLyrics,
		Synced:     standardizeLRC(r.SyncedLyrics),
		Lyricsfile: map[bool]string{true: r.Lyricsfile}[r.HasWordSync],
		SourceURL:  TrackURL(r.ID),
	}
}

func render(t *testing.T, d TrackDiff, pal Palette, opts RenderOptions) string {
	t.Helper()
	var buf bytes.Buffer
	RenderDetails(&buf, d, pal, "", opts)
	return buf.String()
}

func TestFmtShift(t *testing.T) {
	assert.Equal(t, "+300 ms", fmtShift(300*time.Millisecond))
	assert.Equal(t, "-40 ms", fmtShift(-40*time.Millisecond))
	assert.Equal(t, "+0 ms", fmtShift(0))
	assert.Equal(t, "+1.20 s", fmtShift(1200*time.Millisecond))
	assert.Equal(t, "-2.50 s", fmtShift(-2500*time.Millisecond))
}

func TestTimingSummary(t *testing.T) {
	assert.Equal(t, "3 lines retimed, all by +300 ms",
		timingSummary(Timing{Compared: 3, Retimed: 3, Uniform: true, Shift: 300 * time.Millisecond}, "line"))
	assert.Equal(t, "2 of 5 words retimed, median +40 ms, up to +310 ms",
		timingSummary(Timing{Compared: 5, Retimed: 2, Median: 40 * time.Millisecond, MaxAbs: 310 * time.Millisecond}, "word"))

	t.Run("a single line is not plural", func(t *testing.T) {
		assert.Equal(t, "1 line retimed, all by +300 ms",
			timingSummary(Timing{Compared: 1, Retimed: 1, Uniform: true, Shift: 300 * time.Millisecond}, "line"))
		assert.Equal(t, "1 of 1 word retimed, median +40 ms, up to +40 ms",
			timingSummary(Timing{Compared: 1, Retimed: 1, Median: 40 * time.Millisecond, MaxAbs: 40 * time.Millisecond}, "word"))
	})
}

func TestPlural(t *testing.T) {
	assert.Equal(t, "0 lines", plural(0, "line"))
	assert.Equal(t, "1 line", plural(1, "line"))
	assert.Equal(t, "2 lines", plural(2, "line"))
}

func TestHeadline(t *testing.T) {
	opts := DiffOptions{Context: DefaultContext}
	rec := diffRecord
	emb := embFrom(rec)

	t.Run("identical", func(t *testing.T) {
		d := diffAgainst("t.flac", emb, rec, opts)
		assert.Equal(t, "identical", Headline(d))
	})

	t.Run("identical apart from formatting", func(t *testing.T) {
		messy := emb
		messy.Synced = "[ar:X]\n[0:01.0] Hello world\n[00:05.00]  Second line"
		d := diffAgainst("t.flac", messy, rec, opts)
		require.Equal(t, DiffSame, d.Status)
		assert.Contains(t, Headline(d), "formatted differently")
	})

	t.Run("changed lyrics list what changed", func(t *testing.T) {
		updated := rec
		updated.PlainLyrics = "Hello world\nSecond lines"
		updated.SyncedLyrics = "[00:01.30]Hello world\n[00:05.30]Second line"
		updated.HasWordSync, updated.Lyricsfile = false, ""
		d := diffAgainst("t.flac", emb, updated, opts)
		assert.Equal(t,
			"plain lyrics (+1 −1 lines); synced lyrics (2 lines retimed, all by +300 ms); word-sync would be removed",
			Headline(d))
	})

	t.Run("reworded and retimed synced lyrics", func(t *testing.T) {
		updated := rec
		updated.SyncedLyrics = "[00:01.30]Hello world\n[00:05.30]Second lines"
		d := diffAgainst("t.flac", emb, updated, opts)
		assert.Contains(t, Headline(d), "synced lyrics (+1 −1 lines, 1 line retimed")
	})

	t.Run("word sync available", func(t *testing.T) {
		lineSynced := rec
		lineSynced.HasWordSync, lineSynced.Lyricsfile = false, ""
		d := diffAgainst("t.flac", embFrom(lineSynced), rec, opts)
		require.Equal(t, DiffWordSync, d.Status)
		assert.Equal(t, "word-sync available", Headline(d))
	})

	t.Run("a changed Lyricsfile", func(t *testing.T) {
		updated := rec
		updated.Lyricsfile = strings.Replace(lfBase, "'en'", "'fr'", 1)
		d := diffAgainst("t.flac", emb, updated, opts)
		assert.Equal(t, "word-sync changed", Headline(d))
	})

	t.Run("new lyrics", func(t *testing.T) {
		d := diffAgainst("t.flac", embeddedLyrics{}, rec, opts)
		assert.Equal(t, "no lyrics yet; LRCLIB has some, with word sync", Headline(d))

		plain := rec
		plain.HasWordSync, plain.Lyricsfile = false, ""
		d = diffAgainst("t.flac", embeddedLyrics{}, plain, opts)
		assert.Equal(t, "no lyrics yet; LRCLIB has some", Headline(d))
	})

	t.Run("not found and failed give their reason", func(t *testing.T) {
		assert.Equal(t, "no match on LRCLIB", Headline(compareTrack("t.flac", emb, nil, opts)))

		bad := rec
		bad.Lyricsfile = "a: [unclosed\n"
		d := diffAgainst("t.flac", emb, bad, opts)
		require.Equal(t, DiffFailed, d.Status)
		assert.Equal(t, d.Reason, Headline(d))
	})
}

func TestStatusMarker(t *testing.T) {
	pal := markerPalette()
	for status, want := range map[DiffStatus]string{
		DiffSame:     "<ok>✓</ok>",
		DiffChanged:  "<warn>~</warn>",
		DiffWordSync: "<name>+</name>",
		DiffNew:      "<name>+</name>",
		DiffNotFound: "<faint>✗</faint>",
		DiffFailed:   "<err>!</err>",
	} {
		assert.Equal(t, want, StatusMarker(TrackDiff{Status: status}, pal))
	}
}

func TestRenderDetails(t *testing.T) {
	opts := DiffOptions{Context: DefaultContext}
	plain := PlainPalette()
	rec := diffRecord
	emb := embFrom(rec)

	t.Run("lyrics that are the same", func(t *testing.T) {
		got := render(t, diffAgainst("t.flac", emb, rec, opts), plain, RenderOptions{})
		assert.Equal(t, strings.Join([]string{
			"Record:       the one the file came from, https://lrclib.net/tracks/7",
			"Plain:        identical",
			"Synced:       identical",
			"Word-synced:  identical",
			"",
		}, "\n"), got)
	})

	t.Run("changed plain lyrics come with their diff", func(t *testing.T) {
		updated := rec
		updated.PlainLyrics = "Hello world\nSecond lines"
		got := render(t, diffAgainst("t.flac", emb, updated, opts), plain, RenderOptions{})

		assert.Contains(t, got, "Plain:        changed: +1 −1 lines\n")
		assert.Contains(t, got, "              @@ -1,2 +1,2 @@\n")
		assert.Contains(t, got, "                Hello world\n")
		assert.Contains(t, got, "              - Second line\n")
		assert.Contains(t, got, "              + Second lines\n")
	})

	t.Run("synced lines show their timestamps", func(t *testing.T) {
		updated := rec
		updated.SyncedLyrics = rec.SyncedLyrics + "\n[00:09.00]Third line"
		got := render(t, diffAgainst("t.flac", emb, updated, opts), plain, RenderOptions{})
		assert.Contains(t, got, "Synced:       changed: +1 −0 lines\n")
		assert.Contains(t, got, "              + [00:09.00] Third line\n")
	})

	t.Run("a retiming is summarized, and listed on request", func(t *testing.T) {
		updated := rec
		updated.SyncedLyrics = "[00:01.30]Hello world\n[00:05.30]Second line"

		got := render(t, diffAgainst("t.flac", emb, updated, opts), plain, RenderOptions{})
		assert.Contains(t, got, "Synced:       changed: 2 lines retimed, all by +300 ms\n")
		assert.NotContains(t, got, "@@", "no text changed, so there is nothing to show a diff of")
		assert.NotContains(t, got, "→")

		got = render(t, diffAgainst("t.flac", emb, updated, opts), plain, RenderOptions{ListRetimed: true})
		assert.Contains(t, got, "              [00:01.00] → [00:01.30] (+300 ms) Hello world\n")
		assert.Contains(t, got, "              [00:05.00] → [00:05.30] (+300 ms) Second line\n")
	})

	t.Run("changed words are highlighted within a changed line", func(t *testing.T) {
		updated := rec
		updated.PlainLyrics = "Hello world\nSecond lines"
		got := render(t, diffAgainst("t.flac", emb, updated, opts), markerPalette(), RenderOptions{})

		assert.Contains(t, got, "<del>- </del><del>Second </del><DEL>line</DEL>")
		assert.Contains(t, got, "<add>+ </add><add>Second </add><ADD>lines</ADD>")
	})

	t.Run("things with nothing to compare are left out", func(t *testing.T) {
		got := render(t, diffAgainst("t.mp3", embeddedLyrics{Plain: rec.PlainLyrics}, rec, opts), plain, RenderOptions{})
		assert.Equal(t, "Plain:        identical\n", got)
	})

	t.Run("a file with none says so", func(t *testing.T) {
		got := render(t, diffAgainst("t.flac", embeddedLyrics{}, rec, opts), plain, RenderOptions{})
		assert.Contains(t, got, "Plain:        none in the file; LRCLIB has 2 lines\n")
		assert.Contains(t, got, "Synced:       none in the file; LRCLIB has 2 lines\n")
		assert.Contains(t, got, "Word-synced:  available from LRCLIB; the file has none\n")
		assert.NotContains(t, got, "@@")
	})

	t.Run("a record that isn't the file's, or isn't recorded", func(t *testing.T) {
		other := embFrom(rec)
		other.SourceURL = "https://lrclib.net/tracks/99"
		got := render(t, diffAgainst("t.flac", other, rec, opts), plain, RenderOptions{})
		assert.Contains(t, got, "Record:       the file came from https://lrclib.net/tracks/99; LRCLIB now matches https://lrclib.net/tracks/7\n")

		unknown := embFrom(rec)
		unknown.SourceURL = ""
		got = render(t, diffAgainst("t.flac", unknown, rec, opts), plain, RenderOptions{})
		assert.Contains(t, got, "Record:       not recorded in the file; LRCLIB matches https://lrclib.net/tracks/7\n")
	})

	t.Run("not found and failed have no details, the headline says why", func(t *testing.T) {
		assert.Empty(t, render(t, compareTrack("t.flac", emb, nil, opts), plain, RenderOptions{}))

		bad := rec
		bad.Lyricsfile = "a: [unclosed\n"
		assert.Empty(t, render(t, diffAgainst("t.flac", emb, bad, opts), plain, RenderOptions{}))
	})

	t.Run("labels are bold and each line starts with the indent", func(t *testing.T) {
		var buf bytes.Buffer
		RenderDetails(&buf, diffAgainst("t.flac", emb, rec, opts), markerPalette(), "    ", RenderOptions{})
		for ln := range strings.SplitSeq(strings.TrimSuffix(buf.String(), "\n"), "\n") {
			assert.True(t, strings.HasPrefix(ln, "    "), ln)
		}
		assert.Contains(t, buf.String(), "<bold>Plain:</bold>")
	})
}

func TestRenderDetails_WordSynced(t *testing.T) {
	opts := DiffOptions{Context: DefaultContext}
	plain := PlainPalette()
	rec := diffRecord
	emb := embFrom(rec)

	t.Run("a changed Lyricsfile is described, not diffed", func(t *testing.T) {
		updated := rec
		updated.Lyricsfile = strings.NewReplacer(
			"version: '1.0'", "version: '1.1'",
			"'en'", "'fr'",
			"start_ms: 1000", "start_ms: 1200", "start_ms: 5000", "start_ms: 5200",
			"text: 'world'\n        start_ms: 2000", "text: 'world'\n        start_ms: 2100",
		).Replace(lfBase)

		got := render(t, diffAgainst("t.flac", emb, updated, opts), plain, RenderOptions{})
		assert.Contains(t, got, "Word-synced:  changed\n")
		assert.Contains(t, got, `version: "1.0" → "1.1"`)
		assert.Contains(t, got, "metadata: language: en → fr")
		assert.Contains(t, got, "line timing: 2 lines retimed, all by +200 ms")
		assert.Contains(t, got, "word timing:")
		assert.NotContains(t, got, "start_ms", "the document's own text isn't shown unless --raw")
	})

	t.Run("a word-synced record removed", func(t *testing.T) {
		lineSynced := rec
		lineSynced.HasWordSync, lineSynced.Lyricsfile = false, ""
		got := render(t, diffAgainst("t.flac", emb, lineSynced, opts), plain, RenderOptions{})
		assert.Contains(t, got, "Word-synced:  the file has one; LRCLIB's record is not word-synced, so it would be removed\n")
	})

	t.Run("differences in unread fields are flagged", func(t *testing.T) {
		updated := rec
		updated.Lyricsfile = lfBase + "future_field: 42\n"
		got := render(t, diffAgainst("t.flac", emb, updated, opts), plain, RenderOptions{})
		assert.Contains(t, got, "other: the documents differ in fields this command doesn't read; --raw shows them")
	})

	t.Run("raw shows the documents' text diff", func(t *testing.T) {
		updated := rec
		updated.Lyricsfile = strings.Replace(lfBase, "'en'", "'fr'", 1)
		d := diffAgainst("t.flac", emb, updated, DiffOptions{Context: 1, Raw: true})
		got := render(t, d, plain, RenderOptions{})
		assert.Contains(t, got, "raw: +1 −1 lines")
		assert.Contains(t, got, "- ")
		assert.Contains(t, got, "language: 'fr'")
	})

	t.Run("an unreadable document shows its error and nothing that wasn't computed", func(t *testing.T) {
		stored := embFrom(rec)
		stored.Lyricsfile = "lines: [unclosed\n"
		got := render(t, diffAgainst("t.flac", stored, rec, opts), plain, RenderOptions{})
		assert.Contains(t, got, "error: stored lyricsfile:")
		assert.NotContains(t, got, "+0 −0")
		assert.NotContains(t, got, "lines:")
		assert.NotContains(t, got, "plain:")
	})

	t.Run("without --raw there is no raw diff", func(t *testing.T) {
		updated := rec
		updated.Lyricsfile = strings.Replace(lfBase, "'en'", "'fr'", 1)
		got := render(t, diffAgainst("t.flac", emb, updated, opts), plain, RenderOptions{})
		assert.NotContains(t, got, "raw:")
		assert.NotContains(t, got, "+0 −0")
	})

	t.Run("line retimings are listed on request", func(t *testing.T) {
		updated := rec
		updated.Lyricsfile = strings.Replace(lfBase, "start_ms: 1000", "start_ms: 1200", 1)
		d := diffAgainst("t.flac", emb, updated, opts)
		assert.NotContains(t, render(t, d, plain, RenderOptions{}), "→ [")
		assert.Contains(t, render(t, d, plain, RenderOptions{ListRetimed: true}), "[00:01.00] → [00:01.20] (+200 ms) Hello world")
	})
}
