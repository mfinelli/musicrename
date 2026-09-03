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

	"charm.land/lipgloss/v2"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/hasher"
	"github.com/mfinelli/musicrename/internal/playlist"
)

var playlistSumsCmd = &cobra.Command{
	Use:   "sums [library-root-root]",
	Short: "Generate sums.md5 checksums for the library-wide playlists/ tree",
	Long: `Computes MD5 checksums for every file under library-root-root's
playlists/ directory and writes them to playlists/sums.md5 in a format 
compatible with 'md5sum -c'.

Unlike album or video sums.md5, which are one-per-directory, this is a single
checksum file covering the entire playlists/ tree at once (there is no
library-wide-vs-single-item distinction to make here).

If library-root-root is omitted it defaults to the current working directory.

An existing playlists/sums.md5 is always an error unless --force is passed.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runPlaylistSums,
}

func init() {
	playlistSumsCmd.Flags().Bool("force", false, "Overwrite an existing playlists/sums.md5")
	playlistCmd.AddCommand(playlistSumsCmd)
}

func runPlaylistSums(cmd *cobra.Command, args []string) error {
	root := "."
	if len(args) > 0 {
		root = args[0]
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", root, err)
	}

	force, _ := cmd.Flags().GetBool("force")
	out := cmd.OutOrStdout()
	isTTY := isatty.IsTerminal(os.Stdout.Fd())

	dir := playlist.Dir(absRoot)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		fmt.Fprintln(out, "No playlists found.")
		return nil
	} else if err != nil {
		return fmt.Errorf("reading %s: %w", dir, err)
	}

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
