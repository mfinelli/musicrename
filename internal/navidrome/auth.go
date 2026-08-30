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
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// clientName identifies this tool to the Subsonic API via the "c"
// parameter.
const clientName = "musicrename"

// subsonicAPIVersion is the Subsonic API version this client speaks.
const subsonicAPIVersion = "1.16.1"

// pingTimeout bounds how long a single validation request is allowed to
// take, so a bad URL (unreachable host, wrong port) fails promptly instead
// of hanging 'login' indefinitely.
const pingTimeout = 10 * time.Second

// saltedToken computes a fresh Subsonic auth token for password: a random
// hex salt and token = md5(password + salt). A new salt is generated on
// every call, matching the Subsonic API's recommended (non-replayable) token
// authentication method rather than sending the plaintext password on the
// wire.
func saltedToken(password string) (token, salt string, err error) {
	saltBytes := make([]byte, 16)
	if _, err := rand.Read(saltBytes); err != nil {
		return "", "", fmt.Errorf("generating salt: %w", err)
	}
	salt = hex.EncodeToString(saltBytes)

	// MD5 is required here, not chosen: the Subsonic API's wire protocol
	// mandates token = md5(password + salt) as its authentication scheme.
	// This value is computed fresh per request and discarded immediately,
	// so CodeQL's underlying concern (an algorithm too fast to resist
	// offline brute-forcing of a *stored* password hash) doesn't apply:
	// nothing here is stored or compared locally. A stronger algorithm
	// would only break authentication against Navidrome (and any other
	// Subsonic-compatible server), not make anything more secure.
	sum := md5.Sum([]byte(password + salt))
	token = hex.EncodeToString(sum[:])
	return token, salt, nil
}

// authParams returns the base query parameters (u, t, s, v, c, f) shared by
// every Subsonic API request for the given username/password.
func authParams(username, password string) (url.Values, error) {
	token, salt, err := saltedToken(password)
	if err != nil {
		return nil, err
	}

	v := url.Values{}
	v.Set("u", username)
	v.Set("t", token)
	v.Set("s", salt)
	v.Set("v", subsonicAPIVersion)
	v.Set("c", clientName)
	v.Set("f", "json")
	return v, nil
}

// subsonicError mirrors the Subsonic API's error object, present on a
// non-"ok" response.
type subsonicError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// subsonicEnvelope mirrors the outer "subsonic-response" wrapper every
// Subsonic API JSON response is nested under.
type subsonicEnvelope struct {
	SubsonicResponse struct {
		Status string         `json:"status"`
		Error  *subsonicError `json:"error"`
	} `json:"subsonic-response"`
}

// Ping validates baseURL/username/password against the Subsonic API's
// /rest/ping endpoint. It returns an error if the server is unreachable,
// the response isn't well-formed JSON in the expected shape, or the server
// reports anything other than "ok" (e.g. a bad password) with the server's
// own error message included when available.
//
// 'login' calls this before saving anything to disk, so a typo'd URL or
// wrong password is caught immediately rather than failing later during a
// sync.
func Ping(baseURL, username, password string) error {
	params, err := authParams(username, password)
	if err != nil {
		return err
	}

	reqURL := strings.TrimRight(baseURL, "/") + "/rest/ping?" + params.Encode()

	client := &http.Client{Timeout: pingTimeout}
	resp, err := client.Get(reqURL)
	if err != nil {
		return fmt.Errorf("contacting %s: %w", baseURL, err)
	}
	defer resp.Body.Close()

	var env subsonicEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("decoding response from %s: %w", baseURL, err)
	}

	if env.SubsonicResponse.Status != "ok" {
		if env.SubsonicResponse.Error != nil {
			return fmt.Errorf("server rejected credentials: %s", env.SubsonicResponse.Error.Message)
		}
		return fmt.Errorf("server returned status %q", env.SubsonicResponse.Status)
	}
	return nil
}
