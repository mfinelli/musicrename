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

// Package lyrics fetches lyrics from LRCLIB and embeds them into audio file
// tags. It supports FLAC, MP3, and M4A files with format-appropriate storage:
//
//   - FLAC: synced LRC text in LYRICS, plain text in UNSYNCEDLYRICS.
//   - MP3:  plain text in USLT (via go-taglib's normalised LYRICS key).
//   - M4A:  plain text in ©lyr (via go-taglib's normalised LYRICS key).
//
// All LRC timestamps are normalised to [mm:ss.xx] / [hh:mm:ss.xx] format
// (two-digit centiseconds) before embedding, matching the behaviour of the
// --standardize force.xx flag from the previous Python workflow. LRC metadata
// header tags and comment lines are stripped so only the lyric content is
// embedded.
//
// Lyrics are fetched from the LRCLIB public API using a four-step strategy
// that progressively relaxes the duration constraint before falling back to a
// fuzzy title/artist/album search. Requests are rate-limited client-side to
// five per second as a courtesy to the free public API.
//
// The typical call sequence is:
//
//	summary, err := lyrics.Fetch(ctx, tracks, false, func(path string, status lyrics.LyricStatus) {
//	    fmt.Printf("\r  → %s", filepath.Base(path))
//	})
package lyrics

import (
	"context"
	"time"
)

// LyricStatus represents the outcome for a single track after processing.
type LyricStatus uint8

const (
	// StatusEmbedded means lyrics were found and written to the file's tags.
	StatusEmbedded LyricStatus = iota
	// StatusSkipped means the file already had lyrics and --force was not set.
	StatusSkipped
	// StatusNotFound means LRCLIB returned no result after all fetch steps.
	StatusNotFound
	// StatusFailed means an error occurred during fetching or tag writing.
	StatusFailed
)

// TrackInfo contains the metadata needed to fetch and embed lyrics for a
// single audio file. The command layer builds these from metadata.Track plus
// a taglib properties read for the duration.
type TrackInfo struct {
	Path     string
	Title    string
	Artist   string
	Album    string
	Duration time.Duration
}

// Summary holds aggregate counts from a Fetch run, suitable for the
// end-of-run summary line printed by the cobra command.
type Summary struct {
	Embedded int
	Skipped  int // already had lyrics; --force not set
	NotFound int // no result from LRCLIB after all fetch steps
	Failed   int // error during fetch or tag write
}

// Fetch fetches and embeds lyrics for each track. It is the primary entry
// point for the lyrics package.
//
// For each track the four-step LRCLIB fetch strategy is attempted. Existing
// lyrics tags are left untouched unless force is true, in which case they are
// always overwritten.
//
// progress, if non-nil, is called after each track is processed. The command
// layer passes a TTY-gated closure for live terminal feedback; nil disables
// all progress output.
//
// TODO: implement
func Fetch(_ context.Context, _ []TrackInfo, _ bool, _ func(string, LyricStatus)) (Summary, error) {
	_ = newLrclibClient()
	return Summary{}, nil
}
