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

// Package renamesync keeps sums.md5 (internal/hasher) and album-local target
// selection manifests ({target}.m3u8, internal/playlist) in sync with the
// moves described by a completed [planner.Plan] (internal/planner), after
// [executor.Execute] (internal/executor) has already performed them.
//
// Sync is a best-effort follow-up, not a filesystem-changing step in its own
// right in the same sense as executor: by the time it runs, every file move
// has already succeeded, so a checksum/manifest bookkeeping failure is
// reported as a warning rather than treated as fatal or rolled back.
package renamesync

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mfinelli/musicrename/internal/hasher"
	"github.com/mfinelli/musicrename/internal/metadata"
	"github.com/mfinelli/musicrename/internal/planner"
	"github.com/mfinelli/musicrename/internal/playlist"
	"github.com/mfinelli/musicrename/internal/target"
)

// Sync walks every move in plan and, for each real (non-no-op) move whose
// path relative to its own album root actually changed (a real filename
// change, or a case-only rename; a directory-only move needs no follow-up,
// since sums.md5 and manifest entries are relative to the album root, not
// absolute):
//
//   - Updates that entry's filename in place in sums.md5 (never rehashing:
//     the file's content is unchanged, only its name is), if sums.md5
//     exists in the album and skipMD5 is false.
//   - For audio files specifically, updates any {target}.m3u8 that
//     references the old filename, unless skipPlaylists is true. When that
//     actually rewrites a manifest's content, the manifest's own sums.md5
//     entry (if any) is then rehashed via [hasher.UpdateFile] rather than
//     [hasher.RenameFile] (its bytes genuinely changed this time, unlike
//     the audio file's own filename-only rename above, so a full rehash of
//     that one file is correct and necessary here, not something to avoid).
//
// A move is only acted on if its NewPath actually exists on disk afterward.
// The executor may have skipped a specific move due to a race condition
// (something appearing at the destination between planning and execution)
// without that aborting the whole run, and this guards against rewriting
// sums.md5/manifests to reference a file that was never actually created.
//
// Sync assumes executor.Execute has already run successfully; it makes no
// attempt to move files itself, and calling it against a plan that hasn't
// been executed (or that failed partway through) will produce meaningless
// results. Returned warnings should be surfaced alongside any other
// warnings from planning/execution, not treated as fatal (every touched
// file has already been successfully moved by the time Sync runs).
func Sync(plan *planner.Plan, skipMD5, skipPlaylists bool) []string {
	if skipMD5 && skipPlaylists {
		return nil
	}

	var warnings []string

	for _, ap := range plan.Albums {
		for _, op := range ap.Moves {
			if op.IsNoOp {
				continue
			}

			oldRel, err := filepath.Rel(ap.SourceDir, op.OldPath)
			if err != nil {
				continue
			}
			newRel, err := filepath.Rel(ap.DestDir, op.NewPath)
			if err != nil {
				continue
			}
			if oldRel == newRel {
				continue
			}

			if _, err := os.Stat(op.NewPath); err != nil {
				// The move didn't actually happen (e.g. a race-condition
				// skip reported separately by the executor); nothing here
				// should reference a file that was never created.
				continue
			}

			if !skipMD5 {
				found, err := hasher.RenameFile(ap.DestDir, oldRel, newRel)
				switch {
				case err != nil:
					warnings = append(warnings, fmt.Sprintf(
						"updating sums.md5 for %s: %v", newRel, err,
					))
				case !found:
					if _, statErr := os.Stat(filepath.Join(ap.DestDir, hasher.SumsFilename)); statErr == nil {
						warnings = append(warnings, fmt.Sprintf(
							"sums.md5 exists but has no entry for %s; leaving as-is", oldRel,
						))
					}
				}
			}

			if !skipPlaylists && metadata.IsAudioExt(filepath.Ext(op.NewPath)) {
				for _, tgt := range target.Names {
					changed, err := playlist.RenameEntry(ap.DestDir, tgt, oldRel, newRel)
					if err != nil {
						warnings = append(warnings, fmt.Sprintf(
							"updating %s for %s: %v", playlist.ManifestFilename(tgt), newRel, err,
						))
						continue
					}

					if changed && !skipMD5 {
						manifestName := playlist.ManifestFilename(tgt)
						if err := hasher.UpdateFile(ap.DestDir, manifestName); err != nil {
							warnings = append(warnings, fmt.Sprintf(
								"updating sums.md5 for %s: %v", manifestName, err,
							))
						}
					}
				}
			}
		}
	}

	return warnings
}
