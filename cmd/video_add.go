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
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
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
			return video.VideoExtensions, cobra.ShellCompDirectiveFilterFileExt
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

	// add only ever prompts for the required fields (artist/title); album
	// and year remain flag-only here, unlike "video edit" (video_edit.go,
	// which also uses promptMissingVideoFields below) which prompts for
	// anything not supplied.
	values := videoFieldValues{Artist: artist, Title: title, Album: album, Year: year}
	if err := promptMissingVideoFields(&values,
		strings.TrimSpace(artist) == "",
		strings.TrimSpace(title) == "",
		false,
		false,
	); err != nil {
		return fmt.Errorf("prompting: %w", err)
	}

	result, err := video.Add(absRoot, video.AddInput{
		SourcePath: absSrc,
		Artist:     values.Artist,
		Title:      values.Title,
		Album:      values.Album,
		Year:       values.Year,
	})
	if err != nil {
		return fmt.Errorf("add: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintln(out)
	fmt.Fprintf(out, "video: %s\n", result.VideoPath)
	fmt.Fprintf(out, "nfo:   %s\n", result.NFOPath)
	if result.InfoPath != "" {
		fmt.Fprintf(out, "info:  %s\n", result.InfoPath)
	}
	return nil
}

// videoFieldValues holds the four musicvideo.nfo fields. Used both to seed
// prompts (with the current value, for "video edit") or a blank starting
// point (for "video add"), and to carry the final values back out. Shared
// with video_edit.go's runVideoEdit.
type videoFieldValues struct {
	Artist string
	Title  string
	Album  string
	Year   string
}

// promptMissingVideoFields runs a single huh form prompting only for the
// fields the caller marks as needed (needArtist, needTitle, needAlbum,
// needYear), pre-filled with whatever is already in *values — for "video
// add" that's blank; for "video edit" that's the field's current value from
// the existing nfo, so pressing enter without typing anything keeps it
// unchanged (or, for an already-empty optional field, leaves it empty and
// excluded from the nfo, matching Add/Edit's omitempty behavior).
//
// Artist and Title are validated as non-empty when prompted; Album and Year
// accept anything, including empty. If no field is needed, this is a no-op:
// huh is never invoked and no terminal interaction occurs, so callers that
// supplied every field via flags never hit a TUI at all.
func promptMissingVideoFields(values *videoFieldValues, needArtist, needTitle, needAlbum, needYear bool) error {
	requireNonEmpty := func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("required")
		}
		return nil
	}

	var fields []huh.Field
	if needArtist {
		fields = append(fields, huh.NewInput().
			Title("Artist").
			Value(&values.Artist).
			Validate(requireNonEmpty))
	}
	if needTitle {
		fields = append(fields, huh.NewInput().
			Title("Title").
			Value(&values.Title).
			Validate(requireNonEmpty))
	}
	if needAlbum {
		fields = append(fields, huh.NewInput().
			Title("Album (optional)").
			Value(&values.Album))
	}
	if needYear {
		fields = append(fields, huh.NewInput().
			Title("Year (optional)").
			Value(&values.Year))
	}

	if len(fields) == 0 {
		return nil
	}

	return huh.NewForm(huh.NewGroup(fields...)).Run()
}
