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

package lyrics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"golang.org/x/time/rate"
)

const lrclibBase = "https://lrclib.net/api"

// lrclibTrack is the JSON representation of a single track returned by the
// LRCLIB API. Both /get and /search use this same shape (search returns a
// slice of them).
type lrclibTrack struct {
	ID           int     `json:"id"`
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	AlbumName    string  `json:"albumName"`
	Duration     float64 `json:"duration"` // seconds
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  string  `json:"plainLyrics"`
	SyncedLyrics string  `json:"syncedLyrics"`
}

// lrclibClient wraps an HTTP client with a token-bucket rate limiter so that
// all LRCLIB requests, regardless of which step of the fetch strategy they
// come from, are subject to the same global limit.
type lrclibClient struct {
	http    *http.Client
	limiter *rate.Limiter
	base    string // overridable in tests
}

// newLrclibClient returns a client configured to respect LRCLIB's recommended
// rate of no more than five requests per second.
func newLrclibClient() *lrclibClient {
	return &lrclibClient{
		http:    &http.Client{Timeout: 10 * time.Second},
		limiter: rate.NewLimiter(rate.Limit(5), 1),
		base:    lrclibBase,
	}
}

// get calls the LRCLIB /get endpoint with the provided metadata and a
// specific duration in seconds. It returns nil, nil when the server responds
// with 404 (no match for that exact combination).
func (c *lrclibClient) get(ctx context.Context, trackName, artistName, albumName string, durationSecs int) (*lrclibTrack, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter: %w", err)
	}

	params := url.Values{}
	params.Set("track_name", trackName)
	params.Set("artist_name", artistName)
	params.Set("album_name", albumName)
	params.Set("duration", strconv.Itoa(durationSecs))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/get?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("/get returned HTTP %d", resp.StatusCode)
	}

	var track lrclibTrack
	if err := json.NewDecoder(resp.Body).Decode(&track); err != nil {
		return nil, fmt.Errorf("decode /get response: %w", err)
	}
	return &track, nil
}

// search calls the LRCLIB /search endpoint with title, artist, and album but
// no duration constraint. It returns the first result, or nil, nil when the
// server returns an empty list.
func (c *lrclibClient) search(ctx context.Context, trackName, artistName, albumName string) (*lrclibTrack, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter: %w", err)
	}

	params := url.Values{}
	params.Set("track_name", trackName)
	params.Set("artist_name", artistName)
	params.Set("album_name", albumName)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/search?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("/search returned HTTP %d", resp.StatusCode)
	}

	var tracks []lrclibTrack
	if err := json.NewDecoder(resp.Body).Decode(&tracks); err != nil {
		return nil, fmt.Errorf("decode /search response: %w", err)
	}
	if len(tracks) == 0 {
		return nil, nil
	}
	return &tracks[0], nil
}

// fetchForTrack implements the four-step fetch strategy described in the
// design document, stopping as soon as any step returns a match:
//
//  1. Exact duration via /get.
//  2. /get with duration −1 s then +1 s.
//  3. /get with duration −2 s then +2 s.
//  4. Fuzzy /search with no duration constraint.
//
// In the common case (exact match at step 1) only a single HTTP request is
// made. The worst case is six requests (one exact + four delta + one search).
// Returns nil, nil when no lyrics are found after all steps.
func (c *lrclibClient) fetchForTrack(ctx context.Context, trackName, artistName, albumName string, duration time.Duration) (*lrclibTrack, error) {
	secs := int(duration.Seconds())

	// Step 1: exact duration.
	result, err := c.get(ctx, trackName, artistName, albumName, secs)
	if err != nil || result != nil {
		return result, err
	}

	// Steps 2–3: progressively relaxed duration, trying negative delta first
	// on the assumption that LRCLIB durations are more likely to be slightly
	// shorter than the on-disk file's reported length.
	for _, delta := range []int{-1, +1, -2, +2} {
		result, err = c.get(ctx, trackName, artistName, albumName, secs+delta)
		if err != nil || result != nil {
			return result, err
		}
	}

	// Step 4: fuzzy search, no duration constraint.
	return c.search(ctx, trackName, artistName, albumName)
}
