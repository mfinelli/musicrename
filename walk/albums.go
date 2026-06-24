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

func walkAndParseAlbum(dryrun bool, cwd, artist string) error {
	albums, err := os.ReadDir(cwd)
	if err != nil {
		return err
	}

	for _, album := range albums {
		if album.IsDir() {
			err = sanitizeAlbum(dryrun, cwd, artist, album.Name())
			if err != nil {
				return err
			}
		} else {
			log.Info("Skipping non-album-directory", "artist", artist, "file", album.Name())
		}
	}

	return nil
}

func sanitizeAlbum(dryrun bool, cwd, artist, album string) error {
	sanitized, err := util.SanitizePathSegment(album)
	if err != nil {
		return err
	}

	lower := strings.ToLower(sanitized)
	if len(lower) > ALBUM_MAXLENGTH {
		truncated := lower[0:ALBUM_MAXLENGTH]
		lower, err = util.SanitizePathSegment(truncated)
		if err != nil {
			return err
		}
	}

	final := albumSpecialCase(lower)

	if final != album {
		log.Info("Renaming album", "artist", artist, "original", album, "new", final)
		if !dryrun {
			err = os.Rename(path.Join(cwd, album), path.Join(cwd, final))
			if err != nil {
				return err
			}
			err = walkAndParseAlbumContents(dryrun, path.Join(cwd, final), artist, final)
			if err != nil {
				return err
			}
		} else {
			err = walkAndParseAlbumContents(dryrun, path.Join(cwd, album), artist, final)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func albumSpecialCase(album string) string {
	switch album {
	default:
		return album
	}
}
