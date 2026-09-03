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
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/video"
)

var videoExtractAudioCmd = &cobra.Command{
	Use:   "extract-audio <file>",
	Short: "Extract a video's audio stream into a standalone derived audio file",
	Long: `Remuxes (not re-encodes) a video's audio stream into a standalone file next
to it, tags it from musicvideo.nfo, and computes ReplayGain via rsgain. For 
tracks that exist only as a music video: this is what makes them reachable via 
Navidrome (which has no video support) and syncable to targets that don't 
support video.

Requires an existing musicvideo.nfo with both artist and title set (run
'mrr video add' or 'mrr video edit' first if necessary).

By default, errors if a derived audio file already exists. Pass --retag to
update only its tags (doesn't recompute ReplayGain) for when 'video edit' 
changed the artist/title but the video's actual content is unchanged. Pass
--force to fully re-extract for when the source video's actual content changed
(e.g., a re-fetch). --retag and --force are mutually exclusive.

If more than one derived audio file for the video already exists (because a 
previous re-extraction's stale file wasn't cleaned up, or manual tampering) is 
an error (even with --force) so you need resolve it manually first.`,
	Args: cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// Restrict file completion to supported video extensions.
		return []string{"mp4", "webm", "mkv"}, cobra.ShellCompDirectiveFilterFileExt
	},
	RunE: runVideoExtractAudio,
}

func init() {
	videoExtractAudioCmd.Flags().Bool("retag", false, "Update only the derived audio file's tags without re-encoding")
	videoExtractAudioCmd.Flags().Bool("force", false, "Fully re-extract even if a derived audio file already exists")
	videoCmd.AddCommand(videoExtractAudioCmd)
}

func runVideoExtractAudio(cmd *cobra.Command, args []string) error {
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

	retag, _ := cmd.Flags().GetBool("retag")
	force, _ := cmd.Flags().GetBool("force")
	if retag && force {
		return fmt.Errorf("--retag and --force are mutually exclusive")
	}

	dst, err := video.ExtractAudio(context.Background(), absPath, *nfo, video.ExtractAudioOptions{
		Retag: retag,
		Force: force,
	})
	if err != nil {
		return fmt.Errorf("extract-audio: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintln(out)
	fmt.Fprintf(out, "video: %s\n", absPath)
	switch {
	case retag:
		fmt.Fprintf(out, "audio retagged: %s\n", dst)
	case force:
		fmt.Fprintf(out, "audio re-extracted: %s\n", dst)
	default:
		fmt.Fprintf(out, "audio extracted: %s\n", dst)
	}
	return nil
}
