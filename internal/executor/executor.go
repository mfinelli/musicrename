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

package executor

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mfinelli/musicrename/internal/planner"
)

// Execute performs the filesystem changes defined in the Plan.
// libraryRoot is used as the stopping point for empty-directory cleanup;
// it must be the same value passed to planner.New.
// It returns a slice of warnings (e.g., race conditions) and a final error
// if the process must abort.
func Execute(plan *planner.Plan, libraryRoot string) ([]string, error) {
	var warnings []string
	touchedDirs := make(map[string]struct{})

	for _, album := range plan.Albums {
		// 1. Ensure the album destination directory exists with correct
		// casing. This handles the case where a same-named but
		// differently-cased directory already exists on macOS HFS+.
		if err := ensureDir(album.DestDir); err != nil {
			return nil, fmt.Errorf("failed to prepare directory %s: %w", album.DestDir, err)
		}

		for _, op := range album.Moves {
			if op.IsNoOp {
				continue
			}

			// Track all source directories for cleanup later, not
			// just the album root. This ensures artwork/, scans/,
			// and extras/ subdirs are also cleaned up if emptied.
			touchedDirs[filepath.Dir(op.OldPath)] = struct{}{}

			// Ensure the destination parent exists. This is
			// necessary for files going into artwork/, scans/, and
			// extras/ subdirectories, which are not created by the
			// album-level ensureDir call above.
			if err := os.MkdirAll(filepath.Dir(op.NewPath), 0755); err != nil {
				return nil, fmt.Errorf("failed to create directory %s: %w", filepath.Dir(op.NewPath), err)
			}

			// 2. Race Condition Check.
			// If a file appeared at the destination since planning,
			// skip with a warning rather than aborting the run.
			if _, err := os.Stat(op.NewPath); err == nil {
				warnings = append(warnings, fmt.Sprintf(
					"race condition: file already exists at %s, skipping move",
					op.NewPath,
				))
				continue
			}

			// 3. Perform the Move.
			var err error
			if op.IsCaseOnly {
				err = moveCaseInsensitive(op)
			} else {
				err = moveWithFallback(op.OldPath, op.NewPath)
			}

			if err != nil {
				return nil, fmt.Errorf("failed to move %s to %s: %w", op.OldPath, op.NewPath, err)
			}
		}
	}

	// 4. Best-effort cleanup of empty source directories, bubbling upward
	// to (but not including) libraryRoot.
	for dir := range touchedDirs {
		cleanupEmpty(dir, libraryRoot)
	}

	return warnings, nil
}

// ensureDir ensures the directory at path exists with the correct casing.
// If the directory does not exist, it is created (along with any missing
// parents) via os.MkdirAll. If it does exist on a case-insensitive
// filesystem (e.g. macOS HFS+) with different casing, it is renamed to
// match path exactly via a temporary intermediate name.
func ensureDir(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return os.MkdirAll(path, 0755)
	} else if err != nil {
		return err
	}

	// The path exists. On a case-insensitive filesystem it may exist with
	// different casing. Read the parent directory to obtain the actual
	// on-disk name; info.Name() would only reflect what we asked for, not
	// what is really there.
	parent := filepath.Dir(path)
	targetName := filepath.Base(path)

	entries, err := os.ReadDir(parent)
	if err != nil {
		return err
	}

	var actualName string
	for _, e := range entries {
		if strings.EqualFold(e.Name(), targetName) {
			actualName = e.Name()
			break
		}
	}

	if actualName == "" || actualName == targetName {
		// Either already correct, or not found in the parent listing
		// (shouldn't happen after Stat succeeded, but be safe).
		return nil
	}

	// Case mismatch: rename via a temp name in the same parent (same
	// filesystem) to avoid a silent no-op on case-insensitive systems.
	tmpDir := filepath.Join(parent, fmt.Sprintf(
		"%s.musicrename-tmp-%d", targetName, time.Now().UnixNano(),
	))
	if err := os.Rename(filepath.Join(parent, actualName), tmpDir); err != nil {
		return err
	}
	return os.Rename(tmpDir, path)
}

// moveCaseInsensitive renames a file via a temporary intermediate path to
// handle case-only changes on case-insensitive filesystems, where a direct
// rename from "Foo.flac" to "foo.flac" would be a silent no-op.
// The temporary path is placed in the same directory as the destination to
// guarantee it is on the same filesystem.
func moveCaseInsensitive(op planner.MoveOperation) error {
	parent := filepath.Dir(op.NewPath)
	tmpPath := filepath.Join(parent, fmt.Sprintf(
		".musicrename-tmp-%d", time.Now().UnixNano(),
	))
	if err := os.Rename(op.OldPath, tmpPath); err != nil {
		return err
	}
	return os.Rename(tmpPath, op.NewPath)
}

// moveWithFallback attempts os.Rename and falls back to copyAndDelete when
// the source and destination are on different filesystems (EXDEV).
func moveWithFallback(oldPath, newPath string) error {
	err := os.Rename(oldPath, newPath)
	if err == nil {
		return nil
	}

	if linkErr, ok := err.(*os.LinkError); ok {
		if linkErr.Err == syscall.EXDEV {
			return copyAndDelete(oldPath, newPath)
		}
	}

	return err
}

// copyAndDelete copies src to dst preserving file permissions, then removes
// src. It is used as a fallback for cross-device moves where os.Rename fails.
// If the copy fails partway through, dst is removed to avoid leaving a
// partial file on disk.
func copyAndDelete(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	info, err := sourceFile.Stat()
	if err != nil {
		return err
	}

	destFile, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	// No defer for destFile: we close it explicitly below so we can
	// capture the error, which is where buffered writes may surface.

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		destFile.Close()
		os.Remove(dst)
		return err
	}

	if err := destFile.Close(); err != nil {
		os.Remove(dst)
		return err
	}

	return os.Remove(src)
}

// cleanupEmpty removes dir if it is empty, then walks upward removing each
// ancestor until it reaches libraryRoot, encounters a non-empty directory,
// or hits a filesystem error. This is best-effort: errors are silently
// ignored per the design (only dirs the tool emptied are candidates).
func cleanupEmpty(dir, libraryRoot string) {
	for {
		// Never remove the library root itself or climb above it.
		if dir == libraryRoot {
			return
		}
		// Guard against climbing past the filesystem root.
		if dir == filepath.Dir(dir) {
			return
		}
		if err := os.Remove(dir); err != nil {
			// Non-empty directory or unrelated error; stop climbing.
			return
		}
		dir = filepath.Dir(dir)
	}
}
