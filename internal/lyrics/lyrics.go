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
// before embedding. LRC metadata header tags and comment lines are stripped so
// only the lyric content is embedded. Any [offset:±N] tag is applied to all
// timestamps before being discarded.
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
//
// FetchByID embeds a single, specific LRCLIB track (identified by ID; see
// ParseID for extracting one from a lrclib.net URL) directly, bypassing the
// title/artist/album/duration matching strategy above entirely.
package lyrics

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"go.senan.xyz/taglib"
)

// LyricStatus represents the outcome for a single track after processing.
type LyricStatus uint8

const (
	// StatusEmbedded means lyrics were found and written to the file's tags.
	StatusEmbedded LyricStatus = iota
	// StatusSkipped means the file already had lyrics and --force was not set.
	StatusSkipped
	// StatusNotFound means LRCLIB returned no usable result after all fetch
	// steps. This includes the case where a result was returned but contained
	// no plain-text lyrics for an MP3 or M4A file.
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
	NotFound int // no usable result from LRCLIB after all fetch steps
	Failed   int // error during fetch or tag write
}

// Fetch fetches and embeds lyrics for each track in sequence. It is the
// primary entry point for the lyrics package.
//
// For each track the four-step LRCLIB fetch strategy is attempted. Existing
// lyrics tags are left untouched unless force is true, in which case they are
// always re-fetched and overwritten.
//
// progress, if non-nil, is called after each track is processed with the
// track's path and its outcome. The command layer passes a TTY-gated closure
// for live terminal feedback; nil disables all progress output.
func Fetch(ctx context.Context, tracks []TrackInfo, force bool, progress func(string, LyricStatus)) (Summary, error) {
	return fetch(ctx, newLrclibClient(), tracks, force, progress)
}

// FetchByID fetches a single, specific LRCLIB track by its numeric ID (see
// ParseID) and embeds it into path's tags, bypassing the title/artist/
// album/duration matching strategy in Fetch entirely.
//
// It exists for tracks where on-disk tags (e.g. additional artists appended
// to the title) prevent Fetch's matching strategy from finding the right
// result, but the person already knows which LRCLIB track is correct.
//
// Existing lyrics tags are left untouched unless force is true, exactly as
// in Fetch. If id doesn't exist on LRCLIB at all, that's reported as an
// error (the caller supplied a specific, presumably verified, ID); if id
// exists but has nothing embeddable for path's format, that's StatusNotFound,
// matching Fetch's behaviour for the equivalent case.
func FetchByID(ctx context.Context, path string, id int, force bool) (LyricStatus, error) {
	return fetchByID(ctx, newLrclibClient(), path, id, force)
}

// fetchByID is the internal implementation, accepting an explicit client so
// that tests can inject a test server without network access.
func fetchByID(ctx context.Context, c *lrclibClient, path string, id int, force bool) (LyricStatus, error) {
	if status, skip := checkSkip(path, force); skip {
		return status, nil
	}

	result, err := c.getByID(ctx, id)
	if err != nil {
		return StatusFailed, err
	}
	if result == nil {
		return StatusFailed, fmt.Errorf("lrclib: no track with id %d", id)
	}

	return applyResult(path, result), nil
}

// fetch is the internal implementation, accepting an explicit client so that
// tests can inject a test server without network access.
func fetch(ctx context.Context, c *lrclibClient, tracks []TrackInfo, force bool, progress func(string, LyricStatus)) (Summary, error) {
	var summary Summary

	for _, track := range tracks {
		status := processTrack(ctx, c, track, force)

		switch status {
		case StatusEmbedded:
			summary.Embedded++
		case StatusSkipped:
			summary.Skipped++
		case StatusNotFound:
			summary.NotFound++
		case StatusFailed:
			summary.Failed++
		}

		if progress != nil {
			progress(track.Path, status)
		}
	}

	return summary, nil
}

// processTrack handles the full lifecycle for a single track: skip check,
// LRCLIB fetch, and tag write. It returns the final LyricStatus and never
// returns an error (failures are captured as StatusFailed so the caller can
// continue processing the remaining tracks).
func processTrack(ctx context.Context, c *lrclibClient, track TrackInfo, force bool) LyricStatus {
	if status, skip := checkSkip(track.Path, force); skip {
		return status
	}

	result, err := c.fetchForTrack(ctx, track.Title, track.Artist, track.Album, track.Duration)
	if err != nil {
		return StatusFailed
	}
	return applyResult(track.Path, result)
}

// checkSkip reports whether processing should stop before ever contacting
// LRCLIB, per the standard --force semantics shared by Fetch and FetchByID.
// When skip is true, status is the final status to report (StatusSkipped or
// StatusFailed); when skip is false, status is meaningless and processing
// should continue to the fetch step.
func checkSkip(path string, force bool) (status LyricStatus, skip bool) {
	if force {
		return 0, false
	}
	has, err := hasLyrics(path)
	if err != nil {
		return StatusFailed, true
	}
	if has {
		return StatusSkipped, true
	}
	return 0, false
}

// applyResult embeds a fetched LRCLIB result (if any) into path's tags and
// returns the final LyricStatus, shared by Fetch and FetchByID.
func applyResult(path string, result *lrclibTrack) LyricStatus {
	if result == nil {
		return StatusNotFound
	}

	embedded, err := embedLyrics(path, result.SyncedLyrics, result.PlainLyrics)
	if err != nil {
		return StatusFailed
	}
	if !embedded {
		// Format doesn't support synced lyrics and no plain lyrics available.
		return StatusNotFound
	}

	return StatusEmbedded
}

// hasLyrics reports whether path already has lyrics embedded in its tags.
// For FLAC, either LYRICS or UNSYNCEDLYRICS counts. For MP3 and M4A, the
// normalised LYRICS key (USLT / ©lyr) is checked.
func hasLyrics(path string) (bool, error) {
	f, err := taglib.OpenReadOnly(path)
	if err != nil {
		return false, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	tags := f.Tags()
	if tags == nil {
		return false, nil
	}

	switch strings.ToLower(filepath.Ext(path)) {
	case ".flac":
		if vals := tags[taglib.Lyrics]; len(vals) > 0 && vals[0] != "" {
			return true, nil
		}
		if vals := tags["UNSYNCEDLYRICS"]; len(vals) > 0 && vals[0] != "" {
			return true, nil
		}
		return false, nil
	default: // .mp3, .m4a
		vals := tags[taglib.Lyrics]
		return len(vals) > 0 && vals[0] != "", nil
	}
}

// embedLyrics writes synced and/or plain lyrics to the audio file at path.
// Format-specific rules from the design doc:
//
//   - FLAC: synced LRC text -> LYRICS (after standardisation), plain -> UNSYNCEDLYRICS.
//   - MP3/M4A: plain text -> LYRICS only. Tracks with only synced lyrics
//     available are not embedded (returns false, nil).
//
// Returns (true, nil) when at least one tag was written, (false, nil) when
// nothing was written, and (false, err) on failure.
func embedLyrics(path, synced, plain string) (bool, error) {
	tags := make(map[string][]string)

	switch strings.ToLower(filepath.Ext(path)) {
	case ".flac":
		if synced != "" {
			tags[taglib.Lyrics] = []string{standardizeLRC(synced)}
		}
		if plain != "" {
			tags["UNSYNCEDLYRICS"] = []string{plain}
		}
	default: // .mp3, .m4a
		// Synced-only tracks are not embedded for these formats per design.
		if plain != "" {
			tags[taglib.Lyrics] = []string{plain}
		}
	}

	if len(tags) == 0 {
		return false, nil
	}

	f, err := taglib.Open(path)
	if err != nil {
		return false, fmt.Errorf("open for writing: %w", err)
	}
	defer f.Close()

	// WriteOption(0): update the specified tags without clearing other tags.
	if err := f.WriteTags(tags, taglib.WriteOption(0)); err != nil {
		return false, fmt.Errorf("write tags: %w", err)
	}
	return true, nil
}
