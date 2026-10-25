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
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// Disc is a disc ID attached to a release, and the medium of the release it
// belongs to.
type Disc struct {
	ID string
	// Medium is the position of the medium (disc, side) within the release,
	// starting at 1.
	Medium int
}

// ReleaseDiscs is the disc IDs attached to one release.
type ReleaseDiscs struct {
	// Media is how many media the release has, whether or not they have disc
	// IDs.
	Media int
	// Discs holds every disc ID attached to any of them. It is empty for a
	// release no one has attached any to.
	Discs []Disc
}

// ReleaseRef identifies a release, with enough of its details to tell it from
// the others of the same album.
type ReleaseRef struct {
	ID             string
	Title          string
	Disambiguation string
	Status         string
	Country        string
	Date           string
}

// URL returns the address of the release's page on MusicBrainz.
func (r ReleaseRef) URL() string {
	return "https://musicbrainz.org/release/" + r.ID
}

// discsResponse is the part of a release lookup with inc=discids that is read.
type discsResponse struct {
	Media []struct {
		Position int `json:"position"`
		Discs    []struct {
			ID string `json:"id"`
		} `json:"discs"`
	} `json:"media"`
}

// discIDResponse is the part of a disc ID lookup that is read: the releases the
// disc ID is attached to.
type discIDResponse struct {
	Releases []struct {
		ID             string `json:"id"`
		Title          string `json:"title"`
		Disambiguation string `json:"disambiguation"`
		Status         string `json:"status"`
		Country        string `json:"country"`
		Date           string `json:"date"`
	} `json:"releases"`
}

// getJSON fetches endpoint (a path such as "/release/<id>") with params and
// decodes the JSON response into v. It reports whether MusicBrainz knows the
// thing asked for: a 404 is found=false with no error, and anything else that
// is not a 200 is an error.
func (c *client) getJSON(ctx context.Context, endpoint string, params url.Values, v any) (found bool, err error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return false, fmt.Errorf("rate limiter: %w", err)
	}

	params.Set("fmt", "json")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return false, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		// A failed request's error repeats the whole URL, which says nothing a
		// person reading it needs; the cause (no such host, connection refused,
		// a timeout) is what matters.
		if urlErr, ok := errors.AsType[*url.Error](err); ok {
			err = urlErr.Err
		}
		return false, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("lookup returned HTTP %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return false, fmt.Errorf("decode response: %w", err)
	}
	return true, nil
}

// getReleaseDiscs fetches the disc IDs attached to the release identified by
// mbid. It returns ErrNotFound if MusicBrainz has no such release.
func (c *client) getReleaseDiscs(ctx context.Context, mbid string) (*ReleaseDiscs, error) {
	var resp discsResponse
	found, err := c.getJSON(ctx, "/release/"+url.PathEscape(mbid), url.Values{"inc": {"discids"}}, &resp)
	if err != nil {
		return nil, fmt.Errorf("release disc IDs: %w", err)
	}
	if !found {
		return nil, ErrNotFound
	}

	out := &ReleaseDiscs{Media: len(resp.Media)}
	for _, m := range resp.Media {
		for _, d := range m.Discs {
			out.Discs = append(out.Discs, Disc{ID: d.ID, Medium: m.Position})
		}
	}
	return out, nil
}

// getDiscReleases fetches the releases the disc ID is attached to. A disc ID
// MusicBrainz has never been given is not an error: there are simply none.
func (c *client) getDiscReleases(ctx context.Context, discID string) ([]ReleaseRef, error) {
	var resp discIDResponse
	found, err := c.getJSON(ctx, "/discid/"+url.PathEscape(discID), url.Values{}, &resp)
	if err != nil {
		return nil, fmt.Errorf("disc ID lookup: %w", err)
	}
	if !found {
		return nil, nil
	}

	refs := make([]ReleaseRef, len(resp.Releases))
	for i, r := range resp.Releases {
		refs[i] = ReleaseRef(r)
	}
	return refs, nil
}

// Lookup answers questions about disc IDs: which are attached to a release, and
// which releases a disc ID is attached to. It remembers what it has been told, so
// that checking several discs of one release (a multi-disc rip) asks about the
// release once. Like the rest of this package it is meant for one on-demand
// check of one album, not for sweeping a library.
type Lookup struct {
	c        *client
	releases map[string]*ReleaseDiscs
	discs    map[string][]ReleaseRef
}

// NewLookup returns a Lookup that talks to MusicBrainz, within its rate limit.
func NewLookup() *Lookup {
	return newLookup(newClient())
}

func newLookup(c *client) *Lookup {
	return &Lookup{
		c:        c,
		releases: map[string]*ReleaseDiscs{},
		discs:    map[string][]ReleaseRef{},
	}
}

// ReleaseDiscs returns the disc IDs attached to the release with the given
// MusicBrainz ID, or ErrNotFound if there is no such release.
func (l *Lookup) ReleaseDiscs(ctx context.Context, releaseID string) (*ReleaseDiscs, error) {
	if r, ok := l.releases[releaseID]; ok {
		return r, nil
	}
	r, err := l.c.getReleaseDiscs(ctx, releaseID)
	if err != nil {
		return nil, err
	}
	l.releases[releaseID] = r
	return r, nil
}

// DiscReleases returns the releases the disc ID is attached to; none if
// MusicBrainz has never been given it.
func (l *Lookup) DiscReleases(ctx context.Context, discID string) ([]ReleaseRef, error) {
	if r, ok := l.discs[discID]; ok {
		return r, nil
	}
	r, err := l.c.getDiscReleases(ctx, discID)
	if err != nil {
		return nil, err
	}
	l.discs[discID] = r
	return r, nil
}
