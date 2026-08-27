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

package video

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
)

// FindVideoDirs walks root and returns every leaf directory that directly
// contains a video file, sorted. A directory is treated as a leaf (not
// descended into further) as soon as it's found to contain one, matching the
// assumption elsewhere in this package that video directories don't nest.
//
// This makes no assumption about musicvideo.nfo's presence or correctness.
// Callers that care (e.g. "video check") should inspect that separately via
// ReadNFO. Unlike Scan (rename.go), which also requires a valid nfo to
// include a directory, FindVideoDirs is the more permissive, general-purpose
// "where are all the videos" building block shared by sums/check.
func FindVideoDirs(root string) ([]string, error) {
	var dirs []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}

		hasVideo, err := dirHasVideoFile(path)
		if err != nil {
			return fmt.Errorf("checking %s: %w", path, err)
		}
		if !hasVideo {
			return nil
		}

		dirs = append(dirs, path)
		return filepath.SkipDir
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(dirs)
	return dirs, nil
}
