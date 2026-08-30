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
	"fmt"
	"os"
	"path/filepath"
)

// CapacityReport summarizes the storage impact of a planned sync: how much
// additional space the plan needs, how much will be freed by its deletions,
// and how much is actually free on the device right now (all from a single
// Statfs call plus data already gathered while planning).
type CapacityReport struct {
	// NeededBytes is the estimated total size of every add/regenerate in
	// the plan, taken from each entry's *source* file's current size.
	// Importantly NOT the eventual on-device size, which for a transcode
	// or a resize isn't knowable without actually doing the work. This is
	// an intentional approximation: a transcoded file is very unlikely to
	// end up the same size as its source, usually smaller for a lossy
	// re-encode, so NeededBytes tends to overestimate for transcoding
	// targets which is a conservative direction to be wrong in for a
	// capacity check.
	NeededBytes int64
	// FreedBytes is the total size of every deletion in the plan, taken
	// directly from CurrentState's own on-device stat
	// ([DeviceEntry.Size]). This is exact since this is the device's
	// current bytes, not a prediction about a file that doesn't exist yet.
	FreedBytes int64
	// AvailableBytes is the device's free space right now, from a single
	// Statfs call against devicePath.
	AvailableBytes int64
}

// Sufficient reports whether the device has enough free space for the
// plan, crediting space that will be freed by the plan's deletions since
// deletions always happen before any add/regenerate needs the room, so
// there's no ordering hazard in counting them together.
func (r CapacityReport) Sufficient() bool {
	return r.NeededBytes <= r.AvailableBytes+r.FreedBytes
}

// CheckCapacity computes a CapacityReport for diff against devicePath.
// Entries whose source file can no longer be stat'd (should not normally
// happen since diff was just computed against the same library but filesystems
// can change under you) are silently excluded from NeededBytes rather than
// failing the whole report; an approximate capacity check with one missing
// entry is still far more useful than none at all.
func CheckCapacity(
	libraryRootRoot, devicePath string, diff *DiffResult, current *CurrentStateResult,
) (*CapacityReport, error) {
	report := &CapacityReport{}

	for _, change := range diff.Changes {
		switch change.Action {
		case ActionAdd, ActionRegenerate:
			sourcePath := filepath.Join(libraryRootRoot, change.Entry.Root, change.Entry.Rel)
			if info, err := os.Stat(sourcePath); err == nil {
				report.NeededBytes += info.Size()
			}
		case ActionDelete:
			if dev, ok := current.Entries[change.Entry]; ok {
				report.FreedBytes += dev.Size
			}
		}
	}

	available, err := statfsAvailable(devicePath)
	if err != nil {
		return nil, fmt.Errorf("checking free space on %s: %w", devicePath, err)
	}
	report.AvailableBytes = available

	return report, nil
}
