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
	"github.com/mfinelli/musicrename/internal/navidrome"
	"github.com/mfinelli/musicrename/internal/playlist"
)

// PullResult summarizes what a pull run did (or, in dry-run mode, would
// do). Paths are absolute local filesystem paths.
type PullResult struct {
	Created   []string
	Updated   []string
	Unchanged []string
	Deleted   []string
	Warnings  []string
}

func (r *PullResult) merge(other *PullResult) {
	r.Created = append(r.Created, other.Created...)
	r.Updated = append(r.Updated, other.Updated...)
	r.Unchanged = append(r.Unchanged, other.Unchanged...)
	r.Deleted = append(r.Deleted, other.Deleted...)
	r.Warnings = append(r.Warnings, other.Warnings...)
}

// PullAll fetches every playlist from client and reconciles them against
// libraryRootRoot's playlists/ tree: every local file's content is
// overwritten with whatever Navidrome currently holds for it, a
// remote playlist with no correlated local file yet gets a new one
// (dropped flat at playlists/; no #TARGETS: directive, so it
// applies to every target by default), and a local file whose
// #NAVIDROME-ID no longer appears in the (successfully fetched) list is
// deleted. A bulk list that came back successfully and simply
// doesn't include a previously-known ID is itself confirmed absence,
// without needing a separate per-ID existence check the way [PullOne]
// does.
//
// If dryRun is true, no files are created, updated, or deleted (the
// returned PullResult still reports what would have happened).
func PullAll(client *subsonic.Client, libraryRootRoot string, dryRun bool) (*PullResult, error) {
	result := &PullResult{}

	localByID, indexWarnings, err := indexLocalPlaylistsByID(libraryRootRoot)
	if err != nil {
		return nil, err
	}
	result.Warnings = append(result.Warnings, indexWarnings...)

	remote, err := client.GetPlaylists(nil)
	if err != nil {
		return nil, fmt.Errorf("fetching playlists: %w", err)
	}

	playlistsDir := playlist.Dir(libraryRootRoot)
	seen := make(map[string]bool, len(remote))

	for _, rp := range remote {
		seen[rp.ID] = true

		full, err := client.GetPlaylist(rp.ID)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				"fetching playlist %q (%s): %v", rp.Name, rp.ID, err,
			))
			continue
		}

		localPath, existed := localByID[rp.ID]
		if !existed {
			localPath = filepath.Join(playlistsDir, newPlaylistFilename(playlistsDir, full.Name))
		}

		pr, err := applyRemotePlaylist(libraryRootRoot, localPath, full, dryRun)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				"applying playlist %q (%s): %v", rp.Name, rp.ID, err,
			))
			continue
		}
		result.merge(pr)
	}

	for id, path := range localByID {
		if seen[id] {
			continue
		}
		if !dryRun {
			if err := os.Remove(path); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("removing %s: %v", path, err))
				continue
			}
			if err := removeLocalSums(libraryRootRoot, path); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf(
					"updating %s for removed %s: %v", hasher.SumsFilename, path, err,
				))
			}
		}
		result.Deleted = append(result.Deleted, path)
	}

	sort.Strings(result.Created)
	sort.Strings(result.Updated)
	sort.Strings(result.Unchanged)
	sort.Strings(result.Deleted)

	return result, nil
}

// PullOne pulls a single already-correlated local playlist file at path.
// path must already exist and have a #NAVIDROME-ID (there is nothing to pull
// against otherwise; push it first, or author it some other way, before
// pulling it).
//
// If the server confirms the correlated playlist no longer exists (a
// genuine "not found" response), the local file is deleted to match
// (self-healing) rather than treated as an error. Any other failure (network,
// auth, 5xx) aborts without touching the file: a generic failure must never be
// mistaken for confirmed absence.
func PullOne(client *subsonic.Client, path string, dryRun bool) (*PullResult, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	gp, err := playlist.ReadGlobalPlaylist(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if !gp.HasNavidromeID {
		return nil, fmt.Errorf("%s has no #NAVIDROME-ID; nothing to pull — push it first", path)
	}

	full, err := client.GetPlaylist(gp.NavidromeID)
	if err != nil {
		if code, ok := navidrome.ErrCode(err); ok && code == navidrome.ErrCodeNotFound {
			result := &PullResult{}
			if !dryRun {
				if rmErr := os.Remove(path); rmErr != nil {
					return nil, fmt.Errorf("removing %s: %w", path, rmErr)
				}
				if sumsErr := removeLocalSums(libraryRootRootFor(path), path); sumsErr != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf(
						"updating %s for removed %s: %v", hasher.SumsFilename, path, sumsErr,
					))
				}
			}
			result.Deleted = append(result.Deleted, path)
			return result, nil
		}
		return nil, fmt.Errorf("fetching playlist: %w", err)
	}

	return applyRemotePlaylist(libraryRootRootFor(path), path, full, dryRun)
}

// applyRemotePlaylist reconciles a single fetched remote playlist against
// localPath (which may or may not exist yet). #TARGETS: is synced from the
// remote comment's musicrename-managed suffix, not preserved from the
// existing local file (Navidrome has no directive concept of its own, but
// does have a plain comment field, which musicrename manages a suffix of).
// A target added, changed, or removed there (including by hand, in the
// Navidrome app) is reconciled locally the same way name and entries already
// are.
//
// A remote entry whose path doesn't resolve to an actual local file is
// skipped with a warning rather than failing the whole operation.
func applyRemotePlaylist(
	libraryRootRoot, localPath string, remote *subsonic.Playlist, dryRun bool,
) (*PullResult, error) {
	result := &PullResult{}

	existing, err := playlist.ReadGlobalPlaylist(localPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", localPath, err)
	}

	var entries []string
	for _, e := range remote.Entry {
		if _, statErr := os.Stat(filepath.Join(libraryRootRoot, e.Path)); statErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				"%s: entry %q does not resolve to a local file; skipped", localPath, e.Path,
			))
			continue
		}
		entries = append(entries, e.Path)
	}

	// #TARGETS: is synced from the remote comment's musicrename-managed
	// suffix, not preserved from the existing local file. A target added,
	// changed, or removed on the remote side (including from Navidrome's
	// UI, since the suffix is just a comment) is reconciled locally the
	// same way name and entries already are.
	_, targets, hasTargets := parseCommentTargets(remote.Comment)

	updated := &playlist.GlobalPlaylist{
		Name:           remote.Name,
		NavidromeID:    remote.ID,
		HasNavidromeID: true,
		Targets:        targets,
		HasTargets:     hasTargets,
		Entries:        entries,
	}

	if existing.HasNavidromeID {
		if existing.Name == updated.Name &&
			stringSlicesEqual(existing.Entries, updated.Entries) &&
			existing.HasTargets == updated.HasTargets &&
			stringSetsEqual(existing.Targets, updated.Targets) {
			result.Unchanged = append(result.Unchanged, localPath)
			return result, nil
		}
		if !dryRun {
			if err := playlist.WriteGlobalPlaylist(localPath, updated); err != nil {
				return nil, err
			}
			if sumsErr := updateLocalSums(libraryRootRoot, localPath); sumsErr != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf(
					"updating %s for %s: %v", hasher.SumsFilename, localPath, sumsErr,
				))
			}
		}
		result.Updated = append(result.Updated, localPath)
		return result, nil
	}

	if !dryRun {
		if err := playlist.WriteGlobalPlaylist(localPath, updated); err != nil {
			return nil, err
		}
		if sumsErr := updateLocalSums(libraryRootRoot, localPath); sumsErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				"updating %s for %s: %v", hasher.SumsFilename, localPath, sumsErr,
			))
		}
	}
	result.Created = append(result.Created, localPath)
	return result, nil
}

// indexLocalPlaylistsByID walks libraryRootRoot's playlists/ tree
// (playlist.WalkTree) and returns a map of #NAVIDROME-ID -> local file
// path, for every file that has one. Files without a #NAVIDROME-ID (never
// pushed) are not included since pull has nothing to correlate them against.
// A file that fails to read produces a warning rather than aborting
// indexing entirely.
func indexLocalPlaylistsByID(libraryRootRoot string) (map[string]string, []string, error) {
	byID := make(map[string]string)
	var warnings []string

	err := playlist.WalkTree(libraryRootRoot, func(path string) error {
		id, ok, err := playlist.ReadNavidromeID(path)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("reading %s: %v", path, err))
			return nil
		}
		if ok {
			byID[id] = path
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return byID, warnings, nil
}
