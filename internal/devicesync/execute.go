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

package devicesync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mfinelli/musicrename/internal/artwork"
	"github.com/mfinelli/musicrename/internal/hasher"
	"github.com/mfinelli/musicrename/internal/target"
)

// ExecuteResult summarizes what Execute actually did (or, in dry-run mode,
// would do).
type ExecuteResult struct {
	Created  []DesiredEntry
	Updated  []DesiredEntry
	Warnings []string
}

// Execute applies diff's Add and Regenerate changes: creates or regenerates
// each entry via [PrepareTrack] (audio) or [artwork.ResizeFile] (external
// artwork), then updates the affected album's on-device sums.md5 and (for a
// derived file) {target}.src.md5 to match.
//
// TODO: Delete changes and empty-directory cleanup are not handled here.
// diff.Changes entries with ActionDelete or ActionSkip are simply ignored.
//
// Changes are grouped by album so that (1) an embedding target's album
// artwork is resized at most once and reused for every track in that
// album needing it, rather than once per track, and (2) each album's
// sums.md5/{target}.src.md5 are read once, updated in memory for every
// changed entry in that album, and written back once, rather than one
// read-modify-write round trip per file.
//
// If dryRun is true, nothing is written or read-modify-written (the
// returned ExecuteResult still reports what would have happened, and no
// album's checksum files are touched at all).
func Execute(
	ctx context.Context, libraryRootRoot, devicePath, targetName string, diff *DiffResult, dryRun bool,
) (*ExecuteResult, error) {
	def, ok := target.DefinitionFor(targetName)
	if !ok {
		return nil, fmt.Errorf("unknown target %q", targetName)
	}

	result := &ExecuteResult{}

	type albumWork struct {
		key     AlbumKey
		changes []PlannedChange
	}
	albumIndex := make(map[AlbumKey]int)
	var albums []albumWork

	for _, change := range diff.Changes {
		if change.Action != ActionAdd && change.Action != ActionRegenerate {
			continue
		}
		key := AlbumKey{Root: change.Entry.Root, Dir: filepath.ToSlash(filepath.Dir(change.Entry.Rel))}
		if idx, ok := albumIndex[key]; ok {
			albums[idx].changes = append(albums[idx].changes, change)
			continue
		}
		albumIndex[key] = len(albums)
		albums = append(albums, albumWork{key: key, changes: []PlannedChange{change}})
	}

	for _, aw := range albums {
		if err := executeAlbum(
			ctx, libraryRootRoot, devicePath, targetName, def, aw.key, aw.changes, dryRun, result,
		); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// executeAlbum handles every add/regenerate change within a single album,
// batching that album's sums.md5/{target}.src.md5 read-modify-write into
// one pass regardless of how many of its files are changing.
func executeAlbum(
	ctx context.Context, libraryRootRoot, devicePath, targetName string, def target.Definition,
	key AlbumKey, changes []PlannedChange, dryRun bool, result *ExecuteResult,
) error {
	sourceAlbumDir := filepath.Join(libraryRootRoot, key.Root, key.Dir)
	deviceAlbumDir := filepath.Join(devicePath, key.Root, key.Dir)
	srcSumsFilename := target.SrcSumsFilename(targetName)

	sourceSums, _, err := hasher.ReadSums(sourceAlbumDir, hasher.SumsFilename)
	if err != nil {
		return fmt.Errorf("reading %s: %w", filepath.Join(sourceAlbumDir, hasher.SumsFilename), err)
	}

	deviceSums, _, err := hasher.ReadSums(deviceAlbumDir, hasher.SumsFilename)
	if err != nil {
		return fmt.Errorf("reading %s: %w", filepath.Join(deviceAlbumDir, hasher.SumsFilename), err)
	}
	if deviceSums == nil {
		deviceSums = make(map[string]string)
	}

	deviceSrcSums, _, err := hasher.ReadSums(deviceAlbumDir, srcSumsFilename)
	if err != nil {
		return fmt.Errorf("reading %s: %w", filepath.Join(deviceAlbumDir, srcSumsFilename), err)
	}
	if deviceSrcSums == nil {
		deviceSrcSums = make(map[string]string)
	}

	// For an embedding target, resize the album's artwork at most once
	// here, reused for every track in this album that needs it below,
	// not once per track.
	var artBytes []byte
	var artName string
	hasArt := false
	if def.EmbedArt {
		name, found, ferr := findPrimaryArt(sourceAlbumDir)
		if ferr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %v", sourceAlbumDir, ferr))
		} else if found {
			hasArt = true
			artName = name
			if !dryRun {
				raw, rerr := os.ReadFile(filepath.Join(sourceAlbumDir, artName))
				if rerr != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("reading %s: %v", artName, rerr))
				} else if resized, rzerr := artwork.Resize(raw, def.ArtMaxDimension); rzerr != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("resizing %s: %v", artName, rzerr))
				} else {
					artBytes = resized
				}
			}
		}
	}

	changed := false
	for _, change := range changes {
		entry := change.Entry
		sourceName := filepath.Base(entry.Rel)
		deviceName := filepath.Base(deviceRelFor(entry.Rel, def))
		sourcePath := filepath.Join(sourceAlbumDir, sourceName)
		destPath := filepath.Join(deviceAlbumDir, deviceName)
		srcHash := sourceSums[sourceName] // "" if missing; diff already decided this needs (re)generating regardless

		if dryRun {
			if change.Action == ActionAdd {
				result.Created = append(result.Created, entry)
			} else {
				result.Updated = append(result.Updated, entry)
			}
			continue
		}

		var writeErr error
		if isArtworkName(sourceName) {
			writeErr = artwork.ResizeFile(sourcePath, destPath, def.ArtMaxDimension)
		} else {
			writeErr = PrepareTrack(ctx, sourcePath, destPath, def, artBytes)
		}
		if writeErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %v", entry.Rel, writeErr))
			continue
		}

		outHash, hashErr := hasher.HashFile(destPath)
		if hashErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("hashing %s: %v", destPath, hashErr))
			continue
		}
		deviceSums[deviceName] = outHash
		changed = true

		if outHash == srcHash {
			// Byte-identical: an ordinary passthrough, no sidecar entry
			// is needed or wanted and if one exists from a previous
			// derived write, it's now stale and must be removed, or the
			// next Diff run would find a leftover SrcHash that no longer
			// reflects how this file was actually produced.
			delete(deviceSrcSums, deviceName)
		} else {
			deviceSrcSums[deviceName] = srcHash
		}

		if change.Action == ActionAdd {
			result.Created = append(result.Created, entry)
		} else {
			result.Updated = append(result.Updated, entry)
		}
	}

	if dryRun {
		return nil
	}

	if def.EmbedArt && hasArt {
		// The album-level artwork bookkeeping entry which is
		// not a real on-device file, but the same valid sums format,
		// recording whatever the source's currently-known artwork hash
		// is (possibly "", mirroring how Diff already treats a missing
		// one rather than erroring here).
		deviceSrcSums[artName] = sourceSums[artName]
		changed = true
	}

	if !changed {
		return nil
	}

	if err := hasher.WriteSums(deviceAlbumDir, hasher.SumsFilename, deviceSums); err != nil {
		return fmt.Errorf("writing %s: %w", filepath.Join(deviceAlbumDir, hasher.SumsFilename), err)
	}
	if len(deviceSrcSums) > 0 {
		if err := hasher.WriteSums(deviceAlbumDir, srcSumsFilename, deviceSrcSums); err != nil {
			return fmt.Errorf("writing %s: %w", filepath.Join(deviceAlbumDir, srcSumsFilename), err)
		}
	}

	return nil
}
