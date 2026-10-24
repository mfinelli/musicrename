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
)

func TestNormalizeDrive(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"ASUS    SDRW-08V1M-U   Adapter: 1  ID: 0", "asus sdrw-08v1m-u"},
		{"ASUS    SDRW-08V1M-U   Adapter: 3  ID: 1", "asus sdrw-08v1m-u"},
		{"_NEC DVD_RW ND-3550A Adapter: 1 ID: 0", "nec dvd_rw nd-3550a"},
		{"HL-DT-STDVD-RAM GSA-H20L   Adapter: 2  ID: 0", "hl-dt-stdvd-ram gsa-h20l"},
		{"PLEXTOR DVDR PX-755A (revision 1.08) Adapter: 1 ID: 0", "plextor dvdr px-755a"},
		{"  Mixed   CASE  Drive ", "mixed case drive"},
		{"", ""},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, normalizeDrive(tc.in))
		})
	}
}

func TestKnownDrives(t *testing.T) {
	assert.Equal(t, driveProfile{Offset: 6, Overread: false}, knownDrives["asus sdrw-08v1m-u"])

	for name := range knownDrives {
		assert.Equal(t, name, normalizeDrive(name), "table keys must already be normalized or they can never match")
	}
}
