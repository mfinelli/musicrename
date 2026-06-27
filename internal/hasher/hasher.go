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

// Package hasher generates sums.md5 files for album directories in a format
// fully compatible with `md5sum -c`. It computes MD5 digests directly via
// [crypto/md5] rather than shelling out, so no external tool is required to
// produce the file. Verification with `md5sum -c` works on any system that
// has md5sum installed, regardless of whether musicrename is present.
//
// The typical call sequence is:
//
//	err := hasher.Hash("/path/to/album", nil)
//
// or with progress feedback:
//
//	err := hasher.Hash("/path/to/album", func(rel string) {
//	    fmt.Printf("\r  → %s", rel)
//	})
package hasher

import (
	"crypto/md5"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SumsFilename is the name of the checksum file written at the album root.
// It is exported so the metadata scanner and other packages can reference it
// without importing a string literal.
const SumsFilename = "sums.md5"

// textExtensions is the set of file extensions treated as text by md5sum.
// Files with these extensions use the two-space separator in the output;
// all other files use the binary format (space + asterisk).
// Classification is by extension only; no magic-byte inspection is performed.
var textExtensions = map[string]bool{
	".cue":  true,
	".log":  true,
	".m3u":  true,
	".m3u8": true,
	".txt":  true,
}

// Hash computes MD5 checksums for all files under dir (recursively),
// excluding sums.md5 itself, and writes the result to dir/sums.md5 in
// a format compatible with `md5sum -c`. Any existing sums.md5 is overwritten.
//
// Files are processed in sorted order so the output is stable across runs
// and diffs cleanly between library updates.
//
// progress, if non-nil, is called with the relative path of each file
// immediately before it is hashed. This provides live feedback for slow
// media; the caller is responsible for any terminal formatting.
func Hash(dir string, progress func(string)) error {
	files, err := collectFiles(dir)
	if err != nil {
		return fmt.Errorf("scanning %s: %w", dir, err)
	}

	var sb strings.Builder
	for _, rel := range files {
		if progress != nil {
			progress(rel)
		}

		sum, err := hashFile(filepath.Join(dir, rel))
		if err != nil {
			return fmt.Errorf("hashing %s: %w", rel, err)
		}

		if isTextFile(rel) {
			fmt.Fprintf(&sb, "%s  %s\n", sum, rel)
		} else {
			fmt.Fprintf(&sb, "%s *%s\n", sum, rel)
		}
	}

	dest := filepath.Join(dir, SumsFilename)
	if err := os.WriteFile(dest, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", dest, err)
	}
	return nil
}

// collectFiles walks dir recursively and returns all file paths relative to
// dir in sorted order, excluding SumsFilename.
func collectFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == SumsFilename {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// hashFile returns the lowercase hex MD5 digest of the file at path.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// isTextFile reports whether path should use the text-mode separator in the
// md5sum output (two spaces instead of space + asterisk). Classification is
// based purely on the file extension; no magic-byte inspection is performed.
func isTextFile(path string) bool {
	return textExtensions[strings.ToLower(filepath.Ext(path))]
}
