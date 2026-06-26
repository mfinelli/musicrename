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

package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewAlbum(t *testing.T) {
	const path = "/music/b/beyonce/2003 dangerously in love"
	album := NewAlbum(path)

	assert.Equal(t, path, album.RootPath)

	// Tracks must be initialised so callers can append without a nil check.
	assert.NotNil(t, album.Tracks)
	assert.Empty(t, album.Tracks)

	// Assets must be initialised so callers can index without a nil check.
	assert.NotNil(t, album.Assets)
	assert.Empty(t, album.Assets)
}
