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
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.senan.xyz/taglib"
	"golang.org/x/time/rate"

	"github.com/mfinelli/musicrename/internal/testutil"
)

// readLyricsTags opens path and returns the values of LYRICS and UNSYNCEDLYRICS.
func readLyricsTags(t *testing.T, path string) (lyricsTag, unsyncedTag string) {
	t.Helper()
	f, err := taglib.OpenReadOnly(path)
	require.NoError(t, err)
	defer f.Close()
	tags := f.Tags()
	if vals := tags[taglib.Lyrics]; len(vals) > 0 {
		lyricsTag = vals[0]
	}
	if vals := tags["UNSYNCEDLYRICS"]; len(vals) > 0 {
		unsyncedTag = vals[0]
	}
	return
}

// readTag opens path and returns the first value of the given tag, or "" if
// the tag is absent.
func readTag(t *testing.T, path, key string) string {
	t.Helper()
	f, err := taglib.OpenReadOnly(path)
	require.NoError(t, err)
	defer f.Close()
	if vals := f.Tags()[key]; len(vals) > 0 {
		return vals[0]
	}
	return ""
}

// lrclibResponse mirrors the unexported lrclibTrack for use in test handlers.
type lrclibResponse struct {
	ID           int     `json:"id"`
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	AlbumName    string  `json:"albumName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  string  `json:"plainLyrics"`
	SyncedLyrics string  `json:"syncedLyrics"`
	HasWordSync  bool    `json:"hasWordSync"`
	Lyricsfile   string  `json:"lyricsfile"`
}

// wordSyncedYAML is a small, valid word-synced Lyricsfile document, modelled
// on the example in the draft specification.
const wordSyncedYAML = `version: '1.0'

metadata:
  title: 'Test Track'
  artist: 'Test Artist'
  duration_ms: 6000

lines:
  - text: 'Hello World'
    start_ms: 1000
    end_ms: 5000
    words:
      - text: 'Hello '
        start_ms: 1000
        end_ms: 3000
      - text: 'World'
        start_ms: 3000
        end_ms: 5000

plain: |
  Hello
  World
`

func testFetchClient(base string) *lrclibClient {
	return &lrclibClient{
		http:    &http.Client{},
		limiter: rate.NewLimiter(rate.Inf, 1),
		base:    base,
		sleep:   noSleep,
	}
}

func TestHasLyrics(t *testing.T) {
	t.Run("FLAC with LYRICS tag returns true", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		require.NoError(t, taglib.WriteTags(path, map[string][]string{
			taglib.Lyrics: {"[00:01.00]Hello"},
		}, 0))
		got, err := hasLyrics(path)
		require.NoError(t, err)
		assert.True(t, got)
	})

	t.Run("FLAC with UNSYNCEDLYRICS tag returns true", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		require.NoError(t, taglib.WriteTags(path, map[string][]string{
			"UNSYNCEDLYRICS": {"Hello world"},
		}, 0))
		got, err := hasLyrics(path)
		require.NoError(t, err)
		assert.True(t, got)
	})

	t.Run("FLAC with no lyrics returns false", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		got, err := hasLyrics(path)
		require.NoError(t, err)
		assert.False(t, got)
	})

	t.Run("MP3 with LYRICS tag returns true", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.mp3", nil)
		require.NoError(t, taglib.WriteTags(path, map[string][]string{
			taglib.Lyrics: {"Hello world"},
		}, 0))
		got, err := hasLyrics(path)
		require.NoError(t, err)
		assert.True(t, got)
	})

	t.Run("MP3 with no lyrics returns false", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.mp3", nil)
		got, err := hasLyrics(path)
		require.NoError(t, err)
		assert.False(t, got)
	})

	t.Run("M4A with LYRICS tag returns true", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.m4a", nil)
		require.NoError(t, taglib.WriteTags(path, map[string][]string{
			taglib.Lyrics: {"Hello world"},
		}, 0))
		got, err := hasLyrics(path)
		require.NoError(t, err)
		assert.True(t, got)
	})

	t.Run("non-existent file returns error", func(t *testing.T) {
		_, err := hasLyrics("/nonexistent/track.flac")
		assert.Error(t, err)
	})
}

func TestEmbedLyrics(t *testing.T) {
	synced := "[00:01.00] Hello\n[00:05.00] World"
	plain := "Hello\nWorld"

	t.Run("FLAC embeds synced into LYRICS and plain into UNSYNCEDLYRICS", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		embedded, err := embedLyrics(path, embedInput{Synced: synced, Plain: plain})
		require.NoError(t, err)
		assert.True(t, embedded)

		lyricsTag, unsyncedTag := readLyricsTags(t, path)
		// LYRICS gets the standardized LRC (space after ] stripped)
		assert.Equal(t, "[00:01.00]Hello\n[00:05.00]World", lyricsTag)
		assert.Equal(t, plain, unsyncedTag)
	})

	t.Run("FLAC with synced only embeds LYRICS, leaves UNSYNCEDLYRICS empty", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		embedded, err := embedLyrics(path, embedInput{Synced: synced})
		require.NoError(t, err)
		assert.True(t, embedded)

		lyricsTag, unsyncedTag := readLyricsTags(t, path)
		assert.NotEmpty(t, lyricsTag)
		assert.Empty(t, unsyncedTag)
	})

	t.Run("FLAC with plain only embeds UNSYNCEDLYRICS, leaves LYRICS empty", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		embedded, err := embedLyrics(path, embedInput{Plain: plain})
		require.NoError(t, err)
		assert.True(t, embedded)

		lyricsTag, unsyncedTag := readLyricsTags(t, path)
		assert.Empty(t, lyricsTag)
		assert.Equal(t, plain, unsyncedTag)
	})

	t.Run("MP3 embeds plain into LYRICS", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.mp3", nil)
		embedded, err := embedLyrics(path, embedInput{Synced: synced, Plain: plain})
		require.NoError(t, err)
		assert.True(t, embedded)

		lyricsTag, _ := readLyricsTags(t, path)
		assert.Equal(t, plain, lyricsTag)
	})

	t.Run("MP3 with synced only returns false without writing", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.mp3", nil)
		embedded, err := embedLyrics(path, embedInput{Synced: synced})
		require.NoError(t, err)
		assert.False(t, embedded)

		lyricsTag, _ := readLyricsTags(t, path)
		assert.Empty(t, lyricsTag)
	})

	t.Run("M4A embeds plain into LYRICS", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.m4a", nil)
		embedded, err := embedLyrics(path, embedInput{Plain: plain})
		require.NoError(t, err)
		assert.True(t, embedded)

		lyricsTag, _ := readLyricsTags(t, path)
		assert.Equal(t, plain, lyricsTag)
	})

	t.Run("FLAC writes the source URL alongside synced lyrics", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		embedded, err := embedLyrics(path, embedInput{Synced: synced, Plain: plain, SourceURL: "https://lrclib.net/tracks/42"})
		require.NoError(t, err)
		assert.True(t, embedded)
		assert.Equal(t, "https://lrclib.net/tracks/42", readTag(t, path, SourceURLTag))
	})

	t.Run("FLAC with plain only does not write the source URL", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		embedded, err := embedLyrics(path, embedInput{Plain: plain, SourceURL: "https://lrclib.net/tracks/42"})
		require.NoError(t, err)
		assert.True(t, embedded)
		assert.Empty(t, readTag(t, path, SourceURLTag))
	})

	t.Run("FLAC with an empty source URL does not write the tag", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		embedded, err := embedLyrics(path, embedInput{Synced: synced, Plain: plain})
		require.NoError(t, err)
		assert.True(t, embedded)
		assert.Empty(t, readTag(t, path, SourceURLTag))
	})

	t.Run("MP3 does not write the source URL", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.mp3", nil)
		embedded, err := embedLyrics(path, embedInput{Synced: synced, Plain: plain, SourceURL: "https://lrclib.net/tracks/42"})
		require.NoError(t, err)
		assert.True(t, embedded)
		assert.Empty(t, readTag(t, path, SourceURLTag))
	})

	t.Run("both synced and plain empty returns false without writing", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		embedded, err := embedLyrics(path, embedInput{})
		require.NoError(t, err)
		assert.False(t, embedded)
	})
}

func TestEmbedLyrics_Lyricsfile(t *testing.T) {
	synced := "[00:01.00] Hello\n[00:05.00] World"
	plain := "Hello\nWorld"
	const url = "https://lrclib.net/tracks/42"

	t.Run("FLAC stores a word-synced Lyricsfile verbatim", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		embedded, err := embedLyrics(path, embedInput{
			Synced: synced, Plain: plain, SourceURL: url,
			WordSynced: true, Lyricsfile: wordSyncedYAML,
		})
		require.NoError(t, err)
		assert.True(t, embedded)
		assert.Equal(t, wordSyncedYAML, readTag(t, path, LyricsfileTag))
		assert.Equal(t, url, readTag(t, path, SourceURLTag))
	})

	t.Run("FLAC does not store the Lyricsfile of a record without word sync", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		embedded, err := embedLyrics(path, embedInput{
			Synced: synced, Plain: plain, SourceURL: url,
			WordSynced: false, Lyricsfile: wordSyncedYAML,
		})
		require.NoError(t, err)
		assert.True(t, embedded)
		assert.Empty(t, readTag(t, path, LyricsfileTag))
	})

	t.Run("new synced lyrics without word sync remove a stale Lyricsfile and URL", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		require.NoError(t, taglib.WriteTags(path, map[string][]string{
			taglib.Lyrics: {"[00:01.00]old"},
			SourceURLTag:  {"https://lrclib.net/tracks/999"},
			LyricsfileTag: {wordSyncedYAML},
			taglib.Title:  {"Keep Me"},
		}, 0))

		embedded, err := embedLyrics(path, embedInput{Synced: synced, Plain: plain})
		require.NoError(t, err)
		assert.True(t, embedded)

		assert.Empty(t, readTag(t, path, LyricsfileTag), "the old record's Lyricsfile must not outlive its lyrics")
		assert.Empty(t, readTag(t, path, SourceURLTag), "the old record's URL must not outlive its lyrics")
		assert.Equal(t, "Keep Me", readTag(t, path, taglib.Title), "unrelated tags are left alone")
	})

	t.Run("new word-synced lyrics replace the previous Lyricsfile", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		require.NoError(t, taglib.WriteTags(path, map[string][]string{
			taglib.Lyrics: {"[00:01.00]old"},
			LyricsfileTag: {"version: '1.0'\nmetadata:\n  title: old\n  artist: old\n"},
		}, 0))

		_, err := embedLyrics(path, embedInput{
			Synced: synced, SourceURL: url, WordSynced: true, Lyricsfile: wordSyncedYAML,
		})
		require.NoError(t, err)
		assert.Equal(t, wordSyncedYAML, readTag(t, path, LyricsfileTag))
	})

	t.Run("plain-only results leave an existing Lyricsfile alone", func(t *testing.T) {
		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		require.NoError(t, taglib.WriteTags(path, map[string][]string{
			taglib.Lyrics: {"[00:01.00]old"},
			SourceURLTag:  {url},
			LyricsfileTag: {wordSyncedYAML},
		}, 0))

		embedded, err := embedLyrics(path, embedInput{Plain: plain})
		require.NoError(t, err)
		assert.True(t, embedded)

		// The synced lyrics weren't replaced, so what describes them stays.
		assert.Equal(t, wordSyncedYAML, readTag(t, path, LyricsfileTag))
		assert.Equal(t, url, readTag(t, path, SourceURLTag))
	})

	t.Run("an invalid Lyricsfile fails and writes nothing at all", func(t *testing.T) {
		for name, doc := range map[string]string{
			"malformed YAML": "version: '1.0'\nlines: [unclosed\n",
			"empty":          "",
			"whitespace":     "  \n\n",
			"not a mapping":  "- a\n- b\n",
			"duplicate keys": "version: '1.0'\nversion: '1.0'\nmetadata:\n  title: t\n  artist: a\n",
		} {
			t.Run(name, func(t *testing.T) {
				path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
				embedded, err := embedLyrics(path, embedInput{
					Synced: synced, Plain: plain, SourceURL: url,
					WordSynced: true, Lyricsfile: doc,
				})
				require.Error(t, err)
				assert.False(t, embedded)

				// Nothing was written, not even the lyrics that were fine, so
				// the file isn't skipped as already having lyrics next time.
				lyricsTag, unsyncedTag := readLyricsTags(t, path)
				assert.Empty(t, lyricsTag)
				assert.Empty(t, unsyncedTag)
				assert.Empty(t, readTag(t, path, SourceURLTag))
				assert.Empty(t, readTag(t, path, LyricsfileTag))
			})
		}
	})

	t.Run("an invalid Lyricsfile is not checked when it isn't stored", func(t *testing.T) {
		bad := "version: '1.0'\nlines: [unclosed\n"

		// Not word-synced: the document isn't used, so it can't fail the track.
		flac := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		embedded, err := embedLyrics(flac, embedInput{Synced: synced, WordSynced: false, Lyricsfile: bad})
		require.NoError(t, err)
		assert.True(t, embedded)

		// Plain-only: the synced lyrics (and so the document) aren't written.
		flac = testutil.MakeAudioFile(t, t.TempDir(), "plain.flac", nil)
		embedded, err = embedLyrics(flac, embedInput{Plain: plain, WordSynced: true, Lyricsfile: bad})
		require.NoError(t, err)
		assert.True(t, embedded)

		// MP3 and M4A never store it.
		for _, name := range []string{"track.mp3", "track.m4a"} {
			path := testutil.MakeAudioFile(t, t.TempDir(), name, nil)
			embedded, err := embedLyrics(path, embedInput{Synced: synced, Plain: plain, WordSynced: true, Lyricsfile: bad})
			require.NoError(t, err, name)
			assert.True(t, embedded, name)
			assert.Empty(t, readTag(t, path, LyricsfileTag), name)
		}
	})
}

func TestValidateLyricsfile(t *testing.T) {
	t.Run("accepts a valid word-synced document", func(t *testing.T) {
		assert.NoError(t, validateLyricsfile(wordSyncedYAML))
	})

	t.Run("accepts a document that breaks the draft's other rules", func(t *testing.T) {
		// Only YAML validity is checked: version and timing rules belong to
		// whatever reads the document later, and LRCLIB accepts files that
		// don't follow every rule in the draft.
		assert.NoError(t, validateLyricsfile("version: '9.9'\nlines:\n  - text: x\n    start_ms: -5\n"))
	})

	t.Run("rejects documents that don't parse as a YAML mapping", func(t *testing.T) {
		for name, doc := range map[string]string{
			"empty":          "",
			"only comments":  "# nothing here\n",
			"scalar":         "just text\n",
			"sequence":       "- a\n",
			"bad indent":     "a:\n  b: 1\n c: 2\n",
			"duplicate keys": "a: 1\na: 2\n",
		} {
			assert.Error(t, validateLyricsfile(doc), name)
		}
	})
}

func makeLrclibServer(t *testing.T, resp lrclibResponse) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/get":
			json.NewEncoder(w).Encode(resp)
		case "/search":
			json.NewEncoder(w).Encode([]lrclibResponse{resp})
		}
	}))
}

func TestFetch(t *testing.T) {
	syncedLRC := "[00:01.00] Hello\n[00:05.00] World"
	plainText := "Hello\nWorld"

	apiResp := lrclibResponse{
		ID: 1, TrackName: "Test Track", ArtistName: "Test Artist",
		AlbumName: "Test Album", Duration: 1,
		PlainLyrics: plainText, SyncedLyrics: syncedLRC,
	}

	t.Run("embeds lyrics for FLAC and reports StatusEmbedded", func(t *testing.T) {
		srv := makeLrclibServer(t, apiResp)
		defer srv.Close()

		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		tracks := []TrackInfo{{
			Path: path, Title: "Test Track", Artist: "Test Artist",
			Album: "Test Album", Duration: time.Second,
		}}

		var gotStatus LyricStatus
		summary, err := fetch(context.Background(), testFetchClient(srv.URL), tracks, Options{}, func(_ string, s LyricStatus) {
			gotStatus = s
		})
		require.NoError(t, err)
		assert.Equal(t, StatusEmbedded, gotStatus)
		assert.Equal(t, Summary{Embedded: 1}, summary)

		lyricsTag, unsyncedTag := readLyricsTags(t, path)
		assert.NotEmpty(t, lyricsTag)
		assert.Equal(t, plainText, unsyncedTag)
		assert.Equal(t, "https://lrclib.net/tracks/1", readTag(t, path, SourceURLTag))
	})

	t.Run("skips track that already has lyrics when not forcing", func(t *testing.T) {
		srv := makeLrclibServer(t, apiResp)
		defer srv.Close()

		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		require.NoError(t, taglib.WriteTags(path, map[string][]string{
			taglib.Lyrics: {"existing lyrics"},
		}, 0))

		tracks := []TrackInfo{{Path: path, Title: "T", Artist: "A", Album: "B", Duration: time.Second}}

		var gotStatus LyricStatus
		summary, err := fetch(context.Background(), testFetchClient(srv.URL), tracks, Options{}, func(_ string, s LyricStatus) {
			gotStatus = s
		})
		require.NoError(t, err)
		assert.Equal(t, StatusSkipped, gotStatus)
		assert.Equal(t, Summary{Skipped: 1}, summary)
		assert.Empty(t, readTag(t, path, SourceURLTag))
	})

	t.Run("overwrites existing lyrics when force is true", func(t *testing.T) {
		srv := makeLrclibServer(t, apiResp)
		defer srv.Close()

		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		require.NoError(t, taglib.WriteTags(path, map[string][]string{
			taglib.Lyrics: {"old lyrics"},
		}, 0))

		tracks := []TrackInfo{{Path: path, Title: "T", Artist: "A", Album: "B", Duration: time.Second}}
		summary, err := fetch(context.Background(), testFetchClient(srv.URL), tracks, Options{Force: true}, nil)
		require.NoError(t, err)
		assert.Equal(t, Summary{Embedded: 1}, summary)

		lyricsTag, _ := readLyricsTags(t, path)
		assert.NotEqual(t, "old lyrics", lyricsTag) // overwritten
		assert.Equal(t, "https://lrclib.net/tracks/1", readTag(t, path, SourceURLTag))
	})

	t.Run("force replaces a previously recorded source URL", func(t *testing.T) {
		srv := makeLrclibServer(t, apiResp)
		defer srv.Close()

		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		require.NoError(t, taglib.WriteTags(path, map[string][]string{
			taglib.Lyrics: {"old lyrics"},
			SourceURLTag:  {"https://lrclib.net/tracks/999"},
		}, 0))

		tracks := []TrackInfo{{Path: path, Title: "T", Artist: "A", Album: "B", Duration: time.Second}}
		_, err := fetch(context.Background(), testFetchClient(srv.URL), tracks, Options{Force: true}, nil)
		require.NoError(t, err)

		assert.Equal(t, "https://lrclib.net/tracks/1", readTag(t, path, SourceURLTag))
	})

	t.Run("does not write a source URL when the response has no ID", func(t *testing.T) {
		noID := apiResp
		noID.ID = 0
		srv := makeLrclibServer(t, noID)
		defer srv.Close()

		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		tracks := []TrackInfo{{Path: path, Title: "T", Artist: "A", Album: "B", Duration: time.Second}}
		summary, err := fetch(context.Background(), testFetchClient(srv.URL), tracks, Options{}, nil)
		require.NoError(t, err)
		assert.Equal(t, Summary{Embedded: 1}, summary)
		assert.Empty(t, readTag(t, path, SourceURLTag))
	})

	t.Run("reports StatusNotFound when LRCLIB returns no result", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/get":
				w.WriteHeader(http.StatusNotFound)
			case "/search":
				json.NewEncoder(w).Encode([]lrclibResponse{})
			}
		}))
		defer srv.Close()

		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		tracks := []TrackInfo{{Path: path, Title: "Unknown", Artist: "Nobody", Album: "Nothing", Duration: time.Second}}

		summary, err := fetch(context.Background(), testFetchClient(srv.URL), tracks, Options{}, nil)
		require.NoError(t, err)
		assert.Equal(t, Summary{NotFound: 1}, summary)
	})

	t.Run("reports StatusNotFound for MP3 when only synced lyrics available", func(t *testing.T) {
		srv := makeLrclibServer(t, lrclibResponse{
			ID: 1, SyncedLyrics: syncedLRC, PlainLyrics: "", // no plain
		})
		defer srv.Close()

		path := testutil.MakeAudioFile(t, t.TempDir(), "track.mp3", nil)
		tracks := []TrackInfo{{Path: path, Title: "T", Artist: "A", Album: "B", Duration: time.Second}}

		summary, err := fetch(context.Background(), testFetchClient(srv.URL), tracks, Options{}, nil)
		require.NoError(t, err)
		assert.Equal(t, Summary{NotFound: 1}, summary)
	})

	t.Run("reports StatusFailed on HTTP error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		tracks := []TrackInfo{{Path: path, Title: "T", Artist: "A", Album: "B", Duration: time.Second}}

		summary, err := fetch(context.Background(), testFetchClient(srv.URL), tracks, Options{}, nil)
		require.NoError(t, err) // Fetch itself does not error; failures are per-track
		assert.Equal(t, Summary{Failed: 1}, summary)
	})

	t.Run("processes multiple tracks and aggregates summary", func(t *testing.T) {
		callCount := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			if callCount == 1 {
				// First track: exact match
				json.NewEncoder(w).Encode(apiResp)
			} else {
				// Second track: not found
				switch r.URL.Path {
				case "/get":
					w.WriteHeader(http.StatusNotFound)
				case "/search":
					json.NewEncoder(w).Encode([]lrclibResponse{})
				}
			}
		}))
		defer srv.Close()

		dir := t.TempDir()
		path1 := testutil.MakeAudioFile(t, dir, "01 track.flac", nil)
		path2 := testutil.MakeAudioFile(t, dir, "02 track.flac", nil)

		tracks := []TrackInfo{
			{Path: path1, Title: "Found", Artist: "A", Album: "B", Duration: time.Second},
			{Path: path2, Title: "Missing", Artist: "A", Album: "B", Duration: time.Second},
		}

		summary, err := fetch(context.Background(), testFetchClient(srv.URL), tracks, Options{}, nil)
		require.NoError(t, err)
		assert.Equal(t, 1, summary.Embedded)
		assert.Equal(t, 1, summary.NotFound)
	})

	t.Run("progress callback receives correct path and status", func(t *testing.T) {
		srv := makeLrclibServer(t, apiResp)
		defer srv.Close()

		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		tracks := []TrackInfo{{Path: path, Title: "T", Artist: "A", Album: "B", Duration: time.Second}}

		var cbPath string
		var cbStatus LyricStatus
		_, err := fetch(context.Background(), testFetchClient(srv.URL), tracks, Options{}, func(p string, s LyricStatus) {
			cbPath = p
			cbStatus = s
		})
		require.NoError(t, err)
		assert.Equal(t, path, cbPath)
		assert.Equal(t, StatusEmbedded, cbStatus)
	})

	t.Run("nil progress callback is safe", func(t *testing.T) {
		srv := makeLrclibServer(t, apiResp)
		defer srv.Close()

		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		tracks := []TrackInfo{{Path: path, Title: "T", Artist: "A", Album: "B", Duration: time.Second}}

		_, err := fetch(context.Background(), testFetchClient(srv.URL), tracks, Options{}, nil)
		assert.NoError(t, err)
	})

	t.Run("empty track list returns zero summary without error", func(t *testing.T) {
		srv := makeLrclibServer(t, apiResp)
		defer srv.Close()

		summary, err := fetch(context.Background(), testFetchClient(srv.URL), nil, Options{}, nil)
		require.NoError(t, err)
		assert.Equal(t, Summary{}, summary)
	})

	t.Run("context cancellation is propagated and reported as failed", func(t *testing.T) {
		srv := makeLrclibServer(t, apiResp)
		defer srv.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		tracks := []TrackInfo{{Path: path, Title: "T", Artist: "A", Album: "B", Duration: time.Second}}

		summary, err := fetch(ctx, testFetchClient(srv.URL), tracks, Options{}, nil)
		require.NoError(t, err)
		// The cancelled context causes the HTTP request to fail -> StatusFailed
		assert.Equal(t, fmt.Sprintf("%d", summary.Embedded+summary.Failed+summary.Skipped+summary.NotFound), "1")
	})
}

func TestFetchRateLimited(t *testing.T) {
	t.Run("stops the run when LRCLIB keeps rate limiting", func(t *testing.T) {
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer srv.Close()

		dir := t.TempDir()
		var tracks []TrackInfo
		for _, name := range []string{"01.flac", "02.flac", "03.flac"} {
			path := testutil.MakeAudioFile(t, dir, name, nil)
			tracks = append(tracks, TrackInfo{
				Path: path, Title: name, Artist: "A", Album: "B", Duration: time.Second,
			})
		}

		var reported []string
		summary, err := fetch(context.Background(), testFetchClient(srv.URL), tracks, Options{}, func(path string, _ LyricStatus) {
			reported = append(reported, path)
		})

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrRateLimited)

		// Only the first track was attempted: one request plus the retries,
		// and no requests at all for the remaining tracks.
		assert.Equal(t, int32(1+lrclibMaxRetries), hits.Load())
		assert.Equal(t, Summary{Failed: 1}, summary)
		assert.Equal(t, []string{tracks[0].Path}, reported)
	})

	t.Run("an ordinary failure does not stop the run", func(t *testing.T) {
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		dir := t.TempDir()
		var tracks []TrackInfo
		for _, name := range []string{"01.flac", "02.flac"} {
			path := testutil.MakeAudioFile(t, dir, name, nil)
			tracks = append(tracks, TrackInfo{
				Path: path, Title: name, Artist: "A", Album: "B", Duration: time.Second,
			})
		}

		summary, err := fetch(context.Background(), testFetchClient(srv.URL), tracks, Options{}, nil)
		require.NoError(t, err)
		assert.Equal(t, Summary{Failed: 2}, summary)
		assert.Equal(t, int32(2), hits.Load())
	})
}

func makeLrclibByIDServer(t *testing.T, id int, resp lrclibResponse) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != fmt.Sprintf("/get/%d", id) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestFetchByID(t *testing.T) {
	syncedLRC := "[00:01.00] Hello\n[00:05.00] World"
	plainText := "Hello\nWorld"

	apiResp := lrclibResponse{
		ID: 42, TrackName: "Test Track", ArtistName: "Test Artist",
		AlbumName: "Test Album", Duration: 1,
		PlainLyrics: plainText, SyncedLyrics: syncedLRC,
	}

	t.Run("embeds lyrics for FLAC and reports StatusEmbedded", func(t *testing.T) {
		srv := makeLrclibByIDServer(t, 42, apiResp)
		defer srv.Close()

		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)

		status, err := fetchByID(context.Background(), testFetchClient(srv.URL), path, 42, Options{})
		require.NoError(t, err)
		assert.Equal(t, StatusEmbedded, status)

		lyricsTag, unsyncedTag := readLyricsTags(t, path)
		assert.NotEmpty(t, lyricsTag)
		assert.Equal(t, plainText, unsyncedTag)
		assert.Equal(t, "https://lrclib.net/tracks/42", readTag(t, path, SourceURLTag))
	})

	t.Run("skips track that already has lyrics when not forcing", func(t *testing.T) {
		srv := makeLrclibByIDServer(t, 42, apiResp)
		defer srv.Close()

		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		require.NoError(t, taglib.WriteTags(path, map[string][]string{
			taglib.Lyrics: {"existing lyrics"},
		}, 0))

		status, err := fetchByID(context.Background(), testFetchClient(srv.URL), path, 42, Options{})
		require.NoError(t, err)
		assert.Equal(t, StatusSkipped, status)
		assert.Empty(t, readTag(t, path, SourceURLTag))
	})

	t.Run("overwrites existing lyrics when force is true", func(t *testing.T) {
		srv := makeLrclibByIDServer(t, 42, apiResp)
		defer srv.Close()

		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)
		require.NoError(t, taglib.WriteTags(path, map[string][]string{
			taglib.Lyrics: {"old lyrics"},
		}, 0))

		status, err := fetchByID(context.Background(), testFetchClient(srv.URL), path, 42, Options{Force: true})
		require.NoError(t, err)
		assert.Equal(t, StatusEmbedded, status)

		lyricsTag, _ := readLyricsTags(t, path)
		assert.NotEqual(t, "old lyrics", lyricsTag)
		assert.Equal(t, "https://lrclib.net/tracks/42", readTag(t, path, SourceURLTag))
	})

	t.Run("reports StatusNotFound for MP3 when only synced lyrics available", func(t *testing.T) {
		srv := makeLrclibByIDServer(t, 42, lrclibResponse{
			ID: 42, SyncedLyrics: syncedLRC, PlainLyrics: "",
		})
		defer srv.Close()

		path := testutil.MakeAudioFile(t, t.TempDir(), "track.mp3", nil)

		status, err := fetchByID(context.Background(), testFetchClient(srv.URL), path, 42, Options{})
		require.NoError(t, err)
		assert.Equal(t, StatusNotFound, status)
	})

	t.Run("returns an error when the id doesn't exist on LRCLIB", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)

		status, err := fetchByID(context.Background(), testFetchClient(srv.URL), path, 999999, Options{})
		assert.Error(t, err)
		assert.Equal(t, StatusFailed, status)
	})

	t.Run("reports StatusFailed on HTTP error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		path := testutil.MakeAudioFile(t, t.TempDir(), "track.flac", nil)

		status, err := fetchByID(context.Background(), testFetchClient(srv.URL), path, 42, Options{})
		assert.Error(t, err)
		assert.Equal(t, StatusFailed, status)
	})
}

func TestFetchWordSynced(t *testing.T) {
	syncedLRC := "[00:01.00] Hello\n[00:05.00] World"
	plainText := "Hello\nWorld"

	wordSynced := lrclibResponse{
		ID: 7, TrackName: "Test Track", ArtistName: "Test Artist",
		AlbumName: "Test Album", Duration: 1,
		PlainLyrics: plainText, SyncedLyrics: syncedLRC,
		HasWordSync: true, Lyricsfile: wordSyncedYAML,
	}
	track := func(t *testing.T, name string) TrackInfo {
		t.Helper()
		path := testutil.MakeAudioFile(t, t.TempDir(), name, nil)
		return TrackInfo{Path: path, Title: "T", Artist: "A", Album: "B", Duration: time.Second}
	}

	t.Run("stores the Lyricsfile of a word-synced FLAC match", func(t *testing.T) {
		srv := makeLrclibServer(t, wordSynced)
		defer srv.Close()

		tr := track(t, "track.flac")
		summary, err := fetch(context.Background(), testFetchClient(srv.URL), []TrackInfo{tr}, Options{}, nil)
		require.NoError(t, err)
		assert.Equal(t, Summary{Embedded: 1}, summary)

		assert.Equal(t, wordSyncedYAML, readTag(t, tr.Path, LyricsfileTag))
		assert.Equal(t, "https://lrclib.net/tracks/7", readTag(t, tr.Path, SourceURLTag))
		lyricsTag, _ := readLyricsTags(t, tr.Path)
		assert.Equal(t, "[00:01.00]Hello\n[00:05.00]World", lyricsTag, "LYRICS is still the line-synced LRC")
	})

	t.Run("fails a track whose Lyricsfile is invalid and writes nothing", func(t *testing.T) {
		bad := wordSynced
		bad.Lyricsfile = "version: '1.0'\nlines: [unclosed\n"
		srv := makeLrclibServer(t, bad)
		defer srv.Close()

		tr := track(t, "track.flac")
		var got LyricStatus
		summary, err := fetch(context.Background(), testFetchClient(srv.URL), []TrackInfo{tr}, Options{}, func(_ string, s LyricStatus) { got = s })
		require.NoError(t, err, "a bad document fails the track, not the whole run")
		assert.Equal(t, StatusFailed, got)
		assert.Equal(t, Summary{Failed: 1}, summary)

		lyricsTag, unsyncedTag := readLyricsTags(t, tr.Path)
		assert.Empty(t, lyricsTag)
		assert.Empty(t, unsyncedTag)

		// So the next run tries again instead of skipping it.
		has, err := hasLyrics(tr.Path)
		require.NoError(t, err)
		assert.False(t, has)
	})

	t.Run("a bad Lyricsfile doesn't stop the remaining tracks", func(t *testing.T) {
		bad := wordSynced
		bad.Lyricsfile = "- not\n- a mapping\n"
		srv := makeLrclibServer(t, bad)
		defer srv.Close()

		first, second := track(t, "one.flac"), track(t, "two.flac")
		summary, err := fetch(context.Background(), testFetchClient(srv.URL), []TrackInfo{first, second}, Options{}, nil)
		require.NoError(t, err)
		assert.Equal(t, Summary{Failed: 2}, summary)
	})

	t.Run("an invalid Lyricsfile doesn't matter for MP3", func(t *testing.T) {
		bad := wordSynced
		bad.Lyricsfile = "version: '1.0'\nlines: [unclosed\n"
		srv := makeLrclibServer(t, bad)
		defer srv.Close()

		tr := track(t, "track.mp3")
		summary, err := fetch(context.Background(), testFetchClient(srv.URL), []TrackInfo{tr}, Options{}, nil)
		require.NoError(t, err)
		assert.Equal(t, Summary{Embedded: 1}, summary)
		assert.Empty(t, readTag(t, tr.Path, LyricsfileTag))
	})

	t.Run("--force replaces a stale Lyricsfile when the new record has no word sync", func(t *testing.T) {
		lineSynced := wordSynced
		lineSynced.ID, lineSynced.HasWordSync = 8, false
		srv := makeLrclibServer(t, lineSynced)
		defer srv.Close()

		tr := track(t, "track.flac")
		require.NoError(t, taglib.WriteTags(tr.Path, map[string][]string{
			taglib.Lyrics: {"[00:01.00]old"},
			SourceURLTag:  {"https://lrclib.net/tracks/7"},
			LyricsfileTag: {wordSyncedYAML},
		}, 0))

		summary, err := fetch(context.Background(), testFetchClient(srv.URL), []TrackInfo{tr}, Options{Force: true}, nil)
		require.NoError(t, err)
		assert.Equal(t, Summary{Embedded: 1}, summary)
		assert.Equal(t, "https://lrclib.net/tracks/8", readTag(t, tr.Path, SourceURLTag))
		assert.Empty(t, readTag(t, tr.Path, LyricsfileTag))
	})

	t.Run("without --force an existing file is skipped and gains nothing", func(t *testing.T) {
		srv := makeLrclibServer(t, wordSynced)
		defer srv.Close()

		tr := track(t, "track.flac")
		require.NoError(t, taglib.WriteTags(tr.Path, map[string][]string{
			taglib.Lyrics: {"[00:01.00]existing"},
		}, 0))

		summary, err := fetch(context.Background(), testFetchClient(srv.URL), []TrackInfo{tr}, Options{}, nil)
		require.NoError(t, err)
		assert.Equal(t, Summary{Skipped: 1}, summary)
		assert.Empty(t, readTag(t, tr.Path, LyricsfileTag))
	})

	t.Run("FetchByID stores the Lyricsfile too", func(t *testing.T) {
		srv := makeLrclibByIDServer(t, 7, wordSynced)
		defer srv.Close()

		tr := track(t, "track.flac")
		status, err := fetchByID(context.Background(), testFetchClient(srv.URL), tr.Path, 7, Options{})
		require.NoError(t, err)
		assert.Equal(t, StatusEmbedded, status)
		assert.Equal(t, wordSyncedYAML, readTag(t, tr.Path, LyricsfileTag))
	})

	t.Run("FetchByID fails on an invalid Lyricsfile", func(t *testing.T) {
		bad := wordSynced
		bad.Lyricsfile = "version: '1.0'\nlines: [unclosed\n"
		srv := makeLrclibByIDServer(t, 7, bad)
		defer srv.Close()

		tr := track(t, "track.flac")
		status, err := fetchByID(context.Background(), testFetchClient(srv.URL), tr.Path, 7, Options{})
		require.NoError(t, err)
		assert.Equal(t, StatusFailed, status)
		lyricsTag, _ := readLyricsTags(t, tr.Path)
		assert.Empty(t, lyricsTag)
	})
}

func TestFetchSkipLyricsfile(t *testing.T) {
	syncedLRC := "[00:01.00] Hello\n[00:05.00] World"

	wordSynced := lrclibResponse{
		ID: 7, TrackName: "Test Track", ArtistName: "Test Artist",
		AlbumName: "Test Album", Duration: 1,
		PlainLyrics: "Hello\nWorld", SyncedLyrics: syncedLRC,
		HasWordSync: true, Lyricsfile: wordSyncedYAML,
	}
	skip := Options{SkipLyricsfile: true}
	track := func(t *testing.T, name string) TrackInfo {
		t.Helper()
		path := testutil.MakeAudioFile(t, t.TempDir(), name, nil)
		return TrackInfo{Path: path, Title: "T", Artist: "A", Album: "B", Duration: time.Second}
	}

	t.Run("embeds the lyrics but not the Lyricsfile of a word-synced match", func(t *testing.T) {
		srv := makeLrclibServer(t, wordSynced)
		defer srv.Close()

		tr := track(t, "track.flac")
		summary, err := fetch(context.Background(), testFetchClient(srv.URL), []TrackInfo{tr}, skip, nil)
		require.NoError(t, err)
		assert.Equal(t, Summary{Embedded: 1}, summary)

		lyricsTag, unsyncedTag := readLyricsTags(t, tr.Path)
		assert.Equal(t, "[00:01.00]Hello\n[00:05.00]World", lyricsTag)
		assert.Equal(t, "Hello\nWorld", unsyncedTag)
		assert.Equal(t, "https://lrclib.net/tracks/7", readTag(t, tr.Path, SourceURLTag))
		assert.Empty(t, readTag(t, tr.Path, LyricsfileTag))
	})

	t.Run("an invalid Lyricsfile no longer fails the track", func(t *testing.T) {
		bad := wordSynced
		bad.Lyricsfile = "version: '1.0'\nlines: [unclosed\n"
		srv := makeLrclibServer(t, bad)
		defer srv.Close()

		// Without the option it fails and writes nothing...
		failing := track(t, "failing.flac")
		summary, err := fetch(context.Background(), testFetchClient(srv.URL), []TrackInfo{failing}, Options{}, nil)
		require.NoError(t, err)
		assert.Equal(t, Summary{Failed: 1}, summary)

		// ...and with it, the line-synced lyrics are embedded as usual.
		tr := track(t, "track.flac")
		summary, err = fetch(context.Background(), testFetchClient(srv.URL), []TrackInfo{tr}, skip, nil)
		require.NoError(t, err)
		assert.Equal(t, Summary{Embedded: 1}, summary)
		lyricsTag, _ := readLyricsTags(t, tr.Path)
		assert.NotEmpty(t, lyricsTag)
		assert.Empty(t, readTag(t, tr.Path, LyricsfileTag))
	})

	t.Run("with --force it removes the Lyricsfile of the lyrics it replaces", func(t *testing.T) {
		srv := makeLrclibServer(t, wordSynced)
		defer srv.Close()

		tr := track(t, "track.flac")
		require.NoError(t, taglib.WriteTags(tr.Path, map[string][]string{
			taglib.Lyrics: {"[00:01.00]old"},
			SourceURLTag:  {"https://lrclib.net/tracks/3"},
			LyricsfileTag: {"version: '1.0'\nmetadata:\n  title: old\n  artist: old\n"},
		}, 0))

		summary, err := fetch(context.Background(), testFetchClient(srv.URL), []TrackInfo{tr},
			Options{Force: true, SkipLyricsfile: true}, nil)
		require.NoError(t, err)
		assert.Equal(t, Summary{Embedded: 1}, summary)

		// The old document described the old lyrics, which are gone.
		assert.Empty(t, readTag(t, tr.Path, LyricsfileTag))
		assert.Equal(t, "https://lrclib.net/tracks/7", readTag(t, tr.Path, SourceURLTag))
	})

	t.Run("without --force an existing file is skipped and keeps its Lyricsfile", func(t *testing.T) {
		srv := makeLrclibServer(t, wordSynced)
		defer srv.Close()

		tr := track(t, "track.flac")
		require.NoError(t, taglib.WriteTags(tr.Path, map[string][]string{
			taglib.Lyrics: {"[00:01.00]existing"},
			LyricsfileTag: {wordSyncedYAML},
		}, 0))

		summary, err := fetch(context.Background(), testFetchClient(srv.URL), []TrackInfo{tr}, skip, nil)
		require.NoError(t, err)
		assert.Equal(t, Summary{Skipped: 1}, summary)
		assert.Equal(t, wordSyncedYAML, readTag(t, tr.Path, LyricsfileTag))
	})

	t.Run("FetchByID honors it too", func(t *testing.T) {
		srv := makeLrclibByIDServer(t, 7, wordSynced)
		defer srv.Close()

		tr := track(t, "track.flac")
		status, err := fetchByID(context.Background(), testFetchClient(srv.URL), tr.Path, 7, skip)
		require.NoError(t, err)
		assert.Equal(t, StatusEmbedded, status)
		assert.Empty(t, readTag(t, tr.Path, LyricsfileTag))
		lyricsTag, _ := readLyricsTags(t, tr.Path)
		assert.NotEmpty(t, lyricsTag)
	})
}
