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
	"path/filepath"
	"sort"
	"strings"

	"github.com/mfinelli/musicrename/internal/hasher"
	"github.com/mfinelli/musicrename/internal/target"
)

// ChangeAction is what needs to happen to bring one device entry in line
// with desired state.
type ChangeAction int

const (
	// ActionSkip means the entry is already correct on the device;
	// nothing needs to happen.
	ActionSkip ChangeAction = iota
	// ActionAdd means the entry is desired but not yet on the device.
	ActionAdd
	// ActionRegenerate means the entry is on the device but needs to be
	// recreated which is either  a real detected change, or there is no
	// way to verify one way or the other at all (i.e., missing sums.md5).
	ActionRegenerate
	// ActionDelete means the entry is on the device but no longer
	// desired.
	ActionDelete
)

// String renders a ChangeAction as a lowercase word, for logging/display.
func (a ChangeAction) String() string {
	switch a {
	case ActionSkip:
		return "skip"
	case ActionAdd:
		return "add"
	case ActionRegenerate:
		return "regenerate"
	case ActionDelete:
		return "delete"
	default:
		return "unknown"
	}
}

// PlannedChange is one entry's outcome from [Diff].
type PlannedChange struct {
	Entry  DesiredEntry
	Action ChangeAction
}

// DiffResult is the output of [Diff].
type DiffResult struct {
	Changes []PlannedChange
	// Warnings does not include anything already reported by whatever
	// produced desired/current (Diff only ever appends its own new
	// warnings here, specifically the "no sums.md5 recorded" case).
	// A caller presenting a complete picture to the user should
	// concatenate desired.Warnings, current.Warnings, and this.
	Warnings []string
}

// Diff compares desired and current state for targetName and decides what
// needs to happen to each entry: add anything desired but missing from the
// device, delete anything on the device no longer desired, and for anything
// present on both sides, either skip it (confirmed unchanged) or regenerate
// it.
//
// An entry counts as unchanged if *either* of two independent checks
// succeeds:
//
//   - Its on-device sums.md5 hash equals the source's current sums.md5
//     hash directly (true for an ordinary passthrough file, but also true
//     for a derived file whose last regeneration happened to produce bytes
//     identical to source. Note that a JPEG artwork file already within a
//     target's dimension limit is returned by internal/artwork.Resize
//     completely unchanged, so an artwork entry can perfectly well satisfy
//     this check too, not just an audio one.
//   - Its {target}.src.md5 sidecar's recorded source hash equals the
//     source's current sums.md5 hash which is the case for a file whose last
//     regeneration actually transformed the bytes (a real transcode, or a
//     real resize/format conversion), so the first check can never succeed
//     for it no matter how unchanged the source actually is.
//
// This intentionally does not first classify an entry as "passthrough" or
// "derived" from a static rule (an accepted audio format vs. everything
// else). Trying both checks and accepting either one sidesteps needing to
// predict in advance which one a given entry "should" use instead it just
// asks the question that's actually relevant for both: is there *any*
// recorded hash that still matches the source? A transformed file's on-device
// hash can never coincidentally equal source's raw hash (different bytes by
// construction: a resize changes dimensions, a transcode changes format
// entirely), so there's no risk of the first check masking a real change for
// that kind of file.
//
// If *neither* check can even be attempted because the source album has no
// sums.md5, or has one but no entry for this specific file (e.g. added
// since the last real `sums` run) then the entry is treated exactly like a
// detected mismatch: regenerate, never skip. This case gets its own distinct
// warning, since (unlike an ordinary regenerate) it's actionable: it
// means `sums` was never run for that album.
//
// For a target that embeds artwork (currently `sdcard`), an audio entry that
// passes the checks above is *additionally* required to match on its album's
// artwork: the recorded artwork hash from current.AlbumArtHash (read from the
// {target}.src.md5 sidecar's entry for the artwork filename, e.g.,
// "folder.jpg", only ever populated for an embedding target) must equal the
// artwork's current source hash ([findArtworkHash] against the same cached
// per-album source sums used for the audio comparison, so no extra I/O).
// If they differ (including the case where one side has artwork recorded
// and the other doesn't) the entry regenerates even though the audio
// itself is unchanged, since embedding is what makes the artwork part of
// the file at all: without this, an artwork-only change would be
// completely invisible to a diff that only ever compared each track's
// audio hash. This check is skipped entirely (both for cost and because it
// doesn't apply) whenever the audio comparison above already failed (there's
// no reason to also check artwork when the entry's going to regenerate
// regardless) and for a non-embedding target, where it never runs at all.
//
// current.Entries is looked up by [deviceRelFor]'s translation of each
// desired entry, not the entry itself (a desired entry's Rel always
// reflects its *source* file, but the on-device file it corresponds to can
// have a different extension like a transcoded audio file, or artwork that
// went through a PNG-to-JPEG conversion). Comparing untranslated would mean
// a correctly-synced transcoded file could never be recognized as present:
// it would look permanently missing (added again) while its real,
// already-correct on-device file looks orphaned (deleted) on every
// single sync. PlannedChange.Entry itself stays source-keyed regardless
// (callers locating the source file need that), only the lookup key
// changes.
func Diff(
	libraryRootRoot, targetName string, desired *DesiredStateResult, current *CurrentStateResult,
) (*DiffResult, error) {
	def, ok := target.DefinitionFor(targetName)
	if !ok {
		return nil, fmt.Errorf("unknown target %q", targetName)
	}

	result := &DiffResult{}
	// Keyed by the translated on-device entry ([deviceRelFor]), not the
	// raw desired entry.
	desiredDeviceSet := make(map[DesiredEntry]bool, len(desired.Entries))

	// album directory -> (filename -> source hash), read at most once per
	// album regardless of how many of its files are desired.
	sourceHashCache := make(map[string]map[string]string)

	sourceHashFor := func(entry DesiredEntry) (hash string, found bool, err error) {
		sourcePath := filepath.Join(libraryRootRoot, entry.Root, entry.Rel)
		albumDir := filepath.Dir(sourcePath)
		name := filepath.Base(sourcePath)

		sums, cached := sourceHashCache[albumDir]
		if !cached {
			sums, _, err = hasher.ReadSums(albumDir, hasher.SumsFilename)
			if err != nil {
				return "", false, err
			}
			sourceHashCache[albumDir] = sums // nil is a valid, meaningful cached value
		}
		if sums == nil {
			return "", false, nil
		}
		hash, found = sums[name]
		return hash, found, nil
	}

	for _, entry := range desired.Entries {
		// current.Entries is keyed by whatever's actually on the device,
		// which is not always entry itself: a transcoded audio file or a
		// format-converted artwork file ends up under a different
		// extension on-device than its source has. Comparing against the
		// untranslated entry would mean a correctly-synced transcoded
		// file could never match resulting in permanently looking both
		// missing (so it gets added again) and orphaned (so its real,
		// correct on-device file gets deleted) on every single sync.
		deviceEntry := DesiredEntry{Root: entry.Root, Rel: deviceRelFor(entry.Rel, def)}
		desiredDeviceSet[deviceEntry] = true

		dev, onDevice := current.Entries[deviceEntry]
		if !onDevice {
			result.Changes = append(result.Changes, PlannedChange{Entry: entry, Action: ActionAdd})
			continue
		}

		srcHash, srcFound, err := sourceHashFor(entry)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				"%s/%s: reading source sums.md5: %v", entry.Root, entry.Rel, err,
			))
			result.Changes = append(result.Changes, PlannedChange{Entry: entry, Action: ActionRegenerate})
			continue
		}
		if !srcFound {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				"no sums.md5 recorded for %s/%s; will be recopied (retranscoded, if this "+
					"target transcodes it) on every sync until you run 'musicrename sums'",
				entry.Root, entry.Rel,
			))
			result.Changes = append(result.Changes, PlannedChange{Entry: entry, Action: ActionRegenerate})
			continue
		}

		unchanged := dev.Hash == srcHash || (dev.HasSrcHash && dev.SrcHash == srcHash)

		if unchanged && def.EmbedArt {
			albumDir := filepath.Dir(filepath.Join(libraryRootRoot, entry.Root, entry.Rel))
			albumKey := AlbumKey{Root: entry.Root, Dir: filepath.ToSlash(filepath.Dir(entry.Rel))}

			currentArt, _ := findArtworkHash(sourceHashCache[albumDir])
			recordedArt := current.AlbumArtHash[albumKey]
			unchanged = recordedArt == currentArt
		}

		action := ActionRegenerate
		if unchanged {
			action = ActionSkip
		}
		result.Changes = append(result.Changes, PlannedChange{Entry: entry, Action: action})
	}

	for entry := range current.Entries {
		if !desiredDeviceSet[entry] {
			result.Changes = append(result.Changes, PlannedChange{Entry: entry, Action: ActionDelete})
		}
	}

	sort.Slice(result.Changes, func(i, j int) bool {
		if result.Changes[i].Entry.Root != result.Changes[j].Entry.Root {
			return result.Changes[i].Entry.Root < result.Changes[j].Entry.Root
		}
		return result.Changes[i].Entry.Rel < result.Changes[j].Entry.Rel
	})

	return result, nil
}

// findArtworkHash looks up the recorded hash of whichever primary artwork
// file is present in sums (a filename -> hash map, either a parsed sums.md5
// or a parsed {target}.src.md5, so this is a pure lookup, no extra I/O beyond
// whatever already read that map). Matching is case-insensitive, with a
// deterministic first-match-in-primaryArtFilenames-order tie-break for
// the rare case where more than one candidate exists (which is already its
// own `check` finding) mirroring [findPrimaryArt]'s exact same matching and
// tie-break rules, just operating on an already-parsed map instead of a
// directory listing. A nil sums map (the file didn't exist at all) is handled
// the same as an empty one: not found, no error.
func findArtworkHash(sums map[string]string) (hash string, found bool) {
	lowerSums := make(map[string]string, len(sums))
	for name, h := range sums {
		lowerSums[strings.ToLower(name)] = h
	}
	for _, artName := range primaryArtFilenames {
		if h, ok := lowerSums[artName]; ok {
			return h, true
		}
	}
	return "", false
}
