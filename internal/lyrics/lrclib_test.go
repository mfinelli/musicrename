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
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// noSleep is a sleep function that returns immediately, so retry tests don't
// actually wait.
func noSleep(context.Context, time.Duration) error { return nil }

// newTestClient returns an lrclibClient pointed at base with rate limiting
// disabled and retry waits skipped so tests run without artificial delay.
func newTestClient(base string) *lrclibClient {
	return &lrclibClient{
		http:    &http.Client{},
		limiter: rate.NewLimiter(rate.Inf, 1),
		base:    base,
		sleep:   noSleep,
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
			_ = json.NewEncoder(w).Encode(sampleTrack)
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
			_, _ = w.Write([]byte("not json"))
		}))
		defer srv.Close()

		c := newTestClient(srv.URL)
		_, err := c.get(context.Background(), "Test", "Artist", "Album", 180)
		assert.Error(t, err)
	})

	t.Run("honors context cancellation", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(sampleTrack)
		}))
		defer srv.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel immediately

		c := newTestClient(srv.URL)
		_, err := c.get(ctx, "Test", "Artist", "Album", 180)
		assert.Error(t, err)
	})
}

func TestLrclibClient_GetByID(t *testing.T) {
	t.Run("returns track on 200", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/get/1", r.URL.Path)
			_ = json.NewEncoder(w).Encode(sampleTrack)
		}))
		defer srv.Close()

		c := newTestClient(srv.URL)
		got, err := c.getByID(context.Background(), 1)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "Back In Black", got.TrackName)
		assert.Equal(t, sampleTrack.PlainLyrics, got.PlainLyrics)
		assert.Equal(t, sampleTrack.SyncedLyrics, got.SyncedLyrics)
	})

	t.Run("returns nil on 404", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		c := newTestClient(srv.URL)
		got, err := c.getByID(context.Background(), 999999)
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("returns error on unexpected status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		c := newTestClient(srv.URL)
		_, err := c.getByID(context.Background(), 1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "500")
	})

	t.Run("returns error on malformed JSON", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("not json"))
		}))
		defer srv.Close()

		c := newTestClient(srv.URL)
		_, err := c.getByID(context.Background(), 1)
		assert.Error(t, err)
	})

	t.Run("honors context cancellation", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(sampleTrack)
		}))
		defer srv.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		c := newTestClient(srv.URL)
		_, err := c.getByID(ctx, 1)
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
			_ = json.NewEncoder(w).Encode(results)
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
			_ = json.NewEncoder(w).Encode([]lrclibTrack{})
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
			_ = json.NewEncoder(w).Encode(sampleTrack)
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
					_ = json.NewEncoder(w).Encode(sampleTrack)
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
				_ = json.NewEncoder(w).Encode([]lrclibTrack{sampleTrack})
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
				_ = json.NewEncoder(w).Encode([]lrclibTrack{})
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

func TestNewLrclibClient(t *testing.T) {
	c := newLrclibClient()
	assert.Equal(t, rate.Every(lrclibRequestInterval), c.limiter.Limit())
	assert.Equal(t, 1, c.limiter.Burst())
	assert.Equal(t, lrclibBase, c.base)

	// The request rate must stay under LRCLIB's documented allowance of
	// roughly 90 requests per minute for identified clients.
	perMinute := float64(time.Minute) / float64(lrclibRequestInterval)
	assert.Less(t, perMinute, 90.0)
}

func TestLrclibClient_UserAgent(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("User-Agent"))
		switch r.URL.Path {
		case "/search":
			_ = json.NewEncoder(w).Encode([]lrclibTrack{sampleTrack})
		default:
			_ = json.NewEncoder(w).Encode(sampleTrack)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	ctx := context.Background()

	_, err := c.get(ctx, "T", "A", "B", 180)
	require.NoError(t, err)
	_, err = c.search(ctx, "T", "A", "B")
	require.NoError(t, err)
	_, err = c.getByID(ctx, 1)
	require.NoError(t, err)

	require.Len(t, seen, 3)
	for _, ua := range seen {
		assert.Equal(t, userAgent(), ua)
	}
}

func TestUserAgent(t *testing.T) {
	// Version is package state set by the command layer; restore it so that
	// other tests are unaffected.
	orig := Version
	t.Cleanup(func() { Version = orig })

	t.Run("names the project, its version, and a link", func(t *testing.T) {
		Version = "4.6.0"
		assert.Equal(t, "musicrename v4.6.0 (https://github.com/mfinelli/musicrename)", userAgent())
	})

	t.Run("omits the version when it isn't set", func(t *testing.T) {
		Version = ""
		assert.Equal(t, "musicrename (https://github.com/mfinelli/musicrename)", userAgent())
	})
}

// throttleServer answers the first n requests with status (and the given
// Retry-After header, if non-empty) and every request after that with
// sampleTrack. It counts every request it receives in hits.
func throttleServer(t *testing.T, n int, status int, retryAfter string, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if int(hits.Add(1)) <= n {
			if retryAfter != "" {
				w.Header().Set("Retry-After", retryAfter)
			}
			w.WriteHeader(status)
			return
		}
		_ = json.NewEncoder(w).Encode(sampleTrack)
	}))
}

func TestLrclibClient_Throttling(t *testing.T) {
	// recordSleeps returns a sleep function that appends every requested
	// wait to *waits instead of sleeping.
	recordSleeps := func(waits *[]time.Duration) func(context.Context, time.Duration) error {
		return func(_ context.Context, d time.Duration) error {
			*waits = append(*waits, d)
			return nil
		}
	}

	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run("retries HTTP "+strconv.Itoa(status)+" after Retry-After and then succeeds", func(t *testing.T) {
			var hits atomic.Int32
			srv := throttleServer(t, 1, status, "2", &hits)
			defer srv.Close()

			var waits []time.Duration
			c := newTestClient(srv.URL)
			c.sleep = recordSleeps(&waits)

			got, err := c.get(context.Background(), "T", "A", "B", 180)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, int32(2), hits.Load())

			// The wait honors Retry-After, plus at most 30% jitter.
			require.Len(t, waits, 1)
			assert.GreaterOrEqual(t, waits[0], 2*time.Second)
			assert.LessOrEqual(t, waits[0], 2*time.Second+time.Duration(0.3*float64(2*time.Second)))
		})
	}

	t.Run("uses a one second default when Retry-After is missing or malformed", func(t *testing.T) {
		for _, header := range []string{"", "soon", "-5"} {
			var hits atomic.Int32
			srv := throttleServer(t, 1, http.StatusTooManyRequests, header, &hits)

			var waits []time.Duration
			c := newTestClient(srv.URL)
			c.sleep = recordSleeps(&waits)

			got, err := c.get(context.Background(), "T", "A", "B", 180)
			srv.Close()
			require.NoError(t, err, "Retry-After %q", header)
			require.NotNil(t, got)

			require.Len(t, waits, 1, "Retry-After %q", header)
			assert.GreaterOrEqual(t, waits[0], lrclibDefaultRetryWait)
			assert.LessOrEqual(t, waits[0], time.Duration(1.3*float64(lrclibDefaultRetryWait)))
		}
	})

	t.Run("applies to search and getByID too", func(t *testing.T) {
		var hits atomic.Int32
		srv := throttleServer(t, 1, http.StatusTooManyRequests, "0", &hits)
		defer srv.Close()

		c := newTestClient(srv.URL)
		got, err := c.getByID(context.Background(), 1)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, int32(2), hits.Load())
	})

	t.Run("gives up with ErrRateLimited once retries are exhausted", func(t *testing.T) {
		var hits atomic.Int32
		srv := throttleServer(t, 1000, http.StatusTooManyRequests, "1", &hits)
		defer srv.Close()

		var waits []time.Duration
		c := newTestClient(srv.URL)
		c.sleep = recordSleeps(&waits)

		_, err := c.get(context.Background(), "T", "A", "B", 180)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrRateLimited)
		assert.Equal(t, int32(1+lrclibMaxRetries), hits.Load())
		assert.Len(t, waits, lrclibMaxRetries)
	})

	t.Run("reports a persistent 503 as a plain HTTP error", func(t *testing.T) {
		var hits atomic.Int32
		srv := throttleServer(t, 1000, http.StatusServiceUnavailable, "1", &hits)
		defer srv.Close()

		c := newTestClient(srv.URL)
		_, err := c.get(context.Background(), "T", "A", "B", 180)
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrRateLimited)
		assert.Contains(t, err.Error(), "503")
		assert.Equal(t, int32(1+lrclibMaxRetries), hits.Load())
	})

	t.Run("does not wait out an excessive Retry-After", func(t *testing.T) {
		var hits atomic.Int32
		srv := throttleServer(t, 1000, http.StatusTooManyRequests, "3600", &hits)
		defer srv.Close()

		var waits []time.Duration
		c := newTestClient(srv.URL)
		c.sleep = recordSleeps(&waits)

		_, err := c.get(context.Background(), "T", "A", "B", 180)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrRateLimited)
		assert.Equal(t, int32(1), hits.Load())
		assert.Empty(t, waits)
	})

	t.Run("does not retry other errors", func(t *testing.T) {
		var hits atomic.Int32
		srv := throttleServer(t, 1000, http.StatusInternalServerError, "", &hits)
		defer srv.Close()

		c := newTestClient(srv.URL)
		_, err := c.get(context.Background(), "T", "A", "B", 180)
		require.Error(t, err)
		assert.Equal(t, int32(1), hits.Load())
	})

	t.Run("stops waiting when the context is cancelled", func(t *testing.T) {
		var hits atomic.Int32
		srv := throttleServer(t, 1000, http.StatusTooManyRequests, "30", &hits)
		defer srv.Close()

		ctx, cancel := context.WithCancel(context.Background())
		c := newTestClient(srv.URL)
		c.sleep = nil // use the real, context-aware sleep
		time.AfterFunc(50*time.Millisecond, cancel)

		start := time.Now()
		_, err := c.get(ctx, "T", "A", "B", 180)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
		assert.Less(t, time.Since(start), 5*time.Second)
		assert.Equal(t, int32(1), hits.Load())
	})
}
