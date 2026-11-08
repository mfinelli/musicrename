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
//   - FLAC: synced LRC text in LYRICS, plain text in UNSYNCEDLYRICS, the
//     lrclib.net page of the track the synced lyrics came from in LRCLIB_URL,
//     and, when that track has word-level timings, its raw Lyricsfile YAML
//     document in LYRICSFILE.
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
// fuzzy title/artist/album search. Requests identify the tool with a
// User-Agent, as LRCLIB's API documentation requires, and are rate-limited
// client-side to stay within the allowance for identified clients. A 429 or
// 503 response is retried after waiting as long as the server's Retry-After
// header asks; if LRCLIB keeps answering 429, Fetch stops the whole run with
// ErrRateLimited instead of carrying on with the remaining tracks.
//
// The typical call sequence is:
//
//	summary, err := lyrics.Fetch(ctx, tracks, lyrics.Options{}, func(path string, status lyrics.LyricStatus) {
//	    fmt.Printf("\r  → %s", filepath.Base(path))
//	})
//
// FetchByID embeds a single, specific LRCLIB track (identified by ID; see
// ParseID for extracting one from a lrclib.net URL) directly, bypassing the
// title/artist/album/duration matching strategy above entirely.
package lyrics

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"go.senan.xyz/taglib"
	"go.yaml.in/yaml/v3"
)

// SourceURLTag is the name of the custom tag that records which LRCLIB track
// a file's synced lyrics came from, as the lrclib.net page for that track
// (see TrackURL) so that it can be opened in a browser to inspect. It is
// written together with synced lyrics, which are only embedded in FLAC files,
// so it only ever appears on FLAC files.
const SourceURLTag = "LRCLIB_URL"

// LyricsfileTag is the name of the custom tag that holds the raw Lyricsfile
// YAML document (https://github.com/tranxuanthang/lyricsfile) of the LRCLIB
// track a file's synced lyrics came from, for tracks that have word-level
// timings. It is stored verbatim, as LRCLIB returned it, after checking only
// that it parses as YAML, so that the data is on hand if a future player or
// tool can use it. Like SourceURLTag it is written together with the synced
// lyrics, so it only ever appears on FLAC files, and it is removed again if
// the synced lyrics are replaced by ones from a record without word sync.
const LyricsfileTag = "LYRICSFILE"

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

// Options controls how Fetch and FetchByID treat the files they are given.
// The zero value is the default behavior.
type Options struct {
	// Force re-fetches and overwrites the lyrics of files that already have
	// some, instead of skipping them.
	Force bool

	// SkipLyricsfile leaves a record's Lyricsfile out entirely. It is not
	// checked, so even one that isn't valid YAML can't fail the track, and
	// it is not stored. Any LyricsfileTag the file already has is removed
	// along with the lyrics it described, exactly as if the new record had
	// no word sync, so that the tag never outlives its lyrics.
	SkipLyricsfile bool
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
// lyrics tags are left untouched unless opts.Force is true, in which case they
// are always re-fetched and overwritten.
//
// progress, if non-nil, is called after each track is processed with the
// track's path and its outcome. The command layer passes a TTY-gated closure
// for live terminal feedback; nil disables all progress output.
//
// Per-track failures are counted in the returned Summary and don't stop the
// run. The one exception is LRCLIB rate limiting that persists after retrying:
// the track that hit it is counted as failed, the run stops, and Fetch returns
// the Summary so far along with an error wrapping ErrRateLimited.
func Fetch(ctx context.Context, tracks []TrackInfo, opts Options, progress func(string, LyricStatus)) (Summary, error) {
	return fetch(ctx, newLrclibClient(), tracks, opts, progress)
}

// FetchByID fetches a single, specific LRCLIB track by its numeric ID (see
// ParseID) and embeds it into path's tags, bypassing the title/artist/
// album/duration matching strategy in Fetch entirely.
//
// It exists for tracks where on-disk tags (e.g. additional artists appended
// to the title) prevent Fetch's matching strategy from finding the right
// result, but the person already knows which LRCLIB track is correct.
//
// Existing lyrics tags are left untouched unless opts.Force is true, exactly as
// in Fetch. If id doesn't exist on LRCLIB at all, that's reported as an
// error (the caller supplied a specific, presumably verified, ID); if id
// exists but has nothing embeddable for path's format, that's StatusNotFound,
// matching Fetch's behaviour for the equivalent case.
func FetchByID(ctx context.Context, path string, id int, opts Options) (LyricStatus, error) {
	return fetchByID(ctx, newLrclibClient(), path, id, opts)
}

// fetchByID is the internal implementation, accepting an explicit client so
// that tests can inject a test server without network access.
func fetchByID(ctx context.Context, c *lrclibClient, path string, id int, opts Options) (LyricStatus, error) {
	if status, skip := checkSkip(path, opts.Force); skip {
		return status, nil
	}

	result, err := c.getByID(ctx, id)
	if err != nil {
		return StatusFailed, err
	}
	if result == nil {
		return StatusFailed, fmt.Errorf("lrclib: no track with id %d", id)
	}

	return applyResult(path, result, opts), nil
}

// fetch is the internal implementation, accepting an explicit client so that
// tests can inject a test server without network access.
func fetch(ctx context.Context, c *lrclibClient, tracks []TrackInfo, opts Options, progress func(string, LyricStatus)) (Summary, error) {
	var summary Summary

	for _, track := range tracks {
		status, err := processTrack(ctx, c, track, opts)

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

		// Keep going after an ordinary failure, but never keep sending
		// requests to a server that is telling us to slow down.
		if err != nil {
			return summary, err
		}
	}

	return summary, nil
}

// processTrack handles the full lifecycle for a single track: skip check,
// LRCLIB fetch, and tag write. It returns the final LyricStatus. Failures are
// captured as StatusFailed with a nil error so the caller can continue with
// the remaining tracks; the only error returned is one wrapping
// ErrRateLimited (alongside StatusFailed), which the caller should treat as a
// reason to stop.
func processTrack(ctx context.Context, c *lrclibClient, track TrackInfo, opts Options) (LyricStatus, error) {
	if status, skip := checkSkip(track.Path, opts.Force); skip {
		return status, nil
	}

	result, err := c.fetchForTrack(ctx, track.Title, track.Artist, track.Album, track.Duration)
	if err != nil {
		if errors.Is(err, ErrRateLimited) {
			return StatusFailed, err
		}
		return StatusFailed, nil
	}
	return applyResult(track.Path, result, opts), nil
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
func applyResult(path string, result *lrclibTrack, opts Options) LyricStatus {
	if result == nil {
		return StatusNotFound
	}

	embedded, err := embedLyrics(path, inputFor(result, opts))
	if err != nil {
		return StatusFailed
	}
	if !embedded {
		// Format doesn't support synced lyrics and no plain lyrics available.
		return StatusNotFound
	}

	return StatusEmbedded
}

// inputFor builds what embedLyrics should write for a fetched LRCLIB result.
func inputFor(result *lrclibTrack, opts Options) embedInput {
	// An ID of zero means the response didn't identify the track (LRCLIB
	// IDs start at one), in which case there's nothing to link to.
	var sourceURL string
	if result.ID > 0 {
		sourceURL = TrackURL(result.ID)
	}

	return embedInput{
		Synced:     result.SyncedLyrics,
		Plain:      result.PlainLyrics,
		SourceURL:  sourceURL,
		WordSynced: result.HasWordSync && !opts.SkipLyricsfile,
		Lyricsfile: result.Lyricsfile,
	}
}

// hasLyrics reports whether path already has lyrics embedded in its tags.
// For FLAC, either LYRICS or UNSYNCEDLYRICS counts. For MP3 and M4A, the
// normalised LYRICS key (USLT / ©lyr) is checked.
func hasLyrics(path string) (bool, error) {
	e, err := readEmbedded(path)
	if err != nil {
		return false, err
	}
	return e.Plain != "" || e.Synced != "", nil
}

// embeddedLyrics is what a file's tags currently hold, by role rather than by
// tag name, so that callers don't each need to know how the formats differ.
type embeddedLyrics struct {
	Plain      string // plain, unsynced lyrics
	Synced     string // line-synced LRC text (FLAC only)
	Lyricsfile string // raw Lyricsfile YAML (FLAC only), see LyricsfileTag
	SourceURL  string // lrclib.net page of the source record (FLAC only)
}

// readEmbedded reads the lyrics-related tags of path. For FLAC, Synced is
// LYRICS and Plain is UNSYNCEDLYRICS; for MP3 and M4A, Plain is the normalised
// LYRICS key (USLT / ©lyr) and the other fields are always empty, since those
// formats don't store them.
func readEmbedded(path string) (embeddedLyrics, error) {
	f, err := taglib.OpenReadOnly(path)
	if err != nil {
		return embeddedLyrics{}, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	tags := f.Tags()
	first := func(key string) string {
		if vals := tags[key]; len(vals) > 0 {
			return vals[0]
		}
		return ""
	}

	if strings.ToLower(filepath.Ext(path)) == ".flac" {
		return embeddedLyrics{
			Plain:      first("UNSYNCEDLYRICS"),
			Synced:     first(taglib.Lyrics),
			Lyricsfile: first(LyricsfileTag),
			SourceURL:  first(SourceURLTag),
		}, nil
	}
	return embeddedLyrics{Plain: first(taglib.Lyrics)}, nil
}

// embedInput is the LRCLIB data embedLyrics may write to a file's tags.
type embedInput struct {
	Synced    string // line-synced LRC text
	Plain     string // plain, unsynced text
	SourceURL string // lrclib.net page of the record, for SourceURLTag

	// WordSynced reports whether the record has word-level timings, and
	// Lyricsfile is its raw Lyricsfile YAML document, for LyricsfileTag.
	WordSynced bool
	Lyricsfile string
}

// planTags returns the tags embedLyrics would write for in to the file at
// path, or an error if in can't be embedded (see embedLyrics). A tag whose
// value is an empty slice is one that would be removed. The map is empty when
// there is nothing to write for path's format.
//
// It is separate from embedLyrics, which just writes the result, so that
// anything wanting to know what a write would do (the lyrics diff command) asks
// the same code that does the writing and can't drift from it.
func planTags(path string, in embedInput) (map[string][]string, error) {
	tags := make(map[string][]string)

	switch strings.ToLower(filepath.Ext(path)) {
	case ".flac":
		if in.Synced != "" {
			tags[taglib.Lyrics] = []string{standardizeLRC(in.Synced)}

			// An empty value makes go-taglib delete the key, which is how
			// a tag left over from the file's previous lyrics is removed.
			tags[SourceURLTag] = []string{}
			if in.SourceURL != "" {
				tags[SourceURLTag] = []string{in.SourceURL}
			}

			tags[LyricsfileTag] = []string{}
			if in.WordSynced {
				if err := validateLyricsfile(in.Lyricsfile); err != nil {
					return nil, err
				}
				tags[LyricsfileTag] = []string{in.Lyricsfile}
			}
		}
		if in.Plain != "" {
			tags["UNSYNCEDLYRICS"] = []string{in.Plain}
		}
	default: // .mp3, .m4a
		// Synced-only tracks are not embedded for these formats per design.
		if in.Plain != "" {
			tags[taglib.Lyrics] = []string{in.Plain}
		}
	}

	return tags, nil
}

// embedLyrics writes synced and/or plain lyrics to the audio file at path.
// Format-specific rules from the design doc:
//
//   - FLAC: synced LRC text -> LYRICS (after standardisation), plain -> UNSYNCEDLYRICS.
//     Whenever synced lyrics are written, SourceURLTag and LyricsfileTag are
//     written or removed in the same write, so that they always describe the
//     same record as LYRICS and can't be left over from a different one:
//     SourceURLTag is set when in.SourceURL is non-empty, and LyricsfileTag is
//     set when in.WordSynced is true. A word-synced record whose Lyricsfile is
//     empty or isn't valid YAML is an error and nothing at all is written, so
//     that the file still has no lyrics and a later run retries it, instead of
//     being skipped for already having lyrics but missing the word-sync data.
//     Plain-only results don't touch either tag.
//   - MP3/M4A: plain text -> LYRICS only. Tracks with only synced lyrics
//     available are not embedded (returns false, nil), and the Lyricsfile is
//     ignored, including when it is invalid.
//
// Returns (true, nil) when at least one tag was written, (false, nil) when
// nothing was written, and (false, err) on failure.
func embedLyrics(path string, in embedInput) (bool, error) {
	tags, err := planTags(path, in)
	if err != nil {
		return false, err
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

// validateLyricsfile reports why doc can't be stored in LyricsfileTag: it's
// empty, isn't valid YAML, has a duplicate key (which the Lyricsfile draft
// specification forbids and the YAML decoder rejects), or isn't a mapping at
// the top level. That is deliberately all it checks. The format is still a
// draft that LRCLIB doesn't enforce completely, so rejecting a document for
// breaking a rule of the specification could fail tracks whose lyrics are fine,
// and the document is stored verbatim either way; checking the version and
// the rest is for whatever later reads it.
func validateLyricsfile(doc string) error {
	if strings.TrimSpace(doc) == "" {
		return errors.New("lyricsfile: record has word sync but no lyricsfile")
	}
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(doc), &parsed); err != nil {
		return fmt.Errorf("lyricsfile: invalid YAML: %w", err)
	}
	if len(parsed) == 0 {
		return errors.New("lyricsfile: not a YAML mapping")
	}
	return nil
}
