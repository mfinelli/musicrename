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

import "fmt"

// PlanResult is the output of [Plan]: everything a caller needs to decide
// whether to proceed with a sync, and to actually run one via [Execute]
// afterward.
type PlanResult struct {
	Diff     *DiffResult
	Capacity *CapacityReport
	// Warnings aggregates every warning produced while planning
	// (DesiredState's, CurrentState's, and Diff's) since a caller
	// presenting a complete picture needs all three, not just Diff's.
	Warnings []string
}

// Plan computes the full sync plan for targetName: desired state, current
// on-device state, the diff between them, and a capacity check which is
// everything needed to decide whether to proceed and to know what the device
// has room for, without writing or removing anything on the device itself
// (CurrentState only ever reads it).
func Plan(libraryRootRoot, devicePath, targetName string) (*PlanResult, error) {
	desired, err := DesiredState(libraryRootRoot, targetName)
	if err != nil {
		return nil, fmt.Errorf("computing desired state: %w", err)
	}
	current, err := CurrentState(devicePath, targetName)
	if err != nil {
		return nil, fmt.Errorf("reading device state: %w", err)
	}
	diff, err := Diff(libraryRootRoot, targetName, desired, current)
	if err != nil {
		return nil, fmt.Errorf("computing diff: %w", err)
	}
	capacity, err := CheckCapacity(libraryRootRoot, devicePath, diff, current)
	if err != nil {
		return nil, fmt.Errorf("checking device capacity: %w", err)
	}

	var warnings []string
	warnings = append(warnings, desired.Warnings...)
	warnings = append(warnings, current.Warnings...)
	warnings = append(warnings, diff.Warnings...)

	return &PlanResult{Diff: diff, Capacity: capacity, Warnings: warnings}, nil
}

// ChangeCounts tallies a [DiffResult]'s changes by action (for a summary
// line or a pre-execution confirmation prompt, without a caller needing to
// range over Changes itself).
type ChangeCounts struct {
	Add, Regenerate, Delete, Skip int
}

// Total is the number of changes across every action, including Skip.
func (c ChangeCounts) Total() int {
	return c.Add + c.Regenerate + c.Delete + c.Skip
}

// CountChanges tallies diff.Changes by action.
func CountChanges(diff *DiffResult) ChangeCounts {
	var c ChangeCounts
	for _, change := range diff.Changes {
		switch change.Action {
		case ActionAdd:
			c.Add++
		case ActionRegenerate:
			c.Regenerate++
		case ActionDelete:
			c.Delete++
		case ActionSkip:
			c.Skip++
		}
	}
	return c
}

// FormatBytes renders n as a human-readable size (kB/MB/GB, 1000-based to
// match how storage capacity is normally advertised and how df/most OS
// disk utilities already report it, not 1024-based binary units).
func FormatBytes(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"kB", "MB", "GB", "TB", "PB", "EB"}
	f := float64(n)
	for _, u := range units {
		f /= 1000
		if f < 1000 {
			return fmt.Sprintf("%.1f %s", f, u)
		}
	}
	return fmt.Sprintf("%.1f %s", f, units[len(units)-1])
}
