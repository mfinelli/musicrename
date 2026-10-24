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
	"strings"
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
	// Each is the REM DISCID line EAC wrote in that rip's cue sheet, so the
	// algorithm is checked against EAC's own output. The last is a disc with
	// audio before track 1, whose first offset is not the usual 150.
	tests := []struct {
		fixture string
		want    string
	}{
		{fixtureBlack, "7509E00A"},
		{fixtureBday, "13111213"},
		{fixtureCentral, "3E099506"},
		{fixtureTransistor, "310F4D15"}, // track 1 starts at sector 12000
	}
	for _, tc := range tests {
		t.Run(tc.fixture, func(t *testing.T) {
			l, err := ParseLog(fixture(t, tc.fixture))
			require.NoError(t, err)

			assert.Equal(t, tc.want, l.FreeDBID())
		})
	}

	t.Run("agrees with the cue sheets", func(t *testing.T) {
		for log, cue := range map[string]string{
			fixtureBlack:      fixtureBlackCue,
			fixtureBday:       fixtureBdayCue,
			fixtureCentral:    fixtureCentralCue,
			fixtureTransistor: fixtureTransistorCue,
		} {
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

func TestDiscID(t *testing.T) {
	t.Run("the real discs", func(t *testing.T) {
		// Each of these was opened on MusicBrainz and was the release ripped,
		// except Central Park, whose ID was computed independently and not
		// looked up.
		tests := []struct {
			fixture string
			want    string
		}{
			{fixtureBlack, "NdW5DdItgg6nqzd5d6Ade54jPKA-"},
			{fixtureBday, "m_Hx4aO9Zo.DVd9OnSa0YGvodeo-"},
			{fixtureCentral, "1RenVg1qGKl3xwjJ9D0vL2g6VBo-"},
			{fixtureTransistor, "FUQaFCauuRGo0EzScvPFCWWr1vc-"}, // audio before track 1
		}
		for _, tc := range tests {
			t.Run(tc.fixture, func(t *testing.T) {
				l, err := ParseLog(fixture(t, tc.fixture))
				require.NoError(t, err)

				assert.Equal(t, tc.want, l.DiscID())
			})
		}
	})

	t.Run("the vector MusicBrainz's libraries are tested with", func(t *testing.T) {
		// The TOC string 4+96+2D2B+6256+B327+D84A in the cdtoc library's tests:
		// four tracks at offsets 0x96, 0x2D2B, 0x6256 and 0xB327, with the
		// lead-out at 0xD84A. EAC counts sectors from 150 less.
		l := &Log{TOC: []TOCTrack{
			{1, 0x96 - 150, 0x2D2B - 150 - 1},
			{2, 0x2D2B - 150, 0x6256 - 150 - 1},
			{3, 0x6256 - 150, 0xB327 - 150 - 1},
			{4, 0xB327 - 150, 0xD84A - 150 - 1},
		}}

		assert.Equal(t, "nljDXdC8B_pDwbdY1vZJvdrAZI4-", l.DiscID())
	})

	t.Run("a disc whose first track is not track 1", func(t *testing.T) {
		// The offsets go in the slots of their tracks' numbers; those before
		// the first track stay zero. Computed with a separate implementation.
		l := &Log{TOC: []TOCTrack{{3, 0, 19849}, {4, 19850, 39849}}}

		assert.Equal(t, "RGpbpRYp.9w.RgUFOxPTbuN0lHI-", l.DiscID())
	})

	t.Run("always 28 characters from the alphabet", func(t *testing.T) {
		for _, name := range allFixtures {
			l, err := ParseLog(fixture(t, name))
			require.NoError(t, err)

			id := l.DiscID()

			assert.Len(t, id, 28, name)
			assert.Regexp(t, `^[A-Za-z0-9._-]{28}$`, id, name)
			assert.True(t, strings.HasSuffix(id, "-"), "a 20-byte hash always ends in padding: %s", name)
		}
	})

	t.Run("the freedb ID is a different thing", func(t *testing.T) {
		l, err := ParseLog(fixture(t, fixtureBlack))
		require.NoError(t, err)

		assert.NotEqual(t, l.FreeDBID(), l.DiscID())
	})

	t.Run("no table of contents", func(t *testing.T) {
		assert.Equal(t, "", (&Log{}).DiscID())
	})

	t.Run("a track number a disc cannot have", func(t *testing.T) {
		assert.Equal(t, "", (&Log{TOC: []TOCTrack{{0, 0, 100}}}).DiscID())
		assert.Equal(t, "", (&Log{TOC: []TOCTrack{{100, 0, 100}}}).DiscID())
	})
}
