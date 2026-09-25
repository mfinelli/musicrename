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

package musicbrainz

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRecordingComposers(t *testing.T) {
	brian := Artist{ID: "49acbee5-5fb5-4cf1-868f-67bc01de2d84", Name: "Brian Johnson"}
	angus := Artist{ID: "a0a54c69-b0d2-4d2a-bcf8-994b8846b0d8", Name: "Angus Young"}
	engineer := Artist{ID: "dfcd362d-8172-4e7d-a64e-b8f7577fb437", Name: "Tony Platt"}

	t.Run("no relations at all", func(t *testing.T) {
		r := Recording{}
		assert.Empty(t, r.Composers())
	})

	t.Run("personnel credits only, no performance relation", func(t *testing.T) {
		r := Recording{Relations: []RecordingRelation{
			{Type: "engineer", TargetType: "artist"},
		}}
		assert.Empty(t, r.Composers())
	})

	t.Run("a performance relation whose work has writer credits", func(t *testing.T) {
		r := Recording{Relations: []RecordingRelation{
			{Type: "engineer", TargetType: "artist"}, // mixed in, must be ignored
			{
				Type: "performance", TargetType: "work",
				Work: &Work{
					ID:    "e21b57da-06bd-3394-80f5-d7bb41ffb32a",
					Title: "Hells Bells",
					Relations: []WorkRelation{
						{Type: "writer", TargetType: "artist", Artist: brian},
						{Type: "writer", TargetType: "artist", Artist: angus},
					},
				},
			},
		}}
		got := r.Composers()
		assert.Equal(t, []Composer{
			{Type: "writer", Artist: brian},
			{Type: "writer", Artist: angus},
		}, got)
	})

	t.Run("work-to-work relations (based on, other version, ...) are ignored", func(t *testing.T) {
		r := Recording{Relations: []RecordingRelation{
			{
				Type: "performance", TargetType: "work",
				Work: &Work{
					Relations: []WorkRelation{
						{Type: "writer", TargetType: "artist", Artist: brian},
						{Type: "based on", TargetType: "work"},
						{Type: "other version", TargetType: "work"},
					},
				},
			},
		}}
		assert.Equal(t, []Composer{{Type: "writer", Artist: brian}}, r.Composers())
	})

	t.Run("a relation typed as a work link but with a nil Work is skipped, not a panic", func(t *testing.T) {
		r := Recording{Relations: []RecordingRelation{
			{Type: "performance", TargetType: "work", Work: nil},
		}}
		assert.NotPanics(t, func() { r.Composers() })
		assert.Empty(t, r.Composers())
	})

	t.Run("multiple linked works each contribute their own writers", func(t *testing.T) {
		r := Recording{Relations: []RecordingRelation{
			{Type: "performance", TargetType: "work", Work: &Work{
				Relations: []WorkRelation{{Type: "writer", TargetType: "artist", Artist: brian}},
			}},
			{Type: "performance", TargetType: "work", Work: &Work{
				Relations: []WorkRelation{{Type: "composer", TargetType: "artist", Artist: angus}},
			}},
		}}
		assert.Equal(t, []Composer{
			{Type: "writer", Artist: brian},
			{Type: "composer", Artist: angus},
		}, r.Composers())
	})

	t.Run("personnel and performance relations interleaved", func(t *testing.T) {
		r := Recording{Relations: []RecordingRelation{
			{Type: "engineer", TargetType: "artist", Work: nil},
			{Type: "performance", TargetType: "work", Work: &Work{
				Relations: []WorkRelation{{Type: "writer", TargetType: "artist", Artist: brian}},
			}},
			{Type: "vocal", TargetType: "artist"},
		}}
		assert.Equal(t, []Composer{{Type: "writer", Artist: brian}}, r.Composers())
		// engineer/vocal credits never surface, even though they're present
		// in Relations.
		assert.NotContains(t, r.Composers(), Composer{Type: "engineer", Artist: engineer})
	})
}
