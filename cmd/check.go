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

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/checker"
)

var (
	checkOKStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))  // green
	checkFindingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // bright yellow, matches renameWarningStyle
)

var checkCmd = &cobra.Command{
	Use:   "check [path]",
	Short: "Audit the library for metadata and naming issues",
	Long: `Scans the target path and reports any metadata or structural issues.

If path is an audio file (.flac, .mp3, .m4a), only per-track checks are run
(missing tags, ReplayGain, embedded artwork). Directory-level checks such as
artwork presence and sums.md5 are skipped.

If path is a directory that directly contains audio files it is treated as a
single album and all checks run, except path-conformance (which requires a
library root).

If path is a directory with no audio files directly inside it is treated as a
library root and the full check suite runs on every album found within it,
including path-conformance checks.

If path is omitted it defaults to the current working directory.

Exits with a non-zero status code when any findings are present, enabling use
in scripts.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runCheck,
}

func init() {
	rootCmd.AddCommand(checkCmd)
}

func runCheck(cmd *cobra.Command, args []string) error {
	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", path, err)
	}

	out := cmd.OutOrStdout()

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("could not access %q: %w", path, err)
	}

	if !info.IsDir() {
		// Single audio file mode.
		ext := strings.ToLower(filepath.Ext(absPath))
		switch ext {
		case ".flac", ".mp3", ".m4a":
			return runCheckTrack(out, absPath)
		default:
			return fmt.Errorf(
				"%q is not a supported audio file (expected .flac, .mp3, or .m4a)",
				filepath.Base(absPath),
			)
		}
	}

	// Directory mode: check whether the directory directly contains audio to
	// decide between single-album and library mode. sumsIsAlbumRoot is defined
	// in sums.go and performs the same heuristic used by the sums command.
	isAlbum, err := sumsIsAlbumRoot(absPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", absPath, err)
	}

	if isAlbum {
		return runCheckAlbum(out, absPath)
	}
	return runCheckLibrary(out, absPath)
}

// runCheckLibrary runs the full check suite on a library root directory. The
// library root is known so path-conformance checks are performed on every album.
func runCheckLibrary(out io.Writer, root string) error {
	fmt.Fprintln(out, renameHeaderStyle.Render("Checking library..."))
	fmt.Fprintln(out)

	result, err := checker.CheckLibrary(root)
	if err != nil {
		return err
	}

	total := checkPrintFindings(out, result, root)
	checkPrintSummary(out, len(result.Albums), "album", total)

	if result.HasWarnings() {
		os.Exit(1)
	}
	return nil
}

// runCheckAlbum runs all checks on a single album directory. Path-conformance
// is skipped because no library root is available from the command line.
func runCheckAlbum(out io.Writer, albumPath string) error {
	fmt.Fprintln(out, renameHeaderStyle.Render("Checking album..."))
	fmt.Fprintln(out)

	ar, err := checker.CheckAlbum(albumPath, "")
	if err != nil {
		return err
	}
	result := &checker.Result{Albums: []checker.AlbumResult{*ar}}

	// Use the album's parent as the display root so the header shows the
	// album's base name (e.g. "[2000] album") rather than a "." or full path.
	total := checkPrintFindings(out, result, filepath.Dir(albumPath))
	checkPrintSummary(out, 1, "album", total)

	if result.HasWarnings() {
		os.Exit(1)
	}
	return nil
}

// runCheckTrack runs track-level checks only on a single audio file.
// Directory-level checks are skipped because album context is unavailable.
func runCheckTrack(out io.Writer, filePath string) error {
	fmt.Fprintln(out, renameHeaderStyle.Render("Checking track..."))
	fmt.Fprintln(out)

	ar, err := checker.CheckTrack(filePath)
	if err != nil {
		return err
	}
	result := &checker.Result{Albums: []checker.AlbumResult{*ar}}

	total := checkPrintFindings(out, result, filepath.Dir(filePath))
	checkPrintSummary(out, 1, "track", total)

	if result.HasWarnings() {
		os.Exit(1)
	}
	return nil
}

// checkPrintFindings iterates over every album in result that has at least one
// warning and prints them grouped under the album path. Paths are shown
// relative to displayRoot where possible. Returns the total finding count.
func checkPrintFindings(out io.Writer, result *checker.Result, displayRoot string) int {
	total := 0

	for _, ar := range result.Albums {
		if len(ar.Warnings) == 0 {
			continue
		}
		total += len(ar.Warnings)

		// Album header: relative path from the display root.
		relPath, err := filepath.Rel(displayRoot, ar.AlbumPath)
		if err != nil || strings.HasPrefix(relPath, "..") {
			relPath = ar.AlbumPath
		}
		fmt.Fprintln(out, "  "+renameAlbumStyle.Render(relPath))

		for _, w := range ar.Warnings {
			// For album-level warnings (the warning's path is the album
			// directory itself) use "[album]" as the label so it's clear
			// the finding is not about a specific file. For track-level
			// warnings show just the base filename to keep lines short.
			var pathLabel string
			if w.Path == ar.AlbumPath {
				pathLabel = "[album]"
			} else {
				pathLabel = filepath.Base(w.Path)
			}
			fmt.Fprintln(out, "    "+checkFindingStyle.Render("⚠  "+pathLabel+": "+w.Message))
		}

		fmt.Fprintln(out)
	}

	if total == 0 {
		fmt.Fprintln(out, "  "+checkOKStyle.Render("✓  No issues found."))
		fmt.Fprintln(out)
	}

	return total
}

// checkPrintSummary writes the horizontal rule and the summary line at the
// bottom of check output. unitLabel should be "album", "track", etc. and is
// automatically pluralised.
func checkPrintSummary(out io.Writer, unitCount int, unitLabel string, findings int) {
	unit := unitLabel
	if unitCount != 1 {
		unit += "s"
	}
	finding := "finding"
	if findings != 1 {
		finding += "s"
	}
	summaryText := fmt.Sprintf("%d %s · %d %s", unitCount, unit, findings, finding)
	fmt.Fprintln(out, renameRuleStyle.Render(strings.Repeat("─", len(summaryText))))
	fmt.Fprintln(out, renameBoldStyle.Render(summaryText))
}
