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

package planner

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mfinelli/musicrename/internal/metadata"
	"github.com/mfinelli/musicrename/internal/sanitize"
)

// MoveOperation describes the intended movement of a single file.
type MoveOperation struct {
	OldPath    string
	NewPath    string
	IsCaseOnly bool
	IsNoOp     bool
}

// AlbumPlan groups all moves associated with a single album.
type AlbumPlan struct {
	AlbumArtist string
	AlbumName   string
	Moves       []MoveOperation
}

// Plan is the final target state for the entire library.
type Plan struct {
	Albums []AlbumPlan
}

type planner struct {
	libraryRoot string
}

func New(libraryRoot string) *planner {
	return &planner{libraryRoot: libraryRoot}
}

// PlanLibrary converts a slice of processed albums into a global Move Plan.
func (p *planner) PlanLibrary(albums []*metadata.Album) (*Plan, error) {
	globalPlan := &Plan{}
	// Track destination paths to detect collisions globally
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

func (p *planner) planAlbum(album *metadata.Album, globalDests map[string]string) (*AlbumPlan, error) {
	// 1. Resolve Album-Level Metadata
	rawArtist := album.ResolveAlbumArtist()
	if rawArtist == "" {
		return nil, fmt.Errorf("cannot resolve artist for album at %s", album.RootPath)
	}

	sanArtist := sanitize.CleanStringResult(rawArtist, sanitize.ArtistOverride)
	truncArtist := sanitize.Truncate(sanArtist.Value, 60)

	artistFolderPath, err := sanitize.GetFirstLetterPath(truncArtist)
	if err != nil {
		return nil, fmt.Errorf("artist path error: %w", err)
	}

	// Handle Album name and Year
	// We use the first track to get the album/year as they are consistent per album
	var rawAlbum, rawYear string
	if len(album.Tracks) > 0 {
		rawAlbum = album.Tracks[0].Album
		rawYear = album.Tracks[0].Year
	}

	sanAlbum := sanitize.CleanStringResult(rawAlbum, sanitize.AlbumOverride)
	truncAlbum := sanitize.Truncate(sanAlbum.Value, 60)

	// Folder format: [Year] Album Name
	albumFolderName := truncAlbum
	if rawYear != "" {
		albumFolderName = fmt.Sprintf("[%s] %s", rawYear, truncAlbum)
	}

	fullAlbumDir := filepath.Join(p.libraryRoot, artistFolderPath, truncArtist, albumFolderName)

	// 2. Determine Track Numbering Strategy
	maxTrack := 0
	hasMultiDisc := false
	for _, t := range album.Tracks {
		if t.TrackNumber > maxTrack {
			maxTrack = t.TrackNumber
		}
		if t.DiscNumber > 1 {
			hasMultiDisc = true
		}
	}
	padding := 2
	if maxTrack > 99 {
		padding = 3
	}

	albumPlan := &AlbumPlan{
		AlbumArtist: truncArtist,
		AlbumName:   albumFolderName,
		Moves:       []MoveOperation{},
	}

	// 3. Plan Audio Files
	for _, track := range album.Tracks {
		// Title fallback logic is handled in metadata.Track, we just sanitize the result
		sanTitle := sanitize.CleanStringResult(track.Title, sanitize.TrackOverride)
		truncTitle := sanitize.Truncate(sanTitle.Value, 40)

		// Construct filename: [Disc-][Track] Title.ext
		trackNumStr := fmt.Sprintf("%0*d", padding, track.TrackNumber)
		fileName := fmt.Sprintf("%s %s%s", trackNumStr, truncTitle, filepath.Ext(track.Path))
		if hasMultiDisc {
			fileName = fmt.Sprintf("%d-%s %s%s", track.DiscNumber, trackNumStr, truncTitle, filepath.Ext(track.Path))
		}

		newPath := filepath.Join(fullAlbumDir, fileName)
		op, err := p.createMoveOp(track.Path, newPath, globalDests)
		if err != nil {
			return nil, err
		}
		albumPlan.Moves = append(albumPlan.Moves, op)
	}

	// 4. Plan Assets (Art, Scans, Extras)
	for cat, paths := range album.Assets {
		for _, oldPath := range paths {
			ext := filepath.Ext(oldPath)
			stem := strings.TrimSuffix(filepath.Base(oldPath), ext)

			var newPath string
			switch cat {
			case metadata.CatPrimaryArt:
				// folder.jpg or folder.png
				newPath = filepath.Join(fullAlbumDir, "folder"+ext)
			case metadata.CatArtwork:
				destDir := filepath.Join(fullAlbumDir, "artwork")
				truncatedStem := sanitize.TruncateWithOffset(stem, "artwork", 80)
				newPath = filepath.Join(destDir, truncatedStem+ext)
			case metadata.CatScan:
				destDir := filepath.Join(fullAlbumDir, "scans")
				truncatedStem := sanitize.TruncateWithOffset(stem, "scans", 80)
				newPath = filepath.Join(destDir, truncatedStem+ext)
			case metadata.CatExtras:
				destDir := filepath.Join(fullAlbumDir, "extras")
				truncatedStem := sanitize.TruncateWithOffset(stem, "extras", 80)
				newPath = filepath.Join(destDir, truncatedStem+ext)
			default:
				// For RootText or Unknown, we keep them at root but sanitize the name
				sanFile := sanitize.CleanStringResult(stem, sanitize.TrackOverride)
				truncFile := sanitize.Truncate(sanFile.Value, 40)
				newPath = filepath.Join(fullAlbumDir, truncFile+ext)
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

func (p *planner) createMoveOp(oldPath, newPath string, globalDests map[string]string) (MoveOperation, error) {
	// Collision Detection
	if existingOld, exists := globalDests[newPath]; exists && existingOld != oldPath {
		return MoveOperation{}, fmt.Errorf("collision detected: %s and %s both resolve to %s", existingOld, oldPath, newPath)
	}
	globalDests[newPath] = oldPath

	// No-Op and Case-Only Detection
	if oldPath == newPath {
		return MoveOperation{OldPath: oldPath, NewPath: newPath, IsNoOp: true}, nil
	}

	if strings.EqualFold(oldPath, newPath) {
		return MoveOperation{OldPath: oldPath, NewPath: newPath, IsCaseOnly: true}, nil
	}

	return MoveOperation{OldPath: oldPath, NewPath: newPath}, nil
}
