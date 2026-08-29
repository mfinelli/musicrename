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

package navidromesync

import (
	"fmt"
	"os"

	subsonic "github.com/supersonic-app/go-subsonic/subsonic"

	"github.com/mfinelli/musicrename/internal/navidrome"
	"github.com/mfinelli/musicrename/internal/playlist"
)

// DeleteOne performs an explicit single-playlist delete:
// reads the #NAVIDROME-ID from the local file at path, deletes the
// corresponding remote playlist by that ID, then removes the local file.
// This is the only sanctioned way to perform a real, intended deletion
// (contrast with pull's self-healing behavior, which never deletes a
// remote playlist and only ever deletes a local file on confirmed remote
// absence).
//
// Errors immediately, without deleting anything, if path doesn't exist or
// has no #NAVIDROME-ID (there is nothing remote to delete in that case).
//
// If the remote delete fails because the playlist is already gone
// (confirmed not-found), the local file is still removed since that's the
// actual end state being asked for, and it's already half-achieved. Any
// other remote failure (auth, network, 5xx) aborts without touching the local
// file at all: a generic failure must never be treated as confirmed absence.
func DeleteOne(client *subsonic.Client, path string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	gp, err := playlist.ReadGlobalPlaylist(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if !gp.HasNavidromeID {
		return fmt.Errorf("%s has no #NAVIDROME-ID; nothing to delete remotely", path)
	}

	if err := client.DeletePlaylist(gp.NavidromeID); err != nil {
		if code, ok := navidrome.ErrCode(err); !(ok && code == navidrome.ErrCodeNotFound) {
			return fmt.Errorf("deleting remote playlist: %w", err)
		}
		// Already gone remotely; still remove the local file below, since
		// that's the desired end state either way.
	}

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("removing %s: %w", path, err)
	}

	return nil
}
