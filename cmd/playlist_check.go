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
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/checker"
)

var playlistCheckCmd = &cobra.Command{
	Use:   "check [library-root-root]",
	Short: "Audit the library-wide playlists/ tree for broken references",
	Long: `Scans the playlists/ tree under library-root-root —
playlists/*.m3u8 (applies to every target) and playlists/{target}/*.m3u8
(target-specific) — and reports:

  - An entry whose path does not resolve to an actual file anywhere under
    library-root-root.
  - Two or more playlist files sharing the same #NAVIDROME-ID directive, 
    which would otherwise silently correlate to the same remote Navidrome
    playlist.

This does not check album-local target manifests (ipod.m3u8, sdcard.m3u8,
etc., inside an album directory) which are audited by 'musicrename check'
instead, since they follow that command's per-album scope model rather than
this one.

If library-root-root is omitted it defaults to the current working directory.

Exits with a non-zero status code when any findings are present, enabling use
in scripts.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runPlaylistCheck,
}

func init() {
	playlistCmd.AddCommand(playlistCheckCmd)
}

func runPlaylistCheck(cmd *cobra.Command, args []string) error {
	root := "."
	if len(args) > 0 {
		root = args[0]
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", root, err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, renameHeaderStyle.Render("Checking playlists..."))
	fmt.Fprintln(out)

	result, err := checker.CheckPlaylists(absRoot)
	if err != nil {
		return err
	}

	if len(result.Warnings) == 0 {
		fmt.Fprintln(out, "  "+checkOKStyle.Render("✓  No issues found."))
		fmt.Fprintln(out)
	} else {
		for _, w := range result.Warnings {
			relPath, relErr := filepath.Rel(absRoot, w.Path)
			if relErr != nil || strings.HasPrefix(relPath, "..") {
				relPath = w.Path
			}
			fmt.Fprintln(out, "  "+checkFindingStyle.Render("⚠  "+relPath+": "+w.Message))
		}
		fmt.Fprintln(out)
	}

	checkPrintSummary(out, 1, "playlist tree", len(result.Warnings))

	if result.HasWarnings() {
		os.Exit(1)
	}
	return nil
}
