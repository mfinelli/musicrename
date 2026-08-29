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

package target

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValid(t *testing.T) {
	t.Run("returns true for every name in Names", func(t *testing.T) {
		for _, n := range Names {
			assert.True(t, Valid(n), "expected %q to be valid", n)
		}
	})

	t.Run("returns false for an unknown target", func(t *testing.T) {
		assert.False(t, Valid("chromecast"))
	})

	t.Run("is case-sensitive", func(t *testing.T) {
		assert.False(t, Valid("IPOD"))
	})

	t.Run("returns false for an empty string", func(t *testing.T) {
		assert.False(t, Valid(""))
	})
}

func TestSrcSumsFilename(t *testing.T) {
	assert.Equal(t, "ipod.src.md5", SrcSumsFilename("ipod"))
	assert.Equal(t, "sdcard.src.md5", SrcSumsFilename("sdcard"))
}
