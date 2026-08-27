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

import "github.com/spf13/cobra"

// videoCmd is the parent for the music-video command family.
// The video library is a tree kept completely separate from the audio
// library, so its subcommands live under their own namespace rather than
// alongside rename/sums/check/lyrics/inspect.
var videoCmd = &cobra.Command{
	Use:   "video",
	Short: "Manage a music-video library, kept separate from the audio library",
	Long: `Commands for managing music videos as a tree that is completely
separate from the audio library, with its own root path.

Intended workflow for adding a new video:
  mrr video fetch <url>   # download via yt-dlp, write info.txt
  mrr video add <file>    # sanitize, file into the video library, write musicvideo.nfo`,
}

func init() {
	rootCmd.AddCommand(videoCmd)
}
