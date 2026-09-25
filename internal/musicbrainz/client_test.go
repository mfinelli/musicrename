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

package musicbrainz

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// testClient returns a *client pointed at base with an unlimited rate
// limiter, so tests run at full speed rather than MusicBrainz's real
// 1-request-per-second limit. Mirrors internal/lyrics's testFetchClient.
func testClient(base string) *client {
	return &client{
		http:    &http.Client{},
		limiter: rate.NewLimiter(rate.Inf, 1),
		base:    base,
	}
}

func TestGetRelease(t *testing.T) {
	t.Run("decodes a successful response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/release/d1d196ec-bb2b-4636-81a0-b8f5c5774514", r.URL.Path)
			assert.Equal(t, userAgent, r.Header.Get("User-Agent"))
			assert.Equal(t, "json", r.URL.Query().Get("fmt"))

			inc := r.URL.Query().Get("inc")
			for _, want := range []string{
				"recordings", "artist-credits", "recording-level-rels", "work-rels",
				"work-level-rels", "artist-rels", "release-groups", "labels", "isrcs", "genres",
			} {
				assert.Contains(t, inc, want)
			}

			json.NewEncoder(w).Encode(Release{ID: "d1d196ec-bb2b-4636-81a0-b8f5c5774514", Title: "Back in Black"})
		}))
		defer srv.Close()

		release, err := testClient(srv.URL).getRelease(context.Background(), "d1d196ec-bb2b-4636-81a0-b8f5c5774514")
		require.NoError(t, err)
		assert.Equal(t, "Back in Black", release.Title)
	})

	t.Run("returns ErrNotFound on HTTP 404", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		_, err := testClient(srv.URL).getRelease(context.Background(), "00000000-0000-0000-0000-000000000000")
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("errors on an unexpected HTTP status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		_, err := testClient(srv.URL).getRelease(context.Background(), "d1d196ec-bb2b-4636-81a0-b8f5c5774514")
		assert.ErrorContains(t, err, "500")
	})

	t.Run("errors on malformed JSON", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("not json"))
		}))
		defer srv.Close()

		_, err := testClient(srv.URL).getRelease(context.Background(), "d1d196ec-bb2b-4636-81a0-b8f5c5774514")
		assert.Error(t, err)
	})
}
