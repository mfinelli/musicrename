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
	"crypto/md5"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.senan.xyz/taglib"

	"github.com/mfinelli/musicrename/internal/testutil"
)

// diffRecord is the LRCLIB record most diff tests start from: it has line-synced
// and plain lyrics, and word sync.
var diffRecord = lrclibResponse{
	ID: 7, TrackName: "Test Track", ArtistName: "Test Artist",
	AlbumName: "Test Album", Duration: 1,
	PlainLyrics:  "Hello world\nSecond line",
	SyncedLyrics: "[00:01.00]Hello world\n[00:05.00]Second line",
	HasWordSync:  true, Lyricsfile: lfBase,
}

// asTrack converts a test response to the type the code under test reads.
func asTrack(r lrclibResponse) *lrclibTrack {
	return &lrclibTrack{
		ID: r.ID, TrackName: r.TrackName, ArtistName: r.ArtistName, AlbumName: r.AlbumName,
		Duration: r.Duration, Instrumental: r.Instrumental, PlainLyrics: r.PlainLyrics,
		SyncedLyrics: r.SyncedLyrics, HasWordSync: r.HasWordSync, Lyricsfile: r.Lyricsfile,
	}
}

// embeddedFrom makes a file of the given name and embeds r into it exactly as
// `lyrics` would, so that a diff against r has nothing to find.
func embeddedFrom(t *testing.T, name string, r lrclibResponse) string {
	t.Helper()
	path := testutil.MakeAudioFile(t, t.TempDir(), name, nil)
	_, err := embedLyrics(path, inputFor(asTrack(r), Options{}))
	require.NoError(t, err)
	return path
}

func diffTrackInfo(path string) TrackInfo {
	return TrackInfo{Path: path, Title: "T", Artist: "A", Album: "B", Duration: time.Second}
}

// diffOne diffs path against a server returning r for every request.
func diffOne(t *testing.T, path string, r lrclibResponse, opts DiffOptions) TrackDiff {
	t.Helper()
	srv := makeLrclibServer(t, r)
	defer srv.Close()

	var got []TrackDiff
	_, err := diff(context.Background(), testFetchClient(srv.URL), []TrackInfo{diffTrackInfo(path)}, opts,
		func(d TrackDiff) { got = append(got, d) })
	require.NoError(t, err)
	require.Len(t, got, 1)
	return got[0]
}

func fileMD5(t *testing.T, path string) [16]byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return md5.Sum(data)
}

func TestDiff_Statuses(t *testing.T) {
	opts := DiffOptions{Context: DefaultContext}

	t.Run("lyrics the file already has are Same", func(t *testing.T) {
		path := embeddedFrom(t, "track.flac", diffRecord)
		d := diffOne(t, path, diffRecord, opts)

		assert.Equal(t, DiffSame, d.Status)
		assert.Equal(t, RecordSame, d.Record)
		require.NotNil(t, d.Plain)
		assert.True(t, d.Plain.Same)
		require.NotNil(t, d.Synced)
		assert.True(t, d.Synced.Same())
		require.NotNil(t, d.Word)
		assert.Equal(t, WordIdentical, d.Word.State)
		assert.True(t, d.HadPlain && d.HadSynced && d.HadWord)
	})

	t.Run("reworded synced lyrics are Changed", func(t *testing.T) {
		path := embeddedFrom(t, "track.flac", diffRecord)
		updated := diffRecord
		updated.SyncedLyrics = "[00:01.00]Hello world\n[00:05.00]Second lines"
		updated.HasWordSync, updated.Lyricsfile = false, ""

		d := diffOne(t, path, updated, opts)
		assert.Equal(t, DiffChanged, d.Status)
		require.NotNil(t, d.Synced)
		assert.False(t, d.Synced.Text.Same)
	})

	t.Run("retimed synced lyrics are Changed, with no text change", func(t *testing.T) {
		path := embeddedFrom(t, "track.flac", diffRecord)
		updated := diffRecord
		updated.SyncedLyrics = "[00:01.30]Hello world\n[00:05.30]Second line"

		d := diffOne(t, path, updated, opts)
		assert.Equal(t, DiffChanged, d.Status)
		require.NotNil(t, d.Synced)
		assert.True(t, d.Synced.Text.Same)
		assert.True(t, d.Synced.Timing.Uniform)
	})

	t.Run("changed plain lyrics are Changed", func(t *testing.T) {
		path := embeddedFrom(t, "track.flac", diffRecord)
		updated := diffRecord
		updated.PlainLyrics = "Hello world\nSecond lines"

		d := diffOne(t, path, updated, opts)
		assert.Equal(t, DiffChanged, d.Status)
		require.NotNil(t, d.Plain)
		assert.False(t, d.Plain.Same)
	})

	t.Run("word sync the file lacks is WordSync, when nothing else differs", func(t *testing.T) {
		lineSynced := diffRecord
		lineSynced.HasWordSync, lineSynced.Lyricsfile = false, ""
		path := embeddedFrom(t, "track.flac", lineSynced)

		d := diffOne(t, path, diffRecord, opts)
		assert.Equal(t, DiffWordSync, d.Status)
		require.NotNil(t, d.Word)
		assert.Equal(t, WordAvailable, d.Word.State)
		assert.False(t, d.HadWord)
	})

	t.Run("word sync available with other changes is Changed", func(t *testing.T) {
		lineSynced := diffRecord
		lineSynced.HasWordSync, lineSynced.Lyricsfile = false, ""
		path := embeddedFrom(t, "track.flac", lineSynced)

		updated := diffRecord
		updated.PlainLyrics = "Hello world\nSecond lines"
		d := diffOne(t, path, updated, opts)
		assert.Equal(t, DiffChanged, d.Status)
		assert.Equal(t, WordAvailable, d.Word.State)
	})

	t.Run("a different Lyricsfile is Changed", func(t *testing.T) {
		path := embeddedFrom(t, "track.flac", diffRecord)
		updated := diffRecord
		updated.Lyricsfile = strings.Replace(lfBase, "start_ms: 1000", "start_ms: 1200", 1)

		d := diffOne(t, path, updated, opts)
		assert.Equal(t, DiffChanged, d.Status)
		require.NotNil(t, d.Word)
		assert.Equal(t, WordChanged, d.Word.State)
		assert.True(t, d.Plain.Same)
		assert.True(t, d.Synced.Same())
	})

	t.Run("a record that has lost its word sync would remove the stored one", func(t *testing.T) {
		path := embeddedFrom(t, "track.flac", diffRecord)
		updated := diffRecord
		updated.HasWordSync, updated.Lyricsfile = false, ""

		d := diffOne(t, path, updated, opts)
		assert.Equal(t, DiffChanged, d.Status)
		assert.Equal(t, WordRemoved, d.Word.State)
	})

	t.Run("a file with no lyrics is New", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		d := diffOne(t, path, diffRecord, opts)
		assert.Equal(t, DiffNew, d.Status)
		assert.False(t, d.HadPlain || d.HadSynced || d.HadWord)
	})

	t.Run("no match on LRCLIB is NotFound", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/search" {
				w.Write([]byte("[]"))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		path := embeddedFrom(t, "track.flac", diffRecord)
		var got TrackDiff
		_, err := diff(context.Background(), testFetchClient(srv.URL), []TrackInfo{diffTrackInfo(path)}, opts,
			func(d TrackDiff) { got = d })
		require.NoError(t, err)
		assert.Equal(t, DiffNotFound, got.Status)
		assert.NotEmpty(t, got.Reason)
	})

	t.Run("a file whose lyrics only differ in formatting is Same", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		require.NoError(t, taglib.WriteTags(path, map[string][]string{
			taglib.Lyrics:    {"[ar:Someone]\n[0:01.0] Hello world\n[00:05.00]   Second line\n"},
			"UNSYNCEDLYRICS": {"Hello world  \r\nSecond line\r\n"},
			LyricsfileTag:    {lfBase},
		}, 0))

		d := diffOne(t, path, diffRecord, opts)
		assert.Equal(t, DiffSame, d.Status)
		require.NotNil(t, d.Synced)
		assert.True(t, d.Synced.FormatOnly)
	})
}

func TestDiff_WhatARefetchWouldWrite(t *testing.T) {
	opts := DiffOptions{Context: DefaultContext}

	t.Run("lyrics a refetch would leave alone are not compared", func(t *testing.T) {
		// The file has plain lyrics and the record doesn't: a refetch doesn't
		// touch UNSYNCEDLYRICS then, so it isn't a difference.
		path := embeddedFrom(t, "track.flac", diffRecord)
		syncedOnly := diffRecord
		syncedOnly.PlainLyrics = ""

		d := diffOne(t, path, syncedOnly, opts)
		assert.Nil(t, d.Plain)
		assert.Equal(t, DiffSame, d.Status)
	})

	t.Run("a record with plain lyrics only changes nothing synced", func(t *testing.T) {
		path := embeddedFrom(t, "track.flac", diffRecord)
		plainOnly := diffRecord
		plainOnly.SyncedLyrics, plainOnly.HasWordSync, plainOnly.Lyricsfile = "", false, ""

		d := diffOne(t, path, plainOnly, opts)
		assert.Nil(t, d.Synced)
		assert.Nil(t, d.Word, "a refetch keeps the synced lyrics, and so the Lyricsfile that describes them")
		assert.Equal(t, DiffSame, d.Status)
	})

	t.Run("MP3 only compares plain lyrics", func(t *testing.T) {
		path := embeddedFrom(t, "track.mp3", diffRecord)
		updated := diffRecord
		updated.PlainLyrics = "Hello world\nSecond lines"

		d := diffOne(t, path, updated, opts)
		assert.Equal(t, DiffChanged, d.Status)
		require.NotNil(t, d.Plain)
		assert.Nil(t, d.Synced)
		assert.Nil(t, d.Word)
		assert.Equal(t, RecordUnknown, d.Record)
	})

	t.Run("MP3 against a record with only synced lyrics has nothing to embed", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.mp3", nil)
		syncedOnly := diffRecord
		syncedOnly.PlainLyrics = ""

		d := diffOne(t, path, syncedOnly, opts)
		assert.Equal(t, DiffNotFound, d.Status)
		assert.NotEmpty(t, d.Reason)
	})

	t.Run("an invalid Lyricsfile is Failed, and says how to get past it", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		bad := diffRecord
		bad.Lyricsfile = "lines: [unclosed\n"

		d := diffOne(t, path, bad, opts)
		assert.Equal(t, DiffFailed, d.Status)
		assert.Contains(t, d.Reason, "lyricsfile")
		assert.Contains(t, d.Reason, "--skip-lyricsfile")
	})

	t.Run("with SkipLyricsfile the Lyricsfile is ignored, so a stored one would go", func(t *testing.T) {
		skip := DiffOptions{Context: DefaultContext, SkipLyricsfile: true}

		bad := diffRecord
		bad.Lyricsfile = "lines: [unclosed\n"
		fresh := testutil.MakeAudioFile(t, t.TempDir(), "fresh.flac", nil)
		d := diffOne(t, fresh, bad, skip)
		assert.Equal(t, DiffNew, d.Status, "no longer fails")
		require.NotNil(t, d.Word)
		assert.Equal(t, WordNone, d.Word.State)

		stored := embeddedFrom(t, "stored.flac", diffRecord)
		d = diffOne(t, stored, diffRecord, skip)
		assert.Equal(t, DiffChanged, d.Status)
		assert.Equal(t, WordRemoved, d.Word.State)
	})
}

func TestDiff_Record(t *testing.T) {
	opts := DiffOptions{Context: DefaultContext}

	t.Run("a file without a source URL doesn't say", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		require.NoError(t, taglib.WriteTags(path, map[string][]string{
			taglib.Lyrics: {"[00:01.00]Hello world\n[00:05.00]Second line"},
		}, 0))
		d := diffOne(t, path, diffRecord, opts)
		assert.Equal(t, RecordUnknown, d.Record)
		assert.Empty(t, d.EmbeddedURL)
		assert.Equal(t, "https://lrclib.net/tracks/7", d.CandidateURL)
	})

	t.Run("the same record", func(t *testing.T) {
		d := diffOne(t, embeddedFrom(t, "track.flac", diffRecord), diffRecord, opts)
		assert.Equal(t, RecordSame, d.Record)
		assert.Equal(t, d.CandidateURL, d.EmbeddedURL)
	})

	t.Run("a different record", func(t *testing.T) {
		other := diffRecord
		other.ID = 99
		d := diffOne(t, embeddedFrom(t, "track.flac", other), diffRecord, opts)
		assert.Equal(t, RecordDifferent, d.Record)
		assert.Equal(t, "https://lrclib.net/tracks/99", d.EmbeddedURL)
		assert.Equal(t, "https://lrclib.net/tracks/7", d.CandidateURL)
		// Which record it is doesn't make the lyrics themselves differ.
		assert.Equal(t, DiffSame, d.Status)
	})
}

func TestDiff_Options(t *testing.T) {
	t.Run("never changes the file", func(t *testing.T) {
		path := embeddedFrom(t, "track.flac", diffRecord)
		before := fileMD5(t, path)

		updated := diffRecord
		updated.PlainLyrics = "Totally\ndifferent"
		updated.SyncedLyrics = "[00:02.00]Totally\n[00:04.00]different"
		d := diffOne(t, path, updated, DiffOptions{Context: DefaultContext, Raw: true})
		require.Equal(t, DiffChanged, d.Status)

		assert.Equal(t, before, fileMD5(t, path))
	})

	t.Run("Context sets how much of the lyrics a diff keeps", func(t *testing.T) {
		var plain, synced []string
		for i := 1; i <= 30; i++ {
			plain = append(plain, fmt.Sprintf("line %d", i))
			synced = append(synced, fmt.Sprintf("[00:%02d.00]line %d", i, i))
		}
		long := diffRecord
		long.PlainLyrics, long.SyncedLyrics = strings.Join(plain, "\n"), strings.Join(synced, "\n")
		long.HasWordSync, long.Lyricsfile = false, ""
		path := embeddedFrom(t, "track.flac", long)

		updated := long
		plain[14] = "line 15, changed"
		updated.PlainLyrics = strings.Join(plain, "\n")

		few := diffOne(t, path, updated, DiffOptions{Context: 2})
		require.NotNil(t, few.Plain)
		require.Len(t, few.Plain.Hunks, 1)
		assert.Len(t, few.Plain.Hunks[0].Lines, 6) // 2 + deleted + inserted + 2

		full := diffOne(t, path, updated, DiffOptions{Context: FullContext})
		require.Len(t, full.Plain.Hunks, 1)
		assert.Len(t, full.Plain.Hunks[0].Lines, 31)
	})

	t.Run("Raw asks for the text diff of the Lyricsfile", func(t *testing.T) {
		path := embeddedFrom(t, "track.flac", diffRecord)
		updated := diffRecord
		updated.Lyricsfile = strings.Replace(lfBase, "'en'", "'fr'", 1)

		without := diffOne(t, path, updated, DiffOptions{Context: DefaultContext})
		assert.Empty(t, without.Word.Raw.Hunks)

		with := diffOne(t, path, updated, DiffOptions{Context: DefaultContext, Raw: true})
		assert.False(t, with.Word.Raw.Same)
	})
}

func TestDiff_Run(t *testing.T) {
	opts := DiffOptions{Context: DefaultContext}

	t.Run("reports every track and counts them", func(t *testing.T) {
		srv := makeLrclibServer(t, diffRecord)
		defer srv.Close()

		same := embeddedFrom(t, "same.flac", diffRecord)
		fresh := testutil.MakeAudioFile(t, t.TempDir(), "new.flac", nil)
		stale := embeddedFrom(t, "stale.flac", func() lrclibResponse {
			r := diffRecord
			r.PlainLyrics = "Something\nelse"
			return r
		}())
		missing := "/nonexistent/missing.flac"

		var order []string
		summary, err := diff(context.Background(), testFetchClient(srv.URL),
			[]TrackInfo{diffTrackInfo(same), diffTrackInfo(fresh), diffTrackInfo(stale), diffTrackInfo(missing)}, opts,
			func(d TrackDiff) { order = append(order, d.Path) })
		require.NoError(t, err)

		assert.Equal(t, DiffSummary{Same: 1, New: 1, Changed: 1, Failed: 1}, summary)
		assert.Equal(t, 4, summary.Total())
		assert.Equal(t, []string{same, fresh, stale, missing}, order)
	})

	t.Run("an unreadable file is a failed result, not an error", func(t *testing.T) {
		srv := makeLrclibServer(t, diffRecord)
		defer srv.Close()

		var got TrackDiff
		summary, err := diff(context.Background(), testFetchClient(srv.URL),
			[]TrackInfo{diffTrackInfo("/nonexistent/x.flac")}, opts, func(d TrackDiff) { got = d })
		require.NoError(t, err)
		assert.Equal(t, DiffFailed, got.Status)
		assert.Contains(t, got.Reason, "reading tags")
		assert.Equal(t, 1, summary.Failed)
	})

	t.Run("a server error is a failed result and the run goes on", func(t *testing.T) {
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		a := testutil.MakeAudioFile(t, t.TempDir(), "a.flac", nil)
		b := testutil.MakeAudioFile(t, t.TempDir(), "b.flac", nil)
		summary, err := diff(context.Background(), testFetchClient(srv.URL),
			[]TrackInfo{diffTrackInfo(a), diffTrackInfo(b)}, opts, nil)
		require.NoError(t, err)
		assert.Equal(t, DiffSummary{Failed: 2}, summary)
		assert.Equal(t, int32(2), hits.Load())
	})

	t.Run("a failed request's reason leaves out the URL", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		base := srv.URL
		srv.Close() // so the request is refused

		path := testutil.MakeAudioFile(t, t.TempDir(), "a.flac", nil)
		var got TrackDiff
		_, err := diff(context.Background(), testFetchClient(base), []TrackInfo{diffTrackInfo(path)}, opts,
			func(d TrackDiff) { got = d })
		require.NoError(t, err)
		assert.Equal(t, DiffFailed, got.Status)
		assert.NotContains(t, got.Reason, "track_name")
		assert.NotContains(t, got.Reason, base)
		assert.NotEmpty(t, got.Reason)
	})

	t.Run("persistent rate limiting stops the run", func(t *testing.T) {
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer srv.Close()

		var tracks []TrackInfo
		for _, name := range []string{"1.flac", "2.flac", "3.flac"} {
			tracks = append(tracks, diffTrackInfo(testutil.MakeAudioFile(t, t.TempDir(), name, nil)))
		}

		var reported int
		summary, err := diff(context.Background(), testFetchClient(srv.URL), tracks, opts, func(TrackDiff) { reported++ })
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrRateLimited)
		assert.Equal(t, DiffSummary{Failed: 1}, summary)
		assert.Equal(t, 1, reported)
		assert.Equal(t, int32(1+lrclibMaxRetries), hits.Load(), "no requests for the other tracks")
	})

	t.Run("no tracks, no results", func(t *testing.T) {
		summary, err := diff(context.Background(), testFetchClient("http://unused.invalid"), nil, opts, nil)
		require.NoError(t, err)
		assert.Equal(t, DiffSummary{}, summary)
	})
}

func TestDiffByID(t *testing.T) {
	opts := DiffOptions{Context: DefaultContext}

	t.Run("compares against the record asked for", func(t *testing.T) {
		srv := makeLrclibByIDServer(t, 7, diffRecord)
		defer srv.Close()

		path := embeddedFrom(t, "track.flac", diffRecord)
		d, err := diffByID(context.Background(), testFetchClient(srv.URL), path, 7, opts)
		require.NoError(t, err)
		assert.Equal(t, DiffSame, d.Status)
		assert.Equal(t, RecordSame, d.Record)
	})

	t.Run("finds the differences from that record", func(t *testing.T) {
		updated := diffRecord
		updated.ID = 8
		updated.PlainLyrics = "Another\nversion"
		srv := makeLrclibByIDServer(t, 8, updated)
		defer srv.Close()

		path := embeddedFrom(t, "track.flac", diffRecord)
		d, err := diffByID(context.Background(), testFetchClient(srv.URL), path, 8, opts)
		require.NoError(t, err)
		assert.Equal(t, DiffChanged, d.Status)
		assert.Equal(t, RecordDifferent, d.Record)
	})

	t.Run("an ID that doesn't exist is an error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		d, err := diffByID(context.Background(), testFetchClient(srv.URL), path, 999, opts)
		assert.Error(t, err)
		assert.Equal(t, DiffFailed, d.Status)
	})

	t.Run("an unreadable file is an error", func(t *testing.T) {
		srv := makeLrclibByIDServer(t, 7, diffRecord)
		defer srv.Close()

		_, err := diffByID(context.Background(), testFetchClient(srv.URL), "/nonexistent/x.flac", 7, opts)
		assert.Error(t, err)
	})
}
