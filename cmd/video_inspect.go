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
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/sanitize"
	"github.com/mfinelli/musicrename/internal/video"
)

var videoInspectCmd = &cobra.Command{
	Use:   "inspect <file>",
	Short: "Display detected and sanitized metadata for a video",
	Long: `Reads a video's musicvideo.nfo and prints its fields alongside the
sanitized artist/title (the values that would be used when filing or
renaming the video). Read-only.`,
	Args: cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// Restrict file completion to supported video extensions.
		return []string{"mp4", "webm", "mkv"}, cobra.ShellCompDirectiveFilterFileExt
	},
	RunE: runVideoInspect,
}

func init() {
	videoCmd.AddCommand(videoInspectCmd)
}

func runVideoInspect(cmd *cobra.Command, args []string) error {
	path := args[0]

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp4", ".webm", ".mkv":
		// supported
	default:
		return fmt.Errorf(
			"%q is not a supported video file (expected .mp4, .webm, or .mkv)",
			filepath.Base(path),
		)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", path, err)
	}
	dir := filepath.Dir(absPath)

	nfo, err := video.ReadNFO(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf(
				"no musicvideo.nfo found for %q (run 'mrr video add' or 'mrr video edit' first)",
				filepath.Base(path),
			)
		}
		return fmt.Errorf("reading metadata for %q: %w", filepath.Base(path), err)
	}

	// Sanitize the fields that feed into directory/file names. Album and
	// Year are stored verbatim (informational/Jellyfin-only), so they have
	// no sanitized form to show.
	cleanTitle := sanitize.CleanStringResult(nfo.Title, sanitize.TrackOverride)
	cleanArtist := sanitize.CleanStringResult(nfo.Artist, sanitize.ArtistOverride)

	out := cmd.OutOrStdout()

	fmt.Fprintln(out, renameHeaderStyle.Render("Inspecting..."))
	fmt.Fprintln(out)

	formatLabel := strings.ToUpper(strings.TrimPrefix(ext, "."))
	fmt.Fprintln(out,
		"  "+
			inspectFilenameStyle.Render(filepath.Base(path))+
			"  "+
			inspectFaintStyle.Render(formatLabel),
	)
	fmt.Fprintln(out)

	inspectPrintTagField(out, "Title", nfo.Title, cleanTitle)
	inspectPrintTagField(out, "Artist", nfo.Artist, cleanArtist)
	fmt.Fprintln(out)

	inspectPrintField(out, "Album", inspectDash(nfo.Album))
	inspectPrintField(out, "Year", inspectDash(nfo.Year))

	return nil
}
