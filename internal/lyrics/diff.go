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

package lyrics

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"go.senan.xyz/taglib"
)

// DiffStatus is the overall outcome of comparing one file's lyrics with what
// LRCLIB would give it now.
type DiffStatus uint8

const (
	// DiffSame means nothing a refetch would write differs in substance from
	// what the file has. A difference only in how the lyrics are formatted
	// doesn't count.
	DiffSame DiffStatus = iota
	// DiffChanged means a refetch would change the lyrics the file has.
	DiffChanged
	// DiffWordSync means the lyrics are the same, but LRCLIB's record has
	// word-synced data the file doesn't: a refetch would only add that.
	DiffWordSync
	// DiffNew means the file has no lyrics and LRCLIB has some for it.
	DiffNew
	// DiffNotFound means LRCLIB has nothing it could embed in this file, so a
	// refetch would change nothing.
	DiffNotFound
	// DiffFailed means the comparison couldn't be made; see TrackDiff.Reason.
	DiffFailed
)

// RecordState says whether the LRCLIB record a refetch would use is the one the
// file's lyrics came from.
type RecordState uint8

const (
	// RecordUnknown means the file doesn't say where its lyrics came from:
	// it has no LRCLIB_URL tag, as for files embedded before the tag existed
	// and all MP3 and M4A files.
	RecordUnknown RecordState = iota
	// RecordSame means the file's LRCLIB_URL is the record a refetch would use.
	RecordSame
	// RecordDifferent means the file's LRCLIB_URL is another record.
	RecordDifferent
)

// DefaultContext is how many unchanged lines of context a diff keeps around
// each change unless asked for more (FullContext) or fewer.
const DefaultContext = 3

// DiffOptions controls what Diff and DiffByID compare and in how much detail.
type DiffOptions struct {
	// Context is how many unchanged lines to keep around each change in the
	// diffs, or FullContext for every line. A zero value means none, not the
	// default; callers that want DefaultContext say so.
	Context int

	// Raw asks for a text diff of the two Lyricsfile documents in addition
	// to the summary of what differs between them.
	Raw bool

	// SkipLyricsfile compares as `lyrics --skip-lyricsfile` would write: the
	// record's Lyricsfile is ignored, so a stored one counts as being removed.
	SkipLyricsfile bool
}

// TrackDiff is the result of comparing one file's lyrics with what a refetch
// from LRCLIB would write. A part of it is nil when there was nothing to
// compare there: the file's format doesn't store that part, or LRCLIB's record
// has nothing for it, so a refetch would leave the file's alone.
type TrackDiff struct {
	Path   string
	Status DiffStatus

	// Reason explains a DiffNotFound or DiffFailed status.
	Reason string

	// EmbeddedURL is the file's LRCLIB_URL tag (empty if it has none) and
	// CandidateURL the page of the record a refetch would use. Record says how
	// they relate.
	EmbeddedURL  string
	CandidateURL string
	Record       RecordState

	// HadPlain, HadSynced and HadWord say which of the lyrics the file has now,
	// so that a diff against nothing can say the file has none.
	HadPlain, HadSynced, HadWord bool

	// Plain compares the plain lyrics, Synced the line-synced lyrics (FLAC
	// only), and Word the word-synced Lyricsfile (FLAC only).
	Plain  *TextDiff
	Synced *SyncedDiff
	Word   *WordDiff
}

// DiffSummary holds aggregate counts from a Diff run.
type DiffSummary struct {
	Same     int
	Changed  int
	WordSync int
	New      int
	NotFound int
	Failed   int
}

// Total is the number of tracks compared.
func (s DiffSummary) Total() int {
	return s.Same + s.Changed + s.WordSync + s.New + s.NotFound + s.Failed
}

// Add counts one track with the given status.
func (s *DiffSummary) Add(status DiffStatus) {
	switch status {
	case DiffSame:
		s.Same++
	case DiffChanged:
		s.Changed++
	case DiffWordSync:
		s.WordSync++
	case DiffNew:
		s.New++
	case DiffNotFound:
		s.NotFound++
	case DiffFailed:
		s.Failed++
	}
}

// Diff compares the lyrics of each track with what `lyrics --force` would
// write to it, fetching each from LRCLIB the same way Fetch does, and calls
// progress (if non-nil) with the result for every track as it is made. It
// never changes a file.
//
// A track that can't be compared is a DiffFailed result and doesn't stop the
// run. As with Fetch, the exception is LRCLIB rate limiting that persists after
// retrying: the track that hit it is reported as failed, the run stops, and
// Diff returns the summary so far along with an error wrapping ErrRateLimited.
func Diff(ctx context.Context, tracks []TrackInfo, opts DiffOptions, progress func(TrackDiff)) (DiffSummary, error) {
	return diff(ctx, newLrclibClient(), tracks, opts, progress)
}

// DiffByID is Diff for a single file against a specific LRCLIB track,
// identified by ID (see ParseID), the way FetchByID is for Fetch. An ID that
// doesn't exist on LRCLIB is an error.
func DiffByID(ctx context.Context, path string, id int, opts DiffOptions) (TrackDiff, error) {
	return diffByID(ctx, newLrclibClient(), path, id, opts)
}

// diff is the internal implementation of Diff, accepting an explicit client so
// that tests can inject a test server without network access.
func diff(ctx context.Context, c *lrclibClient, tracks []TrackInfo, opts DiffOptions, progress func(TrackDiff)) (DiffSummary, error) {
	var summary DiffSummary

	for _, track := range tracks {
		d, err := diffTrack(ctx, c, track, opts)
		summary.Add(d.Status)
		if progress != nil {
			progress(d)
		}
		if errors.Is(err, ErrRateLimited) {
			return summary, err
		}
	}
	return summary, nil
}

// diffTrack compares one track. The error is only ever one that should stop a
// run (see Diff); everything else is a DiffFailed result.
func diffTrack(ctx context.Context, c *lrclibClient, track TrackInfo, opts DiffOptions) (TrackDiff, error) {
	emb, err := readEmbedded(track.Path)
	if err != nil {
		return failedDiff(track.Path, fmt.Errorf("reading tags: %w", err)), nil
	}

	result, err := c.fetchForTrack(ctx, track.Title, track.Artist, track.Album, track.Duration)
	if err != nil {
		d := failedDiff(track.Path, err)
		if errors.Is(err, ErrRateLimited) {
			return d, err
		}
		return d, nil
	}
	return compareTrack(track.Path, emb, result, opts), nil
}

// diffByID is the internal implementation of DiffByID.
func diffByID(ctx context.Context, c *lrclibClient, path string, id int, opts DiffOptions) (TrackDiff, error) {
	emb, err := readEmbedded(path)
	if err != nil {
		return failedDiff(path, fmt.Errorf("reading tags: %w", err)), err
	}

	result, err := c.getByID(ctx, id)
	if err != nil {
		return failedDiff(path, err), err
	}
	if result == nil {
		err := fmt.Errorf("lrclib: no track with id %d", id)
		return failedDiff(path, err), err
	}
	return compareTrack(path, emb, result, opts), nil
}

func failedDiff(path string, err error) TrackDiff {
	// A failed request's error repeats the whole URL, which says nothing a
	// person reading the reason needs; the cause (no such host, connection
	// refused, a timeout) is what matters.
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	return TrackDiff{Path: path, Status: DiffFailed, Reason: err.Error()}
}

// compareTrack compares what a file holds with what writing result to it
// would leave there. result is nil when LRCLIB has no match.
//
// What the write would leave there is asked of planTags, the same code that
// does the writing, so that this can't disagree with `lyrics --force`: a tag
// planTags doesn't mention is one a refetch leaves alone, so it isn't compared.
func compareTrack(path string, emb embeddedLyrics, result *lrclibTrack, opts DiffOptions) TrackDiff {
	d := TrackDiff{
		Path:        path,
		EmbeddedURL: emb.SourceURL,
		HadPlain:    emb.Plain != "",
		HadSynced:   emb.Synced != "",
		HadWord:     emb.Lyricsfile != "",
	}

	if result == nil {
		d.Status, d.Reason = DiffNotFound, "no match on LRCLIB"
		return d
	}

	in := inputFor(result, Options{SkipLyricsfile: opts.SkipLyricsfile})
	d.CandidateURL = in.SourceURL

	plan, err := planTags(path, in)
	if err != nil {
		reason := err.Error()
		if !opts.SkipLyricsfile {
			reason += "; --skip-lyricsfile would embed the lyrics without it"
		}
		d.Status, d.Reason = DiffFailed, reason
		return d
	}
	if len(plan) == 0 {
		d.Status = DiffNotFound
		d.Reason = "LRCLIB has no plain lyrics to embed in this format"
		return d
	}

	first := func(key string) (string, bool) {
		vals, ok := plan[key]
		if !ok || len(vals) == 0 {
			return "", ok
		}
		return vals[0], true
	}

	isFLAC := strings.ToLower(filepath.Ext(path)) == ".flac"

	plainKey := taglib.Lyrics
	if isFLAC {
		plainKey = "UNSYNCEDLYRICS"
	}
	if plain, ok := first(plainKey); ok && plain != "" {
		td := diffPlain(emb.Plain, plain, opts.Context)
		d.Plain = &td
	}

	if isFLAC {
		if synced, ok := first(taglib.Lyrics); ok && synced != "" {
			sd := diffSynced(emb.Synced, synced, opts.Context)
			d.Synced = &sd
		}
		// The Lyricsfile is only part of a write that replaces the synced
		// lyrics, which planTags says by mentioning the tag (as a removal
		// when the record has no word sync).
		if candidate, ok := first(LyricsfileTag); ok {
			wd := diffLyricsfile(emb.Lyricsfile, candidate, opts.Context, opts.Raw)
			d.Word = &wd
		}

		switch {
		case emb.SourceURL == "":
			d.Record = RecordUnknown
		case emb.SourceURL == in.SourceURL:
			d.Record = RecordSame
		default:
			d.Record = RecordDifferent
		}
	}

	d.Status = d.status(emb)
	return d
}

// status works out the overall DiffStatus from the compared parts.
func (d TrackDiff) status(emb embeddedLyrics) DiffStatus {
	if emb.Plain == "" && emb.Synced == "" {
		return DiffNew
	}

	changed := (d.Plain != nil && !d.Plain.Same) || (d.Synced != nil && !d.Synced.Same())
	if d.Word != nil && d.Word.Differs() && d.Word.State != WordAvailable {
		changed = true
	}
	if changed {
		return DiffChanged
	}
	if d.Word != nil && d.Word.State == WordAvailable {
		return DiffWordSync
	}
	return DiffSame
}
