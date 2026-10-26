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

package exitstatus

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCode(t *testing.T) {
	assert.Equal(t, 1, Code(New(1)))
	assert.Equal(t, 3, Code(New(3)))
	assert.Equal(t, 0, Code(New(0)), "whatever the status is")
	assert.Equal(t, 7, Code(fmt.Errorf("a: %w", fmt.Errorf("b: %w", New(7)))), "however deep")
	assert.Equal(t, 1, Code(errors.New("something else")), "any other error is a failure")
	assert.Equal(t, 1, Code(nil), "and so is nothing that was asked about by mistake")
}

func TestError(t *testing.T) {
	assert.Equal(t, "exit status 3", New(3).Error())

	var exit *Error
	assert.ErrorAs(t, fmt.Errorf("wrapped: %w", New(3)), &exit)
}
