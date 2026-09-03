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
	"os"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/hasher"
	"github.com/mfinelli/musicrename/internal/video"
)

var videoSumsCmd = &cobra.Command{
	Use:   "sums [path]",
	Short: "Generate sums.md5 checksums for a video or video-root",
	Long: `Computes MD5 checksums for all files in a video's directory and writes
them to sums.md5 in a format compatible with 'md5sum -c'. The video file is
hashed in binary format; musicvideo.nfo and info.txt (if present) are hashed
in text format.

If path directly contains a video file it is treated as a single video's
directory and only that directory is processed. Otherwise path is treated as
a video-root and every video directory within it is processed recursively.

If path is omitted it defaults to the current working directory.

In single-video mode, an existing sums.md5 is always an error unless --force
is passed. In video-root mode, video directories that already have a
sums.md5 are silently skipped; pass --force to regenerate them all.`,
	Args: cobra.MaximumNArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// path here is always a directory (a single video's
		// directory, or a video-root) and not the video file itself.
		return nil, cobra.ShellCompDirectiveFilterDirs
	},
	RunE: runVideoSums,
}

func init() {
	videoSumsCmd.Flags().Bool("force", false, "Overwrite existing sums.md5 files")
	videoCmd.AddCommand(videoSumsCmd)
}

func runVideoSums(cmd *cobra.Command, args []string) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", dir, err)
	}

	force, _ := cmd.Flags().GetBool("force")
	out := cmd.OutOrStdout()
	isTTY := isatty.IsTerminal(os.Stdout.Fd())

	isVideoDir, err := dirIsVideoDir(absDir)
	if err != nil {
		return fmt.Errorf("reading %s: %w", absDir, err)
	}

	if isVideoDir {
		return runVideoSumsSingle(out, absDir, force, isTTY)
	}
	return runVideoSumsRoot(out, absDir, force, isTTY)
}

// dirIsVideoDir reports whether dir directly contains at least one video
// file, matching the same heuristic used elsewhere in internal/video
// (dirHasVideoFile), reimplemented here at the cmd layer purely for the
// mode-detection decision, mirroring how sums.go's sumsIsAlbumRoot avoids
// importing an unexported variable from another package.
func dirIsVideoDir(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if !e.IsDir() && video.IsVideoExt(strings.ToLower(filepath.Ext(e.Name()))) {
			return true, nil
		}
	}
	return false, nil
}

// runVideoSumsSingle generates sums.md5 for a single video's directory. It
// refuses to proceed if sums.md5 already exists unless force is true.
func runVideoSumsSingle(out io.Writer, dir string, force, isTTY bool) error {
	sumsPath := filepath.Join(dir, hasher.SumsFilename)
	if _, err := os.Stat(sumsPath); err == nil && !force {
		return fmt.Errorf("%s already exists; use --force to regenerate", sumsPath)
	}

	lipgloss.Fprintln(out, renameHeaderStyle.Render("Hashing files..."))
	fmt.Fprintln(out)

	var count int
	progress := func(rel string) {
		count++
		if isTTY {
			fmt.Fprintf(out, "\r\033[K  → %s", rel)
		}
	}

	if err := hasher.Hash(dir, progress); err != nil {
		if isTTY {
			fmt.Fprint(out, "\r\033[K")
		}
		return fmt.Errorf("hashing %s: %w", dir, err)
	}

	if isTTY {
		fmt.Fprint(out, "\r\033[K")
	}

	lipgloss.Fprintln(out, sumsCheckStyle.Render(
		fmt.Sprintf("✓  sums.md5 written — %d %s", count, pluralFiles(count)),
	))
	return nil
}

// runVideoSumsRoot generates sums.md5 for every video directory found under
// dir. Directories that already have a sums.md5 are skipped unless force is
// true.
func runVideoSumsRoot(out io.Writer, dir string, force, isTTY bool) error {
	dirs, err := video.FindVideoDirs(dir)
	if err != nil {
		return fmt.Errorf("scanning video root: %w", err)
	}

	lipgloss.Fprintln(out, renameHeaderStyle.Render("Generating checksums..."))
	fmt.Fprintln(out)

	if len(dirs) == 0 {
		fmt.Fprintln(out, "No videos found.")
		return nil
	}

	var generated, skipped int

	for _, videoDir := range dirs {
		relPath, err := filepath.Rel(dir, videoDir)
		if err != nil {
			relPath = videoDir
		}

		// Print the video's path before processing so the user can see
		// which one is active while hashing runs below it.
		lipgloss.Fprintln(out, "  "+renameAlbumStyle.Render(relPath))

		sumsPath := filepath.Join(videoDir, hasher.SumsFilename)
		if _, err := os.Stat(sumsPath); err == nil && !force {
			lipgloss.Fprintln(out, "    "+sumsSkipStyle.Render("— skipped"))
			skipped++
			continue
		}

		var count int
		progress := func(rel string) {
			count++
			if isTTY {
				fmt.Fprintf(out, "\r\033[K    → %s", rel)
			}
		}

		if err := hasher.Hash(videoDir, progress); err != nil {
			if isTTY {
				fmt.Fprint(out, "\r\033[K")
			}
			return fmt.Errorf("hashing %s: %w", relPath, err)
		}

		if isTTY {
			fmt.Fprint(out, "\r\033[K")
		}

		lipgloss.Fprintln(out, "    "+sumsCheckStyle.Render(
			fmt.Sprintf("✓  %d %s", count, pluralFiles(count)),
		))
		generated++
	}

	fmt.Fprintln(out)
	summaryText := fmt.Sprintf(
		"%d videos · %d generated · %d skipped",
		len(dirs), generated, skipped,
	)
	lipgloss.Fprintln(out, renameRuleStyle.Render(strings.Repeat("─", len(summaryText))))
	lipgloss.Fprintln(out, renameBoldStyle.Render(summaryText))
	return nil
}
