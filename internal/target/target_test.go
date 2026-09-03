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
	"github.com/stretchr/testify/require"
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

func TestVideoCapableNames(t *testing.T) {
	t.Run("every name is in Names and actually supports video", func(t *testing.T) {
		for _, n := range VideoCapableNames {
			assert.True(t, Valid(n), "expected %q to be a valid target", n)
			def, ok := DefinitionFor(n)
			require.True(t, ok, "expected a Definition for %q", n)
			assert.True(t, def.SupportsVideo, "%q is in VideoCapableNames but its Definition doesn't support video", n)
		}
	})

	t.Run("every video-capable name in Names is included", func(t *testing.T) {
		for _, n := range Names {
			def, ok := DefinitionFor(n)
			require.True(t, ok)
			if def.SupportsVideo {
				assert.Contains(t, VideoCapableNames, n)
			}
		}
	})

	t.Run("matches the current known set (ipod only)", func(t *testing.T) {
		assert.Equal(t, []string{"ipod"}, VideoCapableNames)
	})
}
