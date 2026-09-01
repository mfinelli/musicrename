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

func TestNewBrowseSelection(t *testing.T) {
	t.Run("every original entry starts selected", func(t *testing.T) {
		s := NewBrowseSelection([]string{"a.flac", "b.flac"})
		assert.True(t, s.IsSelected("a.flac"))
		assert.True(t, s.IsSelected("b.flac"))
		assert.Equal(t, 2, s.StagedCount())
	})

	t.Run("an unrelated path is not selected", func(t *testing.T) {
		s := NewBrowseSelection([]string{"a.flac"})
		assert.False(t, s.IsSelected("z.flac"))
	})

	t.Run("empty original: nothing selected", func(t *testing.T) {
		s := NewBrowseSelection(nil)
		assert.Equal(t, 0, s.StagedCount())
	})

	t.Run("no changes: FinalEntries matches original exactly", func(t *testing.T) {
		s := NewBrowseSelection([]string{"a.flac", "b.flac", "c.flac"})
		assert.Equal(t, []string{"a.flac", "b.flac", "c.flac"}, s.FinalEntries())
	})
}

func TestBrowseSelectionApply(t *testing.T) {
	t.Run("staging a new (non-original) track", func(t *testing.T) {
		s := NewBrowseSelection([]string{"a.flac"})
		s.Apply([]string{"a.flac", "n.flac"}, map[string]bool{"a.flac": true, "n.flac": true})

		assert.True(t, s.IsSelected("n.flac"))
		assert.Equal(t, []string{"a.flac", "n.flac"}, s.FinalEntries())
	})

	t.Run("unstaging an original track removes it from FinalEntries", func(t *testing.T) {
		s := NewBrowseSelection([]string{"a.flac", "b.flac"})
		s.Apply([]string{"a.flac", "b.flac"}, map[string]bool{"a.flac": false, "b.flac": true})

		assert.False(t, s.IsSelected("a.flac"))
		assert.Equal(t, []string{"b.flac"}, s.FinalEntries())
	})

	t.Run("staging then unstaging the same new track in one Apply call nets to nothing", func(t *testing.T) {
		s := NewBrowseSelection(nil)
		s.Apply([]string{"n.flac"}, map[string]bool{"n.flac": true})
		s.Apply([]string{"n.flac"}, map[string]bool{"n.flac": false})

		assert.False(t, s.IsSelected("n.flac"))
		assert.Empty(t, s.FinalEntries())
	})

	t.Run("re-staging a previously-unstaged new track appends it again, at the end", func(t *testing.T) {
		s := NewBrowseSelection([]string{"a.flac"})
		s.Apply([]string{"n.flac"}, map[string]bool{"n.flac": true})
		s.Apply([]string{"m.flac"}, map[string]bool{"m.flac": true})
		s.Apply([]string{"n.flac"}, map[string]bool{"n.flac": false})
		s.Apply([]string{"n.flac"}, map[string]bool{"n.flac": true})

		assert.Equal(t, []string{"a.flac", "m.flac", "n.flac"}, s.FinalEntries())
	})

	t.Run("unstaging then restaging an original track never touches addedOrder", func(t *testing.T) {
		s := NewBrowseSelection([]string{"a.flac", "b.flac"})
		s.Apply([]string{"n.flac"}, map[string]bool{"n.flac": true})
		s.Apply([]string{"a.flac"}, map[string]bool{"a.flac": false})
		s.Apply([]string{"a.flac"}, map[string]bool{"a.flac": true})

		// a.flac restored to its original position, not appended after n.flac.
		assert.Equal(t, []string{"a.flac", "b.flac", "n.flac"}, s.FinalEntries())
	})

	t.Run("only candidates are affected, even if selectedAfter mentions others", func(t *testing.T) {
		s := NewBrowseSelection([]string{"a.flac", "b.flac"})
		// b.flac is in selectedAfter as false, but it's not a candidate for
		// this call, so it must be left alone.
		s.Apply([]string{"a.flac"}, map[string]bool{"a.flac": false, "b.flac": false})

		assert.False(t, s.IsSelected("a.flac"))
		assert.True(t, s.IsSelected("b.flac"), "b.flac was not a candidate and must be untouched")
	})

	t.Run("no-op when selectedAfter matches the current state exactly", func(t *testing.T) {
		s := NewBrowseSelection([]string{"a.flac"})
		s.Apply([]string{"a.flac"}, map[string]bool{"a.flac": true})

		assert.Equal(t, []string{"a.flac"}, s.FinalEntries())
	})

	t.Run("multiple albums visited across a session accumulate independently", func(t *testing.T) {
		s := NewBrowseSelection([]string{"orig1.flac"})
		s.Apply(
			[]string{"album1-a.flac", "album1-b.flac"},
			map[string]bool{"album1-a.flac": true, "album1-b.flac": false},
		)
		s.Apply(
			[]string{"album2-a.flac"},
			map[string]bool{"album2-a.flac": true},
		)

		assert.Equal(t, []string{"orig1.flac", "album1-a.flac", "album2-a.flac"}, s.FinalEntries())
	})
}

func TestBrowseSelectionStagedCount(t *testing.T) {
	t.Run("reflects both original and added entries", func(t *testing.T) {
		s := NewBrowseSelection([]string{"a.flac", "b.flac"})
		s.Apply([]string{"n.flac"}, map[string]bool{"n.flac": true})
		assert.Equal(t, 3, s.StagedCount())
	})

	t.Run("decreases when an entry is unstaged", func(t *testing.T) {
		s := NewBrowseSelection([]string{"a.flac", "b.flac"})
		s.Apply([]string{"a.flac"}, map[string]bool{"a.flac": false})
		assert.Equal(t, 1, s.StagedCount())
	})
}

func TestBrowseSelectionHasNewEntries(t *testing.T) {
	t.Run("false when nothing has been touched", func(t *testing.T) {
		s := NewBrowseSelection([]string{"a.flac"})
		assert.False(t, s.HasNewEntries())
	})

	t.Run("false when only original entries were toggled", func(t *testing.T) {
		s := NewBrowseSelection([]string{"a.flac", "b.flac"})
		s.Apply([]string{"a.flac"}, map[string]bool{"a.flac": false})
		assert.False(t, s.HasNewEntries())
	})

	t.Run("true once a new entry is staged", func(t *testing.T) {
		s := NewBrowseSelection([]string{"a.flac"})
		s.Apply([]string{"n.flac"}, map[string]bool{"n.flac": true})
		assert.True(t, s.HasNewEntries())
	})

	t.Run("false again after staging then unstaging the same new entry", func(t *testing.T) {
		s := NewBrowseSelection(nil)
		s.Apply([]string{"n.flac"}, map[string]bool{"n.flac": true})
		s.Apply([]string{"n.flac"}, map[string]bool{"n.flac": false})
		assert.False(t, s.HasNewEntries())
	})

	t.Run("true even if an original entry was also unstaged in the same session", func(t *testing.T) {
		s := NewBrowseSelection([]string{"a.flac"})
		s.Apply([]string{"a.flac"}, map[string]bool{"a.flac": false})
		s.Apply([]string{"n.flac"}, map[string]bool{"n.flac": true})
		assert.True(t, s.HasNewEntries())
	})
}
