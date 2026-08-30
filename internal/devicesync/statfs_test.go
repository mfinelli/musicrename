//go:build linux || darwin

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

package devicesync

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatfsAvailable(t *testing.T) {
	t.Run("returns a plausible positive value for a real, existing directory", func(t *testing.T) {
		avail, err := statfsAvailable(t.TempDir())
		require.NoError(t, err)
		assert.Greater(t, avail, int64(0), "a real mounted filesystem should report some free space")
	})

	t.Run("errors for a path that does not exist", func(t *testing.T) {
		_, err := statfsAvailable(filepath.Join(t.TempDir(), "does", "not", "exist"))
		assert.Error(t, err)
	})
}
