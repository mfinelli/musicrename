/*
 * Copyright © 2019-2026 Mario Finelli
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

package walk

import (
	"os"

	"github.com/charmbracelet/log"
)

const ARTIST_MAXLENGTH = 60
const ALBUM_MAXLENGTH = 80
const SONG_MAXLENGTH = 100

func WalkAndProcessDirectory(dryrun bool, cwd string) error {
	//fileCount := 0
	//dirCount := 0

	artists, err := os.ReadDir(cwd)
	if err != nil {
		return err
	}

	for _, artist := range artists {
		if artist.IsDir() {
			err = walkAndParseArtist(dryrun, cwd, artist.Name())
			if err != nil {
				return err
			}
		} else {
			log.Info("Skipping non-artist-directory", "file", artist.Name())
		}
	}

	return nil
}
