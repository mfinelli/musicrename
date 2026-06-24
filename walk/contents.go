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

package walk

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/mfinelli/musicrename/util"
)

func walkAndParseAlbumContents(dryrun bool, cwd, artist, album string) error {
	items, err := os.ReadDir(cwd)
	if err != nil {
		return err
	}

	for _, item := range items {
		if item.IsDir() {
			err = sanitizeAlbumDir(dryrun, cwd, artist, album, item)
			if err != nil {
				return err
			}
		} else {
			err = sanitizeAlbumItem(dryrun, cwd, artist, album, item.Name())
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func sanitizeAlbumItem(dryrun bool, cwd, artist, album, item string) error {
	filename := filepath.Base(item)
	extension := filepath.Ext(item)
	newext := strings.ToLower(extension)
	name := filename[:len(filename)-len(extension)]
	sanitized, err := util.SanitizePathSegment(name)
	if err != nil {
		return err
	}

	lower := strings.ToLower(sanitized)
	if len(lower) > SONG_MAXLENGTH {
		truncated := lower[0:SONG_MAXLENGTH]
		lower, err = util.SanitizePathSegment(truncated)
		if err != nil {
			return err
		}
	}

	final := albumItemSpecialCase(lower)

	if final != name {
		log.Info("Renaming album item", "artist", artist, "album", album, "original", item, "new", fmt.Sprintf("%s%s", final, newext))
		if !dryrun {
			err = os.Rename(path.Join(cwd, item), path.Join(cwd, fmt.Sprintf("%s%s", final, newext)))
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func albumItemSpecialCase(item string) string {
	switch item {
	default:
		return item
	}
}

func sanitizeAlbumDir(dryrun bool, cwd, artist, album string, item os.DirEntry) error {
	switch item.Name() {
	default:
		log.Warn("Don't know how to handle album subdir", "artist", artist, "album", album, "dir", item.Name())
	}

	return nil
}
