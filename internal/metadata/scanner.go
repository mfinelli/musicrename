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

package metadata

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mfinelli/musicrename/internal/hasher"
)

var (
	// audioExts is the set of file extensions treated as audio tracks.
	audioExts = map[string]bool{".flac": true, ".mp3": true, ".m4a": true}

	// textExts is the set of file extensions treated as plain-text metadata
	// files that live at the album root (e.g. ripping logs, cue sheets).
	textExts = map[string]bool{".log": true, ".cue": true, ".m3u": true, ".m3u8": true, ".txt": true}

	// imageExts is the set of file extensions recognised as image files.
	// Whether a specific image is primary art or supplementary artwork is
	// determined by its filename, not just its extension.
	imageExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true}

	// scanExts is the set of file extensions treated as high-resolution scans,
	// typically stored in the scans/ subdirectory.
	scanExts = map[string]bool{".tiff": true, ".tif": true}
)

// categorizeRootFile determines the category of a file at the album root level.
// It is extracted as a named function so that both processDirectory and tests
// share the same logic rather than duplicating it.
func categorizeRootFile(name string) FileCategory {
	lower := strings.ToLower(name)
	ext := filepath.Ext(lower)

	if audioExts[ext] {
		return CatAudio
	}
	if textExts[ext] || name == hasher.SumsFilename {
		return CatRootText
	}
	if imageExts[ext] {
		// Only the exact filenames folder.jpg / folder.jpeg / folder.png are
		// treated as primary album art; everything else is supplementary artwork.
		if lower == "folder.jpg" || lower == "folder.jpeg" || lower == "folder.png" {
			return CatPrimaryArt
		}
		return CatArtwork
	}
	if scanExts[ext] {
		return CatScan
	}
	return CatUnknown
}

// ScanLibrary walks the root path and identifies albums.
func ScanLibrary(root string) ([]*Album, error) {
	var albums []*Album

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			album, isAlbum := processDirectory(path)
			if isAlbum {
				albums = append(albums, album)
			}
		}
		return nil
	})

	return albums, err
}

// processDirectory inspects path and returns an Album populated with the files
// it contains. Subdirectories named artwork, scans, and extras are descended
// into and their contents categorised accordingly; all other subdirectories are
// treated as unknown. The second return value reports whether any audio files
// were found (directories without audio are not considered album roots).
func processDirectory(path string) (*Album, bool) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, false
	}

	album := NewAlbum(path)
	hasAudio := false

	for _, entry := range entries {
		if entry.IsDir() {
			handleSubDir(album, path, entry.Name())
			continue
		}

		fullPath := filepath.Join(path, entry.Name())
		cat := categorizeRootFile(entry.Name())

		if cat == CatAudio {
			hasAudio = true
			album.Tracks = append(album.Tracks, &Track{Path: fullPath})
		} else {
			album.Assets[cat] = append(album.Assets[cat], fullPath)
		}
	}

	return album, hasAudio
}

// handleSubDir classifies the immediate files inside a known album
// subdirectory (artwork/, scans/, extras/) and appends their absolute paths to
// album.Assets under the appropriate category. Nested subdirectories are
// silently skipped; only regular files are processed.
func handleSubDir(album *Album, root, dirName string) {
	subPath := filepath.Join(root, dirName)
	files, err := os.ReadDir(subPath)
	if err != nil {
		return
	}

	for _, f := range files {
		// Skip any nested subdirectories; we only process regular files.
		if f.IsDir() {
			continue
		}

		fullPath := filepath.Join(subPath, f.Name())

		switch strings.ToLower(dirName) {
		case "artwork":
			album.Assets[CatArtwork] = append(album.Assets[CatArtwork], fullPath)
		case "scans":
			album.Assets[CatScan] = append(album.Assets[CatScan], fullPath)
		case "extras":
			album.Assets[CatExtras] = append(album.Assets[CatExtras], fullPath)
		default:
			album.Assets[CatUnknown] = append(album.Assets[CatUnknown], fullPath)
		}
	}
}

// ProcessLibrary finds albums, reads their tags, and resolves album-level
// metadata. After this call, each Album's ResolvedArtist field is populated.
// Non-fatal issues (unreadable tracks, unresolvable artists) are appended to
// Album.Warnings rather than printed, so the caller can surface them alongside
// the plan rather than interleaved with scan progress.
//
// Note: if ResolvedArtist is empty the album is still returned. The planner
// will error on it and its warnings will not reach the display layer; the
// planner's error message is sufficient in that case.
func ProcessLibrary(root string) ([]*Album, error) {
	albums, err := ScanLibrary(root)
	if err != nil {
		return nil, err
	}

	reader := NewReader()

	for _, album := range albums {
		for _, track := range album.Tracks {
			if err := reader.ReadTrack(track); err != nil {
				album.Warnings = append(album.Warnings,
					fmt.Sprintf("could not read tags for %s: %v", track.Path, err))
			}
		}

		album.ResolvedArtist = album.ResolveAlbumArtist()
		if album.ResolvedArtist == "" {
			album.Warnings = append(album.Warnings,
				fmt.Sprintf("could not resolve artist for album at %s; it will be skipped", album.RootPath))
		}
	}

	return albums, nil
}
