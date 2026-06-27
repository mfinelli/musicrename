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

// Package checker audits a music library for misconfigurations and deviations
// from the naming and metadata conventions enforced by the rename command.
//
// The three entry points cover the three modes exposed by the cobra command:
//
//   - [CheckLibrary] – the target is a library root (no audio files directly
//     inside). All album directories are discovered recursively and the full
//     check suite runs on each, including path-conformance checks.
//   - [CheckAlbum] – the target is a single album directory (directly contains
//     audio files). All checks run; path-conformance checks require a non-empty
//     libraryRoot argument.
//   - [CheckTrack] – the target is a single audio file. Only per-track checks
//     run; directory-level checks (artwork, sums.md5, unknown files,
//     path conformance) are skipped because album context is unavailable.
//
// All three functions return a [Result] that groups [Warning] values by album
// directory. The cobra command is responsible for presenting the warnings to
// the user; this package only collects them.
package checker

import (
	"fmt"
	"os"
	"path/filepath"

	"go.senan.xyz/taglib"

	"github.com/mfinelli/musicrename/internal/hasher"
	"github.com/mfinelli/musicrename/internal/metadata"
	"github.com/mfinelli/musicrename/internal/planner"
)

// Warning represents a single finding discovered during a check run.
type Warning struct {
	// Path is the absolute path of the file or directory the finding
	// relates to. For album-level findings this is the album root directory;
	// for track-level findings it is the audio file path.
	Path string
	// Message describes the finding in human-readable form.
	Message string
}

// AlbumResult groups all warnings discovered for a single album directory.
type AlbumResult struct {
	// AlbumPath is the absolute path to the album directory.
	AlbumPath string
	// Warnings holds all findings for this album. Track-level warnings use
	// the audio file path; album-level warnings use the album directory path.
	Warnings []Warning
}

// Result is the complete output of a check run.
type Result struct {
	// Albums contains one entry per album directory processed during the run,
	// in the order they were discovered.
	Albums []AlbumResult
}

// HasWarnings reports whether any findings were discovered across all albums.
// The cobra command uses this to decide the process exit code.
func (r *Result) HasWarnings() bool {
	for _, a := range r.Albums {
		if len(a.Warnings) > 0 {
			return true
		}
	}
	return false
}

// CheckLibrary scans root as a library root directory, discovers all album
// directories recursively, and runs the full check suite on each. Because the
// library root is known, path-conformance checks (album directory path and
// per-file destination paths) are performed in addition to all other checks.
func CheckLibrary(root string) (*Result, error) {
	albums, err := metadata.ProcessLibrary(root)
	if err != nil {
		return nil, fmt.Errorf("scanning library at %s: %w", root, err)
	}

	result := &Result{}
	for _, album := range albums {
		ar, err := checkAlbum(album, root)
		if err != nil {
			return nil, err
		}
		result.Albums = append(result.Albums, *ar)
	}
	return result, nil
}

// CheckAlbum runs the full check suite on the album directory at albumPath.
// albumPath must directly contain audio files; if it does not, an error is
// returned. When libraryRoot is non-empty, path-conformance checks are
// performed (album directory path and per-file destination paths). When
// libraryRoot is empty, path-conformance is skipped.
func CheckAlbum(albumPath, libraryRoot string) (*AlbumResult, error) {
	albums, err := metadata.ProcessLibrary(albumPath)
	if err != nil {
		return nil, fmt.Errorf("scanning album at %s: %w", albumPath, err)
	}

	// ProcessLibrary may discover nested albums if sub-directories contain
	// audio files. We only want the album rooted at albumPath itself.
	var album *metadata.Album
	for _, a := range albums {
		if a.RootPath == albumPath {
			album = a
			break
		}
	}
	if album == nil {
		return nil, fmt.Errorf("no audio files found directly in %s", albumPath)
	}

	return checkAlbum(album, libraryRoot)
}

// CheckTrack runs track-level checks only on the single audio file at
// filePath. Directory-level checks (artwork, sums.md5, unknown files, path
// conformance) are skipped because album context is not available for a
// single-file invocation.
func CheckTrack(filePath string) (*AlbumResult, error) {
	reader := metadata.NewReader()
	track := &metadata.Track{Path: filePath}
	if err := reader.ReadTrack(track); err != nil {
		return nil, fmt.Errorf("reading track %s: %w", filePath, err)
	}

	ar := &AlbumResult{AlbumPath: filepath.Dir(filePath)}
	checkTrackTags(track, ar)
	checkTrackAudio(track, ar)
	checkTrackFilename(track, ar)
	return ar, nil
}

// checkAlbum is the internal implementation shared by CheckLibrary and
// CheckAlbum. It runs every check category for a single album.
func checkAlbum(album *metadata.Album, libraryRoot string) (*AlbumResult, error) {
	ar := &AlbumResult{AlbumPath: album.RootPath}

	for _, track := range album.Tracks {
		checkTrackTags(track, ar)
		checkTrackAudio(track, ar)
	}

	checkAlbumTags(album, ar)
	checkArtwork(album, ar)
	checkIntegrity(album, ar)
	checkUnknownFiles(album, ar)
	checkNaming(album, libraryRoot, ar)

	return ar, nil
}

// checkTrackTags emits warnings for missing per-track metadata tags. It is
// called for every audio file regardless of the invocation mode.
func checkTrackTags(track *metadata.Track, ar *AlbumResult) {
	if track.Title == "" {
		ar.Warnings = append(ar.Warnings, Warning{
			Path:    track.Path,
			Message: "missing TITLE tag",
		})
	}
	if track.TrackNumber == nil {
		ar.Warnings = append(ar.Warnings, Warning{
			Path:    track.Path,
			Message: "missing TRACKNUMBER tag",
		})
	}
	if track.Year == "" {
		ar.Warnings = append(ar.Warnings, Warning{
			Path:    track.Path,
			Message: "missing DATE tag",
		})
	}
	if track.Artist == "" && track.AlbumArtist == "" {
		ar.Warnings = append(ar.Warnings, Warning{
			Path:    track.Path,
			Message: "missing both ARTIST and ALBUMARTIST tags",
		})
	}
}

// checkTrackAudio performs a second read-only pass over each audio file to
// check for tags and properties not captured by the primary metadata scan.
//
// Specifically it checks for:
//   - missing REPLAYGAIN_TRACK_GAIN tag
//   - missing REPLAYGAIN_ALBUM_GAIN tag
//   - embedded artwork (detected via Properties().Images)
//
// This is a deliberate design choice: metadata.Track stays focused on the
// fields needed for path planning. Checker-specific audio attributes are
// read here in a separate pass rather than expanding the shared data model.
// The WASM call is read-only and inexpensive relative to the overall check run.
func checkTrackAudio(track *metadata.Track, ar *AlbumResult) {
	file, err := taglib.OpenReadOnly(track.Path)
	if err != nil {
		// The primary scan phase already records a warning for unreadable
		// files; skip silently here to avoid duplicating it.
		return
	}
	defer file.Close()

	if tags := file.Tags(); tags != nil {
		if len(tags["REPLAYGAIN_TRACK_GAIN"]) == 0 {
			ar.Warnings = append(ar.Warnings, Warning{
				Path:    track.Path,
				Message: "missing REPLAYGAIN_TRACK_GAIN tag",
			})
		}
		if len(tags["REPLAYGAIN_ALBUM_GAIN"]) == 0 {
			ar.Warnings = append(ar.Warnings, Warning{
				Path:    track.Path,
				Message: "missing REPLAYGAIN_ALBUM_GAIN tag",
			})
		}
	}

	if props := file.Properties(); len(props.Images) > 0 {
		ar.Warnings = append(ar.Warnings, Warning{
			Path:    track.Path,
			Message: fmt.Sprintf("embedded artwork detected (%d image(s))", len(props.Images)),
		})
	}
}

// checkTrackFilename warns when the file's current basename does not match
// what the rename planner would produce from its tags. This check runs in
// track mode where no library root is available, so only the filename
// component (not the full path) is compared.
//
// A synthetic library root is passed to PlanAlbum so that expected paths can
// be computed; only the resulting basename is inspected, so the root value
// does not affect the outcome.
func checkTrackFilename(track *metadata.Track, ar *AlbumResult) {
	// Resolve artist the same way ProcessLibrary does: prefer AlbumArtist,
	// fall back to Artist. If neither is set the missing-artist warning from
	// checkTrackTags already covers the problem; skip silently here.
	resolvedArtist := track.AlbumArtist
	if resolvedArtist == "" {
		resolvedArtist = track.Artist
	}
	if resolvedArtist == "" {
		return
	}

	albumDir := filepath.Dir(track.Path)
	album := metadata.NewAlbum(albumDir)
	album.ResolvedArtist = resolvedArtist
	album.Tracks = []*metadata.Track{track}

	// Any non-empty root works; we only compare basenames below.
	syntheticRoot := filepath.Dir(albumDir)
	albumPlan, err := planner.PlanAlbum(syntheticRoot, album)
	if err != nil {
		// Tag issues that prevent planning are already flagged by
		// checkTrackTags; skip silently to avoid a confusing second warning.
		return
	}

	for _, move := range albumPlan.Moves {
		if move.OldPath != track.Path {
			continue
		}
		expectedBase := filepath.Base(move.NewPath)
		actualBase := filepath.Base(track.Path)
		if expectedBase != actualBase {
			ar.Warnings = append(ar.Warnings, Warning{
				Path:    track.Path,
				Message: fmt.Sprintf("filename does not match spec (expected: %s)", expectedBase),
			})
		}
		break
	}
}

// checkAlbumTags emits warnings for album-level tag problems: inconsistent
// ALBUMARTIST or ALBUM across tracks, partial DISCNUMBER coverage, and
// duplicate track numbers within the same disc.
func checkAlbumTags(album *metadata.Album, ar *AlbumResult) {
	if len(album.Tracks) == 0 {
		return
	}

	first := album.Tracks[0]

	// Inconsistent ALBUMARTIST: all tracks must agree.
	for _, t := range album.Tracks[1:] {
		if t.AlbumArtist != first.AlbumArtist {
			ar.Warnings = append(ar.Warnings, Warning{
				Path:    album.RootPath,
				Message: "inconsistent ALBUMARTIST tags across tracks",
			})
			break
		}
	}

	// Inconsistent ALBUM: all tracks must agree.
	for _, t := range album.Tracks[1:] {
		if t.Album != first.Album {
			ar.Warnings = append(ar.Warnings, Warning{
				Path:    album.RootPath,
				Message: "inconsistent ALBUM tags across tracks",
			})
			break
		}
	}

	// Partial DISCNUMBER: if any track has the tag, all must.
	tracksWithDisc := 0
	for _, t := range album.Tracks {
		if t.DiscNumber > 0 {
			tracksWithDisc++
		}
	}
	if tracksWithDisc > 0 && tracksWithDisc < len(album.Tracks) {
		ar.Warnings = append(ar.Warnings, Warning{
			Path: album.RootPath,
			Message: fmt.Sprintf(
				"partial DISCNUMBER tags: %d of %d tracks have a disc number",
				tracksWithDisc, len(album.Tracks),
			),
		})
	}

	// Duplicate track numbers within the same disc. Tracks without a
	// TrackNumber tag (nil) are excluded (they are already flagged by
	// checkTrackTags and cannot form meaningful duplicates).
	type discTrack struct{ disc, track int }
	seen := make(map[discTrack]string) // key -> first file path that used it
	for _, t := range album.Tracks {
		if t.TrackNumber == nil {
			continue
		}
		key := discTrack{t.DiscNumber, *t.TrackNumber}
		if prev, dup := seen[key]; dup {
			var discPart string
			if t.DiscNumber > 0 {
				discPart = fmt.Sprintf(" on disc %d", t.DiscNumber)
			}
			ar.Warnings = append(ar.Warnings, Warning{
				Path: t.Path,
				Message: fmt.Sprintf(
					"duplicate track number %d%s (also used by %s)",
					*t.TrackNumber, discPart, prev,
				),
			})
		} else {
			seen[key] = t.Path
		}
	}
}

// checkArtwork warns when the album has no primary artwork file
// (folder.jpg or folder.png) or has more than one.
func checkArtwork(album *metadata.Album, ar *AlbumResult) {
	primary := album.Assets[metadata.CatPrimaryArt]
	switch {
	case len(primary) == 0:
		ar.Warnings = append(ar.Warnings, Warning{
			Path:    album.RootPath,
			Message: "missing primary artwork (folder.jpg or folder.png)",
		})
	case len(primary) > 1:
		ar.Warnings = append(ar.Warnings, Warning{
			Path:    album.RootPath,
			Message: fmt.Sprintf("multiple primary artwork files found (%d)", len(primary)),
		})
	}
}

// checkIntegrity warns when sums.md5 is absent from the album directory.
// Verification of the checksums themselves is out of scope; the user can run
// `md5sum -c sums.md5` directly for that.
func checkIntegrity(album *metadata.Album, ar *AlbumResult) {
	sumsPath := filepath.Join(album.RootPath, hasher.SumsFilename)
	if _, err := os.Stat(sumsPath); os.IsNotExist(err) {
		ar.Warnings = append(ar.Warnings, Warning{
			Path:    album.RootPath,
			Message: "missing " + hasher.SumsFilename,
		})
	}
}

// checkUnknownFiles warns for every file that the metadata scanner
// categorised as CatUnknown. Files inside extras/ are categorised as
// CatExtras by the scanner and are never flagged here.
func checkUnknownFiles(album *metadata.Album, ar *AlbumResult) {
	for _, path := range album.Assets[metadata.CatUnknown] {
		ar.Warnings = append(ar.Warnings, Warning{
			Path:    path,
			Message: "unknown file not in extras/",
		})
	}
}

// checkNaming checks whether the album's current on-disk paths match the
// paths that the planner would compute for a fully conformant library.
//
// Filename conformance (basename only) is checked in all modes. When
// libraryRoot is non-empty (library mode), the album directory path is
// also checked against the full expected hierarchy. When libraryRoot is
// empty (album mode), a synthetic root is derived from the album's parent
// directory so that expected filenames can still be computed; the album
// directory check is skipped because the surrounding hierarchy is unknown.
//
// One warning is emitted when the album directory path does not match the
// expected destination (library mode only), and one warning per file whose
// current basename differs from its expected basename.
func checkNaming(album *metadata.Album, libraryRoot string, ar *AlbumResult) {
	// Determine the root to pass to the planner. In library mode the real
	// root gives us full-path information. In album mode we synthesise one
	// from the album's parent so filename computation still works; the
	// resulting full paths are meaningless and are never compared directly.
	root := libraryRoot
	if root == "" {
		root = filepath.Dir(album.RootPath)
	}

	albumPlan, err := planner.PlanAlbum(root, album)
	if err != nil {
		ar.Warnings = append(ar.Warnings, Warning{
			Path:    album.RootPath,
			Message: fmt.Sprintf("could not compute expected path for conformance check: %v", err),
		})
		return
	}

	// Album directory check requires a real library root; skip in album mode.
	if libraryRoot != "" && album.RootPath != albumPlan.DestDir {
		ar.Warnings = append(ar.Warnings, Warning{
			Path:    album.RootPath,
			Message: fmt.Sprintf("album directory does not match spec (expected: %s)", albumPlan.DestDir),
		})
	}

	for _, move := range albumPlan.Moves {
		// In library mode, IsNoOp captures full-path equality and is the
		// right signal. In album mode the synthetic root makes the full
		// paths meaningless, so compare basenames only.
		var wrong bool
		if libraryRoot != "" {
			wrong = !move.IsNoOp
		} else {
			wrong = filepath.Base(move.OldPath) != filepath.Base(move.NewPath)
		}
		if wrong {
			ar.Warnings = append(ar.Warnings, Warning{
				Path:    move.OldPath,
				Message: fmt.Sprintf("filename does not match spec (expected: %s)", filepath.Base(move.NewPath)),
			})
		}
	}
}
