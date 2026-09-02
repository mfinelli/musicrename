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
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mfinelli/musicrename/internal/hasher"
)

// RenameMove describes one video, either needing to move from its current
// directory to a newly-computed one, or already correctly placed there.
type RenameMove struct {
	Bucket string
	Artist string
	Title  string

	OldDir string
	NewDir string

	OldVideoPath string
	NewVideoPath string

	HasInfoTxt bool
	// OldAudioPath/NewAudioPath are the derived audio file's paths,
	// empty if no derived audio file exists for this video.
	OldAudioPath string
	NewAudioPath string
	IsNoOp       bool
	// IsCaseOnly is true when NewDir differs from OldDir only in case,
	// relevant on case-insensitive filesystems (e.g. macOS's default
	// HFS+/APFS).
	IsCaseOnly bool
}

// RenamePlan is the result of scanning a video-root: the moves needed to
// bring every video's location in sync with its musicvideo.nfo, plus
// warnings for anything that couldn't be planned.
type RenamePlan struct {
	Moves    []RenameMove
	Warnings []string
}

// Scan walks videoRoot and builds a RenamePlan reconciling every video's
// current location with what Add would compute from its musicvideo.nfo's
// current Artist/Title.
//
// A directory containing a video file is treated as a leaf and never
// descended into further (video directories don't nest). A leaf missing a
// musicvideo.nfo, containing more than one video file, or whose nfo's
// Artist/Title no longer sanitize to a usable path, is skipped with a
// warning rather than aborting the scan (the same posture as Add toward
// videos it can't confidently place).
func Scan(videoRoot string) (*RenamePlan, error) {
	plan := &RenamePlan{}

	err := filepath.WalkDir(videoRoot, func(path string, d fs.DirEntry, err error) error {
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
			return nil // not a leaf video directory; keep walking downward
		}

		move, warning, err := planEntry(videoRoot, path)
		switch {
		case err != nil:
			return fmt.Errorf("planning %s: %w", path, err)
		case warning != "":
			plan.Warnings = append(plan.Warnings, warning)
		default:
			plan.Moves = append(plan.Moves, *move)
		}
		return filepath.SkipDir
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(plan.Moves, func(i, j int) bool {
		if plan.Moves[i].Artist != plan.Moves[j].Artist {
			return plan.Moves[i].Artist < plan.Moves[j].Artist
		}
		return plan.Moves[i].Title < plan.Moves[j].Title
	})
	sort.Strings(plan.Warnings)

	return plan, nil
}

// planEntry computes the RenameMove for the video directory at dir, or a
// warning string (with a nil move and nil error) if dir can't be planned.
func planEntry(videoRoot, dir string) (*RenameMove, string, error) {
	videoPath, err := soleVideoFile(dir)
	if err != nil {
		return nil, fmt.Sprintf("%s: %v", dir, err), nil
	}

	nfo, err := ReadNFO(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Sprintf("%s: no musicvideo.nfo, skipping", dir), nil
		}
		return nil, "", fmt.Errorf("reading nfo: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(videoPath))
	bucket, newDir, newVideoPath, err := destination(videoRoot, nfo.Artist, nfo.Title, ext)
	if err != nil {
		return nil, fmt.Sprintf("%s: %v", dir, err), nil
	}

	hasInfoTxt := false
	if _, err := os.Stat(filepath.Join(dir, "info.txt")); err == nil {
		hasInfoTxt = true
	}

	// A derived audio file, if one exists, needs its own new path
	// computed: same directory as the video, same stem as NewVideoPath
	// (title-derived, identically to the video itself), its own existing
	// extension carried forward unchanged. More than one existing derived
	// audio file is the same kind of ambiguous state soleVideoFile already
	// refuses to guess at above and so is skipped with a warning rather
	// than moving all of them or picking one arbitrarily, matching
	// ExtractAudio's posture toward the same condition.
	audioFiles, err := DerivedAudioFiles(videoPath)
	if err != nil {
		return nil, "", fmt.Errorf("finding derived audio for %s: %w", videoPath, err)
	}
	var oldAudioPath, newAudioPath string
	switch len(audioFiles) {
	case 0:
		// no derived audio to carry
	case 1:
		oldAudioPath = audioFiles[0]
		audioExt := filepath.Ext(oldAudioPath)
		newStem := strings.TrimSuffix(filepath.Base(newVideoPath), ext)
		newAudioPath = filepath.Join(newDir, newStem+audioExt)
	default:
		return nil, fmt.Sprintf("%s: multiple derived audio files, skipping", dir), nil
	}

	return &RenameMove{
		Bucket:       bucket,
		Artist:       nfo.Artist,
		Title:        nfo.Title,
		OldDir:       dir,
		NewDir:       newDir,
		OldVideoPath: videoPath,
		NewVideoPath: newVideoPath,
		HasInfoTxt:   hasInfoTxt,
		OldAudioPath: oldAudioPath,
		NewAudioPath: newAudioPath,
		IsNoOp:       dir == newDir,
		IsCaseOnly:   dir != newDir && strings.EqualFold(dir, newDir),
	}, "", nil
}

// soleVideoFile returns the single video file directly inside dir, erroring
// if there isn't exactly one (assume exactly one video per directory for now).
func soleVideoFile(dir string) (string, error) {
	files, err := videoFilesIn(dir)
	if err != nil {
		return "", err
	}

	switch len(files) {
	case 0:
		return "", fmt.Errorf("no video file found")
	case 1:
		return files[0], nil
	default:
		return "", fmt.Errorf("multiple video files found (expected exactly one)")
	}
}

// RenameResult describes the outcome of Execute.
type RenameResult struct {
	Warnings []string
}

// Execute performs the moves in plan, moving each video's file, its
// musicvideo.nfo, its info.txt (if present), its derived audio file, and its
// sums.md5/audio.src.md5 (if present) together as a unit. No-op moves are
// skipped. Case-only moves rename the whole directory via a temporary
// intermediate name rather than moving files individually, since on a
// case-insensitive filesystem OldDir and NewDir would otherwise collide;
// sums.md5/audio.src.md5 travel with the directory in that case and need no
// further update, since their filename entries are unaffected by a
// directory-only move.
//
// When a real (non-case-only) move also changes the video's filename
// (title-driven, so it can differ from the directory rename) and sums.md5
// exists, that one entry's filename is updated in place (the hash is left
// untouched, since the file's content didn't change) unless skipMD5 is
// true and the derived audio file's own sums.md5 entry, and audio.src.md5's
// single entry (keyed by the video's filename), are updated the same way,
// since both change under exactly the same title-driven condition as the
// video's filename. A sums.md5/audio.src.md5 that exists but has no entry for
// the old filename produces a warning rather than an error, since it most
// likely means the checksum file was already out of date.
//
// If progress is non-nil, it is called after each real (non-no-op) move.
// Now-empty source directories are removed afterward, bubbling upward but
// never removing or climbing above videoRoot.
func Execute(plan *RenamePlan, videoRoot string, skipMD5 bool, progress func(RenameMove)) (*RenameResult, error) {
	var warnings []string
	touchedDirs := make(map[string]struct{})

	for _, move := range plan.Moves {
		if move.IsNoOp {
			continue
		}

		if move.IsCaseOnly {
			if err := renameDirCaseInsensitive(move.OldDir, move.NewDir); err != nil {
				return nil, fmt.Errorf("renaming %s to %s: %w", move.OldDir, move.NewDir, err)
			}
			if progress != nil {
				progress(move)
			}
			continue
		}

		// Race condition check: something may have appeared at the
		// destination since planning.
		if _, err := os.Stat(move.NewDir); err == nil {
			warnings = append(warnings, fmt.Sprintf(
				"race condition: destination already exists at %s, skipping move", move.NewDir,
			))
			continue
		}

		if err := os.MkdirAll(move.NewDir, 0o755); err != nil {
			return nil, fmt.Errorf("creating %s: %w", move.NewDir, err)
		}

		if err := moveVideoFile(move.OldVideoPath, move.NewVideoPath); err != nil {
			return nil, fmt.Errorf("moving video: %w", err)
		}
		if err := moveVideoFile(nfoPath(move.OldDir), nfoPath(move.NewDir)); err != nil {
			return nil, fmt.Errorf("moving nfo: %w", err)
		}
		if move.HasInfoTxt {
			if err := moveVideoFile(
				filepath.Join(move.OldDir, "info.txt"),
				filepath.Join(move.NewDir, "info.txt"),
			); err != nil {
				return nil, fmt.Errorf("moving info.txt: %w", err)
			}
		}
		if move.OldAudioPath != "" {
			if err := moveVideoFile(move.OldAudioPath, move.NewAudioPath); err != nil {
				return nil, fmt.Errorf("moving derived audio: %w", err)
			}
		}

		// sums.md5, if present, travels with the directory like nfo/info.txt
		// above (moving it is not gated by skipMD5, since leaving it behind
		// would orphan it in a directory that's about to be cleaned up).
		sumsOld := filepath.Join(move.OldDir, hasher.SumsFilename)
		sumsNew := filepath.Join(move.NewDir, hasher.SumsFilename)
		hadSums := false
		if _, err := os.Stat(sumsOld); err == nil {
			hadSums = true
			if err := moveVideoFile(sumsOld, sumsNew); err != nil {
				return nil, fmt.Errorf("moving sums.md5: %w", err)
			}
		}

		// audio.src.md5, if present, travels with the directory the
		// same way (independently of whether a derived audio file
		// currently exists for this move, so an orphaned sidecar
		// still moves along rather than getting left behind).
		audioSumsOld := filepath.Join(move.OldDir, AudioSrcSumsFilename)
		audioSumsNew := filepath.Join(move.NewDir, AudioSrcSumsFilename)
		hadAudioSums := false
		if _, err := os.Stat(audioSumsOld); err == nil {
			hadAudioSums = true
			if err := moveVideoFile(audioSumsOld, audioSumsNew); err != nil {
				return nil, fmt.Errorf("moving %s: %w", AudioSrcSumsFilename, err)
			}
		}

		// The video's filename is title-derived and so can change
		// independently of the directory move (an artist-only change moves
		// the directory but leaves the title-derived filename itself
		// unchanged). If it did change, and sums.md5 exists, update that
		// one entry in place and the derived audio file's sums.md5
		// entry the same way, since its filename is driven by the
		// identical title-derived stem as the video's. audio.src.md5's
		// single entry (keyed by the video's filename, not the audio's) is
		// renamed the same way, via a small local helper since
		// hasher.RenameFile is hardcoded to sums.md5 specifically rather
		// than parameterized by filename the way ReadSums/WriteSums are.
		oldBase := filepath.Base(move.OldVideoPath)
		newBase := filepath.Base(move.NewVideoPath)
		if oldBase != newBase && !skipMD5 {
			if hadSums {
				found, err := hasher.RenameFile(move.NewDir, oldBase, newBase)
				if err != nil {
					warnings = append(warnings, fmt.Sprintf(
						"updating sums.md5 for %s: %v", newBase, err,
					))
				} else if !found {
					warnings = append(warnings, fmt.Sprintf(
						"sums.md5 exists but has no entry for %s; leaving as-is", oldBase,
					))
				}

				if move.OldAudioPath != "" {
					oldAudioBase := filepath.Base(move.OldAudioPath)
					newAudioBase := filepath.Base(move.NewAudioPath)
					found, err := hasher.RenameFile(move.NewDir, oldAudioBase, newAudioBase)
					if err != nil {
						warnings = append(warnings, fmt.Sprintf(
							"updating sums.md5 for %s: %v", newAudioBase, err,
						))
					} else if !found {
						warnings = append(warnings, fmt.Sprintf(
							"sums.md5 exists but has no entry for %s; leaving as-is", oldAudioBase,
						))
					}
				}
			}

			if hadAudioSums {
				found, err := renameNamedSumsEntry(move.NewDir, AudioSrcSumsFilename, oldBase, newBase)
				if err != nil {
					warnings = append(warnings, fmt.Sprintf(
						"updating %s: %v", AudioSrcSumsFilename, err,
					))
				} else if !found {
					warnings = append(warnings, fmt.Sprintf(
						"%s exists but has no entry for %s; leaving as-is", AudioSrcSumsFilename, oldBase,
					))
				}
			}
		}

		touchedDirs[move.OldDir] = struct{}{}

		if progress != nil {
			progress(move)
		}
	}

	for dir := range touchedDirs {
		cleanupEmptyDirs(dir, videoRoot)
	}

	return &RenameResult{Warnings: warnings}, nil
}

// renameNamedSumsEntry is [hasher.RenameFile] generalized to an arbitrary
// sums.md5-formatted filename within dir (needed here because
// hasher.RenameFile is hardcoded to sums.md5 specifically, unlike
// hasher.ReadSums/WriteSums, which are already parameterized by filename).
// Kept local to this package rather than added to internal/hasher's public
// API, since audio.src.md5 is currently the only named sidecar that ever
// needs a single-entry rename.
//
// Mirrors hasher.RenameFile's exact contract: found reports whether an
// existing entry for oldRel was located and renamed (false with a nil error
// if the file existed but had no such entry, or didn't exist at all).
func renameNamedSumsEntry(dir, filename, oldRel, newRel string) (found bool, err error) {
	sums, existed, err := hasher.ReadSums(dir, filename)
	if err != nil {
		return false, err
	}
	if !existed {
		return false, nil
	}

	hash, ok := sums[oldRel]
	if !ok {
		return false, nil
	}

	delete(sums, oldRel)
	sums[newRel] = hash
	if err := hasher.WriteSums(dir, filename, sums); err != nil {
		return false, err
	}
	return true, nil
}

// renameDirCaseInsensitive moves oldDir to newDir via a temporary
// intermediate name. This intentionally duplicates internal/executor's
// analogous moveCaseInsensitive rather than depending on it (see add.go's
// moveVideoFile doc comment for why internal/video keeps its own small
// filesystem helpers instead of depending on the audio-shaped executor
// package).
func renameDirCaseInsensitive(oldDir, newDir string) error {
	parent := filepath.Dir(newDir)
	tmpPath := filepath.Join(parent, fmt.Sprintf(
		".musicrename-tmp-%d", time.Now().UnixNano(),
	))
	if err := os.Rename(oldDir, tmpPath); err != nil {
		return err
	}
	return os.Rename(tmpPath, newDir)
}

// cleanupEmptyDirs removes dir if empty, then walks upward removing each
// now-empty ancestor until it reaches videoRoot, hits a non-empty directory,
// or hits a filesystem error. Best-effort: an error (typically "directory
// not empty", e.g. because a sibling video still occupies an ancestor
// artist directory) simply stops the climb rather than being treated as a
// failure.
func cleanupEmptyDirs(dir, videoRoot string) {
	for {
		if dir == videoRoot {
			return
		}
		if dir == filepath.Dir(dir) {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}
