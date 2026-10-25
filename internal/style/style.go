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

// Package style defines the looks that command output is made of: bold for what
// the eye should land on, faint for what is secondary, and a small set of
// colors for success, warnings, failures, and the names of things.
//
// A command says what it means (style.OK.Render for something that passed,
// style.Warn for something worth a second look) and the colors are decided
// here, once, so the commands agree with each other and a color changes in one
// place.
//
// A few names are roles rather than looks. Header and Rule are, today, the
// same as Bold and Faint, but they mean something particular and are used in
// many places; keeping them as their own names lets how a heading or a rule
// looks change without touching, or being confused with, every use of bold and
// faint.
package style

import "charm.land/lipgloss/v2"

// The looks. Each is a lipgloss.Style, which is a value: a style derived from
// one of these (style.Pointer.Bold(true), say) is a copy and leaves it alone.
var (
	// Bold is for what the eye should land on: names that matter, new paths,
	// totals.
	Bold = lipgloss.NewStyle().Bold(true)

	// Faint is for what is secondary: where something came from, files that
	// were skipped or left alone, notes.
	Faint = lipgloss.NewStyle().Faint(true)

	// OK is for success: a check that passed, a file that was written.
	OK = lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green

	// Warn is for what is worth a second look but is not a failure.
	Warn = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // bright yellow

	// Error is for failures.
	Error = lipgloss.NewStyle().Foreground(lipgloss.Color("9")) // bright red

	// Name is for the names of things: an album, a file.
	Name = lipgloss.NewStyle().Foreground(lipgloss.Color("6")) // cyan

	// Pointer is for arrows and cursors, which point at something.
	Pointer = lipgloss.NewStyle().Foreground(lipgloss.Color("4")) // blue
)

// The roles.
var (
	// Header is for the heading of a command's output or of a section of it.
	Header = Bold

	// Rule is for the line drawn above a summary.
	Rule = Faint
)
