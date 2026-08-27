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

	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/video"
)

var videoCheckCmd = &cobra.Command{
	Use:   "check [path]",
	Short: "Audit the video library for metadata and naming issues",
	Long: `Scans path and reports any metadata or structural issues.

Checks performed on each video directory:
  - exactly one recognized video file is present (not zero, not several)
  - musicvideo.nfo exists
  - musicvideo.nfo has a non-empty title and artist
  - the directory's location matches what 'video add'/'video rename' would
    compute from the nfo (skipped in single-video mode, since no video-root
    is available to compute against)
  - sums.md5 exists

If path directly contains a video file it is treated as a single video's
directory and only that directory is checked. Otherwise path is treated as a
video-root and every video directory found within it is checked.

If path is omitted it defaults to the current working directory.

Exits with a non-zero status code when any findings are present.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runVideoCheck,
}

func init() {
	videoCmd.AddCommand(videoCheckCmd)
}

func runVideoCheck(cmd *cobra.Command, args []string) error {
	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", path, err)
	}

	out := cmd.OutOrStdout()

	// dirIsVideoDir is defined in video_sums.go and reused here for the
	// same single-vs-root mode decision sums makes.
	isVideoDir, err := dirIsVideoDir(absPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", absPath, err)
	}

	if isVideoDir {
		return runVideoCheckSingle(out, absPath)
	}
	return runVideoCheckRoot(out, absPath)
}

// runVideoCheckSingle checks a single video directory. Path-conformance is
// skipped because no video-root is available from the command line.
func runVideoCheckSingle(out io.Writer, dir string) error {
	fmt.Fprintln(out, renameHeaderStyle.Render("Checking video..."))
	fmt.Fprintln(out)

	result, err := video.Check(dir, "")
	if err != nil {
		return err
	}

	// filepath.Dir(dir) as the display root shows dir's base name in
	// the findings header (e.g. "title") rather than "." or a full path,
	// mirroring check.go's runCheckAlbum.
	total := videoCheckPrintFindings(out, result, filepath.Dir(dir))
	checkPrintSummary(out, 1, "video", total)

	if result.HasWarnings() {
		os.Exit(1)
	}
	return nil
}

// runVideoCheckRoot runs the full check suite on every video directory
// found under root, including path-conformance.
func runVideoCheckRoot(out io.Writer, root string) error {
	fmt.Fprintln(out, renameHeaderStyle.Render("Checking video library..."))
	fmt.Fprintln(out)

	result, err := video.CheckAll(root)
	if err != nil {
		return err
	}

	total := videoCheckPrintFindings(out, result, root)
	checkPrintSummary(out, result.Checked, "video", total)

	if result.HasWarnings() {
		os.Exit(1)
	}
	return nil
}

// videoCheckPrintFindings groups result's warnings by their video directory
// and prints them, with paths shown relative to displayRoot where possible.
// Mirrors check.go's checkPrintFindings, adapted to video's flatter
// Warning-only shape (no per-album grouping type to iterate). Groups are
// printed in the order their first warning appears in result.Warnings, which
// is directory-sorted order. Returns the total finding count.
func videoCheckPrintFindings(out io.Writer, result *video.CheckResult, displayRoot string) int {
	if len(result.Warnings) == 0 {
		fmt.Fprintln(out, "  "+checkOKStyle.Render("✓  No issues found."))
		fmt.Fprintln(out)
		return 0
	}

	var order []string
	grouped := make(map[string][]video.Warning)
	for _, w := range result.Warnings {
		if _, ok := grouped[w.Path]; !ok {
			order = append(order, w.Path)
		}
		grouped[w.Path] = append(grouped[w.Path], w)
	}

	for _, path := range order {
		relPath, err := filepath.Rel(displayRoot, path)
		if err != nil || strings.HasPrefix(relPath, "..") {
			relPath = path
		}
		fmt.Fprintln(out, "  "+renameAlbumStyle.Render(relPath))

		for _, w := range grouped[path] {
			fmt.Fprintln(out, "    "+checkFindingStyle.Render("⚠  "+w.Message))
		}
		fmt.Fprintln(out)
	}

	return len(result.Warnings)
}
