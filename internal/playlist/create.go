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

package playlist

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mfinelli/musicrename/internal/hasher"
	"github.com/mfinelli/musicrename/internal/sanitize"
)

// CreateOptions specifies the desired content for a newly-scaffolded
// library-wide playlist file.
type CreateOptions struct {
	// Name becomes the #PLAYLIST: directive value, and (after
	// sanitization) determines the destination filename.
	Name string
	// Targets, if non-nil, becomes the #TARGETS: directive (an empty
	// but non-nil slice writes an explicit empty #TARGETS: (applies to no
	// target); a nil slice (the zero value) omits the directive entirely
	// (applies to every target), matching GlobalPlaylist's Has*-paired
	// field semantics.
	Targets []string
}

// Create scaffolds a new library-wide playlist file under libraryRootRoot's
// playlists/ tree: headers only (per opts), and without any entries.
//
// The destination filename is derived from opts.Name via the same
// sanitization pipeline PlanRenames/ExecuteRenames use (sanitize.CleanString
// with TrackOverride, truncated to 40 characters). An opts.Name that
// sanitizes to an empty string is an error since there's nothing reasonable
// to name the file. A destination that already exists is also an error:
// Create never overwrites an existing playlist.
//
// If playlists/sums.md5 already exists, the new file's entry is added to it
// via hasher.UpdateFile; warning is non-empty (with a nil error) if that
// update fails, since the file itself was still created successfully by
// that point (the same "primary operation already succeeded, don't fail
// over secondary checksum bookkeeping" posture [ExecuteRenames] and the
// navidromesync package's mutating operations already take). This command
// never creates a sums.md5 from scratch; `playlist sums` remains the only
// one that does.
//
// Returns the new file's absolute path on success.
func Create(libraryRootRoot string, opts CreateOptions) (path, warning string, err error) {
	stem := sanitize.Truncate(sanitize.CleanString(opts.Name, sanitize.TrackOverride), 40)
	if stem == "" {
		return "", "", fmt.Errorf("name %q sanitizes to an empty string", opts.Name)
	}

	playlistsDir := Dir(libraryRootRoot)
	rel := stem + ".m3u8"
	dest := filepath.Join(playlistsDir, rel)

	if _, statErr := os.Stat(dest); statErr == nil {
		return "", "", fmt.Errorf("%s already exists", dest)
	} else if !os.IsNotExist(statErr) {
		return "", "", fmt.Errorf("checking %s: %w", dest, statErr)
	}

	gp := &GlobalPlaylist{Name: opts.Name}
	if opts.Targets != nil {
		gp.HasTargets = true
		gp.Targets = opts.Targets
	}

	if err := WriteGlobalPlaylist(dest, gp); err != nil {
		return "", "", err
	}

	if _, statErr := os.Stat(filepath.Join(playlistsDir, hasher.SumsFilename)); statErr == nil {
		if updErr := hasher.UpdateFile(playlistsDir, rel); updErr != nil {
			return dest, fmt.Sprintf("updating %s: %v", hasher.SumsFilename, updErr), nil
		}
	}

	return dest, "", nil
}
