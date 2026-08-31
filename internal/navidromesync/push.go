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

package navidromesync

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	subsonic "github.com/supersonic-app/go-subsonic/subsonic"

	"github.com/mfinelli/musicrename/internal/hasher"
	"github.com/mfinelli/musicrename/internal/playlist"
)

// PushResult summarizes what a push run did (or, in dry-run mode, would
// do). Paths are absolute local filesystem paths.
type PushResult struct {
	Created   []string
	Updated   []string
	Unchanged []string
	Warnings  []string
}

func (r *PushResult) merge(other *PushResult) {
	r.Created = append(r.Created, other.Created...)
	r.Updated = append(r.Updated, other.Updated...)
	r.Unchanged = append(r.Unchanged, other.Unchanged...)
	r.Warnings = append(r.Warnings, other.Warnings...)
}

// PushAll walks libraryRootRoot's playlists/ tree (playlist.WalkTree) and
// pushes every file's content to Navidrome: a file with no #NAVIDROME-ID yet
// is created remotely and has its new ID written back; an already-correlated
// file has its remote name and track list overwritten to match local exactly
// (in order).
//
// The server's full song catalog (path -> song ID) is indexed once
// ([buildSongIndex]) before any playlist is processed, and reused
// across every file.
//
// If dryRun is true, nothing is created or changed remotely, and no local
// file is rewritten with a new ID (the returned PushResult still reports
// what would have happened).
func PushAll(client *subsonic.Client, libraryRootRoot string, dryRun bool) (*PushResult, error) {
	result := &PushResult{}

	index, err := buildSongIndex(client)
	if err != nil {
		return nil, fmt.Errorf("indexing server library: %w", err)
	}

	err = playlist.WalkTree(libraryRootRoot, func(path string) error {
		pr, err := pushOne(client, path, dryRun, index)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		result.merge(pr)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(result.Created)
	sort.Strings(result.Updated)
	sort.Strings(result.Unchanged)

	return result, nil
}

// PushOne pushes a single local playlist file at path. See [pushOne] for the
// actual reconciliation logic; this just builds the one-time song index
// ([buildSongIndex]) that pushOne needs to resolve entries, since a standalone
// single-playlist push still has no other source for it.
func PushOne(client *subsonic.Client, path string, dryRun bool) (*PushResult, error) {
	index, err := buildSongIndex(client)
	if err != nil {
		return nil, fmt.Errorf("indexing server library: %w", err)
	}
	return pushOne(client, path, dryRun, index)
}

// pushOne is the actual per-file push logic, shared by [PushOne] and
// [PushAll]. index maps local relative path -> Navidrome song ID, built
// once per run by the caller rather than searched per track here.
//
// A file with no #NAVIDROME-ID yet is created remotely (name + resolved
// tracks) and, unless dryRun, has the new ID written back into the local
// file's #NAVIDROME-ID: directive. An already-correlated file has its
// remote name, comment, and full track list replaced to match local
// exactly — the current remote state is fetched first so a request that
// removes every existing track can be issued before a second request adds
// the desired tracks back in the correct order, since Subsonic's
// updatePlaylist endpoint has no single "replace all tracks" operation. If
// the remote side already matches local content exactly, no request is
// made at all.
//
// #TARGETS: is pushed as a musicrename-managed suffix on the remote
// comment, preserving whatever human-authored text was already there; local
// #TARGETS: being absent means the suffix is omitted entirely on push,
// reconciling a removal onto the remote side the same way a change is
// reconciled.
//
// An entry not found in index is skipped with a warning rather than
// failing the whole push.
func pushOne(client *subsonic.Client, path string, dryRun bool, index map[string]string) (*PushResult, error) {
	result := &PushResult{}

	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	gp, err := playlist.ReadGlobalPlaylist(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var ids []string
	for _, entry := range gp.Entries {
		id, ok := index[entry]
		if !ok {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				"%s: entry %q did not resolve to any song on the server; skipped", path, entry,
			))
			continue
		}
		ids = append(ids, id)
	}

	name := gp.Name
	if name == "" {
		name = filepath.Base(path)
	}

	if !gp.HasNavidromeID {
		if dryRun {
			result.Created = append(result.Created, path)
			return result, nil
		}

		created, err := client.CreatePlaylist(map[string]string{"name": name})
		if err != nil {
			return nil, fmt.Errorf("creating playlist: %w", err)
		}
		if created == nil {
			return nil, fmt.Errorf("server did not return the newly created playlist")
		}

		// The comment param isn't reliably supported at creation time
		// across Subsonic-compatible servers, so the #TARGETS: suffix
		// is set via a follow-up updatePlaylist call; the same call
		// path already confirmed to work for name changes on an
		// existing playlist, rather than a second, unverified
		// create-time code path.
		if gp.HasTargets {
			comment := composeComment("", gp.Targets, true)
			if err := client.UpdatePlaylist(created.ID, map[string]string{"comment": comment}); err != nil {
				return nil, fmt.Errorf("setting playlist comment: %w", err)
			}
		}

		if len(ids) > 0 {
			if err := client.UpdatePlaylistTracks(created.ID, ids, nil); err != nil {
				return nil, fmt.Errorf("adding tracks to new playlist: %w", err)
			}
		}

		gp.NavidromeID = created.ID
		gp.HasNavidromeID = true
		if err := playlist.WriteGlobalPlaylist(path, gp); err != nil {
			return nil, fmt.Errorf("writing back #NAVIDROME-ID: %w", err)
		}
		if sumsErr := updateLocalSums(libraryRootRootFor(path), path); sumsErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				"updating %s for %s: %v", hasher.SumsFilename, path, sumsErr,
			))
		}

		result.Created = append(result.Created, path)
		return result, nil
	}

	remote, err := client.GetPlaylist(gp.NavidromeID)
	if err != nil {
		return nil, fmt.Errorf("fetching current remote state: %w", err)
	}
	if remote == nil {
		return nil, fmt.Errorf("server returned no playlist for %s", gp.NavidromeID)
	}

	remotePaths := make([]string, len(remote.Entry))
	for i, e := range remote.Entry {
		remotePaths[i] = e.Path
	}

	// Preserve whatever human-authored text is already in the remote
	// comment (only the musicrename-managed #TARGETS: suffix is ours to
	// rewrite).
	remoteHuman, remoteTargets, remoteHasTargets := parseCommentTargets(remote.Comment)
	desiredComment := composeComment(remoteHuman, gp.Targets, gp.HasTargets)

	// Compare against a canonically-recomposed version of the remote
	// comment, not the raw string: a hand-edited or otherwise-unsorted
	// remote suffix would otherwise register as "different" purely due to
	// target order, triggering a needless update.
	normalizedRemoteComment := composeComment(remoteHuman, remoteTargets, remoteHasTargets)

	if remote.Name == name && normalizedRemoteComment == desiredComment && stringSlicesEqual(remotePaths, gp.Entries) {
		result.Unchanged = append(result.Unchanged, path)
		return result, nil
	}

	if dryRun {
		result.Updated = append(result.Updated, path)
		return result, nil
	}

	updateParams := map[string]string{}
	if remote.Name != name {
		updateParams["name"] = name
	}
	if remote.Comment != desiredComment {
		updateParams["comment"] = desiredComment
	}
	if len(updateParams) > 0 {
		if err := client.UpdatePlaylist(gp.NavidromeID, updateParams); err != nil {
			return nil, fmt.Errorf("updating playlist metadata: %w", err)
		}
	}

	if len(remote.Entry) > 0 {
		indices := make([]int, len(remote.Entry))
		for i := range indices {
			indices[i] = i
		}
		if err := client.UpdatePlaylistTracks(gp.NavidromeID, nil, indices); err != nil {
			return nil, fmt.Errorf("clearing existing tracks: %w", err)
		}
	}
	if len(ids) > 0 {
		if err := client.UpdatePlaylistTracks(gp.NavidromeID, ids, nil); err != nil {
			return nil, fmt.Errorf("adding tracks: %w", err)
		}
	}

	result.Updated = append(result.Updated, path)
	return result, nil
}
