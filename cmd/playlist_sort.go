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

	"charm.land/lipgloss/v2"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/completion"
	"github.com/mfinelli/musicrename/internal/playlist"
)

var playlistSortCmd = &cobra.Command{
	Use:   "sort <playlist> [fields]",
	Short: "Reorder a library-wide playlist by track metadata, or shuffle it",
	Long: `Reorders playlist's entries by fields, a comma-separated list of track
metadata fields in precedence order (the first breaks the most ties):
artist, albumartist, album, year, disc, track, title. An entry with no
value for the field being compared sorts after any entry that has one; an
entry that doesn't resolve to a real file at all sorts after every
resolvable one. Ties left unbroken by every given field keep their current
relative order.

With neither fields nor --shuffle given, reapplies whichever criteria were
last used on this playlist or errors if nothing has been remembered yet and
none is given now. Explicit fields or --shuffle always supersede and become
the new remembered criteria.

By default duplicate entries are removed when sorting. Pass --skip-dedupe to
turn this off.

playlist must already exist.`,
	Args: cobra.RangeArgs(1, 2),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return completion.PlaylistArg(cmd, args, toComplete)
		}
		// The [fields] argument: a comma-separated list drawn from
		// playlist.ValidSortFieldNames.
		return completion.CommaSeparated(playlist.ValidSortFieldNames)(cmd, args, toComplete)
	},
	RunE: runPlaylistSort,
}

func init() {
	playlistSortCmd.Flags().Bool("shuffle", false, "Randomize entry order instead of sorting by fields")
	playlistSortCmd.Flags().Bool("dry-run", false, "Preview the new order without changing the file or its remembered criteria")
	playlistSortCmd.Flags().Bool("skip-dedupe", false, "Do not remove duplicate entries before sorting")
	playlistCmd.AddCommand(playlistSortCmd)
}

func runPlaylistSort(cmd *cobra.Command, args []string) error {
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

	shuffle, _ := cmd.Flags().GetBool("shuffle")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	skipDedupe, _ := cmd.Flags().GetBool("skip-dedupe")
	fieldsGiven := len(args) > 1

	if fieldsGiven && shuffle {
		return fmt.Errorf("cannot combine explicit fields with --shuffle")
	}

	entries := gp.Entries
	var removedDupes int
	if !skipDedupe {
		entries, removedDupes = playlist.DedupeEntries(entries)
	}

	shuffleMode, fields, sortSpec, err := resolvePlaylistSortMode(gp, args, fieldsGiven, shuffle)
	if err != nil {
		return err
	}

	var newOrder []string
	if shuffleMode {
		newOrder = playlist.ShuffleEntries(entries)
	} else {
		// Resolving tags is a real per-file open, and a playlist can
		// run to thousands of entries, so render an in-place progress
		// indicator rather than leaving the terminal silent for that
		// whole stretch.
		isTTY := isatty.IsTerminal(os.Stdout.Fd())
		var progress func(rel string)
		if isTTY {
			progress = func(rel string) {
				fmt.Fprintf(out, "\r\033[K  reading tags... %s", rel)
			}
		}
		rows := playlist.ResolveEntryRows(absRoot, entries, progress)
		if isTTY {
			fmt.Fprint(out, "\r\033[K")
		}
		newOrder = playlist.SortEntries(rows, fields)
	}

	if dryRun {
		if removedDupes > 0 {
			fmt.Fprintf(out, "Would also remove %d duplicate entries\n", removedDupes)
		}
		fmt.Fprintln(out, "Would reorder to:")
		for _, r := range newOrder {
			fmt.Fprintln(out, "  "+r)
		}
		return nil
	}

	warning, err := playlist.ApplySort(absRoot, path, newOrder, sortSpec)
	if err != nil {
		return err
	}

	for _, r := range newOrder {
		fmt.Fprintln(out, "  "+r)
	}
	if warning != "" {
		lipgloss.Fprintln(out, renameWarningStyle.Render("⚠ "+warning))
	}
	if removedDupes > 0 {
		fmt.Fprintf(out, "Removed %d duplicate entries\n", removedDupes)
	}
	if shuffleMode {
		fmt.Fprintf(out, "Shuffled %d entries\n", len(newOrder))
	} else {
		fmt.Fprintf(out, "Sorted %d entries by %s\n", len(newOrder), strings.Join(sortSpec, ","))
	}
	return nil
}

// resolvePlaylistSortMode decides what this invocation actually does:
// explicit fields, explicit --shuffle, or reapplying whatever's remembered
// in gp's #SORT: directive when neither was given. sortSpec is what should
// be written back as the new #SORT: directive on success (for the
// reapply case, that's just gp.Sort unchanged, since nothing about the
// criteria themselves changed, only their result).
func resolvePlaylistSortMode(
	gp *playlist.GlobalPlaylist, args []string, fieldsGiven, shuffle bool,
) (shuffleMode bool, fields []playlist.SortField, sortSpec []string, err error) {
	switch {
	case fieldsGiven:
		fields, sortSpec, err = parseSortFields(args[1])
		return false, fields, sortSpec, err

	case shuffle:
		return true, nil, []string{"shuffle"}, nil

	case !gp.HasSort || len(gp.Sort) == 0:
		return false, nil, nil, fmt.Errorf(
			"no #SORT: directive stored on this playlist and no fields given; " +
				"specify fields or --shuffle",
		)

	case len(gp.Sort) == 1 && gp.Sort[0] == "shuffle":
		return true, nil, gp.Sort, nil

	default:
		fields, err = parseSortFieldList(gp.Sort)
		if err != nil {
			return false, nil, nil, fmt.Errorf("stored #SORT: directive: %w", err)
		}
		return false, fields, gp.Sort, nil
	}
}

// parseSortFields splits and validates a comma-separated fields argument
// (the second positional argument on the command line), returning both the validated
// []playlist.SortField for sorting and the plain []string form (trimmed,
// order preserved) to record as the new #SORT: directive.
func parseSortFields(raw string) ([]playlist.SortField, []string, error) {
	var spec []string
	for p := range strings.SplitSeq(raw, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			spec = append(spec, p)
		}
	}
	if len(spec) == 0 {
		return nil, nil, fmt.Errorf("no fields given")
	}
	fields, err := parseSortFieldList(spec)
	return fields, spec, err
}

// parseSortFieldList validates an already-split field list (from the
// command line, or from a stored #SORT: directive being reapplied),
// naming the valid set on the first unrecognized entry.
func parseSortFieldList(spec []string) ([]playlist.SortField, error) {
	fields := make([]playlist.SortField, 0, len(spec))
	for _, p := range spec {
		if !playlist.ValidSortField(p) {
			return nil, fmt.Errorf("unknown sort field %q; valid fields are: %s",
				p, strings.Join(playlist.ValidSortFieldNames, ", "))
		}
		fields = append(fields, playlist.SortField(p))
	}
	return fields, nil
}
