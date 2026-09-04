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

package video

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.senan.xyz/taglib"

	"github.com/mfinelli/musicrename/internal/testutil"
)

func TestWriteDerivedAudioTags(t *testing.T) {
	t.Run("writes title and artist", func(t *testing.T) {
		dir := t.TempDir()
		path := testutil.MakeAudioFile(t, dir, "track.m4a", nil)

		nfo := NFO{Title: "Crazy in Love", Artist: "Beyoncé"}
		require.NoError(t, WriteDerivedAudioTags(path, nfo))

		got, err := taglib.ReadTags(path)
		require.NoError(t, err)
		assert.Equal(t, []string{"Crazy in Love"}, got[taglib.Title])
		assert.Equal(t, []string{"Beyoncé"}, got[taglib.Artist])
	})

	t.Run("writes album and year when present", func(t *testing.T) {
		dir := t.TempDir()
		path := testutil.MakeAudioFile(t, dir, "track.m4a", nil)

		nfo := NFO{Title: "Crazy in Love", Artist: "Beyoncé", Album: "Dangerously in Love", Year: "2003"}
		require.NoError(t, WriteDerivedAudioTags(path, nfo))

		got, err := taglib.ReadTags(path)
		require.NoError(t, err)
		assert.Equal(t, []string{"Dangerously in Love"}, got[taglib.Album])
		assert.Equal(t, []string{"2003"}, got[taglib.Date])
	})

	t.Run("omits album and year when absent from nfo", func(t *testing.T) {
		dir := t.TempDir()
		path := testutil.MakeAudioFile(t, dir, "track.m4a", nil)

		nfo := NFO{Title: "Crazy in Love", Artist: "Beyoncé"}
		require.NoError(t, WriteDerivedAudioTags(path, nfo))

		got, err := taglib.ReadTags(path)
		require.NoError(t, err)
		assert.Empty(t, got[taglib.Album])
		assert.Empty(t, got[taglib.Date])
	})

	t.Run("a later write with no album clears a previously-written one", func(t *testing.T) {
		// The whole point of using taglib.Clear over an additive write:
		// confirms a stale tag from an earlier extraction (before a
		// `video edit` dropped Album) doesn't linger after a --retag.
		dir := t.TempDir()
		path := testutil.MakeAudioFile(t, dir, "track.m4a", nil)

		require.NoError(t, WriteDerivedAudioTags(path, NFO{
			Title: "Crazy in Love", Artist: "Beyoncé", Album: "Dangerously in Love",
		}))
		got, err := taglib.ReadTags(path)
		require.NoError(t, err)
		require.Equal(t, []string{"Dangerously in Love"}, got[taglib.Album])

		require.NoError(t, WriteDerivedAudioTags(path, NFO{
			Title: "Crazy in Love", Artist: "Beyoncé",
		}))
		got, err = taglib.ReadTags(path)
		require.NoError(t, err)
		assert.Empty(t, got[taglib.Album])
	})

	t.Run("preserves tags it doesn't own, e.g. REPLAYGAIN written separately", func(t *testing.T) {
		dir := t.TempDir()
		path := testutil.MakeAudioFile(t, dir, "track.m4a", nil)

		require.NoError(t, taglib.WriteTags(path, map[string][]string{
			"REPLAYGAIN_TRACK_GAIN": {"3.75 dB"},
		}, taglib.WriteOption(0)))

		require.NoError(t, WriteDerivedAudioTags(path, NFO{Title: "Crazy in Love", Artist: "Beyoncé"}))

		got, err := taglib.ReadTags(path)
		require.NoError(t, err)
		assert.Equal(t, []string{"3.75 dB"}, got["REPLAYGAIN_TRACK_GAIN"])
	})

	t.Run("preserves a foreign tag across a second call that also clears a stale owned tag", func(t *testing.T) {
		// Combines the two behaviors WriteDerivedAudioTags must hold at
		// once: it clears its own stale ALBUM (nfo dropped it) while
		// leaving an unrelated foreign tag alone.
		dir := t.TempDir()
		path := testutil.MakeAudioFile(t, dir, "track.m4a", nil)

		require.NoError(t, taglib.WriteTags(path, map[string][]string{
			"REPLAYGAIN_TRACK_GAIN": {"3.75 dB"},
		}, taglib.WriteOption(0)))
		require.NoError(t, WriteDerivedAudioTags(path, NFO{
			Title: "Crazy in Love", Artist: "Beyoncé", Album: "Dangerously in Love",
		}))

		require.NoError(t, WriteDerivedAudioTags(path, NFO{Title: "Crazy in Love", Artist: "Beyoncé"}))

		got, err := taglib.ReadTags(path)
		require.NoError(t, err)
		assert.Empty(t, got[taglib.Album])
		assert.Equal(t, []string{"3.75 dB"}, got["REPLAYGAIN_TRACK_GAIN"])
	})

	t.Run("errors when title is missing", func(t *testing.T) {
		dir := t.TempDir()
		path := testutil.MakeAudioFile(t, dir, "track.m4a", nil)

		err := WriteDerivedAudioTags(path, NFO{Artist: "Beyoncé"})
		assert.Error(t, err)
	})

	t.Run("errors when artist is missing", func(t *testing.T) {
		dir := t.TempDir()
		path := testutil.MakeAudioFile(t, dir, "track.m4a", nil)

		err := WriteDerivedAudioTags(path, NFO{Title: "Crazy in Love"})
		assert.Error(t, err)
	})

	t.Run("errors on a nonexistent file", func(t *testing.T) {
		err := WriteDerivedAudioTags("/nonexistent/track.m4a", NFO{
			Title: "Crazy in Love", Artist: "Beyoncé",
		})
		assert.Error(t, err)
	})
}
