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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	subsonic "github.com/supersonic-app/go-subsonic/subsonic"

	"github.com/mfinelli/musicrename/internal/playlist"
)

// testClient builds a subsonic.Client pointed at url, without going
// through Authenticate (these tests' fake servers don't validate auth
// params at all).
func testClient(url string) *subsonic.Client {
	return &subsonic.Client{
		Client:     &http.Client{Timeout: 5 * time.Second},
		BaseUrl:    url,
		User:       "mario",
		ClientName: "musicrename-test",
		UseJSON:    true,
	}
}

func playlistEntryJSON(paths []string) string {
	entries := make([]string, len(paths))
	for i, p := range paths {
		entries[i] = fmt.Sprintf(`{"id":"song-%d","path":%q}`, i, p)
	}
	return strings.Join(entries, ",")
}

// singlePlaylistJSON builds a getPlaylist response body for one playlist
// with the given id/name/track paths and no comment.
func singlePlaylistJSON(id, name string, paths []string) string {
	return singlePlaylistJSONWithComment(id, name, "", paths)
}

// singlePlaylistJSONWithComment is singlePlaylistJSON with an explicit
// comment field, for tests exercising #TARGETS: reconciliation via the
// musicrename-managed comment suffix (comment.go).
func singlePlaylistJSONWithComment(id, name, comment string, paths []string) string {
	return fmt.Sprintf(
		`{"subsonic-response":{"status":"ok","playlist":{"id":%q,"name":%q,"comment":%q,"entry":[%s]}}}`,
		id, name, comment, playlistEntryJSON(paths),
	)
}

// listPlaylistsJSON builds a getPlaylists (bulk list) response body. The
// real endpoint doesn't include track entries in the list response, only
// id/name: matching that here.
func listPlaylistsJSON(idsAndNames map[string]string) string {
	items := make([]string, 0, len(idsAndNames))
	for id, name := range idsAndNames {
		items = append(items, fmt.Sprintf(`{"id":%q,"name":%q}`, id, name))
	}
	return fmt.Sprintf(`{"subsonic-response":{"status":"ok","playlists":{"playlist":[%s]}}}`, strings.Join(items, ","))
}

func notFoundJSON() string {
	return `{"subsonic-response":{"status":"failed","error":{"code":70,"message":"The requested data was not found."}}}`
}

func genericErrorJSON() string {
	return `{"subsonic-response":{"status":"failed","error":{"code":0,"message":"a database error occurred"}}}`
}

// touchLocalFile creates an empty file at root/relPath, so an entry
// referencing it resolves successfully.
func touchLocalFile(t *testing.T, root, relPath string) {
	t.Helper()
	full := filepath.Join(root, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
	require.NoError(t, os.WriteFile(full, []byte("audio"), 0644))
}

func TestPullAll(t *testing.T) {
	t.Run("a remote #TARGETS: suffix reordered but set-equal to local is Unchanged", func(t *testing.T) {
		root := t.TempDir()
		touchLocalFile(t, root, "main/a/artist/album/01 track.flac")

		localPath := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, playlist.WriteGlobalPlaylist(localPath, &playlist.GlobalPlaylist{
			Name: "Road Trip", NavidromeID: "id-1", HasNavidromeID: true,
			Targets: []string{"ipod", "sdcard"}, HasTargets: true,
			Entries: []string{"main/a/artist/album/01 track.flac"},
		}))

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.Contains(r.URL.Path, "getPlaylists"):
				fmt.Fprint(w, listPlaylistsJSON(map[string]string{"id-1": "Road Trip"}))
			case strings.Contains(r.URL.Path, "getPlaylist"):
				// Same targets, different (unsorted) order: must not
				// register as a change.
				fmt.Fprint(w, singlePlaylistJSONWithComment(
					"id-1", "Road Trip", "[musicrename:targets=sdcard,ipod]",
					[]string{"main/a/artist/album/01 track.flac"},
				))
			}
		}))
		defer srv.Close()

		result, err := PullAll(testClient(srv.URL), root, false)
		require.NoError(t, err)
		require.Len(t, result.Unchanged, 1)
		assert.Empty(t, result.Updated)
	})

	t.Run("creates a new local file for a remote playlist with no local correlation", func(t *testing.T) {
		root := t.TempDir()
		touchLocalFile(t, root, "main/a/artist/album/01 track.flac")

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.Contains(r.URL.Path, "getPlaylists"):
				fmt.Fprint(w, listPlaylistsJSON(map[string]string{"id-1": "Road Trip"}))
			case strings.Contains(r.URL.Path, "getPlaylist"):
				fmt.Fprint(w, singlePlaylistJSON("id-1", "Road Trip", []string{"main/a/artist/album/01 track.flac"}))
			}
		}))
		defer srv.Close()

		result, err := PullAll(testClient(srv.URL), root, false)
		require.NoError(t, err)
		require.Len(t, result.Created, 1)
		assert.Empty(t, result.Updated)
		assert.Empty(t, result.Unchanged)
		assert.Empty(t, result.Deleted)
		assert.Empty(t, result.Warnings)

		gp, err := playlist.ReadGlobalPlaylist(result.Created[0])
		require.NoError(t, err)
		assert.Equal(t, "Road Trip", gp.Name)
		assert.Equal(t, "id-1", gp.NavidromeID)
		assert.False(t, gp.HasTargets, "a newly-pulled playlist gets no #TARGETS: directive")
		assert.Equal(t, []string{"main/a/artist/album/01 track.flac"}, gp.Entries)
		assert.Equal(t, filepath.Join(root, "playlists", "road trip.m3u8"), result.Created[0])
	})

	t.Run("updates an existing correlated file when content differs, syncing #TARGETS: from the remote comment", func(t *testing.T) {
		root := t.TempDir()
		touchLocalFile(t, root, "main/a/artist/album/01 old.flac")
		touchLocalFile(t, root, "main/a/artist/album/02 new.flac")

		localPath := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, playlist.WriteGlobalPlaylist(localPath, &playlist.GlobalPlaylist{
			Name:           "Old Name",
			NavidromeID:    "id-1",
			HasNavidromeID: true,
			Targets:        []string{"ipod"},
			HasTargets:     true,
			Entries:        []string{"main/a/artist/album/01 old.flac"},
		}))

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.Contains(r.URL.Path, "getPlaylists"):
				fmt.Fprint(w, listPlaylistsJSON(map[string]string{"id-1": "New Name"}))
			case strings.Contains(r.URL.Path, "getPlaylist"):
				fmt.Fprint(w, singlePlaylistJSONWithComment(
					"id-1", "New Name", "[musicrename:targets=sdcard]",
					[]string{"main/a/artist/album/02 new.flac"},
				))
			}
		}))
		defer srv.Close()

		result, err := PullAll(testClient(srv.URL), root, false)
		require.NoError(t, err)
		require.Len(t, result.Updated, 1)
		assert.Equal(t, localPath, result.Updated[0])

		gp, err := playlist.ReadGlobalPlaylist(localPath)
		require.NoError(t, err)
		assert.Equal(t, "New Name", gp.Name)
		assert.Equal(t, []string{"main/a/artist/album/02 new.flac"}, gp.Entries)
		assert.Equal(t, []string{"sdcard"}, gp.Targets, "#TARGETS: must come from the remote comment, not stay ipod")
		assert.True(t, gp.HasTargets)
	})

	t.Run("reconciles #TARGETS: removal when the remote comment loses the suffix", func(t *testing.T) {
		root := t.TempDir()
		touchLocalFile(t, root, "main/a/artist/album/01 track.flac")

		localPath := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, playlist.WriteGlobalPlaylist(localPath, &playlist.GlobalPlaylist{
			Name:           "Road Trip",
			NavidromeID:    "id-1",
			HasNavidromeID: true,
			Targets:        []string{"ipod"},
			HasTargets:     true,
			Entries:        []string{"main/a/artist/album/01 track.flac"},
		}))

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.Contains(r.URL.Path, "getPlaylists"):
				fmt.Fprint(w, listPlaylistsJSON(map[string]string{"id-1": "Road Trip"}))
			case strings.Contains(r.URL.Path, "getPlaylist"):
				// Comment has no #musicrename:targets suffix at all now
				// as if it were removed by hand in the Navidrome app.
				fmt.Fprint(w, singlePlaylistJSONWithComment(
					"id-1", "Road Trip", "", []string{"main/a/artist/album/01 track.flac"},
				))
			}
		}))
		defer srv.Close()

		result, err := PullAll(testClient(srv.URL), root, false)
		require.NoError(t, err)
		require.Len(t, result.Updated, 1)

		gp, err := playlist.ReadGlobalPlaylist(localPath)
		require.NoError(t, err)
		assert.False(t, gp.HasTargets, "a removed remote suffix must clear local #TARGETS:, not leave it stale")
		assert.Empty(t, gp.Targets)
	})

	t.Run("leaves an existing correlated file unchanged when content matches", func(t *testing.T) {
		root := t.TempDir()
		touchLocalFile(t, root, "main/a/artist/album/01 track.flac")

		localPath := filepath.Join(root, "playlists", "roadtrip.m3u8")
		gp := &playlist.GlobalPlaylist{
			Name: "Road Trip", NavidromeID: "id-1", HasNavidromeID: true,
			Entries: []string{"main/a/artist/album/01 track.flac"},
		}
		require.NoError(t, playlist.WriteGlobalPlaylist(localPath, gp))
		before, err := os.Stat(localPath)
		require.NoError(t, err)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.Contains(r.URL.Path, "getPlaylists"):
				fmt.Fprint(w, listPlaylistsJSON(map[string]string{"id-1": "Road Trip"}))
			case strings.Contains(r.URL.Path, "getPlaylist"):
				fmt.Fprint(w, singlePlaylistJSON("id-1", "Road Trip", []string{"main/a/artist/album/01 track.flac"}))
			}
		}))
		defer srv.Close()

		result, err := PullAll(testClient(srv.URL), root, false)
		require.NoError(t, err)
		require.Len(t, result.Unchanged, 1)
		assert.Empty(t, result.Updated)
		assert.Empty(t, result.Created)

		after, err := os.Stat(localPath)
		require.NoError(t, err)
		assert.Equal(t, before.ModTime(), after.ModTime(), "an unchanged playlist must not be rewritten")
	})

	t.Run("deletes a local file whose ID no longer appears in the remote list", func(t *testing.T) {
		root := t.TempDir()
		localPath := filepath.Join(root, "playlists", "gone.m3u8")
		require.NoError(t, playlist.WriteGlobalPlaylist(localPath, &playlist.GlobalPlaylist{
			Name: "Gone", NavidromeID: "id-gone", HasNavidromeID: true,
			Entries: []string{"whatever.flac"},
		}))

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "getPlaylists") {
				fmt.Fprint(w, listPlaylistsJSON(map[string]string{})) // empty: nothing remote
			}
		}))
		defer srv.Close()

		result, err := PullAll(testClient(srv.URL), root, false)
		require.NoError(t, err)
		require.Len(t, result.Deleted, 1)
		assert.Equal(t, localPath, result.Deleted[0])
		_, statErr := os.Stat(localPath)
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("skips an entry that doesn't resolve to a local file, with a warning", func(t *testing.T) {
		root := t.TempDir()
		// Note: no local file is created for this entry.

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.Contains(r.URL.Path, "getPlaylists"):
				fmt.Fprint(w, listPlaylistsJSON(map[string]string{"id-1": "Road Trip"}))
			case strings.Contains(r.URL.Path, "getPlaylist"):
				fmt.Fprint(w, singlePlaylistJSON("id-1", "Road Trip", []string{"main/a/artist/album/missing.flac"}))
			}
		}))
		defer srv.Close()

		result, err := PullAll(testClient(srv.URL), root, false)
		require.NoError(t, err)
		require.Len(t, result.Created, 1)
		require.Len(t, result.Warnings, 1)
		assert.Contains(t, result.Warnings[0], "missing.flac")

		gp, err := playlist.ReadGlobalPlaylist(result.Created[0])
		require.NoError(t, err)
		assert.Empty(t, gp.Entries, "the unresolvable entry must not be written")
	})

	t.Run("dry run makes no filesystem changes but still reports intended actions", func(t *testing.T) {
		root := t.TempDir()
		touchLocalFile(t, root, "main/a/artist/album/01 track.flac")

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.Contains(r.URL.Path, "getPlaylists"):
				fmt.Fprint(w, listPlaylistsJSON(map[string]string{"id-1": "Road Trip"}))
			case strings.Contains(r.URL.Path, "getPlaylist"):
				fmt.Fprint(w, singlePlaylistJSON("id-1", "Road Trip", []string{"main/a/artist/album/01 track.flac"}))
			}
		}))
		defer srv.Close()

		result, err := PullAll(testClient(srv.URL), root, true)
		require.NoError(t, err)
		require.Len(t, result.Created, 1)

		_, statErr := os.Stat(result.Created[0])
		assert.True(t, os.IsNotExist(statErr), "dry run must not actually write the file")
	})

	t.Run("a single playlist's detail fetch failure is a warning, not fatal to the whole pull", func(t *testing.T) {
		root := t.TempDir()
		touchLocalFile(t, root, "main/a/artist/album/01 track.flac")

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.Contains(r.URL.Path, "getPlaylists"):
				fmt.Fprint(w, listPlaylistsJSON(map[string]string{
					"id-bad": "Broken", "id-good": "Good",
				}))
			case strings.Contains(r.URL.Path, "getPlaylist"):
				id := r.URL.Query().Get("id")
				if id == "id-bad" {
					fmt.Fprint(w, genericErrorJSON())
				} else {
					fmt.Fprint(w, singlePlaylistJSON("id-good", "Good", []string{"main/a/artist/album/01 track.flac"}))
				}
			}
		}))
		defer srv.Close()

		result, err := PullAll(testClient(srv.URL), root, false)
		require.NoError(t, err)
		require.Len(t, result.Created, 1, "the good playlist should still be pulled")
		require.Len(t, result.Warnings, 1)
		assert.Contains(t, result.Warnings[0], "Broken")
	})

	t.Run("GetPlaylists failure aborts the whole pull", func(t *testing.T) {
		root := t.TempDir()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, genericErrorJSON())
		}))
		defer srv.Close()

		_, err := PullAll(testClient(srv.URL), root, false)
		assert.Error(t, err)
	})
}

func TestPullOne(t *testing.T) {
	t.Run("errors when the file doesn't exist", func(t *testing.T) {
		root := t.TempDir()
		_, err := PullOne(testClient("http://unused"), filepath.Join(root, "playlists", "missing.m3u8"), false)
		assert.Error(t, err)
	})

	t.Run("errors when the file has no #NAVIDROME-ID", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "local-only.m3u8")
		require.NoError(t, playlist.WriteGlobalPlaylist(path, &playlist.GlobalPlaylist{
			Name: "Local Only", Entries: []string{"track.flac"},
		}))

		_, err := PullOne(testClient("http://unused"), path, false)
		assert.Error(t, err)

		// The file must be left completely alone.
		gp, readErr := playlist.ReadGlobalPlaylist(path)
		require.NoError(t, readErr)
		assert.Equal(t, "Local Only", gp.Name)
	})

	t.Run("updates content on a successful fetch, syncing #TARGETS: from the remote comment", func(t *testing.T) {
		root := t.TempDir()
		touchLocalFile(t, root, "main/a/artist/album/02 new.flac")

		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, playlist.WriteGlobalPlaylist(path, &playlist.GlobalPlaylist{
			Name: "Old", NavidromeID: "id-1", HasNavidromeID: true,
			Targets: []string{"sdcard"}, HasTargets: true,
			Entries: []string{"main/a/artist/album/01 old.flac"},
		}))

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, singlePlaylistJSONWithComment(
				"id-1", "New", "[musicrename:targets=ipod]", []string{"main/a/artist/album/02 new.flac"},
			))
		}))
		defer srv.Close()

		result, err := PullOne(testClient(srv.URL), path, false)
		require.NoError(t, err)
		require.Len(t, result.Updated, 1)

		gp, err := playlist.ReadGlobalPlaylist(path)
		require.NoError(t, err)
		assert.Equal(t, "New", gp.Name)
		assert.Equal(t, []string{"main/a/artist/album/02 new.flac"}, gp.Entries)
		assert.Equal(t, []string{"ipod"}, gp.Targets, "#TARGETS: must come from the remote comment, not stay sdcard")
	})

	t.Run("self-heals: deletes the local file on a confirmed not-found response", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "gone.m3u8")
		require.NoError(t, playlist.WriteGlobalPlaylist(path, &playlist.GlobalPlaylist{
			Name: "Gone", NavidromeID: "id-gone", HasNavidromeID: true,
		}))

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, notFoundJSON())
		}))
		defer srv.Close()

		result, err := PullOne(testClient(srv.URL), path, false)
		require.NoError(t, err)
		require.Len(t, result.Deleted, 1)
		_, statErr := os.Stat(path)
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("dry run reports the delete without removing the file", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "gone.m3u8")
		require.NoError(t, playlist.WriteGlobalPlaylist(path, &playlist.GlobalPlaylist{
			Name: "Gone", NavidromeID: "id-gone", HasNavidromeID: true,
		}))

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, notFoundJSON())
		}))
		defer srv.Close()

		result, err := PullOne(testClient(srv.URL), path, true)
		require.NoError(t, err)
		require.Len(t, result.Deleted, 1)
		assert.FileExists(t, path, "dry run must not actually delete the file")
	})

	t.Run("a generic (non-not-found) failure aborts without touching the file", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "playlists", "roadtrip.m3u8")
		require.NoError(t, playlist.WriteGlobalPlaylist(path, &playlist.GlobalPlaylist{
			Name: "Road Trip", NavidromeID: "id-1", HasNavidromeID: true,
		}))

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, genericErrorJSON())
		}))
		defer srv.Close()

		_, err := PullOne(testClient(srv.URL), path, false)
		assert.Error(t, err)
		assert.FileExists(t, path, "a generic failure must never delete the local file")
	})
}
