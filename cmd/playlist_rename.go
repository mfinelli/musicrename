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

	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/playlist"
)

var playlistRenameCmd = &cobra.Command{
	Use:   "rename [library-root-root]",
	Short: "Rename playlists/ tree files to match their #PLAYLIST directive",
	Long: `Scans the playlists/ tree under library-root-root and renames each file to a 
filesystem-safe name derived from its #PLAYLIST: directive.

A file with no #PLAYLIST: directive, or whose name sanitizes to an empty
string, is left alone and reported as skipped rather than treated as an
error. A file already at its correctly-sanitized name produces no output at
all.

If library-root-root is omitted it defaults to the current working
directory.

With --dry-run, prints the planned renames without touching any files.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runPlaylistRename,
}

func init() {
	playlistRenameCmd.Flags().Bool("dry-run", false, "Print planned renames without making any changes")
	playlistCmd.AddCommand(playlistRenameCmd)
}

func runPlaylistRename(cmd *cobra.Command, args []string) error {
	root := "."
	if len(args) > 0 {
		root = args[0]
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", root, err)
	}

	ops, skipped, err := playlist.PlanRenames(absRoot)
	if err != nil {
		return fmt.Errorf("planning renames: %w", err)
	}

	out := cmd.OutOrStdout()
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	if dryRun {
		fmt.Fprintln(out, renameHeaderStyle.Render("Dry run: no files will be renamed."))
	} else {
		fmt.Fprintln(out, renameHeaderStyle.Render("Renaming playlists..."))
	}
	fmt.Fprintln(out)

	if len(skipped) > 0 {
		fmt.Fprintln(out, renameWarningStyle.Render(fmt.Sprintf("⚠  %d skipped", len(skipped))))
		for _, s := range skipped {
			fmt.Fprintln(out, "   "+renameWarningStyle.Render(relPlaylistPath(absRoot, s.Path)+": "+s.Message))
		}
		fmt.Fprintln(out)
	}

	if len(ops) == 0 {
		fmt.Fprintln(out, "Nothing to rename.")
		return nil
	}

	for _, op := range ops {
		arrow := renameArrowStyle.Render("→")
		line := fmt.Sprintf("  %s  %s  %s",
			relPlaylistPath(absRoot, op.OldPath), arrow,
			renameNewPathStyle.Render(relPlaylistPath(absRoot, op.NewPath)),
		)
		fmt.Fprintln(out, line)
	}
	fmt.Fprintln(out)

	var execWarnings []string
	if !dryRun {
		execWarnings, err = playlist.ExecuteRenames(ops)
		if err != nil {
			return fmt.Errorf("executing renames: %w", err)
		}
		for _, w := range execWarnings {
			fmt.Fprintln(out, renameWarningStyle.Render("⚠  "+w))
		}
		if len(execWarnings) > 0 {
			fmt.Fprintln(out)
		}
	}

	summaryText := fmt.Sprintf(
		"%d renames · %d skipped · %d warnings",
		len(ops), len(skipped), len(execWarnings),
	)
	fmt.Fprintln(out, renameRuleStyle.Render(strings.Repeat("─", len(summaryText))))
	fmt.Fprintln(out, renameBoldStyle.Render(summaryText))

	return nil
}

// relPlaylistPath returns path relative to absRoot for display purposes,
// falling back to the absolute path if it can't be made relative (e.g., path
// somehow falls outside absRoot).
func relPlaylistPath(absRoot, path string) string {
	rel, err := filepath.Rel(absRoot, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return rel
}
