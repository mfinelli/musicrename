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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// newTestClient returns an lrclibClient pointed at base with rate limiting
// disabled so tests run without artificial delay.
func newTestClient(base string) *lrclibClient {
	return &lrclibClient{
		http:    &http.Client{},
		limiter: rate.NewLimiter(rate.Inf, 1),
		base:    base,
	}
}

// sampleTrack is a reusable lrclibTrack fixture for handler responses.
var sampleTrack = lrclibTrack{
	ID:           1,
	TrackName:    "Back In Black",
	ArtistName:   "AC/DC",
	AlbumName:    "Back In Black",
	Duration:     255,
	PlainLyrics:  "I'm rolling thunder\nPouring rain",
	SyncedLyrics: "[00:18.00] I'm rolling thunder\n[00:21.00] Pouring rain",
}

func TestLrclibClient_Get(t *testing.T) {
	t.Run("returns track on 200", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/get", r.URL.Path)
			assert.Equal(t, "Back In Black", r.URL.Query().Get("track_name"))
			assert.Equal(t, "AC/DC", r.URL.Query().Get("artist_name"))
			assert.Equal(t, "Back In Black", r.URL.Query().Get("album_name"))
			assert.Equal(t, "255", r.URL.Query().Get("duration"))
			json.NewEncoder(w).Encode(sampleTrack)
		}))
		defer srv.Close()

		c := newTestClient(srv.URL)
		got, err := c.get(context.Background(), "Back In Black", "AC/DC", "Back In Black", 255)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "Back In Black", got.TrackName)
		assert.Equal(t, "AC/DC", got.ArtistName)
		assert.Equal(t, sampleTrack.PlainLyrics, got.PlainLyrics)
		assert.Equal(t, sampleTrack.SyncedLyrics, got.SyncedLyrics)
	})

	t.Run("returns nil on 404", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		c := newTestClient(srv.URL)
		got, err := c.get(context.Background(), "Unknown", "Nobody", "Nothing", 180)
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("returns error on unexpected status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		c := newTestClient(srv.URL)
		_, err := c.get(context.Background(), "Test", "Artist", "Album", 180)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "500")
	})

	t.Run("returns error on malformed JSON", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("not json"))
		}))
		defer srv.Close()

		c := newTestClient(srv.URL)
		_, err := c.get(context.Background(), "Test", "Artist", "Album", 180)
		assert.Error(t, err)
	})

	t.Run("honors context cancellation", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(sampleTrack)
		}))
		defer srv.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel immediately

		c := newTestClient(srv.URL)
		_, err := c.get(ctx, "Test", "Artist", "Album", 180)
		assert.Error(t, err)
	})
}

func TestLrclibClient_Search(t *testing.T) {
	t.Run("returns first result on 200", func(t *testing.T) {
		results := []lrclibTrack{
			{ID: 1, TrackName: "First Result", PlainLyrics: "first lyrics"},
			{ID: 2, TrackName: "Second Result", PlainLyrics: "second lyrics"},
		}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/search", r.URL.Path)
			assert.Equal(t, "Back In Black", r.URL.Query().Get("track_name"))
			assert.Equal(t, "AC/DC", r.URL.Query().Get("artist_name"))
			// duration must NOT be sent to /search
			assert.Empty(t, r.URL.Query().Get("duration"))
			json.NewEncoder(w).Encode(results)
		}))
		defer srv.Close()

		c := newTestClient(srv.URL)
		got, err := c.search(context.Background(), "Back In Black", "AC/DC", "Back In Black")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "First Result", got.TrackName)
	})

	t.Run("returns nil when results are empty", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode([]lrclibTrack{})
		}))
		defer srv.Close()

		c := newTestClient(srv.URL)
		got, err := c.search(context.Background(), "Unknown", "Nobody", "Nothing")
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("returns error on unexpected status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer srv.Close()

		c := newTestClient(srv.URL)
		_, err := c.search(context.Background(), "Test", "Artist", "Album")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "503")
	})
}

func TestLrclibClient_FetchForTrack(t *testing.T) {
	t.Run("returns immediately on exact duration match", func(t *testing.T) {
		callCount := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			json.NewEncoder(w).Encode(sampleTrack)
		}))
		defer srv.Close()

		c := newTestClient(srv.URL)
		got, err := c.fetchForTrack(context.Background(), "Back In Black", "AC/DC", "Back In Black", 255*time.Second)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, 1, callCount, "should stop after exact match without further requests")
	})

	t.Run("tries delta durations after exact miss", func(t *testing.T) {
		requestedDurations := []string{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/get":
				d := r.URL.Query().Get("duration")
				requestedDurations = append(requestedDurations, d)
				if d == "254" { // secs-1 succeeds
					json.NewEncoder(w).Encode(sampleTrack)
					return
				}
				w.WriteHeader(http.StatusNotFound)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer srv.Close()

		c := newTestClient(srv.URL)
		got, err := c.fetchForTrack(context.Background(), "Back In Black", "AC/DC", "Back In Black", 255*time.Second)
		require.NoError(t, err)
		require.NotNil(t, got)
		// exact (255) then −1 (254); should stop there
		assert.Equal(t, []string{"255", "254"}, requestedDurations)
	})

	t.Run("falls all the way through to search when all gets miss", func(t *testing.T) {
		getCount := 0
		searchCalled := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/get":
				getCount++
				w.WriteHeader(http.StatusNotFound)
			case "/search":
				searchCalled = true
				json.NewEncoder(w).Encode([]lrclibTrack{sampleTrack})
			}
		}))
		defer srv.Close()

		c := newTestClient(srv.URL)
		got, err := c.fetchForTrack(context.Background(), "Back In Black", "AC/DC", "Back In Black", 255*time.Second)
		require.NoError(t, err)
		require.NotNil(t, got)
		// 1 exact + 4 delta = 5 /get calls, then 1 /search
		assert.Equal(t, 5, getCount)
		assert.True(t, searchCalled)
	})

	t.Run("returns nil when all steps miss", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/get":
				w.WriteHeader(http.StatusNotFound)
			case "/search":
				json.NewEncoder(w).Encode([]lrclibTrack{})
			}
		}))
		defer srv.Close()

		c := newTestClient(srv.URL)
		got, err := c.fetchForTrack(context.Background(), "Unknown", "Nobody", "Nothing", 180*time.Second)
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("propagates error from get without continuing", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		c := newTestClient(srv.URL)
		_, err := c.fetchForTrack(context.Background(), "Test", "Artist", "Album", 180*time.Second)
		assert.Error(t, err)
	})
}
