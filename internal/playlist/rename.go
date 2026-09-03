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

package playlist

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/mfinelli/musicrename/internal/hasher"
	"github.com/mfinelli/musicrename/internal/sanitize"
)

// RenameOp describes a single planned rename of a library-wide playlist
// file to the sanitized filename implied by its #PLAYLIST: directive.
type RenameOp struct {
	// OldPath is the playlist file's current absolute path.
	OldPath string
	// NewPath is its sanitized destination absolute path, always in the
	// same directory as OldPath.
	NewPath string
}

// Skipped describes a playlist file PlanRenames declined to plan a rename
// for, and why.
type Skipped struct {
	// Path is the absolute path of the skipped playlist file.
	Path string
	// Message describes why it was skipped.
	Message string
}

// PlanRenames walks libraryRootRoot's playlists/ tree (see [WalkTree]) and
// computes the destination filename implied by each playlist's #PLAYLIST:
// directive, passed through the same sanitization pipeline used everywhere
// else in this project (sanitize.CleanString with TrackOverride), truncated
// to 40 characters (the same limit used for other root-level filenames).
//
// A file with no #PLAYLIST: directive, or whose directive value sanitizes
// to an empty string, is skipped and described in the returned slice rather
// than treated as an error, since a hand-created file may simply not have
// the directive set yet. A file already at its correctly-sanitized name is
// omitted from ops entirely since there's nothing to do.
//
// Collision detection runs as part of the walk: if two files' directives
// sanitize to the same destination filename, the first such conflict found
// aborts with an error (ops and skipped are both nil in that case),
// matching planner.PlanLibrary's fail-fast behaviour for the analogous
// album/track rename. A destination that merely already exists on the
// filesystem under an unrelated name (as opposed to being claimed by
// another op here) is not checked at this stage; that's a race-condition
// concern handled by [ExecuteRenames] at execution time instead, the same
// division of responsibility as the planner/executor split for the main
// rename command.
func PlanRenames(libraryRootRoot string) (ops []RenameOp, skipped []Skipped, err error) {
	dests := make(map[string]string) // newPath -> the oldPath already claiming it

	walkErr := WalkTree(libraryRootRoot, func(path string) error {
		gp, readErr := ReadGlobalPlaylist(path)
		if readErr != nil {
			skipped = append(skipped, Skipped{
				Path:    path,
				Message: fmt.Sprintf("could not read playlist: %v", readErr),
			})
			return nil
		}
		if gp.Name == "" {
			skipped = append(skipped, Skipped{
				Path:    path,
				Message: "no #PLAYLIST: directive",
			})
			return nil
		}

		stem := sanitize.Truncate(sanitize.CleanString(gp.Name, sanitize.TrackOverride), 40)
		if stem == "" {
			skipped = append(skipped, Skipped{
				Path:    path,
				Message: fmt.Sprintf("name %q sanitizes to an empty string", gp.Name),
			})
			return nil
		}

		newPath := filepath.Join(filepath.Dir(path), stem+Ext)
		if newPath == path {
			// Already correctly named; nothing to do.
			return nil
		}

		if existingOld, exists := dests[newPath]; exists && existingOld != path {
			return fmt.Errorf(
				"collision detected: %s and %s both sanitize to %s",
				existingOld, path, newPath,
			)
		}
		dests[newPath] = path

		ops = append(ops, RenameOp{OldPath: path, NewPath: newPath})
		return nil
	})
	if walkErr != nil {
		return nil, nil, walkErr
	}

	// Stable, deterministic ordering for display and execution, independent
	// of filesystem walk order.
	sort.Slice(ops, func(i, j int) bool { return ops[i].OldPath < ops[j].OldPath })
	sort.Slice(skipped, func(i, j int) bool { return skipped[i].Path < skipped[j].Path })

	return ops, skipped, nil
}

// ExecuteRenames performs every planned rename in ops via os.Rename, in
// order. If a destination has appeared on the filesystem since planning (a
// race condition if e.g. another process wrote there in the meantime), that
// op is skipped with a warning rather than aborting the remaining renames,
// matching executor.Execute's behaviour for the main rename command. A
// genuine rename failure (e.g. a permissions error) stops immediately and
// returns the error, alongside whatever warnings were collected so far.
//
// libraryRootRoot is used only to resolve each op's path relative to the
// playlists/ tree root (see [Dir]), for updating playlists/sums.md5: a
// successful rename relabels that file's entry there via [hasher.RenameFile]
// (the hash itself is unchanged and only the name is moved). This command
// never creates a sums.md5 from scratch; if one doesn't already exist, no
// attempt is made to update it. If it does exist but has no entry for a
// given oldRel (most likely meaning it was already stale before this call)
// that's reported as a warning rather than silently ignored or treated as a
// hard failure, since the rename itself already succeeded.
func ExecuteRenames(libraryRootRoot string, ops []RenameOp) (warnings []string, err error) {
	playlistsDir := Dir(libraryRootRoot)
	_, statErr := os.Stat(filepath.Join(playlistsDir, hasher.SumsFilename))
	sumsExists := statErr == nil

	for _, op := range ops {
		if _, raceErr := os.Stat(op.NewPath); raceErr == nil {
			warnings = append(warnings, fmt.Sprintf(
				"race condition: file already exists at %s, skipping rename of %s",
				op.NewPath, op.OldPath,
			))
			continue
		}

		if renameErr := os.Rename(op.OldPath, op.NewPath); renameErr != nil {
			return warnings, fmt.Errorf("renaming %s to %s: %w", op.OldPath, op.NewPath, renameErr)
		}

		if !sumsExists {
			continue
		}

		oldRel, oldRelErr := filepath.Rel(playlistsDir, op.OldPath)
		newRel, newRelErr := filepath.Rel(playlistsDir, op.NewPath)
		if oldRelErr != nil || newRelErr != nil {
			warnings = append(warnings, fmt.Sprintf(
				"updating %s for %s: could not resolve path relative to %s",
				hasher.SumsFilename, op.NewPath, playlistsDir,
			))
			continue
		}

		found, sumsErr := hasher.RenameFile(playlistsDir, oldRel, newRel)
		if sumsErr != nil {
			warnings = append(warnings, fmt.Sprintf(
				"updating %s for %s: %v", hasher.SumsFilename, newRel, sumsErr,
			))
		} else if !found {
			warnings = append(warnings, fmt.Sprintf(
				"%s exists but has no entry for %s; leaving as-is", hasher.SumsFilename, oldRel,
			))
		}
	}
	return warnings, nil
}
