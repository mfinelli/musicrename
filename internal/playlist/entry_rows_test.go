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

package playlist

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/metadata"
)

func makeEntryRowAudioFile(t *testing.T, dir, name string, tags map[string]string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))

	args := []string{
		"-y",
		"-f", "lavfi",
		"-i", "anullsrc=r=44100:cl=stereo",
		"-t", "1",
		"-c:a", "flac",
	}
	for k, v := range tags {
		args = append(args, "-metadata", fmt.Sprintf("%s=%s", k, v))
	}
	args = append(args, path)

	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	require.NoErrorf(t, err, "ffmpeg failed: %s", out)

	return path
}

func TestResolveEntryRows(t *testing.T) {
	t.Run("a missing file is reported as Missing, no track", func(t *testing.T) {
		root := t.TempDir()
		rows := ResolveEntryRows(root, []string{"main/a/artist/album/nope.flac"})
		require.Len(t, rows, 1)
		assert.True(t, rows[0].Missing)
		assert.Nil(t, rows[0].Track)
		assert.Contains(t, rows[0].Label, "missing")
	})

	t.Run("a resolving file with readable tags", func(t *testing.T) {
		root := t.TempDir()
		makeEntryRowAudioFile(t, root, "main/a/artist/album/01 track.flac", map[string]string{
			"ARTIST": "The Artist", "TITLE": "The Title",
		})

		rows := ResolveEntryRows(root, []string{"main/a/artist/album/01 track.flac"})
		require.Len(t, rows, 1)
		assert.False(t, rows[0].Missing)
		require.NotNil(t, rows[0].Track)
		assert.Equal(t, "The Artist", rows[0].Track.Artist)
		assert.Equal(t, "The Title", rows[0].Track.Title)
		assert.Equal(t, "The Artist — The Title", rows[0].Label)
	})

	t.Run("a title-less track falls back to the filename stem", func(t *testing.T) {
		root := t.TempDir()
		makeEntryRowAudioFile(t, root, "main/a/artist/album/02 untitled.flac", map[string]string{
			"ARTIST": "The Artist",
		})

		rows := ResolveEntryRows(root, []string{"main/a/artist/album/02 untitled.flac"})
		require.Len(t, rows, 1)
		assert.Equal(t, "The Artist — 02 untitled", rows[0].Label)
	})

	t.Run("an artist-less track shows only the title", func(t *testing.T) {
		root := t.TempDir()
		makeEntryRowAudioFile(t, root, "main/a/artist/album/03.flac", map[string]string{
			"TITLE": "Solo Title",
		})

		rows := ResolveEntryRows(root, []string{"main/a/artist/album/03.flac"})
		require.Len(t, rows, 1)
		assert.Equal(t, "Solo Title", rows[0].Label)
	})

	t.Run("resolves multiple entries independently, in order, bounded by entry count", func(t *testing.T) {
		root := t.TempDir()
		makeEntryRowAudioFile(t, root, "main/a/artist/album/01.flac", map[string]string{"TITLE": "First"})
		makeEntryRowAudioFile(t, root, "main/a/artist/album/02.flac", map[string]string{"TITLE": "Second"})

		rows := ResolveEntryRows(root, []string{
			"main/a/artist/album/01.flac",
			"main/a/artist/album/missing.flac",
			"main/a/artist/album/02.flac",
		})
		require.Len(t, rows, 3)
		assert.Equal(t, "First", rows[0].Label)
		assert.True(t, rows[1].Missing)
		assert.Equal(t, "Second", rows[2].Label)
	})

	t.Run("an unreadable (non-audio) file is reported as Missing", func(t *testing.T) {
		root := t.TempDir()
		full := filepath.Join(root, "main/a/artist/album/notaudio.flac")
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
		require.NoError(t, os.WriteFile(full, []byte("not actually audio"), 0644))

		rows := ResolveEntryRows(root, []string{"main/a/artist/album/notaudio.flac"})
		require.Len(t, rows, 1)
		assert.True(t, rows[0].Missing)
		assert.Nil(t, rows[0].Track)
		assert.Contains(t, rows[0].Label, "unreadable")
	})

	t.Run("empty entries returns an empty, non-nil slice", func(t *testing.T) {
		root := t.TempDir()
		rows := ResolveEntryRows(root, []string{})
		assert.NotNil(t, rows)
		assert.Empty(t, rows)
	})
}

func TestFilterEntryRows(t *testing.T) {
	rowFor := func(rel, artist, album string) EntryRow {
		return EntryRow{Rel: rel, Track: &metadata.Track{Artist: artist, Album: album}}
	}

	t.Run("artist filter, case-insensitive", func(t *testing.T) {
		rows := []EntryRow{
			rowFor("a.flac", "The Beatles", "Abbey Road"),
			rowFor("b.flac", "the beatles", "Let It Be"),
			rowFor("c.flac", "Pink Floyd", "The Wall"),
		}
		kept, removed := FilterEntryRows(rows, "The Beatles", "")
		assert.Equal(t, []string{"c.flac"}, kept)
		assert.ElementsMatch(t, []string{"a.flac", "b.flac"}, removed)
	})

	t.Run("album filter, case-insensitive", func(t *testing.T) {
		rows := []EntryRow{
			rowFor("a.flac", "The Beatles", "Abbey Road"),
			rowFor("b.flac", "Pink Floyd", "abbey road"),
			rowFor("c.flac", "Pink Floyd", "The Wall"),
		}
		kept, removed := FilterEntryRows(rows, "", "Abbey Road")
		assert.Equal(t, []string{"c.flac"}, kept)
		assert.ElementsMatch(t, []string{"a.flac", "b.flac"}, removed)
	})

	t.Run("both filters given: AND, not OR", func(t *testing.T) {
		rows := []EntryRow{
			rowFor("a.flac", "The Beatles", "Abbey Road"),
			rowFor("b.flac", "The Beatles", "Let It Be"),
			rowFor("c.flac", "Pink Floyd", "Abbey Road"),
		}
		kept, removed := FilterEntryRows(rows, "The Beatles", "Abbey Road")
		assert.ElementsMatch(t, []string{"b.flac", "c.flac"}, kept)
		assert.Equal(t, []string{"a.flac"}, removed)
	})

	t.Run("no filters: nothing removed", func(t *testing.T) {
		rows := []EntryRow{rowFor("a.flac", "X", "Y")}
		kept, removed := FilterEntryRows(rows, "", "")
		assert.Equal(t, []string{"a.flac"}, kept)
		assert.Empty(t, removed)
	})

	t.Run("a row with no Track is never matched, always kept", func(t *testing.T) {
		rows := []EntryRow{
			{Rel: "missing.flac", Track: nil, Missing: true},
			rowFor("a.flac", "The Beatles", "Abbey Road"),
		}
		kept, removed := FilterEntryRows(rows, "The Beatles", "")
		assert.Equal(t, []string{"missing.flac"}, kept)
		assert.Equal(t, []string{"a.flac"}, removed)
	})

	t.Run("preserves original row order within kept and removed", func(t *testing.T) {
		rows := []EntryRow{
			rowFor("a.flac", "Keep", "X"),
			rowFor("b.flac", "Remove", "X"),
			rowFor("c.flac", "Keep", "X"),
			rowFor("d.flac", "Remove", "X"),
		}
		kept, removed := FilterEntryRows(rows, "Remove", "")
		assert.Equal(t, []string{"a.flac", "c.flac"}, kept)
		assert.Equal(t, []string{"b.flac", "d.flac"}, removed)
	})
}
