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

// Package planner converts a slice of scanned and tag-read [metadata.Album]
// values into a [Plan]: a structured description of every filesystem move
// needed to bring the library into its target state. No filesystem changes are
// made here; the package is purely computational.
//
// The typical call sequence is:
//
//	albums, _ := metadata.ProcessLibrary(root)
//	p := planner.New(root)
//	plan, _ := p.PlanLibrary(albums)
//
// The returned Plan groups moves by album and carries any non-fatal warnings
// (missing tags, unknown files) alongside the moves so the caller can display
// everything together rather than interleaved with processing output.
package planner

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mfinelli/musicrename/internal/metadata"
	"github.com/mfinelli/musicrename/internal/sanitize"
)

// MoveOperation describes the intended movement of a single file.
type MoveOperation struct {
	// OldPath is the absolute path of the file in its current location.
	OldPath string
	// NewPath is the absolute path of the file in its intended location.
	NewPath string
	// IsCaseOnly is true when OldPath and NewPath differ only in
	// capitalisation. The executor must rename via a temporary intermediate
	// path to avoid a silent no-op on case-insensitive filesystems (e.g.
	// the macOS default HFS+).
	IsCaseOnly bool
	// IsNoOp is true when OldPath and NewPath are identical; the file is
	// already in the correct location and no filesystem change is required.
	IsNoOp bool
}

// AlbumPlan groups all moves associated with a single album.
type AlbumPlan struct {
	// AlbumArtist is the sanitized and truncated album-level artist name as
	// it appears in the directory hierarchy (e.g. "beyonce").
	AlbumArtist string
	// AlbumName is the sanitized album folder name, including the year
	// prefix when the YEAR tag is present (e.g. "[2003] dangerously in love").
	AlbumName string
	// SourceDir is the absolute path of the source album directory (i.e.
	// album.RootPath). It is used by the display layer to show the source
	// location once per album rather than repeating it on every move line.
	SourceDir string
	// DestDir is the absolute path of the target album directory. It allows
	// callers to compute file-relative paths (e.g. for display) without
	// re-deriving the directory from the move operations.
	DestDir string
	// Bucket is the single-character first-letter directory ("a"–"z" or "0")
	// computed from ResolvedArtistSort when present, otherwise from ResolvedArtist.
	// It is provided for display purposes so callers do not need to re-derive it.
	Bucket string
	// Moves contains one entry per file that needs to be moved (or confirmed
	// as a no-op) within this album, including audio tracks and all assets.
	Moves []MoveOperation
	// Warnings holds non-fatal issues discovered while planning this album
	// (missing tags, unknown files). They are seeded from the scan-phase
	// warnings on the source Album so that all warnings for the album surface
	// together in the display layer.
	Warnings []string
}

// Plan is the complete target state for the entire library. It is the primary
// output of [planner.PlanLibrary] and contains one [AlbumPlan] per discovered
// album.
type Plan struct {
	// Albums contains one entry per source album directory, in the order
	// they were processed by PlanLibrary.
	Albums []AlbumPlan
}

// planner holds the library root path and constructs all destination paths
// relative to it. Use [New] to create an instance.
type planner struct {
	// libraryRoot is the absolute path to the top of the target library
	// hierarchy (e.g. /home/user/Music). All destination paths are rooted
	// here.
	libraryRoot string
}

// PlanAlbum computes the target path for every file in a single album and
// returns an AlbumPlan describing the required moves. Unlike PlanLibrary,
// cross-album collision detection is not performed: a fresh destination map
// is created for each call so that callers such as the checker can plan
// albums independently without one album's results affecting another.
func PlanAlbum(libraryRoot string, album *metadata.Album) (*AlbumPlan, error) {
	return New(libraryRoot).planAlbum(album, make(map[string]string))
}

// New returns a planner that will place all files under libraryRoot.
// libraryRoot should be an absolute path; callers should resolve it with
// filepath.Abs before calling New.
func New(libraryRoot string) *planner {
	return &planner{libraryRoot: libraryRoot}
}

// PlanLibrary converts a slice of processed albums into a Plan covering the
// entire library. It iterates albums in order and delegates per-album path
// generation to planAlbum, passing a shared destination map so that
// cross-album collisions (two albums resolving to the same file path) are
// detected globally. The first collision or validation error aborts the run.
func (p *planner) PlanLibrary(albums []*metadata.Album) (*Plan, error) {
	globalPlan := &Plan{}
	// Track destination paths to detect collisions globally.
	destMap := make(map[string]string)

	for _, album := range albums {
		albumPlan, err := p.planAlbum(album, destMap)
		if err != nil {
			return nil, err
		}
		globalPlan.Albums = append(globalPlan.Albums, *albumPlan)
	}

	return globalPlan, nil
}

// assetCategoryOrder defines the canonical iteration order for asset
// categories. A fixed order ensures that dry-run output and move lists are
// stable across runs (ranging over a map is non-deterministic in Go).
var assetCategoryOrder = []metadata.FileCategory{
	metadata.CatPrimaryArt,
	metadata.CatRootText,
	metadata.CatArtwork,
	metadata.CatScan,
	metadata.CatExtras,
	metadata.CatUnknown,
}

// planAlbum computes the target path for every file in album and returns an
// AlbumPlan describing the required moves. globalDests is a map shared across
// all albums in the run; it is updated by each call to createMoveOp so that
// cross-album destination collisions are detected.
//
// planAlbum returns an error on the first collision or hard validation failure
// (e.g. inconsistent DISCNUMBER tags, unresolvable artist). Non-fatal issues
// such as missing tags are appended to AlbumPlan.Warnings instead.
func (p *planner) planAlbum(album *metadata.Album, globalDests map[string]string) (*AlbumPlan, error) {
	// 1. Resolve Album-Level Metadata
	// ResolvedArtist is populated by ProcessLibrary; an empty value means
	// no artist could be determined from any track in the album.
	if album.ResolvedArtist == "" {
		return nil, fmt.Errorf("cannot resolve artist for album at %s", album.RootPath)
	}

	sanArtist := sanitize.CleanStringResult(album.ResolvedArtist, sanitize.ArtistOverride)
	truncArtist := sanitize.Truncate(sanArtist.Value, 60)

	// GetFirstLetterPath already includes the artist name (e.g. "b/beyonce"),
	// so it is used directly as the path component without appending truncArtist again.
	// For first-letter bucketing, prefer the ALBUMARTISTSORT tag when present.
	// This lets "The Beatles" file under "b/the beatles/" rather than "t/".
	// The sort tag is sanitized to extract the bucket letter; the artist folder
	// name always comes from the regular sanitized ALBUMARTIST (truncArtist).
	var artistFolderPath string
	if album.ResolvedArtistSort != "" {
		sanSort := sanitize.CleanStringResult(album.ResolvedArtistSort, sanitize.ArtistOverride)
		truncSort := sanitize.Truncate(sanSort.Value, 60)
		sortPath, err := sanitize.GetFirstLetterPath(truncSort)
		if err != nil {
			return nil, fmt.Errorf("artist sort path error: %w", err)
		}
		// sortPath is "b/beatles the" (take only the bucket component).
		artistFolderPath = filepath.Join(filepath.Dir(sortPath), truncArtist)
	} else {
		var err error
		artistFolderPath, err = sanitize.GetFirstLetterPath(truncArtist)
		if err != nil {
			return nil, fmt.Errorf("artist path error: %w", err)
		}
	}

	// Use the first track for album-level tags (album name, year). These are
	// expected to be consistent across all tracks in the same source folder.
	var rawAlbum, rawYear string
	if len(album.Tracks) > 0 {
		rawAlbum = album.Tracks[0].Album
		rawYear = album.Tracks[0].Year
	}

	sanAlbum := sanitize.CleanStringResult(rawAlbum, sanitize.AlbumOverride)
	truncAlbum := sanitize.Truncate(sanAlbum.Value, 60)

	// Folder format: "[Year] Album Name" or "Album Name" when year is absent.
	albumFolderName := truncAlbum
	if rawYear != "" {
		albumFolderName = fmt.Sprintf("[%s] %s", rawYear, truncAlbum)
	}

	// artistFolderPath is already "x/artist-name"; join directly with the album folder.
	fullAlbumDir := filepath.Join(p.libraryRoot, artistFolderPath, albumFolderName)

	// 2. Validate disc number consistency. If any track carries a DISCNUMBER
	// tag, every track in the album must carry one. A partial set is an error
	// because the resulting filenames would be incoherent (some with a disc
	// prefix, some without).
	tracksWithDisc := 0
	for _, t := range album.Tracks {
		if t.DiscNumber > 0 {
			tracksWithDisc++
		}
	}
	if tracksWithDisc > 0 && tracksWithDisc != len(album.Tracks) {
		return nil, fmt.Errorf(
			"inconsistent DISCNUMBER tags in album at %s: %d of %d tracks have a disc number",
			album.RootPath, tracksWithDisc, len(album.Tracks),
		)
	}

	// 3. Determine track numbering strategy.
	//
	// maxTrack drives zero-padding: 2 digits by default, 3 if any track
	// exceeds 99. A nil TrackNumber (absent tag) is excluded from this
	// calculation; those tracks format as "00" and a warning is emitted by
	// the command layer.
	//
	// hasMultiDisc is true when two or more distinct DISCNUMBER values are
	// present. A single disc always has at most one distinct value, so this
	// is safe even when all tracks share DISCNUMBER=1.
	maxTrack := 0
	discNumbers := make(map[int]bool)
	for _, t := range album.Tracks {
		if t.TrackNumber != nil && *t.TrackNumber > maxTrack {
			maxTrack = *t.TrackNumber
		}
		if t.DiscNumber > 0 {
			discNumbers[t.DiscNumber] = true
		}
	}
	padding := 2
	if maxTrack > 99 {
		padding = 3
	}
	hasMultiDisc := len(discNumbers) > 1

	albumPlan := &AlbumPlan{
		AlbumArtist: truncArtist,
		AlbumName:   albumFolderName,
		SourceDir:   album.RootPath,
		DestDir:     fullAlbumDir,
		Bucket:      filepath.Dir(artistFolderPath),
		Moves:       []MoveOperation{},
		// Seed with any warnings already collected during the scan phase
		// (e.g. unreadable tracks from ProcessLibrary) so all warnings for
		// this album surface together in the display layer.
		Warnings: append([]string{}, album.Warnings...),
	}

	if len(album.Tracks) > 0 && rawYear == "" {
		albumPlan.Warnings = append(albumPlan.Warnings,
			fmt.Sprintf("missing YEAR tag for album at %s", album.RootPath))
	}

	if len(album.Tracks) > 0 && rawAlbum == "" {
		albumPlan.Warnings = append(albumPlan.Warnings,
			fmt.Sprintf("missing ALBUM tag for album at %s", album.RootPath))
	}

	// 4. Plan audio file moves.
	for _, track := range album.Tracks {
		// TITLE fallback: when the tag is absent, use the original filename
		// stem so the file is still placed rather than dropped.
		title := track.Title
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(track.Path), filepath.Ext(track.Path))
			albumPlan.Warnings = append(albumPlan.Warnings,
				fmt.Sprintf("missing TITLE tag for %s (using filename stem)", track.Path))
		}

		sanTitle := sanitize.CleanStringResult(title, sanitize.TrackOverride)
		truncTitle := sanitize.Truncate(sanTitle.Value, 40)

		// Always lowercase the extension for filesystem consistency.
		ext := strings.ToLower(filepath.Ext(track.Path))

		// A nil TrackNumber means the tag was absent; use 0 as the formatted
		// value so the file sorts before track 1.
		trackNum := 0
		if track.TrackNumber != nil {
			trackNum = *track.TrackNumber
		} else {
			albumPlan.Warnings = append(albumPlan.Warnings,
				fmt.Sprintf("missing TRACKNUMBER tag for %s", track.Path))
		}
		trackNumStr := fmt.Sprintf("%0*d", padding, trackNum)

		var fileName string
		if hasMultiDisc {
			fileName = fmt.Sprintf("%d-%s %s%s", track.DiscNumber, trackNumStr, truncTitle, ext)
		} else {
			fileName = fmt.Sprintf("%s %s%s", trackNumStr, truncTitle, ext)
		}

		newPath := filepath.Join(fullAlbumDir, fileName)
		op, err := p.createMoveOp(track.Path, newPath, globalDests)
		if err != nil {
			return nil, err
		}
		albumPlan.Moves = append(albumPlan.Moves, op)
	}

	// 5. Plan asset moves (artwork, scans, extras, root text files).
	// Unknown files are left in place; a warning is emitted by the command layer.
	// Iterate in a fixed category order (assetCategoryOrder) so that move
	// lists and dry-run output are stable across runs.
	for _, cat := range assetCategoryOrder {
		paths, ok := album.Assets[cat]
		if !ok {
			continue
		}

		// Sort paths within each category for additional stability.
		sortedPaths := make([]string, len(paths))
		copy(sortedPaths, paths)
		sort.Strings(sortedPaths)

		for _, oldPath := range sortedPaths {
			// Always lowercase the extension for filesystem consistency.
			ext := strings.ToLower(filepath.Ext(oldPath))
			rawStem := strings.TrimSuffix(filepath.Base(oldPath), filepath.Ext(oldPath))

			var newPath string
			switch cat {
			case metadata.CatPrimaryArt:
				// Primary art is always renamed to folder.{ext}; no sanitization
				// of the stem is needed because the name is hardcoded.
				newPath = filepath.Join(fullAlbumDir, "folder"+ext)

			case metadata.CatArtwork:
				sanStem := sanitize.CleanStringResult(rawStem, sanitize.TrackOverride)
				truncStem := sanitize.TruncateWithOffset(sanStem.Value, "artwork", 40)
				newPath = filepath.Join(fullAlbumDir, "artwork", truncStem+ext)

			case metadata.CatScan:
				sanStem := sanitize.CleanStringResult(rawStem, sanitize.TrackOverride)
				truncStem := sanitize.TruncateWithOffset(sanStem.Value, "scans", 40)
				newPath = filepath.Join(fullAlbumDir, "scans", truncStem+ext)

			case metadata.CatExtras:
				sanStem := sanitize.CleanStringResult(rawStem, sanitize.TrackOverride)
				truncStem := sanitize.TruncateWithOffset(sanStem.Value, "extras", 40)
				newPath = filepath.Join(fullAlbumDir, "extras", truncStem+ext)

			case metadata.CatRootText:
				sanStem := sanitize.CleanStringResult(rawStem, sanitize.TrackOverride)
				truncStem := sanitize.Truncate(sanStem.Value, 40)
				newPath = filepath.Join(fullAlbumDir, truncStem+ext)

			case metadata.CatUnknown:
				// Leave unknown files in place and record a warning.
				albumPlan.Warnings = append(albumPlan.Warnings,
					fmt.Sprintf("unknown file left in place: %s", oldPath))
				continue
			}

			op, err := p.createMoveOp(oldPath, newPath, globalDests)
			if err != nil {
				return nil, err
			}
			albumPlan.Moves = append(albumPlan.Moves, op)
		}
	}

	return albumPlan, nil
}

// createMoveOp registers newPath in globalDests and returns a MoveOperation
// describing the relationship between oldPath and newPath. It fails fast with
// an error if newPath is already claimed by a different source file
// (collision). IsNoOp is set when the paths are identical; IsCaseOnly is set
// when they differ only in case.
func (p *planner) createMoveOp(oldPath, newPath string, globalDests map[string]string) (MoveOperation, error) {
	// Collision detection: fail fast if two source files resolve to the same
	// destination. In practice this should never happen with a well-tagged
	// library, but is caught here rather than at execution time.
	if existingOld, exists := globalDests[newPath]; exists && existingOld != oldPath {
		return MoveOperation{}, fmt.Errorf(
			"collision detected: %s and %s both resolve to %s",
			existingOld, oldPath, newPath,
		)
	}
	globalDests[newPath] = oldPath

	if oldPath == newPath {
		return MoveOperation{OldPath: oldPath, NewPath: newPath, IsNoOp: true}, nil
	}

	// Paths reaching this point have been through the sanitization pipeline
	// and are ASCII-only, so EqualFold behaves identically to a simple
	// case-insensitive byte comparison with no Unicode surprises.
	if strings.EqualFold(oldPath, newPath) {
		return MoveOperation{OldPath: oldPath, NewPath: newPath, IsCaseOnly: true}, nil
	}

	return MoveOperation{OldPath: oldPath, NewPath: newPath}, nil
}
