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

	"github.com/mfinelli/musicrename/internal/planner"
)

// Execute performs the filesystem changes defined in the Plan.
// It returns a slice of warnings (e.g., race conditions) and a final error if the process must abort.
func Execute(plan *planner.Plan) ([]string, error) {
	var warnings []string
	touchedDirs := make(map[string]struct{})

	for _, album := range plan.Albums {
		// 1. Handle Destination Directory Casing/Creation
		if err := ensureDir(album.DestDir); err != nil {
			return nil, fmt.Errorf("failed to prepare directory %s: %w", album.DestDir, err)
		}

		// Record the source directory for cleanup later
		touchedDirs[album.SourceDir] = struct{}{}

		for _, op := range album.Moves {
			if op.IsNoOp {
				continue
			}

			// 2. Race Condition Check
			// If a file appeared at the destination since planning, skip with warning
			if _, err := os.Stat(op.NewPath); err == nil {
				warnings = append(warnings, fmt.Sprintf("race condition: file already exists at %s, skipping move", op.NewPath))
				continue
			}

			// 3. Perform the Move
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

	// 4. Best-effort cleanup of empty source directories
	for dir := range touchedDirs {
		if err := os.Remove(dir); err != nil {
			// We ignore errors here as per design (best-effort)
			// os.Remove only works if the directory is empty
		}
	}

	return warnings, nil
}

// ensureDir ensures the directory exists and has the correct casing.
func ensureDir(path string) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return os.MkdirAll(path, 0755)
	}
	if err != nil {
		return err
	}

	// We need to check if the folder exists but has the WRONG casing.
	// On a case-insensitive system (macOS/Windows), os.Stat returns
	// a result even if the case is different.

	// Get the actual name of the directory as it exists on disk.
	actualName := info.Name()
	// Since path is the full path, we just want the base directory name
	targetName := filepath.Base(path)

	// If the names are NOT exactly identical (case-sensitive),
	// but they ARE identical when ignoring case...
	if actualName != targetName && strings.EqualFold(actualName, targetName) {
		// We have a casing mismatch.
		// We must do the rename dance on the parent directory.
		parent := filepath.Dir(path)
		tmpDir := filepath.Join(parent, targetName+".tmp")

		// Rename existing (wrong case) -> tmp
		// Note: we use the path that os.Stat acknowledged exists
		if err := os.Rename(path, tmpDir); err != nil {
			return err
		}
		// Rename tmp -> target (correct case)
		return os.Rename(tmpDir, path)
	}

	return nil
}

// moveCaseInsensitive handles the rename dance for case-only changes
func moveCaseInsensitive(op planner.MoveOperation) error {
	tmpPath := op.NewPath + ".tmp"
	if err := os.Rename(op.OldPath, tmpPath); err != nil {
		return err
	}
	return os.Rename(tmpPath, op.NewPath)
}

// moveWithFallback attempts os.Rename and falls back to Copy+Delete on EXDEV
func moveWithFallback(oldPath, newPath string) error {
	err := os.Rename(oldPath, newPath)
	if err == nil {
		return nil
	}

	// Check if this is a cross-device link error
	if linkErr, ok := err.(*os.LinkError); ok {
		if linkErr.Err == syscall.EXDEV {
			return copyAndDelete(oldPath, newPath)
		}
	}

	return err
}

// copyAndDelete handles moves across different partitions/devices
func copyAndDelete(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	// Get source permissions
	info, err := sourceFile.Stat()
	if err != nil {
		return err
	}

	destFile, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return err
	}

	// Explicitly close files before removing source
	sourceFile.Close()
	destFile.Close()

	return os.Remove(src)
}
