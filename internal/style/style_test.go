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

package style

import (
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/assert"
)

// hasForeground reports whether a style sets a foreground color. An unset one is
// not nil but lipgloss.NoColor, which reports itself as opaque black.
func hasForeground(s lipgloss.Style) bool {
	_, none := s.GetForeground().(lipgloss.NoColor)
	return !none
}

// rgba returns the color of a style's foreground as a comparable value.
func rgba(t *testing.T, s lipgloss.Style) [4]uint32 {
	t.Helper()
	if !hasForeground(s) {
		t.Fatal("style has no foreground color")
	}
	r, g, b, a := s.GetForeground().RGBA()
	return [4]uint32{r, g, b, a}
}

// color returns lipgloss's value for a terminal color number.
func color(t *testing.T, n string) [4]uint32 {
	t.Helper()
	r, g, b, a := lipgloss.Color(n).RGBA()
	return [4]uint32{r, g, b, a}
}

func TestEmphasis(t *testing.T) {
	t.Run("bold is bold and nothing else", func(t *testing.T) {
		assert.True(t, Bold.GetBold())
		assert.False(t, Bold.GetFaint())
		assert.False(t, hasForeground(Bold))
	})

	t.Run("faint is faint and nothing else", func(t *testing.T) {
		assert.True(t, Faint.GetFaint())
		assert.False(t, Faint.GetBold())
		assert.False(t, hasForeground(Faint))
	})
}

func TestColors(t *testing.T) {
	// These pin each look to what it is documented to be. Output changes
	// colors if one of these does, so a change should be on purpose.
	tests := []struct {
		name  string
		style lipgloss.Style
		color string
	}{
		{"OK is green", OK, "2"},
		{"Warn is bright yellow", Warn, "11"},
		{"Error is bright red", Error, "9"},
		{"Name is cyan", Name, "6"},
		{"Pointer is blue", Pointer, "4"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, color(t, tc.color), rgba(t, tc.style))
			assert.False(t, tc.style.GetBold(), "a color alone does not also bold")
			assert.False(t, tc.style.GetFaint(), "a color alone does not also dim")
		})
	}
}

func TestLooksDiffer(t *testing.T) {
	looks := map[string]lipgloss.Style{
		"Bold": Bold, "Faint": Faint, "OK": OK, "Warn": Warn,
		"Error": Error, "Name": Name, "Pointer": Pointer,
	}

	seen := map[string]string{}
	for name, s := range looks {
		out := s.Render("text")

		assert.Contains(t, out, "text", name)
		assert.NotEqual(t, "text", out, "%s should change how text looks", name)
		if other, dup := seen[out]; dup {
			t.Errorf("%s and %s look the same", name, other)
		}
		seen[out] = name
	}
}

func TestRoles(t *testing.T) {
	t.Run("a header looks as bold does", func(t *testing.T) {
		assert.Equal(t, Bold.Render("text"), Header.Render("text"))
	})

	t.Run("a rule looks as faint does", func(t *testing.T) {
		assert.Equal(t, Faint.Render("text"), Rule.Render("text"))
	})
}

func TestDerivedStylesLeaveTheirBaseAlone(t *testing.T) {
	// The reorder screen derives bold versions of Pointer and Warn. That is only
	// safe because a lipgloss style is a value and deriving one copies it.
	bold := Pointer.Bold(true)

	assert.True(t, bold.GetBold())
	assert.False(t, Pointer.GetBold(), "the shared style is unchanged")
	assert.Equal(t, rgba(t, Pointer), rgba(t, bold), "and the derived one keeps its color")
}
