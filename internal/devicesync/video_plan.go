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

// MergePlans combines an audio [PlanResult] (from [Plan]) with a video one
// (from [VideoPlan]) into a single PlanResult, so a video-capable target
// can be synced in one command, one confirmation prompt, and one [Execute]
// call rather than two entirely separate ones. This works because Execute
// already handles a mix of entry types within a single call and
// executeAlbum dispatches each entry to PrepareTrack, PrepareVideo, or
// artwork.ResizeFile purely based on that entry's extension.
// Entries belonging to different roots already coexist within one
// Execute call so merging the two Diffs' Changes together is
// sufficient; nothing about Execute itself needs to know or care that the
// result came from two separate plans.
//
// video may be nil (a target with SupportsVideo false, or the caller
// deliberately not computing a video plan), in which case audio is returned
// completely unchanged. Reusing MergePlans in the no-video case rather than
// requiring the caller to special-case skipping it keeps every caller's
// control flow the same shape regardless of whether video ended up in scope.
//
// audio.Capacity.AvailableBytes is used as-is rather than combined with
// video.Capacity.AvailableBytes: both numbers came from independently
// querying the exact same device's real free space at effectively the
// same moment, so they should already be identical (or very nearly so).
// Summing them would double-count the device's actual capacity, not
// combine two different quantities the way NeededBytes and FreedBytes (real,
// additive totals of what each plan separately needs or frees) actually are.
func MergePlans(audio, video *PlanResult) *PlanResult {
	if video == nil {
		return audio
	}

	changes := make([]PlannedChange, 0, len(audio.Diff.Changes)+len(video.Diff.Changes))
	changes = append(changes, audio.Diff.Changes...)
	changes = append(changes, video.Diff.Changes...)

	diffWarnings := make([]string, 0, len(audio.Diff.Warnings)+len(video.Diff.Warnings))
	diffWarnings = append(diffWarnings, audio.Diff.Warnings...)
	diffWarnings = append(diffWarnings, video.Diff.Warnings...)

	warnings := make([]string, 0, len(audio.Warnings)+len(video.Warnings))
	warnings = append(warnings, audio.Warnings...)
	warnings = append(warnings, video.Warnings...)

	return &PlanResult{
		Diff: &DiffResult{Changes: changes, Warnings: diffWarnings},
		Capacity: &CapacityReport{
			NeededBytes:    audio.Capacity.NeededBytes + video.Capacity.NeededBytes,
			FreedBytes:     audio.Capacity.FreedBytes + video.Capacity.FreedBytes,
			AvailableBytes: audio.Capacity.AvailableBytes,
		},
		Warnings: warnings,
	}
}

// VideoPlan computes the full video sync plan for targetName: desired state
// ([VideoDesiredState], libraryRootRoot/videos' {target}.m3u8), current
// on-device state, the diff between them, and a capacity check mirroring
// [Plan]'s exact shape and taking the same libraryRootRoot parameter.
func VideoPlan(libraryRootRoot, devicePath, targetName string) (*PlanResult, error) {
	desired, err := VideoDesiredState(libraryRootRoot, targetName)
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
