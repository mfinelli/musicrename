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
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EditInput carries the final (already-resolved) field values for Edit.
// Artist and Title are required; Album and Year are optional (an empty
// value means "no value" and is excluded from the written nfo, same as Add).
type EditInput struct {
	Artist string
	Title  string
	Album  string
	Year   string
}

// EditResult describes the outcome of a successful Edit.
type EditResult struct {
	NFOPath string
	// Created is true when dir had no musicvideo.nfo before this call (Edit
	// wrote a fresh one), and false when an existing one was overwritten.
	Created bool
}

// nfoPath returns the musicvideo.nfo path within dir.
func nfoPath(dir string) string {
	return filepath.Join(dir, NFOFilename)
}

// ReadNFO reads and decodes the musicvideo.nfo sidecar in dir. Used by Edit
// to seed prompts with current values, and available for reuse by
// check/inspect/rename. Returns a wrapped fs.ErrNotExist-compatible error
// (via errors.Is) when dir has no musicvideo.nfo (callers such as Edit's
// cmd-layer caller that want to treat "missing" as "nothing to prefill
// prompts with" rather than a hard failure can check for that specifically).
func ReadNFO(dir string) (*NFO, error) {
	path := nfoPath(dir)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var nfo NFO
	if err := xml.Unmarshal(data, &nfo); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", path, err)
	}
	return &nfo, nil
}

// Edit writes the musicvideo.nfo sidecar in dir with in's values, creating
// it if it doesn't already exist (e.g. a video that was never run through
// Add, or one whose nfo was deleted) or overwriting it if it does.
//
// dir must contain at least one recognized video file; this is a sanity check
// against writing an orphaned nfo into the wrong directory, not a requirement
// that Add was previously run.
//
// Edit does not move the video, rename it, or touch dir itself: if Artist or
// Title changes (or is being set for the first time), dir will no longer
// match what Add/Rename would compute for the new values. Run
// "video rename" afterward to reconcile the location.
func Edit(dir string, in EditInput) (*EditResult, error) {
	artist := strings.TrimSpace(in.Artist)
	title := strings.TrimSpace(in.Title)
	if artist == "" {
		return nil, fmt.Errorf("artist is required")
	}
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}

	hasVideo, err := dirHasVideoFile(dir)
	if err != nil {
		return nil, fmt.Errorf("checking %s: %w", dir, err)
	}
	if !hasVideo {
		return nil, fmt.Errorf("no video file found in %s", dir)
	}

	path := nfoPath(dir)
	created := true
	if _, err := os.Stat(path); err == nil {
		created = false
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("checking %s: %w", path, err)
	}

	nfo := NFO{
		Title:  title,
		Artist: artist,
		Album:  strings.TrimSpace(in.Album),
		Year:   strings.TrimSpace(in.Year),
	}
	if err := writeNFO(path, nfo); err != nil {
		return nil, fmt.Errorf("writing nfo: %w", err)
	}

	return &EditResult{NFOPath: path, Created: created}, nil
}

// dirHasVideoFile reports whether dir directly contains at least one file
// with a recognized video extension.
func dirHasVideoFile(dir string) (bool, error) {
	files, err := videoFilesIn(dir)
	if err != nil {
		return false, err
	}
	return len(files) > 0, nil
}
