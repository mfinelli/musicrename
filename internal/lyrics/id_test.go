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

package lyrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseID(t *testing.T) {
	t.Run("bare numeric id", func(t *testing.T) {
		id, err := ParseID("12345")
		require.NoError(t, err)
		assert.Equal(t, 12345, id)
	})

	t.Run("bare numeric id with surrounding whitespace", func(t *testing.T) {
		id, err := ParseID("  12345\n")
		require.NoError(t, err)
		assert.Equal(t, 12345, id)
	})

	t.Run("full api URL", func(t *testing.T) {
		id, err := ParseID("https://lrclib.net/api/get/12345")
		require.NoError(t, err)
		assert.Equal(t, 12345, id)
	})

	t.Run("URL with trailing slash", func(t *testing.T) {
		id, err := ParseID("https://lrclib.net/api/get/12345/")
		require.NoError(t, err)
		assert.Equal(t, 12345, id)
	})

	t.Run("URL with query string", func(t *testing.T) {
		id, err := ParseID("https://lrclib.net/api/get/12345?foo=bar")
		require.NoError(t, err)
		assert.Equal(t, 12345, id)
	})

	t.Run("URL with fragment", func(t *testing.T) {
		id, err := ParseID("https://lrclib.net/api/get/12345#section")
		require.NoError(t, err)
		assert.Equal(t, 12345, id)
	})

	t.Run("URL without scheme", func(t *testing.T) {
		id, err := ParseID("lrclib.net/12345")
		require.NoError(t, err)
		assert.Equal(t, 12345, id)
	})

	t.Run("empty string is an error", func(t *testing.T) {
		_, err := ParseID("")
		assert.Error(t, err)
	})

	t.Run("whitespace-only string is an error", func(t *testing.T) {
		_, err := ParseID("   ")
		assert.Error(t, err)
	})

	t.Run("non-numeric trailing segment is an error", func(t *testing.T) {
		_, err := ParseID("https://lrclib.net/api/search")
		assert.Error(t, err)
	})
}
