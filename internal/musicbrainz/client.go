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

// Package musicbrainz looks up a single release from the MusicBrainz API
// and diffs it against a snapshot recorded the last time it was looked up,
// so drift in upstream metadata (an editor's correction, e.g.) can be
// detected without confusing it for the user's own subsequent tag edits.
// The comparison is always snapshot-then vs. MusicBrainz-now, not the
// user's current tag vs. MusicBrainz-now.
//
// This package intentionally only supports a single, on-demand, one-release
// lookup at a time. MusicBrainz's own rate-limiting documentation
// (https://musicbrainz.org/doc/MusicBrainz_API/Rate_Limiting) explicitly
// asks API consumers not to poll for metadata changes across a library;
// nothing here should grow into a library-wide sweep.
package musicbrainz

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"golang.org/x/time/rate"
)

// userAgent identifies this tool to MusicBrainz, per their API policy
// (https://musicbrainz.org/doc/MusicBrainz_API): "each request ... needs
// to include a User-Agent header, with enough information ... to contact
// the application maintainer". No version number is included here so that
// this string doesn't need to be kept in sync with cmd/root.go's version
// literal; the repo URL alone already satisfies MusicBrainz's stated purpose
// for this header.
const userAgent = "musicrename (https://github.com/mfinelli/musicrename)"

const apiBase = "https://musicbrainz.org/ws/2"

// client wraps an HTTP client with a token-bucket rate limiter so requests
// stay within MusicBrainz's documented limit of one request per second.
type client struct {
	http    *http.Client
	limiter *rate.Limiter
	base    string // overridable in tests
}

// newClient returns a client configured to respect MusicBrainz's rate
// limit.
func newClient() *client {
	return &client{
		http:    &http.Client{},
		limiter: rate.NewLimiter(rate.Limit(1), 1),
		base:    apiBase,
	}
}

// getRelease fetches the release identified by mbid, including its
// recordings (the track listing), artist credits, each recording's
// relationships (which is how writer/composer credit surfaces), release
// group, labels, ISRCs, and genre tags, in a single request.
//
// recording-level-rels/work-rels/work-level-rels/artist-rels is the
// documented combination for pulling a release's nested recordings'
// linked works, and those works' own artist relationships, in one
// request (https://musicbrainz.org/doc/MusicBrainz_API/Examples); this
// also brings back personnel-credit relations (engineer, producer,
// instrument, vocal, ...) as a side effect which Recording.Composers ignores.
func (c *client) getRelease(ctx context.Context, mbid string) (*Release, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter: %w", err)
	}

	params := url.Values{}
	params.Set("inc", "recordings+artist-credits+recording-level-rels+work-rels+work-level-rels+"+
		"artist-rels+release-groups+labels+isrcs+genres")
	params.Set("fmt", "json")

	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, c.base+"/release/"+url.PathEscape(mbid)+"?"+params.Encode(), nil,
	)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("release request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release lookup returned HTTP %d", resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("decode release response: %w", err)
	}
	return &release, nil
}
