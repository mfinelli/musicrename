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
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const lrclibBase = "https://lrclib.net/api"

// Version is the tool's version, included in the User-Agent sent to LRCLIB.
// It lives in cmd/root.go's version literal (which the release tooling
// updates), and that package can't be imported from here, so the command layer
// copies it into this variable at startup, before any request is made. While
// it is empty the version is simply left out of the User-Agent.
var Version string

// userAgent returns the User-Agent header value that identifies this tool to
// LRCLIB. Its API documentation requires clients to send one naming the
// application, its version, and a link to its homepage (unidentified clients
// get a much lower rate limit), e.g. "musicrename v4.6.0
// (https://github.com/mfinelli/musicrename)".
func userAgent() string {
	const project = "(https://github.com/mfinelli/musicrename)"
	if Version == "" {
		return "musicrename " + project
	}
	return "musicrename v" + Version + " " + project
}

const (
	// lrclibRequestInterval is the minimum time between requests. LRCLIB
	// currently allows roughly 90 requests per minute to an identified
	// client; 700 ms is about 85 per minute, which stays just under that
	// and is also near the 200-500 ms between requests that the API
	// documentation recommends for batch operations. The documented figures
	// are approximate and may change, so the retry handling below is the
	// safety net rather than this constant.
	lrclibRequestInterval = 700 * time.Millisecond

	// lrclibMaxRetries is how many times a request that was answered with
	// 429 or 503 is retried before giving up.
	lrclibMaxRetries = 3

	// lrclibDefaultRetryWait is used when a 429/503 response has no usable
	// Retry-After header (the API documentation says 503s are usually one
	// second).
	lrclibDefaultRetryWait = time.Second

	// lrclibMaxRetryWait is the longest wait the client will sit through. A
	// larger Retry-After means something is wrong enough that stopping is
	// better than leaving a terminal hanging silently for minutes.
	lrclibMaxRetryWait = time.Minute

	// lrclibRetryJitter is the fraction of the wait added at random on top
	// of it, as the API documentation asks, so that many throttled clients
	// don't all retry at the same moment.
	lrclibRetryJitter = 0.3
)

// ErrRateLimited is returned (wrapped) when LRCLIB keeps answering 429 Too
// Many Requests after the client has waited as instructed and retried, or
// asks for a longer wait than the client is willing to sit through. Fetch
// stops the whole run when it sees this, rather than carrying on to the next
// track: the API documentation warns that continuing to send requests while
// being limited may result in a temporary ban.
var ErrRateLimited = errors.New("lrclib: rate limit exceeded")

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
// come from, are subject to the same global limit. All requests go through
// doGet, which also identifies the client and handles 429/503 responses.
type lrclibClient struct {
	http    *http.Client
	limiter *rate.Limiter
	base    string // overridable in tests

	// sleep waits for d or until ctx is done, returning ctx's error in the
	// latter case. nil means a real, context-aware sleep; tests replace it
	// so that retries don't actually wait.
	sleep func(ctx context.Context, d time.Duration) error
}

// newLrclibClient returns a client configured to respect LRCLIB's rate
// limits: one request per lrclibRequestInterval, identified by
// userAgent(), with 429 and 503 responses retried as the server asks.
func newLrclibClient() *lrclibClient {
	return &lrclibClient{
		http:    &http.Client{Timeout: 10 * time.Second},
		limiter: rate.NewLimiter(rate.Every(lrclibRequestInterval), 1),
		base:    lrclibBase,
	}
}

// doGet issues a GET request for rawURL and returns the response, which the
// caller must close. Every LRCLIB request goes through here so that they all
// share the same behavior:
//
//   - each attempt waits on the rate limiter first;
//   - each request carries the User-Agent header LRCLIB requires;
//   - a 429 or 503 response is retried, up to lrclibMaxRetries times, after
//     waiting for the number of seconds in its Retry-After header (one second
//     if it has none) plus a little random jitter;
//   - when retries run out, a 429 becomes an error wrapping ErrRateLimited;
//     a 503 is returned as-is for the caller to report. A Retry-After longer
//     than lrclibMaxRetryWait is not waited out and fails immediately the
//     same way.
//
// Any other status, including other errors such as 500, is returned
// immediately: only throttling is worth retrying.
func (c *lrclibClient) doGet(ctx context.Context, rawURL string) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limiter: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("User-Agent", userAgent())

		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}

		throttled := resp.StatusCode == http.StatusTooManyRequests ||
			resp.StatusCode == http.StatusServiceUnavailable
		if !throttled {
			return resp, nil
		}

		wait := retryAfter(resp)
		tooLong := wait > lrclibMaxRetryWait
		if attempt >= lrclibMaxRetries || tooLong {
			if resp.StatusCode != http.StatusTooManyRequests {
				return resp, nil
			}
			resp.Body.Close()
			if tooLong {
				return nil, fmt.Errorf("%w: server asked to wait %s", ErrRateLimited, wait)
			}
			return nil, fmt.Errorf("%w: still limited after %d retries", ErrRateLimited, lrclibMaxRetries)
		}

		// Drain a little of the body so the connection can be reused, then
		// wait out the throttling before trying again.
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()

		wait += time.Duration(rand.Float64() * lrclibRetryJitter * float64(wait))
		if err := c.sleepFor(ctx, wait); err != nil {
			return nil, err
		}
	}
}

// sleepFor waits for d using the client's sleep function, defaulting to a
// timer that stops early if ctx is done.
func (c *lrclibClient) sleepFor(ctx context.Context, d time.Duration) error {
	if c.sleep != nil {
		return c.sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// retryAfter returns how long resp's Retry-After header asks the client to
// wait. LRCLIB's documentation specifies a number of seconds; a missing,
// malformed, or negative value falls back to lrclibDefaultRetryWait.
func retryAfter(resp *http.Response) time.Duration {
	secs, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After")))
	if err != nil || secs < 0 {
		return lrclibDefaultRetryWait
	}
	return time.Duration(secs) * time.Second
}

// get calls the LRCLIB /get endpoint with the provided metadata and a
// specific duration in seconds. It returns nil, nil when the server responds
// with 404 (no match for that exact combination).
func (c *lrclibClient) get(ctx context.Context, trackName, artistName, albumName string, durationSecs int) (*lrclibTrack, error) {
	params := url.Values{}
	params.Set("track_name", trackName)
	params.Set("artist_name", artistName)
	params.Set("album_name", albumName)
	params.Set("duration", strconv.Itoa(durationSecs))

	resp, err := c.doGet(ctx, c.base+"/get?"+params.Encode())
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
	params := url.Values{}
	params.Set("track_name", trackName)
	params.Set("artist_name", artistName)
	params.Set("album_name", albumName)

	resp, err := c.doGet(ctx, c.base+"/search?"+params.Encode())
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

// getByID calls the LRCLIB /get/{id} endpoint directly, for when the caller
// already knows exactly which LRCLIB track to use (e.g. an ID or URL found
// manually on lrclib.net) and wants to skip title/artist/album/duration
// matching entirely. It returns nil, nil when the server responds with 404
// (no track with that ID).
func (c *lrclibClient) getByID(ctx context.Context, id int) (*lrclibTrack, error) {
	resp, err := c.doGet(ctx, c.base+"/get/"+strconv.Itoa(id))
	if err != nil {
		return nil, fmt.Errorf("get request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("/get/%d returned HTTP %d", id, resp.StatusCode)
	}

	var track lrclibTrack
	if err := json.NewDecoder(resp.Body).Decode(&track); err != nil {
		return nil, fmt.Errorf("decode /get/%d response: %w", id, err)
	}
	return &track, nil
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
