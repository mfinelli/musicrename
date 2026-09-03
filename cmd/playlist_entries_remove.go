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

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/completion"
	"github.com/mfinelli/musicrename/internal/playlist"
)

var playlistEntriesRemoveCmd = &cobra.Command{
	Use:   "remove <playlist>",
	Short: "Remove one or more tracks from a library-wide playlist",
	Long: `With neither --artist nor --album given, shows every current entry as a
pre-checked checkbox (like 'playlist select'); uncheck the ones to remove,
then confirm. With either flag given, matches non-interactively instead:
every entry whose resolved track's tags match all the given flags
(case-insensitive) is removed.

Reading tags is scoped to this playlist's own entries only. An entry
that doesn't resolve to a real file, or whose tags can't be read, is shown
but never auto-matched by --artist/--album (since there's nothing to match
against); it stays unless explicitly unchecked in interactive mode.

playlist must already exist.`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completion.PlaylistArg,
	RunE:              runPlaylistEntriesRemove,
}

func init() {
	playlistEntriesRemoveCmd.Flags().String("artist", "", "Remove entries whose ARTIST tag matches (case-insensitive)")
	playlistEntriesRemoveCmd.Flags().String("album", "", "Remove entries whose ALBUM tag matches (case-insensitive)")
	playlistEntriesRemoveCmd.Flags().Bool("dry-run", false, "Preview what would be removed without changing the file")
	playlistEntriesCmd.AddCommand(playlistEntriesRemoveCmd)
}

func runPlaylistEntriesRemove(cmd *cobra.Command, args []string) error {
	path, err := filepath.Abs(args[0])
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", args[0], err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		return fmt.Errorf("%s: %w", path, statErr)
	}
	absRoot := playlist.LibraryRootRootFor(path)

	gp, err := playlist.ReadGlobalPlaylist(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	out := cmd.OutOrStdout()
	if len(gp.Entries) == 0 {
		fmt.Fprintln(out, "No entries in this playlist.")
		return nil
	}

	// Resolving tags is a real per-file open, and a playlist can run to
	// thousands of entries, so render an in-place progress indicator
	// rather than leaving the terminal silent for that whole stretch
	// (mirrors playlist_sums.go's \r\033[K progress style exactly,
	// including its "do nothing when not a terminal" behavior).
	isTTY := isatty.IsTerminal(os.Stdout.Fd())
	var progress func(rel string)
	if isTTY {
		progress = func(rel string) {
			fmt.Fprintf(out, "\r\033[K  reading tags... %s", rel)
		}
	}
	rows := playlist.ResolveEntryRows(absRoot, gp.Entries, progress)
	if isTTY {
		fmt.Fprint(out, "\r\033[K")
	}

	artist, _ := cmd.Flags().GetString("artist")
	album, _ := cmd.Flags().GetString("album")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	var kept, removed []string
	if artist != "" || album != "" {
		kept, removed = playlist.FilterEntryRows(rows, artist, album)
	} else {
		kept, removed, err = selectPlaylistEntryRows(rows, path)
		if err != nil {
			return err
		}
	}

	if len(removed) == 0 {
		fmt.Fprintln(out, "Nothing to remove.")
		return nil
	}

	if dryRun {
		fmt.Fprintln(out, "Would remove:")
		for _, r := range removed {
			fmt.Fprintln(out, "  - "+r)
		}
		return nil
	}

	warning, err := playlist.SetEntries(absRoot, path, kept)
	if err != nil {
		return err
	}

	for _, r := range removed {
		fmt.Fprintln(out, "  - "+r)
	}
	if warning != "" {
		lipgloss.Fprintln(out, renameWarningStyle.Render("⚠ "+warning))
	}
	fmt.Fprintf(out, "%d removed\n", len(removed))
	return nil
}

// selectPlaylistEntryRows shows every row as a pre-checked checkbox (every
// current entry is kept unless explicitly unchecked) and returns (kept,
// removed) in the playlist's original entry order, and unaffected by
// duplicate entries (matched by index, since a playlist may legitimately
// reference the same path twice). Rows flagged [playlist.EntryRow.Missing] get
// warning styling applied here; the internal/playlist package itself stays
// terminal-agnostic and returns plain-text labels.
func selectPlaylistEntryRows(rows []playlist.EntryRow, path string) (kept, removed []string, err error) {
	options := make([]huh.Option[int], 0, len(rows))
	for i, r := range rows {
		label := r.Label
		if r.Missing {
			label = renameWarningStyle.Render("⚠  " + label)
		}
		options = append(options, huh.NewOption(label, i).Selected(true))
	}

	var selected []int
	field := huh.NewMultiSelect[int]().
		Title(fmt.Sprintf("Remove entries from %s", filepath.Base(path))).
		Options(options...).
		Value(&selected)

	if err := huh.NewForm(huh.NewGroup(field)).Run(); err != nil {
		return nil, nil, fmt.Errorf("prompting: %w", err)
	}

	selectedSet := make(map[int]bool, len(selected))
	for _, i := range selected {
		selectedSet[i] = true
	}

	for i, r := range rows {
		if selectedSet[i] {
			kept = append(kept, r.Rel)
		} else {
			removed = append(removed, r.Rel)
		}
	}
	return kept, removed, nil
}
