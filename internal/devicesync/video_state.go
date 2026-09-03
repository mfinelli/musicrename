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
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/mfinelli/musicrename/internal/playlist"
	"github.com/mfinelli/musicrename/internal/target"
)

// VideoDesiredState computes the full desired-state set for targetName's
// video sync: every entry currently listed in
// libraryRootRoot/videos/{target}.m3u8, resolved to a real file. Unlike
// [DesiredState], there's no per-album manifest union, no global-playlist
// union, and no artwork entries; video selection is a single flat manifest
// at the video root, and Rockbox's video plugin has no use for a
// thumbnail/artwork file.
//
// Takes libraryRootRoot, not the video root directly (matching
// [DesiredState]'s parameter and [Plan]'s calling convention exactly
// (and, downstream, `sync ipod`/`sync sdcard`'s CLI shape,
// <device-path> [library-root]) so that every returned DesiredEntry's
// fixed "videos" Root really does mean "join libraryRootRoot, \"videos\",
// and Rel to reconstruct the real path", the same relationship every
// other DesiredEntry in this package already has to whatever
// libraryRootRoot the caller supplied.
//
// This does not re-validate that targetName was actually allowed to
// select videos in the first place (`video select` already refuses a
// target with SupportsVideo false outright) only that it exists and
// supports video at all, the same two checks [DesiredState] itself makes.
// A target that was never allowed to select simply has no manifest to
// read, resolving to an empty result the same as any other target that's
// never had `video select` run for it, not something worth a special
// early-exit case.
//
// An entry that doesn't resolve to an actual file under libraryRootRoot/
// videos is skipped with a warning rather than included or failing the
// whole computation, matching DesiredState's identical posture toward the
// same situation.
func VideoDesiredState(libraryRootRoot, targetName string) (*DesiredStateResult, error) {
	def, ok := target.DefinitionFor(targetName)
	if !ok {
		return nil, fmt.Errorf("unknown target %q", targetName)
	}
	if !def.SupportsVideo {
		return nil, fmt.Errorf("target %q does not support video", targetName)
	}

	videoRoot := filepath.Join(libraryRootRoot, "videos")
	result := &DesiredStateResult{}

	names, err := playlist.ReadManifest(videoRoot, targetName)
	if err != nil {
		return nil, err
	}

	for _, name := range names {
		full := filepath.Join(videoRoot, name)
		if _, statErr := os.Stat(full); statErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				"%s: entry %q does not resolve to a file", playlist.ManifestFilename(targetName), name,
			))
			continue
		}
		result.Entries = append(result.Entries, DesiredEntry{Root: "videos", Rel: filepath.ToSlash(name)})
	}

	sort.Slice(result.Entries, func(i, j int) bool {
		return result.Entries[i].Rel < result.Entries[j].Rel
	})

	return result, nil
}
