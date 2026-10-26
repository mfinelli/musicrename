/*
 * Copyright © 2020-2026 Mario Finelli
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

// Package exitstatus carries an exit status from where a command decides on it
// to main, which is the only place that ends the process.
package exitstatus

import (
	"errors"
	"strconv"
)

// Error is an error that stands for an exit status. A command returns one when
// it has said everything it has to say and wants the process to end with a
// status.
type Error struct {
	code int
}

// New returns an Error for the exit status.
func New(code int) error {
	return &Error{code: code}
}

func (e *Error) Error() string { return "exit status " + strconv.Itoa(e.code) }

// Code returns the exit status that ending a command with err means: that of an
// Error, however deep in err it is, and 1 for any other.
func Code(err error) int {
	var exit *Error
	if errors.As(err, &exit) {
		return exit.code
	}
	return 1
}
