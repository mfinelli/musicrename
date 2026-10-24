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

package riplog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMSF(t *testing.T) {
	assert.Equal(t, "00:00:00", msf(0))
	assert.Equal(t, "00:00:74", msf(74))
	assert.Equal(t, "00:01:00", msf(75))
	assert.Equal(t, "05:12:30", msf(23430), "a start sector from a real log")
	assert.Equal(t, "01:00:01", msf(75*60+1))
	assert.Equal(t, "69:49:44", msf(314219), "the start of B'day's last track")
}

func TestFreeDBID(t *testing.T) {
	// The first two are the REM DISCID lines EAC wrote in those rips' cue
	// sheets, so the algorithm is checked against EAC's own output.
	tests := []struct {
		fixture string
		want    string
	}{
		{fixtureBlack, "7509E00A"},
		{fixtureBday, "13111213"},
		{fixtureCentral, "3E099506"}, // computed independently; no cue sheet
	}
	for _, tc := range tests {
		t.Run(tc.fixture, func(t *testing.T) {
			l, err := ParseLog(fixture(t, tc.fixture))
			require.NoError(t, err)

			assert.Equal(t, tc.want, l.FreeDBID())
		})
	}

	t.Run("agrees with the cue sheets", func(t *testing.T) {
		for log, cue := range map[string]string{fixtureBlack: fixtureBlackCue, fixtureBday: fixtureBdayCue} {
			l, err := ParseLog(fixture(t, log))
			require.NoError(t, err)
			c, err := ParseCue(fixture(t, cue))
			require.NoError(t, err)

			assert.Equal(t, c.DiscID, l.FreeDBID(), log)
		}
	})

	t.Run("the lead-out is the sector after the last track's end", func(t *testing.T) {
		// The ID counts whole seconds, so an off-by-one in the lead-out only
		// shows when it falls exactly on a second. None of the real discs
		// above does; these do, and their IDs were computed with a separate
		// implementation. End sector 22349 puts the lead-out at 22350, which
		// is exactly 298 seconds; 22348 would give 297.
		tests := []struct {
			name string
			toc  []TOCTrack
			want string
		}{
			{"one track", []TOCTrack{{1, 0, 22349}}, "02012A01"},
			{"two tracks", []TOCTrack{{1, 0, 11174}, {2, 11175, 22349}}, "09012A02"},
			{"one frame earlier", []TOCTrack{{1, 0, 22348}}, "02012901"},
		}
		for _, tc := range tests {
			assert.Equal(t, tc.want, (&Log{TOC: tc.toc}).FreeDBID(), tc.name)
		}
	})

	t.Run("no table of contents", func(t *testing.T) {
		assert.Equal(t, "", (&Log{}).FreeDBID())
	})

	t.Run("a different layout gives a different ID", func(t *testing.T) {
		a := &Log{TOC: []TOCTrack{{1, 0, 20000}, {2, 20001, 40000}}}
		b := &Log{TOC: []TOCTrack{{1, 0, 20000}, {2, 20001, 40100}}}

		assert.NotEqual(t, a.FreeDBID(), b.FreeDBID())
		assert.Len(t, a.FreeDBID(), 8)
	})
}
