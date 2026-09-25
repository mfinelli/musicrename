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

import "context"

// CheckResult is the outcome of a single-album drift check.
type CheckResult struct {
	// FirstCheck is true when dir had no prior snapshot since the current
	// release data was just recorded as the new baseline, and Changes is
	// always empty in this case (nothing existed yet to compare against).
	FirstCheck bool
	// Changes lists every field that differs between the last recorded
	// snapshot and MusicBrainz's current data, in [Diff]'s stable order.
	// Empty if nothing changed, or if FirstCheck is true.
	Changes []string
}

// Check fetches the current MusicBrainz data for the release identified
// by mbid, compares it against dir's existing snapshot (dir/
// [MetadataFilename], if any), and (unless dryRun is true) persists the
// current data as dir's new snapshot, whether or not anything changed, so
// a clean run always reflects "as of now" and a subsequent unmodified run
// reports no further drift. With dryRun true, the comparison and result
// are the same, but nothing is written: a later run (dry or not) against
// an unchanged upstream release reports the identical diff again.
func Check(ctx context.Context, mbid, dir string, dryRun bool) (*CheckResult, error) {
	current, err := newClient().getRelease(ctx, mbid)
	if err != nil {
		return nil, err
	}

	old, err := ReadSnapshot(dir)
	if err != nil {
		return nil, err
	}

	result := &CheckResult{}
	if old == nil {
		result.FirstCheck = true
	} else {
		result.Changes = Diff(old, current)
	}

	if !dryRun {
		if err := WriteSnapshot(dir, current); err != nil {
			return nil, err
		}
	}

	return result, nil
}
