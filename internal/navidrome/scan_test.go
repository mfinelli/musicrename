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

package navidrome

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	subsonic "github.com/supersonic-app/go-subsonic/subsonic"
)

// testScanClient builds a subsonic.Client pointed at a test server, without
// going through Authenticate (Scan doesn't care how the client came to be
// authenticated, and the fake servers in this file don't validate auth
// params at all).
func testScanClient(url string) *subsonic.Client {
	return &subsonic.Client{
		Client:     &http.Client{Timeout: 5 * time.Second},
		BaseUrl:    url,
		User:       "mario",
		ClientName: clientName,
		UseJSON:    true,
	}
}

func scanResponse(scanning bool, count int64) string {
	return fmt.Sprintf(
		`{"subsonic-response":{"status":"ok","scanStatus":{"scanning":%t,"count":%d}}}`,
		scanning, count,
	)
}

func TestScan(t *testing.T) {
	t.Run("returns immediately when StartScan already reports done", func(t *testing.T) {
		var startCalls, statusCalls int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.Contains(r.URL.Path, "startScan"):
				atomic.AddInt32(&startCalls, 1)
				fmt.Fprint(w, scanResponse(false, 42))
			case strings.Contains(r.URL.Path, "getScanStatus"):
				atomic.AddInt32(&statusCalls, 1)
				fmt.Fprint(w, scanResponse(false, 42))
			}
		}))
		defer srv.Close()

		var progressCalls int
		err := Scan(testScanClient(srv.URL), time.Millisecond, func(ScanProgress) { progressCalls++ })
		require.NoError(t, err)
		assert.Equal(t, int32(1), atomic.LoadInt32(&startCalls))
		assert.Equal(t, int32(0), atomic.LoadInt32(&statusCalls),
			"should never poll if StartScan already reported the scan done")
		assert.Zero(t, progressCalls, "no progress calls for an already-finished scan")
	})

	t.Run("polls until done, reporting progress for every still-running check", func(t *testing.T) {
		var statusCalls int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.Contains(r.URL.Path, "startScan"):
				fmt.Fprint(w, scanResponse(true, 0))
			case strings.Contains(r.URL.Path, "getScanStatus"):
				n := atomic.AddInt32(&statusCalls, 1)
				if n < 3 {
					fmt.Fprint(w, scanResponse(true, int64(n)*10))
				} else {
					fmt.Fprint(w, scanResponse(false, 100))
				}
			}
		}))
		defer srv.Close()

		var progressCalls []ScanProgress
		err := Scan(testScanClient(srv.URL), time.Millisecond, func(p ScanProgress) {
			progressCalls = append(progressCalls, p)
		})
		require.NoError(t, err)
		require.Len(t, progressCalls, 3, "one progress call per still-running check (StartScan + 2 polls)")
		assert.Equal(t, int64(0), progressCalls[0].Count)
		assert.Equal(t, int64(10), progressCalls[1].Count)
		assert.Equal(t, int64(20), progressCalls[2].Count)
	})

	t.Run("nil progress callback is safe", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, scanResponse(false, 0))
		}))
		defer srv.Close()

		assert.NoError(t, Scan(testScanClient(srv.URL), time.Millisecond, nil))
	})

	t.Run("propagates a StartScan failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"subsonic-response":{"status":"failed","error":{"code":50,"message":"not authorized"}}}`)
		}))
		defer srv.Close()

		err := Scan(testScanClient(srv.URL), time.Millisecond, nil)
		assert.Error(t, err)
	})

	t.Run("propagates a GetScanStatus failure mid-poll", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.Contains(r.URL.Path, "startScan"):
				fmt.Fprint(w, scanResponse(true, 0))
			case strings.Contains(r.URL.Path, "getScanStatus"):
				fmt.Fprint(w, `{"subsonic-response":{"status":"failed","error":{"code":0,"message":"boom"}}}`)
			}
		}))
		defer srv.Close()

		err := Scan(testScanClient(srv.URL), time.Millisecond, nil)
		assert.Error(t, err)
	})
}
