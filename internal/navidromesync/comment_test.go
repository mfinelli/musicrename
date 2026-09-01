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

package navidromesync

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseCommentDirectives(t *testing.T) {
	t.Run("empty comment: no suffix", func(t *testing.T) {
		human, d := parseCommentDirectives("")
		assert.Equal(t, "", human)
		assert.False(t, d.HasTargets)
		assert.False(t, d.HasSort)
	})

	t.Run("plain human comment with no suffix at all", func(t *testing.T) {
		human, d := parseCommentDirectives("Great road trip mix")
		assert.Equal(t, "Great road trip mix", human)
		assert.False(t, d.HasTargets)
		assert.False(t, d.HasSort)
	})

	t.Run("targets only, human text preceding", func(t *testing.T) {
		human, d := parseCommentDirectives("Great road trip mix [musicrename:targets=ipod,sdcard]")
		assert.Equal(t, "Great road trip mix", human)
		assert.Equal(t, []string{"ipod", "sdcard"}, d.Targets)
		assert.True(t, d.HasTargets)
		assert.False(t, d.HasSort)
	})

	t.Run("sort only", func(t *testing.T) {
		human, d := parseCommentDirectives("[musicrename:sort=artist,album,track]")
		assert.Equal(t, "", human)
		assert.Equal(t, []string{"artist", "album", "track"}, d.Sort)
		assert.True(t, d.HasSort)
		assert.False(t, d.HasTargets)
	})

	t.Run("sort's value order is preserved, never alphabetized on parse", func(t *testing.T) {
		_, d := parseCommentDirectives("[musicrename:sort=track,artist,album]")
		assert.Equal(t, []string{"track", "artist", "album"}, d.Sort)
	})

	t.Run("both directives together, canonical key order", func(t *testing.T) {
		human, d := parseCommentDirectives("Notes [musicrename:sort=artist,album;targets=ipod,sdcard]")
		assert.Equal(t, "Notes", human)
		assert.Equal(t, []string{"artist", "album"}, d.Sort)
		assert.True(t, d.HasSort)
		assert.Equal(t, []string{"ipod", "sdcard"}, d.Targets)
		assert.True(t, d.HasTargets)
	})

	t.Run("both directives, non-canonical key order still parses", func(t *testing.T) {
		human, d := parseCommentDirectives("[musicrename:targets=ipod;sort=artist]")
		assert.Equal(t, "", human)
		assert.Equal(t, []string{"ipod"}, d.Targets)
		assert.Equal(t, []string{"artist"}, d.Sort)
	})

	t.Run("shuffle sentinel round-trips as an ordinary single value", func(t *testing.T) {
		_, d := parseCommentDirectives("[musicrename:sort=shuffle]")
		assert.Equal(t, []string{"shuffle"}, d.Sort)
		assert.True(t, d.HasSort)
	})

	t.Run("present-but-empty targets is distinct from absent", func(t *testing.T) {
		human, d := parseCommentDirectives("Notes [musicrename:targets=]")
		assert.Equal(t, "Notes", human)
		assert.Equal(t, []string{}, d.Targets)
		assert.NotNil(t, d.Targets)
		assert.True(t, d.HasTargets)
	})

	t.Run("present-but-empty sort is distinct from absent", func(t *testing.T) {
		_, d := parseCommentDirectives("[musicrename:sort=]")
		assert.Equal(t, []string{}, d.Sort)
		assert.NotNil(t, d.Sort)
		assert.True(t, d.HasSort)
	})

	t.Run("an unrecognized key within the bracket is ignored, not an error", func(t *testing.T) {
		human, d := parseCommentDirectives("Notes [musicrename:future=whatever;targets=ipod]")
		assert.Equal(t, "Notes", human)
		assert.Equal(t, []string{"ipod"}, d.Targets)
		assert.True(t, d.HasTargets)
	})

	t.Run("a key with no '=' is ignored, not an error", func(t *testing.T) {
		human, d := parseCommentDirectives("Notes [musicrename:garbage;targets=ipod]")
		assert.Equal(t, "Notes", human)
		assert.Equal(t, []string{"ipod"}, d.Targets)
	})

	t.Run("a bracketed substring elsewhere in human text is not mistaken for the suffix", func(t *testing.T) {
		human, d := parseCommentDirectives("See [note] for details")
		assert.Equal(t, "See [note] for details", human)
		assert.False(t, d.HasTargets)
		assert.False(t, d.HasSort)
	})
}

func TestComposeComment(t *testing.T) {
	t.Run("no directives: suffix omitted entirely", func(t *testing.T) {
		assert.Equal(t, "Great road trip mix", composeComment("Great road trip mix", commentDirectives{}))
	})

	t.Run("no directives and no human text: empty string", func(t *testing.T) {
		assert.Equal(t, "", composeComment("", commentDirectives{}))
	})

	t.Run("targets with human text: appended as a suffix", func(t *testing.T) {
		assert.Equal(t, "Great road trip mix [musicrename:targets=ipod,sdcard]", composeComment(
			"Great road trip mix",
			commentDirectives{Targets: []string{"ipod", "sdcard"}, HasTargets: true},
		))
	})

	t.Run("targets' own values are alphabetized regardless of input order", func(t *testing.T) {
		assert.Equal(t, "[musicrename:targets=car,ipod,sdcard]", composeComment(
			"", commentDirectives{Targets: []string{"sdcard", "ipod", "car"}, HasTargets: true},
		))
	})

	t.Run("sort's own values are never reordered", func(t *testing.T) {
		assert.Equal(t, "[musicrename:sort=track,artist,album]", composeComment(
			"", commentDirectives{Sort: []string{"track", "artist", "album"}, HasSort: true},
		))
	})

	t.Run("both directives together are written sort-then-targets (alphabetical key order)", func(t *testing.T) {
		assert.Equal(t, "[musicrename:sort=artist,album;targets=ipod,sdcard]", composeComment(
			"", commentDirectives{
				Targets: []string{"sdcard", "ipod"}, HasTargets: true,
				Sort: []string{"artist", "album"}, HasSort: true,
			},
		))
	})

	t.Run("key order in output is independent of struct field assignment order", func(t *testing.T) {
		// Targets set "first" in the literal, Sort "second" -- output must
		// still be sort-then-targets, since composeComment orders by key,
		// not by whatever order the caller happened to set the fields in.
		got := composeComment("", commentDirectives{
			HasTargets: true, Targets: []string{"ipod"},
			HasSort: true, Sort: []string{"artist"},
		})
		assert.Equal(t, "[musicrename:sort=artist;targets=ipod]", got)
	})

	t.Run("present-but-empty targets list", func(t *testing.T) {
		assert.Equal(t, "Notes [musicrename:targets=]", composeComment(
			"Notes", commentDirectives{Targets: []string{}, HasTargets: true},
		))
	})

	t.Run("present-but-empty sort list", func(t *testing.T) {
		assert.Equal(t, "[musicrename:sort=]", composeComment(
			"", commentDirectives{Sort: []string{}, HasSort: true},
		))
	})

	t.Run("round-trips through parseCommentDirectives", func(t *testing.T) {
		original := composeComment("Great road trip mix", commentDirectives{
			Targets: []string{"ipod", "sdcard"}, HasTargets: true,
			Sort: []string{"artist", "album"}, HasSort: true,
		})
		human, d := parseCommentDirectives(original)
		assert.Equal(t, "Great road trip mix", human)
		assert.Equal(t, []string{"ipod", "sdcard"}, d.Targets)
		assert.Equal(t, []string{"artist", "album"}, d.Sort)
	})

	t.Run("removing both directives produces a comment with no suffix, preserving human text", func(t *testing.T) {
		got := composeComment("Notes", commentDirectives{})
		assert.Equal(t, "Notes", got)

		_, d := parseCommentDirectives(got)
		assert.False(t, d.HasTargets)
		assert.False(t, d.HasSort)
	})

	t.Run("removing only sort while targets remains", func(t *testing.T) {
		got := composeComment("", commentDirectives{Targets: []string{"ipod"}, HasTargets: true})
		assert.Equal(t, "[musicrename:targets=ipod]", got)

		_, d := parseCommentDirectives(got)
		assert.False(t, d.HasSort)
		assert.True(t, d.HasTargets)
	})
}
