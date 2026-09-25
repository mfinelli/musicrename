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
)

// checkServer returns an httptest.Server that always responds with
// release, regardless of the requested MBID since check's tests only ever
// look up one release per case the path isn't worth asserting on here
// (client_test.go already covers request construction).
func checkServer(t *testing.T, release Release) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(release)
	}))
}

func TestCheck(t *testing.T) {
	t.Run("first check for an album records a baseline and reports no changes", func(t *testing.T) {
		dir := t.TempDir()
		release := Release{ID: "d1d196ec-bb2b-4636-81a0-b8f5c5774514", Title: "Back in Black"}
		srv := checkServer(t, release)
		defer srv.Close()

		result, err := check(context.Background(), testClient(srv.URL), release.ID, dir, false)
		require.NoError(t, err)
		assert.True(t, result.FirstCheck)
		assert.Empty(t, result.Changes)

		snap, err := ReadSnapshot(dir)
		require.NoError(t, err)
		assert.Equal(t, release.Title, snap.Title)
	})

	t.Run("a second check against an unchanged release reports no changes", func(t *testing.T) {
		dir := t.TempDir()
		release := Release{ID: "d1d196ec-bb2b-4636-81a0-b8f5c5774514", Title: "Back in Black"}
		srv := checkServer(t, release)
		defer srv.Close()

		_, err := check(context.Background(), testClient(srv.URL), release.ID, dir, false)
		require.NoError(t, err)

		result, err := check(context.Background(), testClient(srv.URL), release.ID, dir, false)
		require.NoError(t, err)
		assert.False(t, result.FirstCheck)
		assert.Empty(t, result.Changes)
	})

	t.Run("a second check against a changed release reports the diff and updates the snapshot", func(t *testing.T) {
		dir := t.TempDir()
		mbid := "d1d196ec-bb2b-4636-81a0-b8f5c5774514"

		srv1 := checkServer(t, Release{ID: mbid, Title: "Back in Black"})
		_, err := check(context.Background(), testClient(srv1.URL), mbid, dir, false)
		require.NoError(t, err)
		srv1.Close()

		srv2 := checkServer(t, Release{ID: mbid, Title: "Back In Black"})
		defer srv2.Close()
		result, err := check(context.Background(), testClient(srv2.URL), mbid, dir, false)
		require.NoError(t, err)
		assert.False(t, result.FirstCheck)
		assert.Contains(t, result.Changes, `title: "Back in Black" → "Back In Black"`)

		snap, err := ReadSnapshot(dir)
		require.NoError(t, err)
		assert.Equal(t, "Back In Black", snap.Title, "snapshot should reflect the new data")
	})

	t.Run("dry run reports the diff but does not update the snapshot", func(t *testing.T) {
		dir := t.TempDir()
		mbid := "d1d196ec-bb2b-4636-81a0-b8f5c5774514"

		srv1 := checkServer(t, Release{ID: mbid, Title: "Back in Black"})
		_, err := check(context.Background(), testClient(srv1.URL), mbid, dir, false)
		require.NoError(t, err)
		srv1.Close()

		srv2 := checkServer(t, Release{ID: mbid, Title: "Back In Black"})
		defer srv2.Close()
		result, err := check(context.Background(), testClient(srv2.URL), mbid, dir, true)
		require.NoError(t, err)
		assert.Contains(t, result.Changes, `title: "Back in Black" → "Back In Black"`)

		snap, err := ReadSnapshot(dir)
		require.NoError(t, err)
		assert.Equal(t, "Back in Black", snap.Title, "dry run must not persist the new data")

		// Running again (still dry) against the same unchanged upstream
		// reports the identical diff, since nothing was ever recorded.
		result2, err := check(context.Background(), testClient(srv2.URL), mbid, dir, true)
		require.NoError(t, err)
		assert.Equal(t, result.Changes, result2.Changes)
	})

	t.Run("dry run on the very first check still records nothing and reports FirstCheck", func(t *testing.T) {
		dir := t.TempDir()
		mbid := "d1d196ec-bb2b-4636-81a0-b8f5c5774514"
		srv := checkServer(t, Release{ID: mbid, Title: "Back in Black"})
		defer srv.Close()

		result, err := check(context.Background(), testClient(srv.URL), mbid, dir, true)
		require.NoError(t, err)
		assert.True(t, result.FirstCheck)

		snap, err := ReadSnapshot(dir)
		require.NoError(t, err)
		assert.Nil(t, snap, "dry run must not write a baseline either")
	})

	t.Run("propagates a client error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		_, err := check(context.Background(), testClient(srv.URL), "00000000-0000-0000-0000-000000000000", t.TempDir(), false)
		assert.ErrorIs(t, err, ErrNotFound)
	})
}
