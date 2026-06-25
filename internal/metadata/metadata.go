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
)

var (
	audioExts = map[string]bool{".flac": true, ".mp3": true, ".m4a": true}
	textExts  = map[string]bool{".log": true, ".cue": true, ".m3u": true, ".m3u8": true, ".txt": true}
	imageExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true}
	scanExts  = map[string]bool{".tiff": true, ".tif": true}
)

// ScanLibrary walks the root path and identifies albums
func ScanLibrary(root string) ([]*Album, error) {
	var albums []*Album

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// We only care about directories that might be album roots
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

func processDirectory(path string) (*Album, bool) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, false
	}

	album := NewAlbum(path)
	hasAudio := false

	for _, entry := range entries {
		if entry.IsDir() {
			// Handle subdirectories (artwork, scans, extras)
			handleSubDir(album, path, entry.Name())
			continue
		}

		fullPath := filepath.Join(path, entry.Name())
		ext := strings.ToLower(filepath.Ext(fullPath))

		if audioExts[ext] {
			hasAudio = true
			album.Tracks = append(album.Tracks, &Track{Path: fullPath})
		} else if textExts[ext] || entry.Name() == "sums.md5" {
			album.Assets[CatRootText] = append(album.Assets[CatRootText], fullPath)
		} else if imageExts[ext] {
			// Primary art: folder.jpg/png stays in root, others go to artwork/
			if strings.HasPrefix(strings.ToLower(entry.Name()), "folder") {
				album.Assets[CatRootText] = append(album.Assets[CatRootText], fullPath)
			} else {
				album.Assets[CatArtwork] = append(album.Assets[CatArtwork], fullPath)
			}
		} else if scanExts[ext] {
			album.Assets[CatScan] = append(album.Assets[CatScan], fullPath)
		} else {
			album.Assets[CatUnknown] = append(album.Assets[CatUnknown], fullPath)
		}
	}

	return album, hasAudio
}

func handleSubDir(album *Album, root, dirName string) {
	subPath := filepath.Join(root, dirName)
	files, err := os.ReadDir(subPath)
	if err != nil {
		return
	}

	for _, f := range files {
		fullPath := filepath.Join(subPath, f.Name())
		// ext := strings.ToLower(filepath.Ext(fullPath))

		switch strings.ToLower(dirName) {
		case "artwork":
			album.Assets[CatArtwork] = append(album.Assets[CatArtwork], fullPath)
		case "scans":
			album.Assets[CatScan] = append(album.Assets[CatScan], fullPath)
		case "extras":
			album.Assets[CatUnknown] = append(album.Assets[CatUnknown], fullPath)
		default:
			album.Assets[CatUnknown] = append(album.Assets[CatUnknown], fullPath)
		}
	}
}

// ProcessLibrary finds albums, reads their tags, and resolves album-level metadata
func ProcessLibrary(root string) ([]*Album, error) {
	albums, err := ScanLibrary(root)
	if err != nil {
		return nil, err
	}

	reader := NewReader()

	for _, album := range albums {
		for _, track := range album.Tracks {
			if err := reader.ReadTrack(track); err != nil {
				// We log a warning but continue processing other tracks
				fmt.Printf("Warning: could not read tags for %s: %v\n", track.Path, err)
			}
		}

		// After reading all tracks, we can resolve the Album Artist if needed
		// (This would be used by the rename logic in the next phase)
		_ = album.ResolveAlbumArtist()
	}

	return albums, nil
}
