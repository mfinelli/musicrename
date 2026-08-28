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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/playlist"
)

// pushFakeServer is a small router + call counter for the push tests
// below, since PushOne can issue several distinct requests (search3,
// createPlaylist, updatePlaylist for both name and track changes, all
// sharing the "updatePlaylist" endpoint name) and some tests need to
// assert how many of a given kind were actually made.
type pushFakeServer struct {
	mu      sync.Mutex
	calls   map[string]int
	search3 string // canned search3 response body
	// createID, if set, is returned as the id in a createPlaylist response.
	createID string
}

func newPushFakeServer() *pushFakeServer {
	return &pushFakeServer{calls: make(map[string]int)}
}

func (f *pushFakeServer) count(endpoint string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[endpoint]
}

func (f *pushFakeServer) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var matched string
		for _, ep := range []string{"search3", "createPlaylist", "updatePlaylist", "getPlaylist", "getPlaylists"} {
			if strings.Contains(r.URL.Path, ep) {
				matched = ep
				break
			}
		}
		f.mu.Lock()
		f.calls[matched]++
		f.mu.Unlock()

		switch matched {
		case "search3":
			body := f.search3
			if body == "" {
				body = `{"subsonic-response":{"status":"ok","searchResult3":{}}}`
			}
			fmt.Fprint(w, body)
		case "createPlaylist":
			id := f.createID
			if id == "" {
				id = "new-id"
			}
			fmt.Fprintf(w, `{"subsonic-response":{"status":"ok","playlist":{"id":%q,"name":"new"}}}`, id)
		case "updatePlaylist":
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok"}}`)
		default:
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok"}}`)
		}
	}))
}

func TestPushOne(t *testing.T) {
	t.Run("errors when the file does not exist", func(t *testing.T) {
		root := t.TempDir()
		_, err := PushOne(testClient("http://unused"), filepath.Join(root, "playlists", "missing.m3u8"), false)
		assert.Error(t, err)
	})

	t.Run("creates a new remote playlist and writes the new ID back locally", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, playlist.WriteGlobalPlaylist(path, &playlist.GlobalPlaylist{
			Name: "Road Trip", Entries: []string{"main/a/artist/album/01 track.flac"},
		}))

		f := newPushFakeServer()
		f.search3 = `{"subsonic-response":{"status":"ok","searchResult3":{"song":[` +
			`{"id":"song-1","path":"main/a/artist/album/01 track.flac"}]}}}`
		f.createID = "created-id"
		srv := f.server()
		defer srv.Close()

		result, err := PushOne(testClient(srv.URL), path, false)
		require.NoError(t, err)
		require.Len(t, result.Created, 1)
		assert.Empty(t, result.Warnings)

		gp, err := playlist.ReadGlobalPlaylist(path)
		require.NoError(t, err)
		assert.Equal(t, "created-id", gp.NavidromeID)
		assert.True(t, gp.HasNavidromeID)
	})

	t.Run("an unresolvable entry produces a warning and is excluded, but push still proceeds", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, playlist.WriteGlobalPlaylist(path, &playlist.GlobalPlaylist{
			Name: "Road Trip", Entries: []string{"main/a/artist/album/gone.flac"},
		}))

		f := newPushFakeServer() // search3 default: no songs found
		srv := f.server()
		defer srv.Close()

		result, err := PushOne(testClient(srv.URL), path, false)
		require.NoError(t, err)
		require.Len(t, result.Created, 1)
		require.Len(t, result.Warnings, 1)
		assert.Contains(t, result.Warnings[0], "gone.flac")
	})

	t.Run("already-correlated file with matching remote content is Unchanged, no update calls made", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, playlist.WriteGlobalPlaylist(path, &playlist.GlobalPlaylist{
			Name: "Road Trip", NavidromeID: "id-1", HasNavidromeID: true,
			Entries: []string{"main/a/artist/album/01 track.flac"},
		}))

		f := newPushFakeServer()
		f.search3 = `{"subsonic-response":{"status":"ok","searchResult3":{"song":[` +
			`{"id":"song-1","path":"main/a/artist/album/01 track.flac"}]}}}`
		srv := f.server()
		defer srv.Close()

		// getPlaylist needs to report the same name/entries as local for
		// this to be classified Unchanged.
		mux := http.NewServeMux()
		mux.HandleFunc("/rest/getPlaylist", func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			f.calls["getPlaylist"]++
			f.mu.Unlock()
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","playlist":{"id":"id-1","name":"Road Trip",`+
				`"entry":[{"id":"song-1","path":"main/a/artist/album/01 track.flac"}]}}}`)
		})
		mux.HandleFunc("/rest/search3", func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			f.calls["search3"]++
			f.mu.Unlock()
			fmt.Fprint(w, f.search3)
		})
		mux.HandleFunc("/rest/updatePlaylist", func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			f.calls["updatePlaylist"]++
			f.mu.Unlock()
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok"}}`)
		})
		srv2 := httptest.NewServer(mux)
		defer srv2.Close()

		result, err := PushOne(testClient(srv2.URL), path, false)
		require.NoError(t, err)
		require.Len(t, result.Unchanged, 1)
		assert.Equal(t, 0, f.count("updatePlaylist"))
	})

	t.Run("resolves all entries with a single search3 call, not one per track", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "big.m3u8")

		entries := make([]string, 25)
		songs := make([]string, 25)
		for i := range entries {
			entries[i] = fmt.Sprintf("main/a/artist/album/%02d track.flac", i)
			songs[i] = fmt.Sprintf(`{"id":"song-%d","path":"main/a/artist/album/%02d track.flac"}`, i, i)
		}
		require.NoError(t, playlist.WriteGlobalPlaylist(path, &playlist.GlobalPlaylist{
			Name: "Big Playlist", Entries: entries,
		}))

		f := newPushFakeServer()
		f.search3 = fmt.Sprintf(`{"subsonic-response":{"status":"ok","searchResult3":{"song":[%s]}}}`,
			strings.Join(songs, ","))
		srv := f.server()
		defer srv.Close()

		result, err := PushOne(testClient(srv.URL), path, false)
		require.NoError(t, err)
		require.Len(t, result.Created, 1)
		assert.Empty(t, result.Warnings)
		assert.Equal(t, 1, f.count("search3"),
			"resolving 25 entries must take exactly one search3 call, not 25")
	})

	t.Run("dry run creates nothing but still reports Created", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, playlist.WriteGlobalPlaylist(path, &playlist.GlobalPlaylist{
			Name: "Road Trip", Entries: []string{"main/a/artist/album/01 track.flac"},
		}))

		f := newPushFakeServer()
		f.search3 = `{"subsonic-response":{"status":"ok","searchResult3":{"song":[` +
			`{"id":"song-1","path":"main/a/artist/album/01 track.flac"}]}}}`
		srv := f.server()
		defer srv.Close()

		result, err := PushOne(testClient(srv.URL), path, true)
		require.NoError(t, err)
		require.Len(t, result.Created, 1)
		assert.Equal(t, 0, f.count("createPlaylist"), "dry run must not actually create anything remotely")

		gp, err := playlist.ReadGlobalPlaylist(path)
		require.NoError(t, err)
		assert.False(t, gp.HasNavidromeID, "dry run must not write back an ID")
	})

	t.Run("creates a new remote playlist with #TARGETS: encoded in the comment", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, playlist.WriteGlobalPlaylist(path, &playlist.GlobalPlaylist{
			Name: "Road Trip", Targets: []string{"ipod", "sdcard"}, HasTargets: true,
			Entries: []string{"main/a/artist/album/01 track.flac"},
		}))

		var capturedComment string
		mux := http.NewServeMux()
		mux.HandleFunc("/rest/search3", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","searchResult3":{"song":[`+
				`{"id":"song-1","path":"main/a/artist/album/01 track.flac"}]}}}`)
		})
		mux.HandleFunc("/rest/createPlaylist", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","playlist":{"id":"new-id","name":"Road Trip"}}}`)
		})
		mux.HandleFunc("/rest/updatePlaylist", func(w http.ResponseWriter, r *http.Request) {
			if c := r.URL.Query().Get("comment"); c != "" {
				capturedComment = c
			}
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok"}}`)
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()

		result, err := PushOne(testClient(srv.URL), path, false)
		require.NoError(t, err)
		require.Len(t, result.Created, 1)
		assert.Equal(t, "[musicrename:targets=ipod,sdcard]", capturedComment)
	})

	t.Run("updates the remote comment's suffix while preserving human text, when local targets change", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, playlist.WriteGlobalPlaylist(path, &playlist.GlobalPlaylist{
			Name: "Road Trip", NavidromeID: "id-1", HasNavidromeID: true,
			Targets: []string{"sdcard"}, HasTargets: true,
			Entries: []string{"main/a/artist/album/01 track.flac"},
		}))

		var capturedComment string
		mux := http.NewServeMux()
		mux.HandleFunc("/rest/search3", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","searchResult3":{"song":[`+
				`{"id":"song-1","path":"main/a/artist/album/01 track.flac"}]}}}`)
		})
		mux.HandleFunc("/rest/getPlaylist", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","playlist":{"id":"id-1","name":"Road Trip",`+
				`"comment":"Great mix [musicrename:targets=ipod]",`+
				`"entry":[{"id":"song-1","path":"main/a/artist/album/01 track.flac"}]}}}`)
		})
		mux.HandleFunc("/rest/updatePlaylist", func(w http.ResponseWriter, r *http.Request) {
			if c := r.URL.Query().Get("comment"); r.URL.Query().Has("comment") {
				capturedComment = c
			}
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok"}}`)
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()

		result, err := PushOne(testClient(srv.URL), path, false)
		require.NoError(t, err)
		require.Len(t, result.Updated, 1)
		assert.Equal(t, "Great mix [musicrename:targets=sdcard]", capturedComment,
			"human text must survive; only the suffix's target list should change")
	})

	t.Run("removes the #TARGETS: suffix but preserves human text, when local #TARGETS: is removed", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, playlist.WriteGlobalPlaylist(path, &playlist.GlobalPlaylist{
			Name: "Road Trip", NavidromeID: "id-1", HasNavidromeID: true,
			// No Targets/HasTargets: locally removed.
			Entries: []string{"main/a/artist/album/01 track.flac"},
		}))

		var capturedComment string
		var commentSent bool
		mux := http.NewServeMux()
		mux.HandleFunc("/rest/search3", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","searchResult3":{"song":[`+
				`{"id":"song-1","path":"main/a/artist/album/01 track.flac"}]}}}`)
		})
		mux.HandleFunc("/rest/getPlaylist", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","playlist":{"id":"id-1","name":"Road Trip",`+
				`"comment":"Great mix [musicrename:targets=ipod]",`+
				`"entry":[{"id":"song-1","path":"main/a/artist/album/01 track.flac"}]}}}`)
		})
		mux.HandleFunc("/rest/updatePlaylist", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Has("comment") {
				commentSent = true
				capturedComment = r.URL.Query().Get("comment")
			}
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok"}}`)
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()

		result, err := PushOne(testClient(srv.URL), path, false)
		require.NoError(t, err)
		require.Len(t, result.Updated, 1)
		require.True(t, commentSent)
		assert.Equal(t, "Great mix", capturedComment, "the suffix must be gone but the human text kept")
	})

	t.Run("a comment-only difference is enough to classify as Updated, not Unchanged", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, playlist.WriteGlobalPlaylist(path, &playlist.GlobalPlaylist{
			Name: "Road Trip", NavidromeID: "id-1", HasNavidromeID: true,
			Targets: []string{"ipod"}, HasTargets: true,
			Entries: []string{"main/a/artist/album/01 track.flac"},
		}))

		mux := http.NewServeMux()
		mux.HandleFunc("/rest/search3", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","searchResult3":{"song":[`+
				`{"id":"song-1","path":"main/a/artist/album/01 track.flac"}]}}}`)
		})
		mux.HandleFunc("/rest/getPlaylist", func(w http.ResponseWriter, r *http.Request) {
			// Same name and entries, but no #TARGETS: suffix remotely yet.
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","playlist":{"id":"id-1","name":"Road Trip",`+
				`"entry":[{"id":"song-1","path":"main/a/artist/album/01 track.flac"}]}}}`)
		})
		mux.HandleFunc("/rest/updatePlaylist", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok"}}`)
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()

		result, err := PushOne(testClient(srv.URL), path, false)
		require.NoError(t, err)
		assert.Len(t, result.Updated, 1)
		assert.Empty(t, result.Unchanged)
	})
}

func TestPushAll(t *testing.T) {
	t.Run("pushes every file found and aggregates results", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "playlists"), 0755))
		require.NoError(t, playlist.WriteGlobalPlaylist(
			filepath.Join(root, "playlists", "a.m3u8"),
			&playlist.GlobalPlaylist{Name: "A", Entries: []string{"main/a/track.flac"}},
		))
		require.NoError(t, playlist.WriteGlobalPlaylist(
			filepath.Join(root, "playlists", "b.m3u8"),
			&playlist.GlobalPlaylist{Name: "B", Entries: []string{"main/b/track.flac"}},
		))

		f := newPushFakeServer() // no songs resolve; both still get Created (empty) with warnings
		srv := f.server()
		defer srv.Close()

		result, err := PushAll(testClient(srv.URL), root, false)
		require.NoError(t, err)
		assert.Len(t, result.Created, 2)
		assert.Len(t, result.Warnings, 2)
	})
}
