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
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.senan.xyz/taglib"
	"golang.org/x/time/rate"
)

// makeAudioFile generates a one-second silent audio file at dir/name. The
// format is inferred from the file extension (.flac, .mp3, .m4a). ffmpeg must
// be installed and on PATH. Duplicated from internal/metadata because
// cross-package import of _test.go helpers is not possible in Go.
func makeAudioFile(t *testing.T, dir, name string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	ext := strings.ToLower(filepath.Ext(name))

	var codec string
	switch ext {
	case ".flac":
		codec = "flac"
	case ".mp3":
		codec = "libmp3lame"
	case ".m4a":
		codec = "aac"
	default:
		t.Fatalf("makeAudioFile: unsupported extension %q", ext)
	}

	args := []string{
		"-y", "-f", "lavfi", "-i", "anullsrc=r=44100:cl=stereo",
		"-t", "1", "-c:a", codec, path,
	}
	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("makeAudioFile: ffmpeg failed: %v\n%s", err, out)
	}
	return path
}

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
}

func testFetchClient(base string) *lrclibClient {
	return &lrclibClient{
		http:    &http.Client{},
		limiter: rate.NewLimiter(rate.Inf, 1),
		base:    base,
	}
}

// --- hasLyrics ---

func TestHasLyrics(t *testing.T) {
	t.Run("FLAC with LYRICS tag returns true", func(t *testing.T) {
		path := makeAudioFile(t, t.TempDir(), "track.flac")
		require.NoError(t, taglib.WriteTags(path, map[string][]string{
			taglib.Lyrics: {"[00:01.00]Hello"},
		}, 0))
		got, err := hasLyrics(path)
		require.NoError(t, err)
		assert.True(t, got)
	})

	t.Run("FLAC with UNSYNCEDLYRICS tag returns true", func(t *testing.T) {
		path := makeAudioFile(t, t.TempDir(), "track.flac")
		require.NoError(t, taglib.WriteTags(path, map[string][]string{
			"UNSYNCEDLYRICS": {"Hello world"},
		}, 0))
		got, err := hasLyrics(path)
		require.NoError(t, err)
		assert.True(t, got)
	})

	t.Run("FLAC with no lyrics returns false", func(t *testing.T) {
		path := makeAudioFile(t, t.TempDir(), "track.flac")
		got, err := hasLyrics(path)
		require.NoError(t, err)
		assert.False(t, got)
	})

	t.Run("MP3 with LYRICS tag returns true", func(t *testing.T) {
		path := makeAudioFile(t, t.TempDir(), "track.mp3")
		require.NoError(t, taglib.WriteTags(path, map[string][]string{
			taglib.Lyrics: {"Hello world"},
		}, 0))
		got, err := hasLyrics(path)
		require.NoError(t, err)
		assert.True(t, got)
	})

	t.Run("MP3 with no lyrics returns false", func(t *testing.T) {
		path := makeAudioFile(t, t.TempDir(), "track.mp3")
		got, err := hasLyrics(path)
		require.NoError(t, err)
		assert.False(t, got)
	})

	t.Run("M4A with LYRICS tag returns true", func(t *testing.T) {
		path := makeAudioFile(t, t.TempDir(), "track.m4a")
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

// --- embedLyrics ---

func TestEmbedLyrics(t *testing.T) {
	synced := "[00:01.00] Hello\n[00:05.00] World"
	plain := "Hello\nWorld"

	t.Run("FLAC embeds synced into LYRICS and plain into UNSYNCEDLYRICS", func(t *testing.T) {
		path := makeAudioFile(t, t.TempDir(), "track.flac")
		embedded, err := embedLyrics(path, synced, plain)
		require.NoError(t, err)
		assert.True(t, embedded)

		lyricsTag, unsyncedTag := readLyricsTags(t, path)
		// LYRICS gets the standardized LRC (space after ] stripped)
		assert.Equal(t, "[00:01.00]Hello\n[00:05.00]World", lyricsTag)
		assert.Equal(t, plain, unsyncedTag)
	})

	t.Run("FLAC with synced only embeds LYRICS, leaves UNSYNCEDLYRICS empty", func(t *testing.T) {
		path := makeAudioFile(t, t.TempDir(), "track.flac")
		embedded, err := embedLyrics(path, synced, "")
		require.NoError(t, err)
		assert.True(t, embedded)

		lyricsTag, unsyncedTag := readLyricsTags(t, path)
		assert.NotEmpty(t, lyricsTag)
		assert.Empty(t, unsyncedTag)
	})

	t.Run("FLAC with plain only embeds UNSYNCEDLYRICS, leaves LYRICS empty", func(t *testing.T) {
		path := makeAudioFile(t, t.TempDir(), "track.flac")
		embedded, err := embedLyrics(path, "", plain)
		require.NoError(t, err)
		assert.True(t, embedded)

		lyricsTag, unsyncedTag := readLyricsTags(t, path)
		assert.Empty(t, lyricsTag)
		assert.Equal(t, plain, unsyncedTag)
	})

	t.Run("MP3 embeds plain into LYRICS", func(t *testing.T) {
		path := makeAudioFile(t, t.TempDir(), "track.mp3")
		embedded, err := embedLyrics(path, synced, plain)
		require.NoError(t, err)
		assert.True(t, embedded)

		lyricsTag, _ := readLyricsTags(t, path)
		assert.Equal(t, plain, lyricsTag)
	})

	t.Run("MP3 with synced only returns false without writing", func(t *testing.T) {
		path := makeAudioFile(t, t.TempDir(), "track.mp3")
		embedded, err := embedLyrics(path, synced, "")
		require.NoError(t, err)
		assert.False(t, embedded)

		lyricsTag, _ := readLyricsTags(t, path)
		assert.Empty(t, lyricsTag)
	})

	t.Run("M4A embeds plain into LYRICS", func(t *testing.T) {
		path := makeAudioFile(t, t.TempDir(), "track.m4a")
		embedded, err := embedLyrics(path, "", plain)
		require.NoError(t, err)
		assert.True(t, embedded)

		lyricsTag, _ := readLyricsTags(t, path)
		assert.Equal(t, plain, lyricsTag)
	})

	t.Run("both synced and plain empty returns false without writing", func(t *testing.T) {
		path := makeAudioFile(t, t.TempDir(), "track.flac")
		embedded, err := embedLyrics(path, "", "")
		require.NoError(t, err)
		assert.False(t, embedded)
	})
}

// --- Fetch (integration) ---

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

		path := makeAudioFile(t, t.TempDir(), "track.flac")
		tracks := []TrackInfo{{
			Path: path, Title: "Test Track", Artist: "Test Artist",
			Album: "Test Album", Duration: time.Second,
		}}

		var gotStatus LyricStatus
		summary, err := fetch(context.Background(), testFetchClient(srv.URL), tracks, false, func(_ string, s LyricStatus) {
			gotStatus = s
		})
		require.NoError(t, err)
		assert.Equal(t, StatusEmbedded, gotStatus)
		assert.Equal(t, Summary{Embedded: 1}, summary)

		lyricsTag, unsyncedTag := readLyricsTags(t, path)
		assert.NotEmpty(t, lyricsTag)
		assert.Equal(t, plainText, unsyncedTag)
	})

	t.Run("skips track that already has lyrics when not forcing", func(t *testing.T) {
		srv := makeLrclibServer(t, apiResp)
		defer srv.Close()

		path := makeAudioFile(t, t.TempDir(), "track.flac")
		require.NoError(t, taglib.WriteTags(path, map[string][]string{
			taglib.Lyrics: {"existing lyrics"},
		}, 0))

		tracks := []TrackInfo{{Path: path, Title: "T", Artist: "A", Album: "B", Duration: time.Second}}

		var gotStatus LyricStatus
		summary, err := fetch(context.Background(), testFetchClient(srv.URL), tracks, false, func(_ string, s LyricStatus) {
			gotStatus = s
		})
		require.NoError(t, err)
		assert.Equal(t, StatusSkipped, gotStatus)
		assert.Equal(t, Summary{Skipped: 1}, summary)
	})

	t.Run("overwrites existing lyrics when force is true", func(t *testing.T) {
		srv := makeLrclibServer(t, apiResp)
		defer srv.Close()

		path := makeAudioFile(t, t.TempDir(), "track.flac")
		require.NoError(t, taglib.WriteTags(path, map[string][]string{
			taglib.Lyrics: {"old lyrics"},
		}, 0))

		tracks := []TrackInfo{{Path: path, Title: "T", Artist: "A", Album: "B", Duration: time.Second}}
		summary, err := fetch(context.Background(), testFetchClient(srv.URL), tracks, true, nil)
		require.NoError(t, err)
		assert.Equal(t, Summary{Embedded: 1}, summary)

		lyricsTag, _ := readLyricsTags(t, path)
		assert.NotEqual(t, "old lyrics", lyricsTag) // overwritten
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

		path := makeAudioFile(t, t.TempDir(), "track.flac")
		tracks := []TrackInfo{{Path: path, Title: "Unknown", Artist: "Nobody", Album: "Nothing", Duration: time.Second}}

		summary, err := fetch(context.Background(), testFetchClient(srv.URL), tracks, false, nil)
		require.NoError(t, err)
		assert.Equal(t, Summary{NotFound: 1}, summary)
	})

	t.Run("reports StatusNotFound for MP3 when only synced lyrics available", func(t *testing.T) {
		srv := makeLrclibServer(t, lrclibResponse{
			ID: 1, SyncedLyrics: syncedLRC, PlainLyrics: "", // no plain
		})
		defer srv.Close()

		path := makeAudioFile(t, t.TempDir(), "track.mp3")
		tracks := []TrackInfo{{Path: path, Title: "T", Artist: "A", Album: "B", Duration: time.Second}}

		summary, err := fetch(context.Background(), testFetchClient(srv.URL), tracks, false, nil)
		require.NoError(t, err)
		assert.Equal(t, Summary{NotFound: 1}, summary)
	})

	t.Run("reports StatusFailed on HTTP error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		path := makeAudioFile(t, t.TempDir(), "track.flac")
		tracks := []TrackInfo{{Path: path, Title: "T", Artist: "A", Album: "B", Duration: time.Second}}

		summary, err := fetch(context.Background(), testFetchClient(srv.URL), tracks, false, nil)
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
		path1 := makeAudioFile(t, dir, "01 track.flac")
		path2 := makeAudioFile(t, dir, "02 track.flac")

		tracks := []TrackInfo{
			{Path: path1, Title: "Found", Artist: "A", Album: "B", Duration: time.Second},
			{Path: path2, Title: "Missing", Artist: "A", Album: "B", Duration: time.Second},
		}

		summary, err := fetch(context.Background(), testFetchClient(srv.URL), tracks, false, nil)
		require.NoError(t, err)
		assert.Equal(t, 1, summary.Embedded)
		assert.Equal(t, 1, summary.NotFound)
	})

	t.Run("progress callback receives correct path and status", func(t *testing.T) {
		srv := makeLrclibServer(t, apiResp)
		defer srv.Close()

		path := makeAudioFile(t, t.TempDir(), "track.flac")
		tracks := []TrackInfo{{Path: path, Title: "T", Artist: "A", Album: "B", Duration: time.Second}}

		var cbPath string
		var cbStatus LyricStatus
		_, err := fetch(context.Background(), testFetchClient(srv.URL), tracks, false, func(p string, s LyricStatus) {
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

		path := makeAudioFile(t, t.TempDir(), "track.flac")
		tracks := []TrackInfo{{Path: path, Title: "T", Artist: "A", Album: "B", Duration: time.Second}}

		_, err := fetch(context.Background(), testFetchClient(srv.URL), tracks, false, nil)
		assert.NoError(t, err)
	})

	t.Run("empty track list returns zero summary without error", func(t *testing.T) {
		srv := makeLrclibServer(t, apiResp)
		defer srv.Close()

		summary, err := fetch(context.Background(), testFetchClient(srv.URL), nil, false, nil)
		require.NoError(t, err)
		assert.Equal(t, Summary{}, summary)
	})

	t.Run("context cancellation is propagated and reported as failed", func(t *testing.T) {
		srv := makeLrclibServer(t, apiResp)
		defer srv.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		path := makeAudioFile(t, t.TempDir(), "track.flac")
		tracks := []TrackInfo{{Path: path, Title: "T", Artist: "A", Album: "B", Duration: time.Second}}

		summary, err := fetch(ctx, testFetchClient(srv.URL), tracks, false, nil)
		require.NoError(t, err)
		// The cancelled context causes the HTTP request to fail -> StatusFailed
		assert.Equal(t, fmt.Sprintf("%d", summary.Embedded+summary.Failed+summary.Skipped+summary.NotFound), "1")
	})
}
