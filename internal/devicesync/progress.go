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

// SyncProgressPhase distinguishes the two kinds of events Execute's
// progress callback delivers.
type SyncProgressPhase int

const (
	// SyncProgressStarted fires once per Add/Regenerate entry, right
	// before work on it begins. Delete entries are not reported at all.
	SyncProgressStarted SyncProgressPhase = iota
	// SyncProgressCopying fires repeatedly (throttled) as a plain byte
	// copy proceeds. It is never sent for a transcode or an artwork
	// resize since those have no further progress to report from here;
	// a transcode's ffmpeg invocation already streams its progress
	// straight to the terminal on its own.
	SyncProgressCopying
)

// SyncProgress is delivered to Execute's progress callback, one event at
// a time, from a single goroutine (Execute processes albums
// sequentially), so a callback need not be concurrency-safe.
type SyncProgress struct {
	Phase SyncProgressPhase

	// Index is this file's 1-based position among Total. Both are set
	// on every event, Started or Copying, for the same file.
	Index int
	Total int

	// Name is the source-relative display path (e.g. "beyonce/2003 -
	// dangerously in love/01 crazy in love.flac").
	Name string

	// Verb is a human-readable present-participle description of what's
	// about to happen ("Copying", "Transcoding", "Resizing artwork").
	// Only set when Phase == SyncProgressStarted.
	Verb string

	// BytesCopied and TotalBytes are only set when Phase ==
	// SyncProgressCopying.
	BytesCopied int64
	TotalBytes  int64
}
