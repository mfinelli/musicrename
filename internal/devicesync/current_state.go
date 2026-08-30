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
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mfinelli/musicrename/internal/hasher"
	"github.com/mfinelli/musicrename/internal/target"
)

// DeviceEntry describes what's currently recorded for one on-device file,
// read from its album's sums.md5 and (if present) {target}.src.md5.
type DeviceEntry struct {
	// Hash is the file's recorded hash from the device's sums.md5
	// (always present if this entry exists at all). This is the hash of
	// the actual on-device bytes, true for both a passthrough and a
	// derived file (the difference between them is only in what that
	// hash is directly comparable against).
	Hash string
	// SrcHash is the source file's hash that produced this on-device
	// file, from {target}.src.md5 which is present only for a derived
	// file (transcoded audio, resized artwork). A passthrough file has no
	// sidecar entry at all, since its own sums.md5 entry is already
	// directly comparable to the source's sums.md5.
	SrcHash string
	// HasSrcHash reports whether SrcHash is meaningful (false for a
	// passthrough file, or for a derived file whose sidecar entry is
	// itself missing or stale).
	HasSrcHash bool
	// Size is the on-device file's actual size in bytes, from a plain
	// os.Stat during the same walk that reads sums.md5 (not from the
	// hash file itself, which doesn't record size). Used for capacity
	// planning to know how much space a deletion would actually free.
	Size int64
}

// CurrentStateResult is the output of [CurrentState].
type CurrentStateResult struct {
	Entries  map[DesiredEntry]DeviceEntry
	Warnings []string
}

// CurrentState walks devicePath (the target's mount point) and returns
// what's currently recorded there for targetName: a map, keyed the same way
// as [DesiredState]'s output ([DesiredEntry] root name plus path relative to
// that root), of every file listed in some on-device album's sums.md5. No
// hashing is performed during this walk (only sums.md5 and {target}.src.md5
// files are read, never the audio/artwork files themselves).
//
// A devicePath that doesn't exist yet (a target that has never been
// synced to before) is not an error; it simply produces an empty result.
// A single album's sums.md5 or {target}.src.md5 failing to read or parse
// is a warning, not a reason to abort discovery for the rest of the
// device: removable flash storage is exactly the kind of thing that can
// have one corrupted file without the rest of the device being unusable.
func CurrentState(devicePath, targetName string) (*CurrentStateResult, error) {
	if !target.Valid(targetName) {
		return nil, fmt.Errorf("unknown target %q", targetName)
	}

	result := &CurrentStateResult{Entries: make(map[DesiredEntry]DeviceEntry)}

	rootEntries, err := os.ReadDir(devicePath)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return nil, fmt.Errorf("reading %s: %w", devicePath, err)
	}

	srcSumsFilename := target.SrcSumsFilename(targetName)

	for _, re := range rootEntries {
		if !re.IsDir() {
			continue
		}
		root := re.Name()
		rootPath := filepath.Join(devicePath, root)

		err := filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || d.Name() != hasher.SumsFilename {
				return nil
			}

			albumDir := filepath.Dir(path)

			sums, _, sumsErr := hasher.ReadSums(albumDir, hasher.SumsFilename)
			if sumsErr != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("reading %s: %v", path, sumsErr))
				return nil
			}

			// Not every album has derived files for this target (a
			// passthrough-only album never gets a sidecar at all), so a
			// missing src.md5 here is completely normal, not a warning.
			srcSums, _, srcErr := hasher.ReadSums(albumDir, srcSumsFilename)
			if srcErr != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf(
					"reading %s: %v", filepath.Join(albumDir, srcSumsFilename), srcErr,
				))
			}

			albumRel, relErr := filepath.Rel(rootPath, albumDir)
			if relErr != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %v", albumDir, relErr))
				return nil
			}

			for name, hash := range sums {
				rel := name
				if albumRel != "." {
					rel = filepath.Join(albumRel, name)
				}

				entry := DesiredEntry{Root: root, Rel: filepath.ToSlash(rel)}
				de := DeviceEntry{Hash: hash}
				if srcHash, ok := srcSums[name]; ok {
					de.SrcHash = srcHash
					de.HasSrcHash = true
				}
				if info, statErr := os.Stat(filepath.Join(albumDir, name)); statErr == nil {
					de.Size = info.Size()
				}
				result.Entries[entry] = de
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("scanning %s: %w", rootPath, err)
		}
	}

	return result, nil
}
