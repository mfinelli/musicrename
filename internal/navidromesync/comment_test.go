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

func TestParseCommentTargets(t *testing.T) {
	t.Run("empty comment: no suffix", func(t *testing.T) {
		human, targets, ok := parseCommentTargets("")
		assert.Equal(t, "", human)
		assert.Nil(t, targets)
		assert.False(t, ok)
	})

	t.Run("plain human comment with no suffix at all", func(t *testing.T) {
		human, targets, ok := parseCommentTargets("Great road trip mix")
		assert.Equal(t, "Great road trip mix", human)
		assert.Nil(t, targets)
		assert.False(t, ok)
	})

	t.Run("suffix with human text preceding it", func(t *testing.T) {
		human, targets, ok := parseCommentTargets("Great road trip mix [musicrename:targets=ipod,sdcard]")
		assert.Equal(t, "Great road trip mix", human)
		assert.Equal(t, []string{"ipod", "sdcard"}, targets)
		assert.True(t, ok)
	})

	t.Run("suffix with no human text at all", func(t *testing.T) {
		human, targets, ok := parseCommentTargets("[musicrename:targets=ipod]")
		assert.Equal(t, "", human)
		assert.Equal(t, []string{"ipod"}, targets)
		assert.True(t, ok)
	})

	t.Run("present-but-empty suffix is distinct from no suffix", func(t *testing.T) {
		human, targets, ok := parseCommentTargets("Notes [musicrename:targets=]")
		assert.Equal(t, "Notes", human)
		assert.Equal(t, []string{}, targets)
		assert.NotNil(t, targets)
		assert.True(t, ok)
	})

	t.Run("a bracketed substring elsewhere in human text is not mistaken for the suffix", func(t *testing.T) {
		human, targets, ok := parseCommentTargets("See [note] for details")
		assert.Equal(t, "See [note] for details", human)
		assert.Nil(t, targets)
		assert.False(t, ok)
	})
}

func TestComposeComment(t *testing.T) {
	t.Run("no targets: suffix omitted entirely", func(t *testing.T) {
		assert.Equal(t, "Great road trip mix", composeComment("Great road trip mix", nil, false))
	})

	t.Run("no targets and no human text: empty string", func(t *testing.T) {
		assert.Equal(t, "", composeComment("", nil, false))
	})

	t.Run("targets with human text: appended as a suffix", func(t *testing.T) {
		assert.Equal(t, "Great road trip mix [musicrename:targets=ipod,sdcard]",
			composeComment("Great road trip mix", []string{"ipod", "sdcard"}, true))
	})

	t.Run("targets with no human text: just the suffix", func(t *testing.T) {
		assert.Equal(t, "[musicrename:targets=ipod]", composeComment("", []string{"ipod"}, true))
	})

	t.Run("present-but-empty targets list", func(t *testing.T) {
		assert.Equal(t, "Notes [musicrename:targets=]", composeComment("Notes", []string{}, true))
	})

	t.Run("round-trips through parseCommentTargets", func(t *testing.T) {
		original := composeComment("Great road trip mix", []string{"ipod", "sdcard"}, true)
		human, targets, ok := parseCommentTargets(original)
		assert.Equal(t, "Great road trip mix", human)
		assert.Equal(t, []string{"ipod", "sdcard"}, targets)
		assert.True(t, ok)
	})

	t.Run("removing targets produces a comment with no suffix, preserving human text", func(t *testing.T) {
		withoutTargets := composeComment("Notes", nil, false)
		assert.Equal(t, "Notes", withoutTargets)

		_, _, ok := parseCommentTargets(withoutTargets)
		assert.False(t, ok, "a comment with the suffix removed must parse back to no targets")
	})
}
