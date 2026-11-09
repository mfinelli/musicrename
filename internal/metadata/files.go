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
	"sort"
	"strings"
)

// AudioFiles returns the audio files directly inside dir, not those in its
// subdirectories, sorted by name.
func AudioFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}

	var paths []string
	for _, e := range entries {
		if !e.IsDir() && IsAudioExt(strings.ToLower(filepath.Ext(e.Name()))) {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// IsAlbumRoot reports whether dir directly contains at least one audio file,
// which is what makes a directory an album and not a library root, for the
// commands that take either.
func IsAlbumRoot(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if !e.IsDir() && IsAudioExt(strings.ToLower(filepath.Ext(e.Name()))) {
			return true, nil
		}
	}
	return false, nil
}

// AlbumFiles is one album found by LibraryAlbums: where it is and its audio
// files.
type AlbumFiles struct {
	// RootPath is the album's directory and Rel the same relative to the
	// library root, for display.
	RootPath string
	Rel      string

	// Paths are its audio files, sorted.
	Paths []string
}

// LibraryAlbums scans the library at root and returns its albums sorted by
// location, each with its audio files sorted. A library with no albums gives no
// albums and no error.
func LibraryAlbums(root string) ([]AlbumFiles, error) {
	albums, err := ScanLibrary(root)
	if err != nil {
		return nil, fmt.Errorf("scanning library: %w", err)
	}

	sort.Slice(albums, func(i, j int) bool {
		return albums[i].RootPath < albums[j].RootPath
	})

	out := make([]AlbumFiles, 0, len(albums))
	for _, album := range albums {
		rel, err := filepath.Rel(root, album.RootPath)
		if err != nil {
			rel = album.RootPath
		}

		paths := make([]string, 0, len(album.Tracks))
		for _, t := range album.Tracks {
			paths = append(paths, t.Path)
		}
		sort.Strings(paths)

		out = append(out, AlbumFiles{RootPath: album.RootPath, Rel: rel, Paths: paths})
	}
	return out, nil
}
