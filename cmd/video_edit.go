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
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/video"
)

var videoEditCmd = &cobra.Command{
	Use:   "edit [file-or-directory]",
	Short: "Create or update the musicvideo.nfo metadata for a video",
	Long: `Updates artist/title/album/year in a video's musicvideo.nfo, creating it
if one doesn't exist yet (e.g. a video that was never run through "video
add", or an nfo that was deleted).

The argument can be the video file itself or its directory; if omitted it
defaults to the current directory, so running this from inside a video's 
folder needs no argument at all.

Any of --artist/--title/--album/--year not passed as a flag is prompted for
interactively, pre-filled with its current value: press enter to keep it
unchanged, or edit/clear it in place. An already-empty (or not-yet-existing)
optional field simply stays empty if you press enter without typing
anything.

To change a field non-interactively, pass it as a flag; to explicitly clear
an already-set optional field non-interactively (skipping the prompt for
it), pass it as an empty flag value, e.g. --year "".

This only writes musicvideo.nfo — it never moves the video, renames the
file, or touches its directory otherwise. If you change artist or title (or
are setting them for the first time), run 'mrr video rename' afterward to
reconcile the video's location with the new values.`,
	Args: cobra.MaximumNArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// Either a video file or a directory is valid here, so fall back to
		// normal completion rather than filtering to video extensions only.
		return nil, cobra.ShellCompDirectiveDefault
	},
	RunE: runVideoEdit,
}

func init() {
	videoEditCmd.Flags().String("artist", "", "New artist name (prompted for if omitted)")
	videoEditCmd.Flags().String("title", "", "New video title (prompted for if omitted)")
	videoEditCmd.Flags().String("album", "", `New album name (prompted for if omitted; pass "" to clear without prompting)`)
	videoEditCmd.Flags().String("year", "", `New release year (prompted for if omitted; pass "" to clear without prompting)`)
	videoCmd.AddCommand(videoEditCmd)
}

func runVideoEdit(cmd *cobra.Command, args []string) error {
	target := "."
	if len(args) > 0 {
		target = args[0]
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", target, err)
	}

	dir, err := videoDirFor(absTarget)
	if err != nil {
		return err
	}

	// A missing nfo isn't an error here: Edit creates one fresh, so the
	// prompts below just start blank instead of pre-filled with existing
	// values.
	current, err := video.ReadNFO(dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("reading current metadata: %w", err)
		}
		current = &video.NFO{}
	}

	// Seed with the current (or blank, if none yet) values so unprompted/
	// unpassed fields, and the pre-filled prompt text for prompted ones,
	// carry the existing content forward unchanged by default.
	values := videoFieldValues{
		Artist: current.Artist,
		Title:  current.Title,
		Album:  current.Album,
		Year:   current.Year,
	}

	flags := cmd.Flags()
	// Flags().Changed distinguishes "explicitly passed" (including an
	// explicit empty string, e.g. --year "" to clear it non-interactively)
	// from "omitted", which is what decides whether each field is prompted
	// for below.
	if flags.Changed("artist") {
		values.Artist, _ = flags.GetString("artist")
	}
	if flags.Changed("title") {
		values.Title, _ = flags.GetString("title")
	}
	if flags.Changed("album") {
		values.Album, _ = flags.GetString("album")
	}
	if flags.Changed("year") {
		values.Year, _ = flags.GetString("year")
	}

	if err := promptMissingVideoFields(&values,
		!flags.Changed("artist"),
		!flags.Changed("title"),
		!flags.Changed("album"),
		!flags.Changed("year"),
	); err != nil {
		return fmt.Errorf("prompting: %w", err)
	}

	result, err := video.Edit(dir, video.EditInput{
		Artist: values.Artist,
		Title:  values.Title,
		Album:  values.Album,
		Year:   values.Year,
	})
	if err != nil {
		return fmt.Errorf("edit: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintln(out)
	if result.Created {
		fmt.Fprintf(out, "nfo created: %s\n", result.NFOPath)
	} else {
		fmt.Fprintf(out, "nfo updated: %s\n", result.NFOPath)
	}
	if values.Artist != current.Artist || values.Title != current.Title {
		fmt.Fprintln(out, "Artist or title changed — run 'mrr video rename' to reconcile the video's location.")
	}
	return nil
}

// videoDirFor resolves path to the directory that should contain (or
// already contains) a video's musicvideo.nfo: path itself if it's a
// directory, or its parent if it's a file.
func videoDirFor(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("could not resolve %q: %w", path, err)
	}
	if info.IsDir() {
		return path, nil
	}
	return filepath.Dir(path), nil
}
