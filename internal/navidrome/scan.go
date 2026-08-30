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

package navidrome

import (
	"fmt"
	"time"

	subsonic "github.com/supersonic-app/go-subsonic/subsonic"
)

// DefaultScanPollInterval is how often [Scan] checks whether a triggered
// scan has finished, when the caller doesn't need a different cadence. Short
// enough that the common case (an incremental scan where little or nothing
// changed since the last sync) is noticed within about a second of actually
// finishing, without polling so aggressively that it's needless chatter
// against the server for a scan that genuinely takes a while.
const DefaultScanPollInterval = 1 * time.Second

// ScanProgress is reported by [Scan] while a triggered scan is still
// running, so a caller can show the user something other than apparent
// silence (a sync operation that scans before doing anything else would
// otherwise look like it had hung for however long the scan takes).
type ScanProgress struct {
	// Elapsed is how long the scan has been running so far.
	Elapsed time.Duration
	// Count is the number of items scanned so far, as last reported by the
	// server. May be 0 even for a genuinely in-progress scan, depending on
	// what the server has counted yet.
	Count int64
}

// Scan triggers a Navidrome library scan via client and polls (every
// pollInterval) until it reports complete, so the caller's view of the library
// is guaranteed current before any track/playlist resolution runs against
// it.
//
// The scan's status (returned by starting it) is checked first, before
// any waiting; the common case (an incremental scan with little or
// nothing changed) often finishes before Scan would even get to its first
// poll, and there's no reason to make that case wait a full pollInterval
// for no benefit.
//
// If progress is non-nil, it is called after every check that finds the
// scan still running (never after starting, and never after the scan is
// confirmed complete) so a caller with an interactive terminal has
// something concrete to show for however long a longer scan takes, rather
// than nothing at all.
func Scan(client *subsonic.Client, pollInterval time.Duration, progress func(ScanProgress)) error {
	status, err := client.StartScan()
	if err != nil {
		return fmt.Errorf("starting scan: %w", err)
	}
	if status == nil {
		return fmt.Errorf("server returned no scan status after starting scan")
	}

	start := time.Now()

	for status.Scanning {
		if progress != nil {
			progress(ScanProgress{Elapsed: time.Since(start), Count: status.Count})
		}

		time.Sleep(pollInterval)

		status, err = client.GetScanStatus()
		if err != nil {
			return fmt.Errorf("checking scan status: %w", err)
		}
		if status == nil {
			return fmt.Errorf("server returned no scan status")
		}
	}

	return nil
}
