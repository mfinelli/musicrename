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

package navidrome

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrCode(t *testing.T) {
	t.Run("extracts the code from the library's fixed error format", func(t *testing.T) {
		err := errors.New("Error #70: The requested data was not found.")
		code, ok := ErrCode(err)
		assert.True(t, ok)
		assert.Equal(t, 70, code)
	})

	t.Run("extracts a multi-digit code", func(t *testing.T) {
		err := errors.New("Error #100: Incompatible client version.")
		code, ok := ErrCode(err)
		assert.True(t, ok)
		assert.Equal(t, 100, code)
	})

	t.Run("returns not-ok for nil", func(t *testing.T) {
		code, ok := ErrCode(nil)
		assert.False(t, ok)
		assert.Zero(t, code)
	})

	t.Run("returns not-ok for an unrelated error", func(t *testing.T) {
		code, ok := ErrCode(errors.New("connection refused"))
		assert.False(t, ok)
		assert.Zero(t, code)
	})

	t.Run("returns not-ok for a wrapped, differently-formatted error", func(t *testing.T) {
		code, ok := ErrCode(fmt.Errorf("fetching playlist: %w", errors.New("Error #70: not found")))
		assert.False(t, ok, "wrapping changes the message prefix, so this must not silently match")
		assert.Zero(t, code)
	})

	t.Run("ErrCodeNotFound matches the real not-found error", func(t *testing.T) {
		code, ok := ErrCode(errors.New("Error #70: The requested data was not found."))
		assert.True(t, ok)
		assert.Equal(t, ErrCodeNotFound, code)
	})
}
