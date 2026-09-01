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

package playlist

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDedupeEntries(t *testing.T) {
	t.Run("no duplicates: unchanged, removed is zero", func(t *testing.T) {
		deduped, removed := DedupeEntries([]string{"a.flac", "b.flac", "c.flac"})
		assert.Equal(t, []string{"a.flac", "b.flac", "c.flac"}, deduped)
		assert.Equal(t, 0, removed)
	})

	t.Run("adjacent duplicate: keeps first occurrence", func(t *testing.T) {
		deduped, removed := DedupeEntries([]string{"a.flac", "a.flac", "b.flac"})
		assert.Equal(t, []string{"a.flac", "b.flac"}, deduped)
		assert.Equal(t, 1, removed)
	})

	t.Run("non-adjacent duplicate: keeps the first position, drops the later one", func(t *testing.T) {
		deduped, removed := DedupeEntries([]string{"a.flac", "b.flac", "a.flac", "c.flac"})
		assert.Equal(t, []string{"a.flac", "b.flac", "c.flac"}, deduped)
		assert.Equal(t, 1, removed)
	})

	t.Run("three occurrences of the same entry: two removed", func(t *testing.T) {
		deduped, removed := DedupeEntries([]string{"a.flac", "a.flac", "a.flac"})
		assert.Equal(t, []string{"a.flac"}, deduped)
		assert.Equal(t, 2, removed)
	})

	t.Run("multiple distinct duplicated entries", func(t *testing.T) {
		deduped, removed := DedupeEntries([]string{"a.flac", "b.flac", "a.flac", "b.flac", "c.flac"})
		assert.Equal(t, []string{"a.flac", "b.flac", "c.flac"}, deduped)
		assert.Equal(t, 2, removed)
	})

	t.Run("empty input: empty output, no panic", func(t *testing.T) {
		deduped, removed := DedupeEntries([]string{})
		assert.NotNil(t, deduped)
		assert.Empty(t, deduped)
		assert.Equal(t, 0, removed)
	})

	t.Run("does not mutate the input slice", func(t *testing.T) {
		entries := []string{"a.flac", "a.flac", "b.flac"}
		DedupeEntries(entries)
		assert.Equal(t, []string{"a.flac", "a.flac", "b.flac"}, entries)
	})
}
