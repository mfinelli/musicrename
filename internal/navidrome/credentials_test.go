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
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withTestConfigDir points userConfigDir at a fresh temp directory for the
// duration of the test, restoring the original afterward. This avoids
// depending on $XDG_CONFIG_HOME (Linux-only; not consulted by
// os.UserConfigDir on macOS) actually being set to something test-safe.
func withTestConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	original := userConfigDir
	userConfigDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { userConfigDir = original })
	return dir
}

func TestConfigDir(t *testing.T) {
	t.Run("creates the musicrename subdirectory with owner-only permissions", func(t *testing.T) {
		base := withTestConfigDir(t)
		dir, err := configDir()
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(base, "musicrename"), dir)

		if runtime.GOOS != "windows" {
			info, err := os.Stat(dir)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
		}
	})

	t.Run("propagates a failure from the underlying resolver", func(t *testing.T) {
		original := userConfigDir
		userConfigDir = func() (string, error) { return "", assert.AnError }
		defer func() { userConfigDir = original }()

		_, err := configDir()
		assert.Error(t, err)
	})
}

func TestCredentialsPath(t *testing.T) {
	base := withTestConfigDir(t)
	path, err := CredentialsPath()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(base, "musicrename", "navidrome.json"), path)
}

func TestLoadCredentials(t *testing.T) {
	t.Run("returns ErrNotLoggedIn when no file exists", func(t *testing.T) {
		withTestConfigDir(t)
		_, err := LoadCredentials()
		assert.ErrorIs(t, err, ErrNotLoggedIn)
	})

	t.Run("returns an error for a malformed file rather than a zero value", func(t *testing.T) {
		base := withTestConfigDir(t)
		dir := filepath.Join(base, "musicrename")
		require.NoError(t, os.MkdirAll(dir, 0o700))
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "navidrome.json"), []byte("not json"), 0o600,
		))

		_, err := LoadCredentials()
		assert.Error(t, err)
		assert.NotErrorIs(t, err, ErrNotLoggedIn)
	})
}

func TestSaveCredentials(t *testing.T) {
	t.Run("writes the file with owner-only permissions", func(t *testing.T) {
		withTestConfigDir(t)
		require.NoError(t, SaveCredentials(Credentials{
			URL: "https://nav.example.com", Username: "mario", Password: "hunter2",
		}))

		path, err := CredentialsPath()
		require.NoError(t, err)

		if runtime.GOOS != "windows" {
			info, err := os.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		}
	})

	t.Run("round-trips through LoadCredentials", func(t *testing.T) {
		withTestConfigDir(t)
		want := Credentials{URL: "https://nav.example.com", Username: "mario", Password: "hunter2"}
		require.NoError(t, SaveCredentials(want))

		got, err := LoadCredentials()
		require.NoError(t, err)
		assert.Equal(t, want, *got)
	})

	t.Run("logging in again overwrites the previous credentials", func(t *testing.T) {
		withTestConfigDir(t)
		require.NoError(t, SaveCredentials(Credentials{
			URL: "https://old.example.com", Username: "old", Password: "old",
		}))
		require.NoError(t, SaveCredentials(Credentials{
			URL: "https://new.example.com", Username: "new", Password: "new",
		}))

		got, err := LoadCredentials()
		require.NoError(t, err)
		assert.Equal(t, "https://new.example.com", got.URL)
		assert.Equal(t, "new", got.Username)
	})
}

func TestClearCredentials(t *testing.T) {
	t.Run("removes an existing credentials file", func(t *testing.T) {
		withTestConfigDir(t)
		require.NoError(t, SaveCredentials(Credentials{URL: "https://nav.example.com"}))

		require.NoError(t, ClearCredentials())

		_, err := LoadCredentials()
		assert.ErrorIs(t, err, ErrNotLoggedIn)
	})

	t.Run("is not an error when not logged in", func(t *testing.T) {
		withTestConfigDir(t)
		assert.NoError(t, ClearCredentials())
	})
}
