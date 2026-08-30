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
	"slices"
	"sort"
	"strings"

	"github.com/mfinelli/musicrename/internal/playlist"
	"github.com/mfinelli/musicrename/internal/target"
)

// DesiredEntry identifies one file that should exist on a target device, as
// a library root name plus a path relative to that root. The same (root,
// relative path) shape used throughout this project's sync design.
type DesiredEntry struct {
	// Root is the library root's name, e.g. "main".
	Root string
	// Rel is the file's path relative to Root, forward-slash separated.
	Rel string
}

// DesiredStateResult is the output of [DesiredState].
type DesiredStateResult struct {
	Entries  []DesiredEntry
	Warnings []string
}

// LibraryRoots enumerates libraryRootRoot's library roots: every direct
// subdirectory except the reserved "playlists" and "videos" names, and
// anything starting with "." (hidden directories are never library roots).
// There is deliberately no configuration for this beyond the two reserved
// names, matching how those names are already reserved everywhere else in
// this project.
func LibraryRoots(libraryRootRoot string) ([]string, error) {
	entries, err := os.ReadDir(libraryRootRoot)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", libraryRootRoot, err)
	}

	var roots []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "playlists" || name == "videos" || strings.HasPrefix(name, ".") {
			continue
		}
		roots = append(roots, name)
	}
	sort.Strings(roots)
	return roots, nil
}

// primaryArtFilenames are the exact filenames (case-insensitive) recognized
// as static primary album art — internal/metadata's CatPrimaryArt category:
// folder.jpg / folder.jpeg / folder.png. Intentionally excludes the animated
// forms (folder.webp, folder.mp4, CatPrimaryArtAnimated) since no sync target
// can use an animated file as external artwork, and this project's pure-Go
// resize (internal/artwork) only decodes JPEG/PNG anyway.
//
// This mirrors internal/metadata/scanner.go's categorization rule rather
// than importing internal/metadata to reuse it directly, since doing so
// would mean a full per-track tag-reading pass just to answer "does this
// album have art". A three-fixed-name set is small and stable enough that
// duplicating the match (not the whole scanning machinery) is the more
// pragmatic tradeoff.
var primaryArtFilenames = []string{"folder.jpg", "folder.jpeg", "folder.png"}

// findPrimaryArt looks for a static primary artwork file directly inside
// albumDir. found is false if none of primaryArtFilenames is present
// (including when albumDir doesn't exist at all); a real read error is
// returned as err. If more than one candidate is present (already a
// `check` finding of its own); an album should have at most one then the
// first match in primaryArtFilenames order is used; findPrimaryArt does not
// itself flag the duplicate.
func findPrimaryArt(albumDir string) (name string, found bool, err error) {
	entries, err := os.ReadDir(albumDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}

	present := make(map[string]string, len(entries)) // lowercase -> actual name
	for _, e := range entries {
		if !e.IsDir() {
			present[strings.ToLower(e.Name())] = e.Name()
		}
	}

	for _, candidate := range primaryArtFilenames {
		if actual, ok := present[candidate]; ok {
			return actual, true, nil
		}
	}
	return "", false, nil
}

// deviceRelFor returns the on-device relative path for a desired entry's
// source-relative path rel, given def (which may differ from rel itself
// whenever the on-device file's extension isn't the same as the source's).
//
// This exists because a desired entry's own Rel always reflects the
// *source* file (e.g., "01 track.flac"), not the target-specific result
// of producing it but two of this project's established behaviors mean the
// two can differ:
//
//   - An audio file whose source extension isn't in def's accepted formats
//     gets transcoded to def's TranscodeFormat, which very often has a
//     different extension (a FLAC source destined for `sdcard` becomes an
//     .mp3 on-device).
//   - Any primary artwork file always ends up as a JPEG on-device
//     (internal/artwork.Resize always outputs JPEG, even for an
//     already-fitting source and a PNG source is always converted), so
//     a "folder.png" source becomes "folder.jpg" on-device.
//
// Without translating between the two, nothing that compares a source-keyed
// desired entry against an on-device-keyed current entry  could ever match a
// transcoded or format-converted file, no matter how correctly it's already
// synced and so every such file would look permanently missing and,
// simultaneously, its real on-device file would look like an orphan to be
// deleted.
func deviceRelFor(rel string, def target.Definition) string {
	base := filepath.Base(rel)

	if isArtworkName(base) {
		// The stem is always effectively "folder" case-insensitively (that's
		// what isArtworkName just confirmed) so canonicalize the whole
		// filename, not just the extension, so a source named e.g.
		// "Folder.JPG" doesn't produce a different on-device filename than
		// one already named "folder.jpg" would.
		return filepath.ToSlash(filepath.Join(filepath.Dir(rel), "folder.jpg"))
	}

	ext := strings.ToLower(filepath.Ext(rel))
	if def.Accepts(ext) {
		return rel
	}

	params, ok := target.EncodeParamsFor(def.TranscodeFormat)
	if !ok {
		// Shouldn't happen for a valid Definition (every TranscodeFormat a
		// real target names has matching EncodeParams, enforced by
		// internal/target's tests) but fall back to the source name
		// rather than guessing or panicking.
		return rel
	}
	stem := strings.TrimSuffix(rel, filepath.Ext(rel))
	return stem + params.Ext
}

// isArtworkName reports whether name (a bare filename, not a path) is a
// primary artwork filename (case-insensitive).
func isArtworkName(name string) bool {
	lower := strings.ToLower(name)
	return slices.Contains(primaryArtFilenames, lower)
}

// DesiredState computes the full desired-state set for targetName: the union,
// across every library root ([LibraryRoots]), of every album's {target}.m3u8
// entries, plus every entry from a library-wide playlist whose #TARGETS:
// directive includes targetName or is absent entirely (membership implies
// selection). The same file referenced by both an album manifest and a
// global playlist appears exactly once in the result.
//
// For a target whose Definition doesn't embed artwork, the primary artwork
// file for any album with at least one selected track is added to the desired
// set too, so external-art targets actually receive a folder image alongside
// the tracks that need it. This is also what makes cleanup correct without
// any special-casing: an album with zero selected tracks contributes no
// artwork entry either, so if every track from a previously-synced album is
// later deselected, its on-device artwork simply stops appearing in the
// desired set on the next sync and gets removed by the ordinary "not in the
// desired set -> delete" rule along with everything else, no different from
// any other file. A target that embeds artwork (e.g., `sdcard`) never gets an
// external artwork entry at all, following the "no external file copied to
// that target" decision.
//
// An entry that doesn't resolve to an actual file under libraryRootRoot is
// skipped with a warning rather than included or failing the whole
// computation which is consistent with how per-file misses (a stale manifest
// entry, an unresolvable playlist entry) are already handled elsewhere in
// this project. A missing primary artwork file is not treated as a miss in
// that sense (it's simply not added) since `check` already owns flagging
// missing album art (§4.3) and this function isn't trying to duplicate that.
func DesiredState(libraryRootRoot, targetName string) (*DesiredStateResult, error) {
	def, ok := target.DefinitionFor(targetName)
	if !ok {
		return nil, fmt.Errorf("unknown target %q", targetName)
	}

	result := &DesiredStateResult{}
	seen := make(map[DesiredEntry]bool)

	add := func(root, rel string) {
		e := DesiredEntry{Root: root, Rel: filepath.ToSlash(rel)}
		if !seen[e] {
			seen[e] = true
			result.Entries = append(result.Entries, e)
		}
	}

	roots, err := LibraryRoots(libraryRootRoot)
	if err != nil {
		return nil, err
	}

	manifestName := playlist.ManifestFilename(targetName)
	for _, root := range roots {
		rootPath := filepath.Join(libraryRootRoot, root)
		err := filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || d.Name() != manifestName {
				return nil
			}

			albumDir := filepath.Dir(path)
			names, readErr := playlist.ReadManifest(albumDir, targetName)
			if readErr != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("reading %s: %v", path, readErr))
				return nil
			}

			for _, name := range names {
				full := filepath.Join(albumDir, name)
				if _, statErr := os.Stat(full); statErr != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf(
						"%s: entry %q does not resolve to a file", path, name,
					))
					continue
				}
				rel, relErr := filepath.Rel(rootPath, full)
				if relErr != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %v", path, relErr))
					continue
				}
				add(root, rel)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("scanning %s: %w", rootPath, err)
		}
	}

	err = playlist.WalkTree(libraryRootRoot, func(path string) error {
		targets, hasTargets, err := playlist.ReadTargets(path)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("reading %s: %v", path, err))
			return nil
		}
		if hasTargets && !containsString(targets, targetName) {
			return nil
		}

		entries, err := playlist.ReadEntries(path)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("reading %s: %v", path, err))
			return nil
		}

		for _, entry := range entries {
			full := filepath.Join(libraryRootRoot, entry)
			if _, statErr := os.Stat(full); statErr != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf(
					"%s: entry %q does not resolve to a file", path, entry,
				))
				continue
			}

			parts := strings.SplitN(filepath.ToSlash(entry), "/", 2)
			if len(parts) != 2 {
				result.Warnings = append(result.Warnings, fmt.Sprintf(
					"%s: entry %q is not root-qualified", path, entry,
				))
				continue
			}
			add(parts[0], parts[1])
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if !def.EmbedArt {
		type albumKey struct{ root, dir string }
		seenAlbums := make(map[albumKey]bool)
		var albumDirs []albumKey
		for _, e := range result.Entries {
			k := albumKey{root: e.Root, dir: filepath.Dir(e.Rel)}
			if !seenAlbums[k] {
				seenAlbums[k] = true
				albumDirs = append(albumDirs, k)
			}
		}

		for _, k := range albumDirs {
			albumPath := filepath.Join(libraryRootRoot, k.root, k.dir)
			artName, found, artErr := findPrimaryArt(albumPath)
			if artErr != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %v", albumPath, artErr))
				continue
			}
			if found {
				add(k.root, filepath.Join(k.dir, artName))
			}
		}
	}

	sort.Slice(result.Entries, func(i, j int) bool {
		if result.Entries[i].Root != result.Entries[j].Root {
			return result.Entries[i].Root < result.Entries[j].Root
		}
		return result.Entries[i].Rel < result.Entries[j].Rel
	})

	return result, nil
}

func containsString(s []string, v string) bool {
	return slices.Contains(s, v)
}
