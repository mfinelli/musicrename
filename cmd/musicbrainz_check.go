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

package cmd

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/metadata"
)

var musicbrainzCheckCmd = &cobra.Command{
	Use:   "check [library-root]",
	Short: "List albums missing a MUSICBRAINZ_ALBUMID tag",
	Long: `Walks library-root and lists every album that has no MUSICBRAINZ_ALBUMID tag. 
Pass --has-id to list the opposite instead: albums that do already have the
tag set.

If library-root is omitted it defaults to the current working directory.`,
	Args: cobra.MaximumNArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveFilterDirs
	},
	RunE: runMusicbrainzCheck,
}

func init() {
	musicbrainzCheckCmd.Flags().Bool("has-id", false,
		"List albums that already have a MUSICBRAINZ_ALBUMID tag, instead of ones missing it")
	musicbrainzCmd.AddCommand(musicbrainzCheckCmd)
}

func runMusicbrainzCheck(cmd *cobra.Command, args []string) error {
	start := time.Now()
	hasID, _ := cmd.Flags().GetBool("has-id")

	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", path, err)
	}

	albums, err := metadata.ProcessLibrary(absPath)
	if err != nil {
		return err
	}

	var matched []string
	for _, album := range albums {
		track := musicbrainzFirstTrack(album)
		if track == nil {
			continue
		}

		mbid, err := readMusicBrainzAlbumID(track.Path)
		if err != nil {
			return err
		}

		missing := mbid == ""
		include := missing
		if hasID {
			include = !missing
		}
		if include {
			matched = append(matched, album.RootPath)
		}
	}

	out := cmd.OutOrStdout()
	printMusicbrainzCheckResult(out, absPath, matched, len(albums), hasID, time.Since(start))
	return nil
}

// printMusicbrainzCheckResult writes matched (each an absolute album path,
// rendered relative to displayRoot where possible) one per line, followed
// by a summary line.
func printMusicbrainzCheckResult(out io.Writer, displayRoot string, matched []string, total int, hasID bool, elapsed time.Duration) {
	for _, albumPath := range matched {
		relPath, err := filepath.Rel(displayRoot, albumPath)
		if err != nil || strings.HasPrefix(relPath, "..") {
			relPath = albumPath
		}
		fmt.Fprintln(out, relPath)
	}

	if len(matched) > 0 {
		fmt.Fprintln(out)
	}

	label := "missing"
	if hasID {
		label = "have"
	}
	lipgloss.Fprintln(out, renameBoldStyle.Render(fmt.Sprintf(
		"%d of %d albums %s MUSICBRAINZ_ALBUMID · %s",
		len(matched), total, label, elapsed.Round(10*time.Millisecond),
	)))
}
