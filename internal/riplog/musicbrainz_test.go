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

package riplog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/musicbrainz"
)

const (
	blackDiscID   = "NdW5DdItgg6nqzd5d6Ade54jPKA-"
	centralDiscID = "1RenVg1qGKl3xwjJ9D0vL2g6VBo-"
)

func TestParseReleaseURL(t *testing.T) {
	const id = "d1d196ec-bb2b-4636-81a0-b8f5c5774514"

	t.Run("the forms of a release URL", func(t *testing.T) {
		for name, text := range map[string]string{
			"the URL alone":              "https://musicbrainz.org/release/" + id,
			"with a line ending":         "https://musicbrainz.org/release/" + id + "\n",
			"with a Windows line ending": "https://musicbrainz.org/release/" + id + "\r\n",
			"with spaces around it":      "  https://musicbrainz.org/release/" + id + "  \n",
			"plain http":                 "http://musicbrainz.org/release/" + id,
			"the beta site":              "https://beta.musicbrainz.org/release/" + id,
			"with www":                   "https://www.musicbrainz.org/release/" + id,
			"on a page of the release":   "https://musicbrainz.org/release/" + id + "/discids",
			"with a query":               "https://musicbrainz.org/release/" + id + "?tab=x",
			"after blank lines":          "\n\n  \nhttps://musicbrainz.org/release/" + id + "\n",
			"with only the first line":   "https://musicbrainz.org/release/" + id + "\nnotes about the release\n",
			"after a byte order mark":    "\ufeffhttps://musicbrainz.org/release/" + id + "\r\n",
			"with the ID in upper case":  "https://musicbrainz.org/release/D1D196EC-BB2B-4636-81A0-B8F5C5774514",
		} {
			got, problem := parseReleaseURL(text)

			assert.Equal(t, "", problem, name)
			assert.Equal(t, id, got, name)
		}
	})

	t.Run("what is not a release URL", func(t *testing.T) {
		tests := map[string]struct {
			text    string
			problem string
		}{
			"empty":                  {"", "it is empty"},
			"only whitespace":        {" \n\r\n  \n", "it is empty"},
			"a release group":        {"https://musicbrainz.org/release-group/" + id, "release group"},
			"an artist":              {"https://musicbrainz.org/artist/" + id, "not a release URL"},
			"another site":           {"https://example.org/release/" + id, "not a release URL"},
			"an ID that is not one":  {"https://musicbrainz.org/release/not-an-id", "not a release URL"},
			"just the ID":            {id, "not a release URL"},
			"text that is not a URL": {"the Back in Black release", "not a release URL"},
			"a lookalike host":       {"https://musicbrainz.org.evil.example/release/" + id, "not a release URL"},
		}
		for name, tc := range tests {
			got, problem := parseReleaseURL(tc.text)

			assert.Equal(t, "", got, name)
			assert.Contains(t, problem, tc.problem, name)
		}
	})
}

func TestDescribeRelease(t *testing.T) {
	ref := musicbrainz.ReleaseRef{ID: testReleaseID, Title: "Back in Black"}

	assert.Equal(t, "Back in Black "+ref.URL(), describeRelease(ref))

	ref.Country, ref.Date = "US", "1980-07-25"
	assert.Equal(t, "Back in Black (US, 1980-07-25) "+ref.URL(), describeRelease(ref))

	ref.Disambiguation = "remastered"
	assert.Equal(t, "Back in Black (US, 1980-07-25, remastered) "+ref.URL(), describeRelease(ref))

	ref.Country, ref.Date = "", ""
	assert.Equal(t, "Back in Black (remastered) "+ref.URL(), describeRelease(ref))
}

func TestCheckFileMusicBrainz(t *testing.T) {
	t.Run("the disc is attached to the release", func(t *testing.T) {
		mb := knowing(blackDiscID)

		c := want(t, blackRip(t).checkWith(t, mb), "musicbrainz", StatusPass)

		assert.Equal(t, blackDiscID+" is attached to the release", c.Detail)
		assert.Equal(t, []string{testReleaseID}, mb.releaseCalls, "the release in musicbrainz.txt is the one asked about")
		assert.Empty(t, mb.discCalls, "nothing else needs asking")
	})

	t.Run("the real disc ID is the one computed from the real log", func(t *testing.T) {
		// This is the check on the whole path: the log's table of contents,
		// the disc ID, musicbrainz.txt, and the comparison.
		rip := blackRip(t)
		mb := knowing(blackDiscID)

		r := rip.checkWith(t, mb)

		want(t, r, "musicbrainz", StatusPass)
	})

	t.Run("a multi-disc release says which medium", func(t *testing.T) {
		rip := centralRip(t)
		mb := &fakeLookup{releases: map[string]*musicbrainz.ReleaseDiscs{testReleaseID: {
			Media: 2,
			Discs: []musicbrainz.Disc{{ID: "otherdisc00000000000000000-", Medium: 2}, {ID: centralDiscID, Medium: 1}},
		}}}

		c := want(t, rip.checkWith(t, mb), "musicbrainz", StatusPass)

		assert.Equal(t, centralDiscID+" is attached to the release, as medium 1 of 2", c.Detail)
	})

	t.Run("a disc on a later medium", func(t *testing.T) {
		mb := &fakeLookup{releases: map[string]*musicbrainz.ReleaseDiscs{testReleaseID: {
			Media: 3,
			Discs: []musicbrainz.Disc{{ID: "otherdisc00000000000000000-", Medium: 1}, {ID: centralDiscID, Medium: 2}},
		}}}

		c := want(t, centralRip(t).checkWith(t, mb), "musicbrainz", StatusPass)

		assert.Equal(t, centralDiscID+" is attached to the release, as medium 2 of 3", c.Detail)
	})

	t.Run("a medium with more than one disc ID", func(t *testing.T) {
		mb := &fakeLookup{releases: map[string]*musicbrainz.ReleaseDiscs{testReleaseID: {
			Media: 1,
			Discs: []musicbrainz.Disc{{ID: "otherdisc00000000000000000-", Medium: 1}, {ID: blackDiscID, Medium: 1}},
		}}}

		want(t, blackRip(t).checkWith(t, mb), "musicbrainz", StatusPass)
	})

	t.Run("a disc ID that is not on the release", func(t *testing.T) {
		mb := knowing("otherdisc00000000000000000-")
		mb.discs = map[string][]musicbrainz.ReleaseRef{blackDiscID: {
			{ID: "0a0a0a0a-0000-4000-8000-000000000001", Title: "Back in Black", Country: "XE", Date: "2003", Disambiguation: "remastered"},
		}}

		r := blackRip(t).checkWith(t, mb)

		c := want(t, r, "musicbrainz", StatusWarning)
		assert.Contains(t, c.Detail, blackDiscID+" is not among the 1 disc ID on the release")
		assert.Contains(t, c.Detail, "different pressing")
		assert.Contains(t, c.Detail, "it is attached to Back in Black (XE, 2003, remastered) https://musicbrainz.org/release/0a0a0a0a-0000-4000-8000-000000000001")
		assert.Equal(t, []string{blackDiscID}, mb.discCalls)
		assert.False(t, r.Failed(), "a warning, so the run does not fail")
		assert.True(t, r.Fails(true), "unless --strict")
	})

	t.Run("several disc IDs, none of them this disc's", func(t *testing.T) {
		mb := &fakeLookup{releases: map[string]*musicbrainz.ReleaseDiscs{testReleaseID: {
			Media: 1, Discs: []musicbrainz.Disc{{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaa", Medium: 1}, {ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbb", Medium: 1}},
		}}}

		c := want(t, blackRip(t).checkWith(t, mb), "musicbrainz", StatusWarning)

		assert.Contains(t, c.Detail, "not among the 2 disc IDs")
		assert.Contains(t, c.Detail, "no release on MusicBrainz has it")
	})

	t.Run("the disc ID is on other releases, which are listed, a few at a time", func(t *testing.T) {
		mb := knowing("otherdisc00000000000000000-")
		var refs []musicbrainz.ReleaseRef
		for _, id := range []string{"1", "2", "3", "4", "5"} {
			refs = append(refs, musicbrainz.ReleaseRef{ID: "0a0a0a0a-0000-4000-8000-00000000000" + id, Title: "Edition " + id})
		}
		mb.discs = map[string][]musicbrainz.ReleaseRef{blackDiscID: refs}

		c := want(t, blackRip(t).checkWith(t, mb), "musicbrainz", StatusWarning)

		assert.Contains(t, c.Detail, "Edition 1")
		assert.Contains(t, c.Detail, "Edition 3")
		assert.NotContains(t, c.Detail, "Edition 4")
		assert.Contains(t, c.Detail, "and 2 more")
	})

	t.Run("which release has it could not be looked up", func(t *testing.T) {
		mb := knowing("otherdisc00000000000000000-")
		mb.discErr = errors.New("HTTP 503")

		c := want(t, blackRip(t).checkWith(t, mb), "musicbrainz", StatusWarning)

		assert.Contains(t, c.Detail, "not among")
		assert.Contains(t, c.Detail, "could not be looked up: HTTP 503")
	})

	t.Run("a release with no disc IDs", func(t *testing.T) {
		mb := &fakeLookup{releases: map[string]*musicbrainz.ReleaseDiscs{testReleaseID: {Media: 1}}}

		r := blackRip(t).checkWith(t, mb)

		c := want(t, r, "musicbrainz", StatusInfo)
		assert.Contains(t, c.Detail, "no disc IDs yet")
		assert.Contains(t, c.Detail, blackDiscID)
		assert.Empty(t, mb.discCalls)
	})

	t.Run("a release MusicBrainz does not have", func(t *testing.T) {
		mb := &fakeLookup{} // knows no releases

		r := blackRip(t).checkWith(t, mb)

		c := want(t, r, "musicbrainz", StatusError)
		assert.Contains(t, c.Detail, testReleaseID)
		assert.Contains(t, c.Detail, "does not have")
		assert.True(t, r.Failed())
	})

	t.Run("MusicBrainz cannot be reached", func(t *testing.T) {
		mb := &fakeLookup{releaseErr: errors.New("release request: dial tcp: no such host")}

		r := blackRip(t).checkWith(t, mb)

		c := want(t, r, "musicbrainz", StatusWarning)
		assert.Contains(t, c.Detail, "could not look up release "+testReleaseID)
		assert.Contains(t, c.Detail, "no such host")
		assert.Contains(t, c.Detail, blackDiscID)
		assert.False(t, r.Failed(), "being offline is not a bad rip")
	})

	t.Run("without a lookup the ID is computed but not compared", func(t *testing.T) {
		r := blackRip(t).checkWith(t, nil)

		c := want(t, r, "musicbrainz", StatusInfo)
		assert.Equal(t, "disc ID "+blackDiscID+", not compared with the release (offline)", c.Detail)
	})

	t.Run("the lookups are bounded in time", func(t *testing.T) {
		var deadline time.Time
		mb := &deadlineLookup{fakeLookup: knowing(blackDiscID), deadline: &deadline}

		blackRip(t).checkWith(t, mb)

		require.False(t, deadline.IsZero(), "the context has a deadline")
		assert.WithinDuration(t, time.Now().Add(lookupTimeout), deadline, 5*time.Second)
	})

	t.Run("a log with no table of contents", func(t *testing.T) {
		text := regexpTOCRows.ReplaceAllString(fixtureText(t, fixtureBlack), "")
		rip := buildRip(t, "Back in Black", []byte(text), cueText(t, fixtureBlackCue))
		mb := &fakeLookup{}

		c := want(t, rip.checkWith(t, mb), "musicbrainz", StatusInfo)

		assert.Contains(t, c.Detail, "no table of contents")
		assert.Empty(t, mb.releaseCalls)
	})
}

// deadlineLookup is a fake that records the deadline of the context it is given.
type deadlineLookup struct {
	*fakeLookup
	deadline *time.Time
}

func (d *deadlineLookup) ReleaseDiscs(ctx context.Context, id string) (*musicbrainz.ReleaseDiscs, error) {
	if dl, ok := ctx.Deadline(); ok {
		*d.deadline = dl
	}
	return d.fakeLookup.ReleaseDiscs(ctx, id)
}

func TestCheckFileReleaseFile(t *testing.T) {
	// None of these may ask MusicBrainz anything: there is no release to ask about.
	noCalls := func(t *testing.T, mb *fakeLookup) {
		t.Helper()
		assert.Empty(t, mb.releaseCalls)
		assert.Empty(t, mb.discCalls)
	}

	t.Run("no musicbrainz.txt", func(t *testing.T) {
		rip := blackRip(t)
		require.NoError(t, os.Remove(filepath.Join(rip.dir, releaseFile)))
		mb := knowing(blackDiscID)

		r := rip.checkWith(t, mb)

		c := want(t, r, "musicbrainz", StatusWarning)
		assert.Contains(t, c.Detail, "no musicbrainz.txt next to the log")
		assert.Contains(t, c.Detail, blackDiscID)
		noCalls(t, mb)
	})

	t.Run("no musicbrainz.txt, offline too", func(t *testing.T) {
		rip := blackRip(t)
		require.NoError(t, os.Remove(filepath.Join(rip.dir, releaseFile)))

		want(t, rip.checkWith(t, nil), "musicbrainz", StatusWarning)
	})

	t.Run("a musicbrainz.txt that cannot be read", func(t *testing.T) {
		rip := blackRip(t)
		require.NoError(t, os.Remove(filepath.Join(rip.dir, releaseFile)))
		require.NoError(t, os.Mkdir(filepath.Join(rip.dir, releaseFile), 0o755))
		mb := knowing(blackDiscID)

		c := want(t, rip.checkWith(t, mb), "musicbrainz", StatusError)

		assert.Contains(t, c.Detail, "could not read musicbrainz.txt")
		noCalls(t, mb)
	})

	t.Run("a musicbrainz.txt that does not hold a release URL", func(t *testing.T) {
		for text, problem := range map[string]string{
			"":   "it is empty",
			"\n": "it is empty",
			"https://musicbrainz.org/release-group/" + testReleaseID: "release group",
			"Back in Black": "not a release URL",
		} {
			rip := blackRip(t)
			require.NoError(t, os.WriteFile(filepath.Join(rip.dir, releaseFile), []byte(text), 0o644))
			mb := knowing(blackDiscID)

			r := rip.checkWith(t, mb)

			c := want(t, r, "musicbrainz", StatusError)
			assert.Contains(t, c.Detail, "does not hold a release URL", text)
			assert.Contains(t, c.Detail, problem, text)
			noCalls(t, mb)
		}
	})

	t.Run("a different release in the file is the one asked about", func(t *testing.T) {
		rip := blackRip(t)
		const other = "0a0a0a0a-0000-4000-8000-000000000009"
		require.NoError(t, os.WriteFile(filepath.Join(rip.dir, releaseFile), []byte("https://musicbrainz.org/release/"+other+"\n"), 0o644))
		mb := &fakeLookup{releases: map[string]*musicbrainz.ReleaseDiscs{other: {Media: 1, Discs: []musicbrainz.Disc{{ID: blackDiscID, Medium: 1}}}}}

		r := rip.checkWith(t, mb)

		want(t, r, "musicbrainz", StatusPass)
		assert.Equal(t, []string{other}, mb.releaseCalls)
	})
}
