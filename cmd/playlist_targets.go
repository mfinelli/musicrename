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

	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/playlist"
)

var playlistTargetsCmd = &cobra.Command{
	Use:   "targets <playlist>",
	Short: "Edit a library-wide playlist's #TARGETS: scope",
	Long: `Rewrites playlist's #TARGETS: directive: --set replaces it with the given
comma-separated target list (an empty value is a valid and means "applies
to no target" which is distinct from omitting the flag); --clear removes the
directive entirely, meaning the playlist applies to every target again.
Exactly one of --set/--clear is required.

playlist must already exist; this command never creates a new file (use
'playlist create' for that).`,
	Args: cobra.ExactArgs(1),
	RunE: runPlaylistTargets,
}

func init() {
	playlistTargetsCmd.Flags().String("set", "", "Comma-separated target names to set (may be empty)")
	playlistTargetsCmd.Flags().Bool("clear", false, "Remove the #TARGETS: directive entirely")
	playlistCmd.AddCommand(playlistTargetsCmd)
}

func runPlaylistTargets(cmd *cobra.Command, args []string) error {
	setGiven := cmd.Flags().Changed("set")
	clear, _ := cmd.Flags().GetBool("clear")

	switch {
	case setGiven && clear:
		return fmt.Errorf("--set and --clear are mutually exclusive")
	case !setGiven && !clear:
		return fmt.Errorf("exactly one of --set or --clear is required")
	}

	path, err := filepath.Abs(args[0])
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", args[0], err)
	}

	// --clear leaves targets as nil (removes the directive); --set
	// resolves it via the same validating parser 'playlist create' uses.
	var targets []string
	if setGiven {
		targets, err = parseTargetsFlag(cmd, "set")
		if err != nil {
			return err
		}
	}

	absRoot := playlist.LibraryRootRootFor(path)
	warning, err := playlist.SetTargets(absRoot, path, targets)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Updated %s\n", relPlaylistPath(absRoot, path))
	if warning != "" {
		fmt.Fprintln(out, renameWarningStyle.Render("⚠ "+warning))
	}
	return nil
}
