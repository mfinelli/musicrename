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
	"time"

	subsonic "github.com/supersonic-app/go-subsonic/subsonic"
)

// clientHTTPTimeout bounds how long a single Subsonic API request is
// allowed to take.
const clientHTTPTimeout = 30 * time.Second

// NewClient builds and authenticates a [subsonic.Client] from creds, ready
// for use by the sync operations in this package.
//
// This authenticates via the supersonic-app/go-subsonic library's
// Authenticate method, which generates its salt with math/rand rather than
// crypto/rand (a known, deliberately accepted tradeoff: its salt/token fields
// are unexported, so there is no way to supply this package's own
// crypto/rand-based saltedToken instead without forking the library).
func NewClient(creds Credentials) (*subsonic.Client, error) {
	client := &subsonic.Client{
		Client:     &http.Client{Timeout: clientHTTPTimeout},
		BaseUrl:    creds.URL,
		User:       creds.Username,
		ClientName: clientName,
		UseJSON:    true,
	}

	if err := client.Authenticate(creds.Password); err != nil {
		return nil, fmt.Errorf("authenticating with %s: %w", creds.URL, err)
	}
	return client, nil
}
