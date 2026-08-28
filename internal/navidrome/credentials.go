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

// Package navidrome implements the pieces needed to authenticate against a
// Navidrome server: credential storage and Subsonic API auth/validation.
package navidrome

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// credentialsFilename is the name of the credentials file within this
// tool's config directory.
const credentialsFilename = "navidrome.json"

// Credentials holds what's needed to authenticate against a single
// Navidrome server via the Subsonic API.
//
// There is no separate "API token" concept in Navidrome distinct from the
// account password (the Subsonic API's auth scheme is a per-request salted
// token computed fresh from the password, not a session or a revocable
// credential). Storing the password is therefore unavoidable; recommend using
// a dedicated Navidrome user for this tool rather than a primary account,
// since there's no way to scope or independently revoke this credential
// otherwise.
type Credentials struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// ErrNotLoggedIn is returned by [LoadCredentials] when no credentials file
// exists.
var ErrNotLoggedIn = errors.New("not logged in; run 'musicrename login' first")

// userConfigDir is os.UserConfigDir, indirected so tests can override it
// without depending on the platform-specific environment variable
// (XDG_CONFIG_HOME on Linux; not consulted at all on macOS) actually being
// set to something test-safe in the environment the tests happen to run in.
var userConfigDir = os.UserConfigDir

// configDir returns this tool's directory under the user's config directory
// (XDG_CONFIG_HOME on Linux via [os.UserConfigDir]; the platform-appropriate
// equivalent elsewhere), creating it with permissions restricted to the owner
// if it doesn't already exist.
func configDir() (string, error) {
	base, err := userConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolving config directory: %w", err)
	}

	dir := filepath.Join(base, "musicrename")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}
	return dir, nil
}

// CredentialsPath returns the absolute path to the stored credentials file,
// creating its parent directory if necessary. The file itself may or may
// not exist yet.
func CredentialsPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, credentialsFilename), nil
}

// LoadCredentials reads and parses the stored credentials. It returns
// [ErrNotLoggedIn] if no credentials file exists.
func LoadCredentials() (*Credentials, error) {
	path, err := CredentialsPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotLoggedIn
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &creds, nil
}

// SaveCredentials writes creds to the credentials file with permissions
// restricted to the owner (0600), overwriting whatever was there before.
// musicrename supports one configured server at a time so logging in again
// simply replaces the previous credentials, there is no concept of multiple
// stored profiles to choose between.
func SaveCredentials(creds Credentials) error {
	path, err := CredentialsPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding credentials: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// ClearCredentials removes the stored credentials file, if present. It is
// not an error to call this when not currently logged in.
//
// This is purely a local file removal (the Subsonic API's stateless,
// per-request auth scheme means there is no server-side session to
// invalidate).
func ClearCredentials() error {
	path, err := CredentialsPath()
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing %s: %w", path, err)
	}
	return nil
}
