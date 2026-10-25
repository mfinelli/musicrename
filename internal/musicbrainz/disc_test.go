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
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testReleaseID = "d1d196ec-bb2b-4636-81a0-b8f5c5774514"
	testDiscID    = "NdW5DdItgg6nqzd5d6Ade54jPKA-"
)

// releaseWithDiscs is what a release lookup with inc=discids looks like, with
// the other fields the real response carries, which are not read.
const releaseWithDiscs = `{
	"id": "d1d196ec-bb2b-4636-81a0-b8f5c5774514",
	"title": "Back in Black",
	"status": "Official",
	"country": "US",
	"media": [
		{
			"position": 1,
			"format": "CD",
			"title": "",
			"track-count": 10,
			"discs": [
				{"id": "NdW5DdItgg6nqzd5d6Ade54jPKA-", "offset-count": 10, "sectors": 189785, "offsets": [150, 23580]}
			]
		}
	]
}`

// serve starts a server that answers every request with status and body, and
// returns a client for it and a count of the requests it has served.
func serve(t *testing.T, status int, body string) (*client, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return testClient(srv.URL), &n
}

func TestGetReleaseDiscs(t *testing.T) {
	t.Run("asks for the disc IDs of one release, as this tool", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/release/"+testReleaseID, r.URL.Path)
			assert.Equal(t, "discids", r.URL.Query().Get("inc"))
			assert.Equal(t, "json", r.URL.Query().Get("fmt"))
			assert.Equal(t, userAgent, r.Header.Get("User-Agent"))
			w.Write([]byte(releaseWithDiscs))
		}))
		defer srv.Close()

		got, err := testClient(srv.URL).getReleaseDiscs(context.Background(), testReleaseID)

		require.NoError(t, err)
		assert.Equal(t, &ReleaseDiscs{Media: 1, Discs: []Disc{{ID: testDiscID, Medium: 1}}}, got)
	})

	t.Run("a multi-disc release, where each medium has its own disc IDs", func(t *testing.T) {
		c, _ := serve(t, 200, `{"media": [
			{"position": 1, "discs": [{"id": "AAAA"}, {"id": "BBBB"}]},
			{"position": 2, "discs": []},
			{"position": 3, "discs": [{"id": "CCCC"}]}
		]}`)

		got, err := c.getReleaseDiscs(context.Background(), testReleaseID)

		require.NoError(t, err)
		assert.Equal(t, 3, got.Media, "a medium with no disc IDs still counts")
		assert.Equal(t, []Disc{{"AAAA", 1}, {"BBBB", 1}, {"CCCC", 3}}, got.Discs)
	})

	t.Run("a release no one has attached a disc ID to", func(t *testing.T) {
		c, _ := serve(t, 200, `{"media": [{"position": 1, "format": "CD"}]}`)

		got, err := c.getReleaseDiscs(context.Background(), testReleaseID)

		require.NoError(t, err)
		assert.Equal(t, 1, got.Media)
		assert.Empty(t, got.Discs)
	})

	t.Run("a release with no media at all", func(t *testing.T) {
		c, _ := serve(t, 200, `{"id": "x"}`)

		got, err := c.getReleaseDiscs(context.Background(), testReleaseID)

		require.NoError(t, err)
		assert.Equal(t, 0, got.Media)
	})

	t.Run("an unknown release is ErrNotFound", func(t *testing.T) {
		c, _ := serve(t, http.StatusNotFound, `{"error": "Not Found"}`)

		_, err := c.getReleaseDiscs(context.Background(), testReleaseID)

		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("an unexpected status is an error", func(t *testing.T) {
		for _, status := range []int{http.StatusServiceUnavailable, http.StatusInternalServerError, http.StatusForbidden} {
			c, _ := serve(t, status, "")

			_, err := c.getReleaseDiscs(context.Background(), testReleaseID)

			assert.ErrorContains(t, err, "HTTP", status)
			assert.NotErrorIs(t, err, ErrNotFound, status)
		}
	})

	t.Run("a response that is not JSON is an error", func(t *testing.T) {
		c, _ := serve(t, 200, "<html>maintenance</html>")

		_, err := c.getReleaseDiscs(context.Background(), testReleaseID)

		assert.ErrorContains(t, err, "decode")
	})

	t.Run("a server that cannot be reached is an error", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		c := testClient(srv.URL)
		srv.Close()

		_, err := c.getReleaseDiscs(context.Background(), testReleaseID)

		assert.Error(t, err)
		assert.NotErrorIs(t, err, ErrNotFound)
		// The cause, not a repeat of the request.
		assert.ErrorContains(t, err, "connection refused")
		assert.NotContains(t, err.Error(), "/release/")
		assert.NotContains(t, err.Error(), "fmt=json")
	})

	t.Run("a base address that cannot be requested is an error", func(t *testing.T) {
		c := testClient("http://[::1")

		_, err := c.getReleaseDiscs(context.Background(), testReleaseID)

		assert.ErrorContains(t, err, "build request")
	})

	t.Run("a cancelled context stops it", func(t *testing.T) {
		c, n := serve(t, 200, releaseWithDiscs)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := c.getReleaseDiscs(ctx, testReleaseID)

		assert.Error(t, err)
		assert.Equal(t, int32(0), n.Load(), "no request is made")
	})
}

// discIDResponseBody is what a disc ID lookup returns: the releases it is
// attached to, among much else.
const discIDResponseBody = `{
	"id": "NdW5DdItgg6nqzd5d6Ade54jPKA-",
	"offset-count": 10,
	"sectors": 189785,
	"releases": [
		{
			"id": "d1d196ec-bb2b-4636-81a0-b8f5c5774514",
			"title": "Back in Black",
			"status": "Official",
			"disambiguation": "",
			"country": "US",
			"date": "1980-07-25",
			"barcode": "074646568022",
			"media": [{"position": 1, "discs": []}]
		},
		{
			"id": "0a0a0a0a-0000-4000-8000-000000000001",
			"title": "Back in Black",
			"status": "Official",
			"disambiguation": "remastered",
			"country": "XE",
			"date": "2003"
		}
	]
}`

func TestGetDiscReleases(t *testing.T) {
	t.Run("asks about one disc ID, as this tool", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/discid/"+testDiscID, r.URL.Path)
			assert.Equal(t, "json", r.URL.Query().Get("fmt"))
			assert.Equal(t, userAgent, r.Header.Get("User-Agent"))
			w.Write([]byte(discIDResponseBody))
		}))
		defer srv.Close()

		got, err := testClient(srv.URL).getDiscReleases(context.Background(), testDiscID)

		require.NoError(t, err)
		assert.Equal(t, []ReleaseRef{
			{ID: "d1d196ec-bb2b-4636-81a0-b8f5c5774514", Title: "Back in Black", Status: "Official", Country: "US", Date: "1980-07-25"},
			{ID: "0a0a0a0a-0000-4000-8000-000000000001", Title: "Back in Black", Status: "Official", Disambiguation: "remastered", Country: "XE", Date: "2003"},
		}, got)
	})

	t.Run("the characters in a disc ID that are special in a URL are escaped", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/discid/a.b_c-", r.URL.EscapedPath())
			w.Write([]byte(`{"releases": []}`))
		}))
		defer srv.Close()

		_, err := testClient(srv.URL).getDiscReleases(context.Background(), "a.b_c-")

		require.NoError(t, err)
	})

	t.Run("a disc ID MusicBrainz has never been given is not an error", func(t *testing.T) {
		c, _ := serve(t, http.StatusNotFound, `{"error": "Not found"}`)

		got, err := c.getDiscReleases(context.Background(), testDiscID)

		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("a disc ID with no releases", func(t *testing.T) {
		c, _ := serve(t, 200, `{"id": "x", "releases": []}`)

		got, err := c.getDiscReleases(context.Background(), testDiscID)

		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("an unexpected status is an error", func(t *testing.T) {
		c, _ := serve(t, http.StatusServiceUnavailable, "")

		_, err := c.getDiscReleases(context.Background(), testDiscID)

		assert.ErrorContains(t, err, "HTTP 503")
	})

	t.Run("a response that is not JSON is an error", func(t *testing.T) {
		c, _ := serve(t, 200, "nope")

		_, err := c.getDiscReleases(context.Background(), testDiscID)

		assert.ErrorContains(t, err, "decode")
	})
}

func TestReleaseRefURL(t *testing.T) {
	assert.Equal(t, "https://musicbrainz.org/release/"+testReleaseID, ReleaseRef{ID: testReleaseID}.URL())
}

func TestLookup(t *testing.T) {
	t.Run("asks once about a release however often it is asked", func(t *testing.T) {
		// A multi-disc rip checks one release once per disc.
		c, n := serve(t, 200, releaseWithDiscs)
		l := newLookup(c)

		for range 3 {
			got, err := l.ReleaseDiscs(context.Background(), testReleaseID)
			require.NoError(t, err)
			assert.Equal(t, 1, got.Media)
		}

		assert.Equal(t, int32(1), n.Load())
	})

	t.Run("different releases are different questions", func(t *testing.T) {
		c, n := serve(t, 200, releaseWithDiscs)
		l := newLookup(c)

		_, err := l.ReleaseDiscs(context.Background(), testReleaseID)
		require.NoError(t, err)
		_, err = l.ReleaseDiscs(context.Background(), "00000000-0000-4000-8000-000000000000")
		require.NoError(t, err)

		assert.Equal(t, int32(2), n.Load())
	})

	t.Run("asks once about a disc ID", func(t *testing.T) {
		c, n := serve(t, 200, discIDResponseBody)
		l := newLookup(c)

		for range 3 {
			got, err := l.DiscReleases(context.Background(), testDiscID)
			require.NoError(t, err)
			assert.Len(t, got, 2)
		}

		assert.Equal(t, int32(1), n.Load())
	})

	t.Run("remembers that a disc ID has no releases", func(t *testing.T) {
		c, n := serve(t, http.StatusNotFound, "")
		l := newLookup(c)

		for range 2 {
			got, err := l.DiscReleases(context.Background(), testDiscID)
			require.NoError(t, err)
			assert.Empty(t, got)
		}

		assert.Equal(t, int32(1), n.Load())
	})

	t.Run("a failure is not remembered, so a second try can succeed", func(t *testing.T) {
		var n atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if n.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.Write([]byte(releaseWithDiscs))
		}))
		defer srv.Close()
		l := newLookup(testClient(srv.URL))

		_, err := l.ReleaseDiscs(context.Background(), testReleaseID)
		assert.Error(t, err)

		got, err := l.ReleaseDiscs(context.Background(), testReleaseID)
		require.NoError(t, err)
		assert.Equal(t, 1, got.Media)
	})

	t.Run("a failed disc ID lookup is an error, and not remembered", func(t *testing.T) {
		var n atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if n.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.Write([]byte(discIDResponseBody))
		}))
		defer srv.Close()
		l := newLookup(testClient(srv.URL))

		_, err := l.DiscReleases(context.Background(), testDiscID)
		assert.ErrorContains(t, err, "HTTP 503")

		got, err := l.DiscReleases(context.Background(), testDiscID)
		require.NoError(t, err)
		assert.Len(t, got, 2)
	})

	t.Run("an unknown release is ErrNotFound, from the lookup too", func(t *testing.T) {
		c, _ := serve(t, http.StatusNotFound, "")

		_, err := newLookup(c).ReleaseDiscs(context.Background(), testReleaseID)

		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("a lookup for real is set up to respect the rate limit", func(t *testing.T) {
		l := NewLookup()

		assert.Equal(t, apiBase, l.c.base)
		assert.NotNil(t, l.c.limiter)
	})
}
