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

var playlistEntriesAddCmd = &cobra.Command{
	Use:   "add <playlist> <path>...",
	Short: "Append one or more tracks to a library-wide playlist",
	Long: `Appends each given path, in order, to playlist's entry list, then rewrites
the file.

Each path may be given relative to the current working directory or as an
absolute path; either way it's resolved and stored relative to the library
root (derived from playlist's own location under playlists/).

A path that doesn't resolve to a real file under the library root is
skipped and reported.

playlist must already exist (use 'playlist create' to scaffold a new one
first).`,
	Args: cobra.MinimumNArgs(2),
	RunE: runPlaylistEntriesAdd,
}

func init() {
	playlistEntriesCmd.AddCommand(playlistEntriesAddCmd)
}

func runPlaylistEntriesAdd(cmd *cobra.Command, args []string) error {
	path, err := filepath.Abs(args[0])
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", args[0], err)
	}
	absRoot := playlist.LibraryRootRootFor(path)

	relPaths := make([]string, 0, len(args)-1)
	for _, raw := range args[1:] {
		abs, err := filepath.Abs(raw)
		if err != nil {
			return fmt.Errorf("could not resolve path %q: %w", raw, err)
		}
		rel, err := filepath.Rel(absRoot, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%s is outside the library root (%s)", raw, absRoot)
		}
		relPaths = append(relPaths, rel)
	}

	added, warnings, err := playlist.AddEntries(absRoot, path, relPaths)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	for _, a := range added {
		fmt.Fprintln(out, "  + "+a)
	}
	for _, w := range warnings {
		fmt.Fprintln(out, renameWarningStyle.Render("⚠ "+w))
	}
	fmt.Fprintf(out, "%d added, %d warning(s)\n", len(added), len(warnings))
	return nil
}
