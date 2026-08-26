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
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/video"
)

var videoFetchCmd = &cobra.Command{
	Use:   "fetch <url> [destination]",
	Short: "Download a video via yt-dlp and write a generated info.txt",
	Long: `Cleans the given URL down to its bare video id (discarding playlist,
timestamp, and tracking query parameters), downloads it with yt-dlp, and
writes info.txt from the resulting metadata (url, title, uploader, upload
date, and description).

This is a standalone download step: it does not prompt for artist/title and
does not file the video into the library. Run 'mrr video add' on the
downloaded file afterward for that.

If destination is omitted it defaults to the current working directory.
Requires yt-dlp to be installed and available on PATH.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runVideoFetch,
}

func init() {
	videoCmd.AddCommand(videoFetchCmd)
}

func runVideoFetch(cmd *cobra.Command, args []string) error {
	rawURL := args[0]

	dir := "."
	if len(args) > 1 {
		dir = args[1]
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", dir, err)
	}
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", absDir, err)
	}

	result, err := video.Fetch(context.Background(), rawURL, absDir)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintln(out)
	fmt.Fprintf(out, "video: %s\n", result.VideoPath)
	fmt.Fprintf(out, "info:  %s\n", result.InfoPath)
	return nil
}
