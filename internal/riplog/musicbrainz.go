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
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mfinelli/musicrename/internal/musicbrainz"
)

// releaseFile is the file, beside the log, that holds the URL of the MusicBrainz
// release the disc was matched to (see WORKFLOW.md).
const releaseFile = "musicbrainz.txt"

// lookupTimeout bounds how long the MusicBrainz lookups may take, so that a
// machine with no network is told so rather than left waiting.
const lookupTimeout = 30 * time.Second

// Lookup is the part of MusicBrainz the disc ID check needs: which disc IDs are
// attached to a release, and which releases a disc ID is attached to.
// *musicbrainz.Lookup is one; tests use a fake.
type Lookup interface {
	// ReleaseDiscs returns the disc IDs attached to a release, or
	// musicbrainz.ErrNotFound if MusicBrainz has no such release.
	ReleaseDiscs(ctx context.Context, releaseID string) (*musicbrainz.ReleaseDiscs, error)
	// DiscReleases returns the releases a disc ID is attached to; none if it is
	// attached to none.
	DiscReleases(ctx context.Context, discID string) ([]musicbrainz.ReleaseRef, error)
}

const uuidPattern = `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`

var (
	reReleaseURL = regexp.MustCompile(`^https?://(?:www\.|beta\.)?musicbrainz\.org/release/(` + uuidPattern + `)(?:[/?#]\S*)?$`)
	reGroupURL   = regexp.MustCompile(`^https?://(?:www\.|beta\.)?musicbrainz\.org/release-group/`)
)

// parseReleaseURL extracts the release ID from the text of musicbrainz.txt, whose
// first non-empty line is the release's URL. If it is not one, the problem says
// why.
func parseReleaseURL(text string) (id, problem string) {
	text = strings.TrimPrefix(text, "\ufeff") // a byte order mark, if an editor added one
	line := ""
	for l := range strings.SplitSeq(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			line = l
			break
		}
	}

	switch {
	case line == "":
		return "", "it is empty"
	case reGroupURL.MatchString(line):
		return "", "that is a release group; the release itself is needed"
	}
	m := reReleaseURL.FindStringSubmatch(line)
	if m == nil {
		return "", fmt.Sprintf("%q is not a release URL such as https://musicbrainz.org/release/<id>", truncate(line, 60))
	}
	return strings.ToLower(m[1]), ""
}

// verifyMusicBrainz adds the check that the disc is one the release in
// musicbrainz.txt has. The disc ID is computed from the log's table of contents,
// so it identifies this disc's track layout: the same album can be several
// releases, one per pressing, and ripping one while tagging another is the
// mistake this finds. mb may be nil, for no network, in which case the ID is
// computed but not compared.
func verifyMusicBrainz(ctx context.Context, r *Result, l *Log, dir string, mb Lookup) {
	const id, name = "musicbrainz", "MusicBrainz"

	disc := l.DiscID()
	if disc == "" {
		r.add(id, name, StatusInfo, "no table of contents to compute a disc ID from")
		return
	}

	data, err := os.ReadFile(filepath.Join(dir, releaseFile))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		r.add(id, name, StatusWarning,
			"no %s next to the log, so the disc ID (%s) cannot be compared with a release", releaseFile, disc)
		return
	case err != nil:
		r.add(id, name, StatusError, "could not read %s: %v", releaseFile, err)
		return
	}

	release, problem := parseReleaseURL(string(data))
	if problem != "" {
		r.add(id, name, StatusError, "%s does not hold a release URL: %s", releaseFile, problem)
		return
	}

	if mb == nil {
		r.add(id, name, StatusInfo, "disc ID %s, not compared with the release (offline)", disc)
		return
	}

	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()

	discs, err := mb.ReleaseDiscs(ctx, release)
	switch {
	case errors.Is(err, musicbrainz.ErrNotFound):
		r.add(id, name, StatusError, "%s names release %s, which MusicBrainz does not have", releaseFile, release)
		return
	case err != nil:
		r.add(id, name, StatusWarning,
			"could not look up release %s, so disc ID %s was not compared: %v", release, disc, err)
		return
	}

	for _, d := range discs.Discs {
		if d.ID != disc {
			continue
		}
		if discs.Media > 1 {
			r.add(id, name, StatusPass, "%s is attached to the release, as medium %d of %d", disc, d.Medium, discs.Media)
		} else {
			r.add(id, name, StatusPass, "%s is attached to the release", disc)
		}
		return
	}

	if len(discs.Discs) == 0 {
		r.add(id, name, StatusInfo,
			"the release has no disc IDs yet, so there is nothing to compare with; this disc's is %s", disc)
		return
	}

	r.add(id, name, StatusWarning,
		"%s is not among the %d %s on the release, which may be a different pressing; %s",
		disc, len(discs.Discs), idNoun(len(discs.Discs)), whereElse(ctx, mb, disc))
}

func idNoun(n int) string {
	if n == 1 {
		return "disc ID"
	}
	return "disc IDs"
}

// whereElse says which releases a disc ID is attached to, for the check to
// suggest where the disc may belong.
func whereElse(ctx context.Context, mb Lookup, disc string) string {
	refs, err := mb.DiscReleases(ctx, disc)
	switch {
	case err != nil:
		return fmt.Sprintf("which release has it could not be looked up: %v", err)
	case len(refs) == 0:
		return "no release on MusicBrainz has it"
	}

	shown := make([]string, 0, maxListed)
	for _, ref := range refs[:min(len(refs), maxListed)] {
		shown = append(shown, describeRelease(ref))
	}
	text := "it is attached to " + strings.Join(shown, "; ")
	if len(refs) > maxListed {
		text += fmt.Sprintf("; and %d more", len(refs)-maxListed)
	}
	return text
}

// describeRelease writes a release as its title, whatever tells it apart from the
// other releases of the album, and its address.
func describeRelease(ref musicbrainz.ReleaseRef) string {
	var details []string
	for _, d := range []string{ref.Country, ref.Date, ref.Disambiguation} {
		if d != "" {
			details = append(details, d)
		}
	}

	text := ref.Title
	if len(details) > 0 {
		text += " (" + strings.Join(details, ", ") + ")"
	}
	return text + " " + ref.URL()
}
