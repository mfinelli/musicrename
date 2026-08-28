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

package navidromesync

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSongIndex(t *testing.T) {
	t.Run("empty catalog produces an empty index in one request", func(t *testing.T) {
		var calls int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&calls, 1)
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","searchResult3":{}}}`)
		}))
		defer srv.Close()

		index, err := buildSongIndex(testClient(srv.URL))
		require.NoError(t, err)
		assert.Empty(t, index)
		assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
	})

	t.Run("a single short page terminates after one request", func(t *testing.T) {
		var calls int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&calls, 1)
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","searchResult3":{"song":[`+
				`{"id":"s1","path":"a.flac"},{"id":"s2","path":"b.flac"}]}}}`)
		}))
		defer srv.Close()

		index, err := buildSongIndex(testClient(srv.URL))
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"a.flac": "s1", "b.flac": "s2"}, index)
		assert.Equal(t, int32(1), atomic.LoadInt32(&calls),
			"a page shorter than the page size must not trigger a second request")
	})

	t.Run("pages until a short page is returned, using the offset each time", func(t *testing.T) {
		// Simulate a catalog of songIndexPageSize + 3 songs: the first
		// full-size page must be followed by exactly one more request for
		// the remaining 3.
		total := songIndexPageSize + 3
		var offsetsSeen []int

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			offset, _ := strconv.Atoi(r.URL.Query().Get("songOffset"))
			offsetsSeen = append(offsetsSeen, offset)

			remaining := total - offset
			if remaining < 0 {
				remaining = 0
			}
			n := remaining
			if n > songIndexPageSize {
				n = songIndexPageSize
			}

			var songs []string
			for i := 0; i < n; i++ {
				id := offset + i
				songs = append(songs, fmt.Sprintf(`{"id":"s%d","path":"track%d.flac"}`, id, id))
			}
			fmt.Fprintf(w, `{"subsonic-response":{"status":"ok","searchResult3":{"song":[%s]}}}`,
				strings.Join(songs, ","))
		}))
		defer srv.Close()

		index, err := buildSongIndex(testClient(srv.URL))
		require.NoError(t, err)
		assert.Len(t, index, total)
		assert.Equal(t, []int{0, songIndexPageSize}, offsetsSeen)
	})

	t.Run("propagates a search failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, genericErrorJSON())
		}))
		defer srv.Close()

		_, err := buildSongIndex(testClient(srv.URL))
		assert.Error(t, err)
	})

	t.Run("skips songs with no path rather than indexing an empty key", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","searchResult3":{"song":[`+
				`{"id":"s1","path":""},{"id":"s2","path":"real.flac"}]}}}`)
		}))
		defer srv.Close()

		index, err := buildSongIndex(testClient(srv.URL))
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"real.flac": "s2"}, index)
	})
}
