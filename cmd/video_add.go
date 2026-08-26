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
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/video"
)

var videoAddCmd = &cobra.Command{
	Use:   "add <file> [video-root]",
	Short: "File a single raw video into the video library",
	Long: `Sanitizes artist/title, computes the destination under video-root, moves
the video file into place, and writes musicvideo.nfo. An info.txt sitting
alongside the source video (as written by 'mrr video fetch') is carried
along too, if present.

--artist and --title are prompted for interactively if not passed as flags.
--album and --year are optional and are never prompted for.

Errors if the destination directory already exists; there is no --force
overwrite path.

If video-root is omitted it defaults to the current working directory.`,
	Args: cobra.RangeArgs(1, 2),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			// Restrict file completion to supported video extensions.
			return []string{"mp4", "webm", "mkv"}, cobra.ShellCompDirectiveFilterFileExt
		}
		// video-root: fall back to normal directory/file completion.
		return nil, cobra.ShellCompDirectiveDefault
	},
	RunE: runVideoAdd,
}

func init() {
	videoAddCmd.Flags().String("artist", "", "Artist name (prompted for if omitted)")
	videoAddCmd.Flags().String("title", "", "Video title (prompted for if omitted)")
	videoAddCmd.Flags().String("album", "", "Album name (optional)")
	videoAddCmd.Flags().String("year", "", "Release year (optional)")
	videoCmd.AddCommand(videoAddCmd)
}

func runVideoAdd(cmd *cobra.Command, args []string) error {
	src := args[0]
	absSrc, err := filepath.Abs(src)
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", src, err)
	}

	videoRoot := "."
	if len(args) > 1 {
		videoRoot = args[1]
	}
	absRoot, err := filepath.Abs(videoRoot)
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", videoRoot, err)
	}

	artist, _ := cmd.Flags().GetString("artist")
	title, _ := cmd.Flags().GetString("title")
	album, _ := cmd.Flags().GetString("album")
	year, _ := cmd.Flags().GetString("year")

	in := cmd.InOrStdin()
	out := cmd.OutOrStdout()

	if strings.TrimSpace(artist) == "" {
		artist, err = promptFor(in, out, "Artist")
		if err != nil {
			return fmt.Errorf("reading artist: %w", err)
		}
	}
	if strings.TrimSpace(title) == "" {
		title, err = promptFor(in, out, "Title")
		if err != nil {
			return fmt.Errorf("reading title: %w", err)
		}
	}

	result, err := video.Add(absRoot, video.AddInput{
		SourcePath: absSrc,
		Artist:     artist,
		Title:      title,
		Album:      album,
		Year:       year,
	})
	if err != nil {
		return fmt.Errorf("add: %w", err)
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "video: %s\n", result.VideoPath)
	fmt.Fprintf(out, "nfo:   %s\n", result.NFOPath)
	if result.InfoPath != "" {
		fmt.Fprintf(out, "info:  %s\n", result.InfoPath)
	}
	return nil
}

// promptFor writes "label: " to out and reads a single line from in,
// returning it with surrounding whitespace trimmed.
func promptFor(in io.Reader, out io.Writer, label string) (string, error) {
	fmt.Fprintf(out, "%s: ", label)

	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		// EOF or no input available; Err is nil on plain EOF, so this is
		// treated the same as an empty line rather than a hard error, and
		// caught downstream by video.Add's required-field validation.
		return "", scanner.Err()
	}
	return strings.TrimSpace(scanner.Text()), nil
}
