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

package completion

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"

	"github.com/mfinelli/musicrename/internal/playlist"
)

func TestPlaylistArg(t *testing.T) {
	t.Run("with no args yet, offers playlist.Extension filtered file completion", func(t *testing.T) {
		got, directive := PlaylistArg(nil, nil, "")
		assert.Equal(t, []string{playlist.Extension}, got)
		assert.Equal(t, cobra.ShellCompDirectiveFilterFileExt, directive)
	})

	t.Run("once an arg is already given, offers nothing further", func(t *testing.T) {
		got, directive := PlaylistArg(nil, []string{"playlists/road-trip.m3u8"}, "")
		assert.Nil(t, got)
		assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
	})
}

func TestCommaSeparated(t *testing.T) {
	candidates := []string{"ipod", "sdcard"}

	t.Run("empty input offers every candidate", func(t *testing.T) {
		got, directive := CommaSeparated(candidates)(nil, nil, "")
		assert.ElementsMatch(t, []string{"ipod", "sdcard"}, got)
		assert.Equal(t, cobra.ShellCompDirectiveNoSpace|cobra.ShellCompDirectiveNoFileComp, directive)
	})

	t.Run("a partial token before any comma is prefix-matched", func(t *testing.T) {
		got, _ := CommaSeparated(candidates)(nil, nil, "sd")
		assert.Equal(t, []string{"sdcard"}, got)
	})

	t.Run("no match for a partial token yields nothing", func(t *testing.T) {
		got, _ := CommaSeparated(candidates)(nil, nil, "chromecast")
		assert.Empty(t, got)
	})

	t.Run("after a completed name and comma, the prefix is preserved verbatim", func(t *testing.T) {
		got, _ := CommaSeparated(candidates)(nil, nil, "ipod,sd")
		assert.Equal(t, []string{"ipod,sdcard"}, got)
	})

	t.Run("a name already listed before the last comma is not offered again", func(t *testing.T) {
		got, _ := CommaSeparated(candidates)(nil, nil, "ipod,")
		assert.Equal(t, []string{"ipod,sdcard"}, got)
	})

	t.Run("every candidate already listed leaves nothing to offer", func(t *testing.T) {
		got, _ := CommaSeparated(candidates)(nil, nil, "ipod,sdcard,")
		assert.Empty(t, got)
	})
}
