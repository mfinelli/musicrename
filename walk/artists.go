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
	"path"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/mfinelli/musicrename/util"
)

func walkAndParseArtist(dryrun bool, cwd, artist string) error {
	sanitized, err := util.SanitizePathSegment(artist)
	if err != nil {
		return err
	}

	lower := strings.ToLower(sanitized)
	if len(lower) > ARTIST_MAXLENGTH {
		truncated := lower[0:ARTIST_MAXLENGTH]
		lower, err = util.SanitizePathSegment(truncated)
		if err != nil {
			return err
		}
	}

	final := artistSpecialCase(lower)

	if final != artist {
		log.Info("Renaming artist", "original", artist, "new", final)
		if !dryrun {
			err = os.Rename(path.Join(cwd, artist), path.Join(cwd, final))
			if err != nil {
				return err
			}
			err = walkAndParseAlbum(dryrun, path.Join(cwd, final), final)
			if err != nil {
				return err
			}
		} else {
			err = walkAndParseAlbum(dryrun, path.Join(cwd, artist), final)
			if err != nil {
				return err
			}
		}
	} else {
		log.Debug("Artist unchanged by sanitization", "artist", final)
	}

	return nil
}

func artistSpecialCase(artist string) string {
	switch artist {
	case "acdc": // AC/DC
		return "ac⁄dc"
	case "pnk": // P!nk
		return "pink"
	default:
		return artist
	}
}
