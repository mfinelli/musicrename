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

	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/playlist"
)

var playlistEntriesDedupeCmd = &cobra.Command{
	Use:   "dedupe <playlist>",
	Short: "Remove duplicate entries from a library-wide playlist",
	Long: `Removes duplicate entries from playlist, keeping each one's first
occurrence; every other entry's relative order is left unchanged.

--check and --dry-run are mutually exclusive: --check exits non-zero if any 
duplicates are found; --dry-run previews what would be removed.

playlist must already exist.`,
	Args: cobra.ExactArgs(1),
	RunE: runPlaylistEntriesDedupe,
}

func init() {
	playlistEntriesDedupeCmd.Flags().Bool(
		"check", false, "Report whether duplicates exist, without making any changes",
	)
	playlistEntriesDedupeCmd.Flags().Bool("dry-run", false, "Preview what would be removed without changing the file")
	playlistEntriesCmd.AddCommand(playlistEntriesDedupeCmd)
}

func runPlaylistEntriesDedupe(cmd *cobra.Command, args []string) error {
	path, err := filepath.Abs(args[0])
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", args[0], err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		return fmt.Errorf("%s: %w", path, statErr)
	}
	absRoot := playlist.LibraryRootRootFor(path)

	check, _ := cmd.Flags().GetBool("check")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if check && dryRun {
		return fmt.Errorf("--check and --dry-run are mutually exclusive")
	}

	gp, err := playlist.ReadGlobalPlaylist(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	out := cmd.OutOrStdout()
	if len(gp.Entries) == 0 {
		fmt.Fprintln(out, "No entries in this playlist.")
		return nil
	}

	deduped, removed := playlist.DedupeEntries(gp.Entries)

	if removed == 0 {
		fmt.Fprintln(out, "No duplicates found.")
		return nil
	}

	if check {
		fmt.Fprintf(out, "%d duplicate entries found\n", removed)
		os.Exit(1)
	}

	if dryRun {
		fmt.Fprintf(out, "Would remove %d duplicate entries\n", removed)
		return nil
	}

	warning, err := playlist.SetEntries(absRoot, path, deduped)
	if err != nil {
		return err
	}
	if warning != "" {
		fmt.Fprintln(out, renameWarningStyle.Render("⚠ "+warning))
	}
	fmt.Fprintf(out, "Removed %d duplicate entries; %d remaining\n", removed, len(deduped))
	return nil
}
