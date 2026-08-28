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
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaltedToken(t *testing.T) {
	t.Run("token matches md5(password + salt)", func(t *testing.T) {
		token, salt, err := saltedToken("hunter2")
		require.NoError(t, err)

		sum := md5.Sum([]byte("hunter2" + salt))
		assert.Equal(t, hex.EncodeToString(sum[:]), token)
	})

	t.Run("salt is different on every call", func(t *testing.T) {
		_, saltA, err := saltedToken("hunter2")
		require.NoError(t, err)
		_, saltB, err := saltedToken("hunter2")
		require.NoError(t, err)
		assert.NotEqual(t, saltA, saltB)
	})

	t.Run("different salts produce different tokens for the same password", func(t *testing.T) {
		tokenA, _, err := saltedToken("hunter2")
		require.NoError(t, err)
		tokenB, _, err := saltedToken("hunter2")
		require.NoError(t, err)
		assert.NotEqual(t, tokenA, tokenB)
	})
}

func TestAuthParams(t *testing.T) {
	v, err := authParams("mario", "hunter2")
	require.NoError(t, err)

	assert.Equal(t, "mario", v.Get("u"))
	assert.Equal(t, subsonicAPIVersion, v.Get("v"))
	assert.Equal(t, clientName, v.Get("c"))
	assert.Equal(t, "json", v.Get("f"))
	assert.NotEmpty(t, v.Get("t"))
	assert.NotEmpty(t, v.Get("s"))

	// The token must actually verify against the salt this call produced.
	sum := md5.Sum([]byte("hunter2" + v.Get("s")))
	assert.Equal(t, hex.EncodeToString(sum[:]), v.Get("t"))
}

func TestPing(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/rest/ping", r.URL.Path)
			assert.Equal(t, "mario", r.URL.Query().Get("u"))
			w.Write([]byte(`{"subsonic-response":{"status":"ok"}}`))
		}))
		defer srv.Close()

		err := Ping(srv.URL, "mario", "hunter2")
		assert.NoError(t, err)
	})

	t.Run("server rejects credentials", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"subsonic-response":{"status":"failed","error":{"code":40,"message":"Wrong username or password"}}}`))
		}))
		defer srv.Close()

		err := Ping(srv.URL, "mario", "wrong")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Wrong username or password")
	})

	t.Run("status failed with no error object still produces an error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"subsonic-response":{"status":"failed"}}`))
		}))
		defer srv.Close()

		err := Ping(srv.URL, "mario", "hunter2")
		assert.Error(t, err)
	})

	t.Run("malformed response body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("not json"))
		}))
		defer srv.Close()

		err := Ping(srv.URL, "mario", "hunter2")
		assert.Error(t, err)
	})

	t.Run("unreachable server", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		srv.Close() // closed before use: guarantees the connection fails

		err := Ping(srv.URL, "mario", "hunter2")
		assert.Error(t, err)
	})

	t.Run("trailing slash on baseURL is handled", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/rest/ping", r.URL.Path)
			w.Write([]byte(`{"subsonic-response":{"status":"ok"}}`))
		}))
		defer srv.Close()

		err := Ping(srv.URL+"/", "mario", "hunter2")
		assert.NoError(t, err)
	})
}
