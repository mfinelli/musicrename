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
	"github.com/mfinelli/musicrename/internal/target"
)

var playlistCreateCmd = &cobra.Command{
	Use:   "create <name> [library-root-root]",
	Short: "Scaffold a new library-wide playlist file",
	Long: `Creates a new file under library-root-root's playlists/ tree with a
#PLAYLIST: directive set to name (and a #TARGETS: directive, if --targets is
given) and no entries. 

An existing file at the destination is an error; this command never overwrites
a file.

If playlists/sums.md5 already exists, the new file's entry is added to it.

If library-root-root is omitted it defaults to the current working
directory.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runPlaylistCreate,
}

func init() {
	playlistCreateCmd.Flags().String("targets", "",
		"Comma-separated target names (e.g. ipod,sdcard); omit the flag entirely for every target")
	playlistCmd.AddCommand(playlistCreateCmd)
}

func runPlaylistCreate(cmd *cobra.Command, args []string) error {
	name := args[0]

	root := "."
	if len(args) > 1 {
		root = args[1]
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", root, err)
	}

	targets, err := parseTargetsFlag(cmd, "targets")
	if err != nil {
		return err
	}

	path, warning, err := playlist.Create(absRoot, playlist.CreateOptions{Name: name, Targets: targets})
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Created %s\n", relPlaylistPath(absRoot, path))
	if warning != "" {
		fmt.Fprintln(out, renameWarningStyle.Render("⚠ "+warning))
	}
	return nil
}

// parseTargetsFlag reads and validates a comma-separated target-name flag.
// Returns nil (meaning "omit the #TARGETS: directive entirely") if the flag
// was never set on the command line at all which is distinct from an
// explicitly empty value, which returns a non-nil empty slice (an explicit
// "applies to no target" state), matching playlist.GlobalPlaylist's
// Has*-paired field semantics. An unrecognized target name is an error naming
// the valid set.
func parseTargetsFlag(cmd *cobra.Command, flag string) ([]string, error) {
	if !cmd.Flags().Changed(flag) {
		return nil, nil
	}

	raw, _ := cmd.Flags().GetString(flag)
	targets := []string{}
	for _, t := range strings.Split(raw, ",") {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if !target.Valid(t) {
			return nil, fmt.Errorf(
				"unknown target %q; valid targets are: %s",
				t, strings.Join(target.Names, ", "),
			)
		}
		targets = append(targets, t)
	}
	return targets, nil
}
