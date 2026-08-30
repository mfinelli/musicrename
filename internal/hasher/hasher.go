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
	"errors"
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
//
// .nfo is included for video.NFOFilename (musicvideo.nfo). This package has
// no video-specific knowledge otherwise; it's included here because the
// classification set is inherently extension-based and shared.
var textExtensions = map[string]bool{
	".cue":  true,
	".log":  true,
	".m3u":  true,
	".m3u8": true,
	".nfo":  true,
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

// HashFile computes the lowercase hex MD5 digest of the file at path, in
// the same format every other hash in this package records and compares.
// Exported for callers outside this package that need to hash a single
// on-device file directly rather than through one of this package's
// read/update primitives.
func HashFile(path string) (string, error) {
	return hashFile(path)
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

// sumEntry is a single parsed line from a sums.md5 file.
type sumEntry struct {
	hash string
	name string
}

// parseSumsFile reads and parses dir/sums.md5. The second return value
// reports whether the file existed; a missing file is not an error and
// returns (nil, false, nil). Malformed lines (shorter than the fixed-width
// "<32-hex-hash><sep>" prefix) are skipped defensively rather than causing
// the whole read to fail.
func parseSumsFile(dir string) ([]sumEntry, bool, error) {
	return parseNamedSumsFile(dir, SumsFilename)
}

// parseNamedSumsFile is [parseSumsFile] generalized to an arbitrary
// filename within dir, sharing the same md5sum-compatible line format.
// Used for reading {target}.src.md5 sidecars, which are structurally
// identical to sums.md5 but live under a different name.
func parseNamedSumsFile(dir, filename string) ([]sumEntry, bool, error) {
	path := filepath.Join(dir, filename)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	var entries []sumEntry
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		// Fixed-width prefix: 32 hex characters, then a two-character
		// separator ("  " for text, " *" for binary), then the filename.
		if len(line) < 34 {
			continue
		}
		entries = append(entries, sumEntry{hash: line[:32], name: line[34:]})
	}
	return entries, true, nil
}

// writeSumsEntries serializes entries to dir/sums.md5, sorted by filename
// (not by the raw line, which would sort by hash first and destroy the
// stable, diffable filename ordering.
func writeSumsEntries(dir string, entries []sumEntry) error {
	return writeNamedSumsEntries(dir, SumsFilename, entries)
}

// writeNamedSumsEntries is [writeSumsEntries] generalized to an arbitrary
// filename within dir, the write-side counterpart to
// [parseNamedSumsFile]/[ReadSums]. Creates dir itself if it doesn't
// already exist which is unlike every other write in this package that only
// ever update an *existing* album's checksum file, this is also used to
// write a brand-new on-device album's sums.md5 on its very first sync,
// where the destination directory may not exist yet at all.
func writeNamedSumsEntries(dir, filename string, entries []sumEntry) error {
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })

	var sb strings.Builder
	for _, e := range entries {
		if isTextFile(e.name) {
			fmt.Fprintf(&sb, "%s  %s\n", e.hash, e.name)
		} else {
			fmt.Fprintf(&sb, "%s *%s\n", e.hash, e.name)
		}
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	dest := filepath.Join(dir, filename)
	if err := os.WriteFile(dest, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", dest, err)
	}
	return nil
}

// WriteSums writes sums to dir/filename in the same md5sum-compatible
// format [ReadSums] parses, sorted by filename (the inverse of ReadSums).
// Works for sums.md5 itself and equally for a {target}.src.md5 sidecar.
// Creates dir/filename (and dir itself) if it doesn't already exist, or
// overwrites it completely if it does (this always writes sums in full,
// there is no targeted single-entry variant the way [UpdateFile] is for
// sums.md5 specifically).
func WriteSums(dir, filename string, sums map[string]string) error {
	entries := make([]sumEntry, 0, len(sums))
	for name, hash := range sums {
		entries = append(entries, sumEntry{hash: hash, name: name})
	}
	return writeNamedSumsEntries(dir, filename, entries)
}

// UpdateFile recomputes the MD5 digest for the single file dir/rel and
// updates (or inserts) its line in dir/sums.md5, leaving every other line
// byte-for-byte as it was. This is intentionally not a full [Hash] re-run:
// recomputing every file's digest would silently overwrite the recorded hash
// of every other, untouched file in the album, masking any real corruption
// (bit rot, a failing drive) that happened to that file since the last real
// sums.md5 generation instead of flagging it. UpdateFile only ever touches
// the one file whose content is known to have actually changed.
//
// If dir/sums.md5 does not exist, UpdateFile is a no-op: it only ever
// updates an existing checksum file; it doesn't create one from scratch.
func UpdateFile(dir, rel string) error {
	entries, existed, err := parseSumsFile(dir)
	if err != nil {
		return fmt.Errorf("reading %s: %w", filepath.Join(dir, SumsFilename), err)
	}
	if !existed {
		return nil
	}

	sum, err := hashFile(filepath.Join(dir, rel))
	if err != nil {
		return fmt.Errorf("hashing %s: %w", rel, err)
	}

	found := false
	for i := range entries {
		if entries[i].name == rel {
			entries[i].hash = sum
			found = true
			break
		}
	}
	if !found {
		entries = append(entries, sumEntry{hash: sum, name: rel})
	}

	return writeSumsEntries(dir, entries)
}

// ReadSums reads and parses a sums.md5-formatted file at dir/filename into
// a map of recorded filename -> hash. Works for sums.md5 itself
// (SumsFilename) and equally for a {target}.src.md5 sidecar, which shares
// the exact same line format under a different name (callers needing the
// latter should pass that filename directly rather than a new dedicated
// function existing for it).
//
// existed is false, with a nil map and nil error, when the file doesn't
// exist at all (this is not treated as an error, matching this package's
// other read primitives).
func ReadSums(dir, filename string) (sums map[string]string, existed bool, err error) {
	entries, existed, err := parseNamedSumsFile(dir, filename)
	if err != nil {
		return nil, false, fmt.Errorf("reading %s: %w", filepath.Join(dir, filename), err)
	}
	if !existed {
		return nil, false, nil
	}

	sums = make(map[string]string, len(entries))
	for _, e := range entries {
		sums[e.name] = e.hash
	}
	return sums, true, nil
}

// RenameFile updates dir/sums.md5 so the entry currently recorded under
// oldRel is recorded under newRel instead, with its hash left completely
// unchanged — the file's content didn't change, only its name did, so there
// is nothing to rehash (contrast with [UpdateFile], for when content did
// change).
//
// found reports whether an existing entry for oldRel was located and
// renamed. A false return (with a nil error) means sums.md5 existed but had
// no entry for oldRel, which most likely means it was already out of date
// before this call (callers may want to surface that as a warning).
//
// If dir/sums.md5 does not exist, RenameFile is a no-op and returns
// (false, nil).
func RenameFile(dir, oldRel, newRel string) (bool, error) {
	entries, existed, err := parseSumsFile(dir)
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", filepath.Join(dir, SumsFilename), err)
	}
	if !existed {
		return false, nil
	}

	found := false
	for i := range entries {
		if entries[i].name == oldRel {
			entries[i].name = newRel
			found = true
			break
		}
	}
	if !found {
		return false, nil
	}

	if err := writeSumsEntries(dir, entries); err != nil {
		return false, err
	}
	return true, nil
}

// RemoveFile deletes dir/rel's line from dir/sums.md5, if present, leaving
// every other line unchanged. No hashing is performed (removal never
// requires reading the file's contents).
//
// If dir/sums.md5 does not exist, or rel has no line in it, RemoveFile is a
// no-op.
func RemoveFile(dir, rel string) error {
	entries, existed, err := parseSumsFile(dir)
	if err != nil {
		return fmt.Errorf("reading %s: %w", filepath.Join(dir, SumsFilename), err)
	}
	if !existed {
		return nil
	}

	filtered := make([]sumEntry, 0, len(entries))
	changed := false
	for _, e := range entries {
		if e.name == rel {
			changed = true
			continue
		}
		filtered = append(filtered, e)
	}
	if !changed {
		return nil
	}

	return writeSumsEntries(dir, filtered)
}
