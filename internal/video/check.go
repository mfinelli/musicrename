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
	"strings"

	"github.com/mfinelli/musicrename/internal/hasher"
)

// Warning represents a single finding discovered during a check.
type Warning struct {
	// Path is the video directory the finding relates to.
	Path string
	// Message describes the finding in human-readable form.
	Message string
}

// CheckResult is the output of a check run.
type CheckResult struct {
	Warnings []Warning
	// Checked is the number of video directories examined. Only populated
	// by CheckAll; a single Check call's caller already knows it checked
	// exactly one directory and has no need to read it back out.
	Checked int
}

// HasWarnings reports whether any findings were discovered.
func (r *CheckResult) HasWarnings() bool {
	return len(r.Warnings) > 0
}

// Check audits a single video directory. videoRoot enables path-conformance
// checking (comparing dir against what Add/Rename would compute from the
// nfo's current Artist/Title); pass "" to skip that check when no
// video-root is available, mirroring internal/checker's posture for a
// single audio file with no library-root context.
//
// Checks performed:
//   - dir contains exactly one recognized video file. More than one, or
//     none, is a finding. This is the same class of anomaly Scan (rename.go)
//     already treats as unplannable, so Check surfaces it too rather than
//     silently skipping the directory.
//   - musicvideo.nfo exists.
//   - musicvideo.nfo has a non-empty title and artist.
//   - (only if videoRoot != "", and only if the above all passed) dir
//     matches what Add/Rename would compute from the nfo.
//   - sums.md5 exists, and when it does, that its recorded entries match
//     the directory's current file listing (a pure listing comparison via
//     hasher.DiffEntries, performed without hashing anything; verifying the
//     checksums themselves is out of scope, use `md5sum -c sums.md5`).
func Check(dir, videoRoot string) (*CheckResult, error) {
	result := &CheckResult{}

	videoPath, soleErr := soleVideoFile(dir)
	if soleErr != nil {
		result.Warnings = append(result.Warnings, Warning{Path: dir, Message: soleErr.Error()})
	}

	nfo, err := ReadNFO(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		result.Warnings = append(result.Warnings, Warning{Path: dir, Message: "missing musicvideo.nfo"})
		nfo = nil
	case err != nil:
		return nil, fmt.Errorf("reading nfo in %s: %w", dir, err)
	}

	if nfo != nil {
		titleOK := strings.TrimSpace(nfo.Title) != ""
		artistOK := strings.TrimSpace(nfo.Artist) != ""
		if !titleOK {
			result.Warnings = append(result.Warnings, Warning{Path: dir, Message: "musicvideo.nfo missing title"})
		}
		if !artistOK {
			result.Warnings = append(result.Warnings, Warning{Path: dir, Message: "musicvideo.nfo missing artist"})
		}

		if videoRoot != "" && titleOK && artistOK && soleErr == nil {
			ext := strings.ToLower(filepath.Ext(videoPath))
			_, wantDir, _, derr := destination(videoRoot, nfo.Artist, nfo.Title, ext)
			if derr == nil && wantDir != dir {
				result.Warnings = append(result.Warnings, Warning{
					Path:    dir,
					Message: fmt.Sprintf("path does not match musicvideo.nfo (expected %s)", wantDir),
				})
			}
		}
	}

	if _, err := os.Stat(filepath.Join(dir, hasher.SumsFilename)); os.IsNotExist(err) {
		result.Warnings = append(result.Warnings, Warning{Path: dir, Message: "missing sums.md5"})
	} else {
		missingFromSums, missingOnDisk, derr := hasher.DiffEntries(dir, hasher.SumsFilename)
		if derr != nil {
			result.Warnings = append(result.Warnings, Warning{
				Path:    dir,
				Message: fmt.Sprintf("could not verify sums.md5 entries: %v", derr),
			})
		}
		for _, name := range missingFromSums {
			result.Warnings = append(result.Warnings, Warning{
				Path:    dir,
				Message: fmt.Sprintf("%s not recorded in sums.md5", name),
			})
		}
		for _, name := range missingOnDisk {
			result.Warnings = append(result.Warnings, Warning{
				Path:    dir,
				Message: fmt.Sprintf("sums.md5 references %q which does not exist", name),
			})
		}
	}

	return result, nil
}

// CheckAll walks videoRoot (via FindVideoDirs, so a directory with zero or
// more than one video file is still included and flagged rather than
// silently skipped) and runs Check on every video directory found,
// aggregating all warnings in directory order.
func CheckAll(videoRoot string) (*CheckResult, error) {
	dirs, err := FindVideoDirs(videoRoot)
	if err != nil {
		return nil, fmt.Errorf("scanning video root: %w", err)
	}

	result := &CheckResult{Checked: len(dirs)}
	for _, dir := range dirs {
		r, err := Check(dir, videoRoot)
		if err != nil {
			return nil, err
		}
		result.Warnings = append(result.Warnings, r.Warnings...)
	}
	return result, nil
}
