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
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/metadata"
	"github.com/mfinelli/musicrename/internal/planner"
)

// Lipgloss styles for the dry-run output. Defined at package level so they are
// initialised once and reused across calls to printDryRun.
var (
	renameHeaderStyle   = lipgloss.NewStyle().Bold(true)
	renameArtistStyle   = lipgloss.NewStyle().Bold(true)
	renameAlbumStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("6")) // cyan
	renameSourceStyle   = lipgloss.NewStyle().Faint(true)
	renameArrowStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("4")) // blue
	renameNewPathStyle  = lipgloss.NewStyle().Bold(true)
	renameNoOpStyle     = lipgloss.NewStyle().Faint(true)
	renameCaseOnlyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // bright yellow
	renameWarningStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // bright yellow
	renameRuleStyle     = lipgloss.NewStyle().Faint(true)
	renameBoldStyle     = lipgloss.NewStyle().Bold(true)
)

var renameCmd = &cobra.Command{
	Use:   "rename [library-root]",
	Short: "Organize your music library by metadata tags",
	Long: `Scans the library root for music files, reads their metadata tags,
and moves them into a normalized directory hierarchy.

If library-root is omitted it defaults to the current working directory.

With --dry-run, prints the planned moves without touching any files.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRename,
}

func init() {
	renameCmd.Flags().Bool("dry-run", false, "Print planned moves without making any changes")
	rootCmd.AddCommand(renameCmd)
}

func runRename(cmd *cobra.Command, args []string) error {
	root := "."
	if len(args) > 0 {
		root = args[0]
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("could not resolve library root %q: %w", root, err)
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")

	// Warnings from ProcessLibrary (unreadable tracks, unresolvable artists)
	// are captured in each Album.Warnings and will flow through into
	// AlbumPlan.Warnings, appearing in the grouped warnings block.
	albums, err := metadata.ProcessLibrary(absRoot)
	if err != nil {
		return fmt.Errorf("scanning library: %w", err)
	}

	p := planner.New(absRoot)
	plan, err := p.PlanLibrary(albums)
	if err != nil {
		return fmt.Errorf("planning moves: %w", err)
	}

	if dryRun {
		printDryRun(cmd.OutOrStdout(), plan)
		return nil
	}

	// TODO: implement execution phase (Phase 4).
	return fmt.Errorf("rename execution is not yet implemented; use --dry-run to preview changes")
}

// printDryRun writes the complete dry-run plan to out, grouped by artist then
// by album within each artist. Warnings are shown at the top; a summary line
// appears at the bottom.
func printDryRun(out io.Writer, plan *planner.Plan) {
	// Collect all warnings across all albums.
	var allWarnings []string
	for _, ap := range plan.Albums {
		allWarnings = append(allWarnings, ap.Warnings...)
	}

	// Header.
	fmt.Fprintln(out, renameHeaderStyle.Render("Dry run: no files will be moved."))
	fmt.Fprintln(out)

	// Warnings block (shown before the plan so the user sees them first).
	if len(allWarnings) > 0 {
		fmt.Fprintln(out, renameWarningStyle.Render(fmt.Sprintf("⚠  %d warning(s)", len(allWarnings))))
		for _, w := range allWarnings {
			fmt.Fprintln(out, "   "+renameWarningStyle.Render(w))
		}
		fmt.Fprintln(out)
	}

	if len(plan.Albums) == 0 {
		fmt.Fprintln(out, "No music files found.")
		return
	}

	// Group and sort albums for display.
	groups := renameGroupByArtist(plan.Albums)
	for _, g := range groups {
		// "b / beyonce" bold artist header.
		fmt.Fprintln(out, renameArtistStyle.Render(g.bucket+" / "+g.artist))

		for _, ap := range g.albums {
			// Album sub-header, indented two spaces.
			fmt.Fprintln(out, "  "+renameAlbumStyle.Render(ap.AlbumName))
			// Source directory shown once per album so it doesn't repeat on
			// every move line.
			fmt.Fprintln(out, "  "+renameSourceStyle.Render("from "+ap.SourceDir))

			for _, op := range ap.Moves {
				// Show only the base filename on the left (the source
				// directory is already shown above). Show the relative
				// destination path in bold so the sanitized result stands out.
				oldName := filepath.Base(op.OldPath)
				relPath, _ := filepath.Rel(ap.DestDir, op.NewPath)
				arrow := renameArrowStyle.Render("→")
				line := fmt.Sprintf("    %s  %s  %s", oldName, arrow, renameNewPathStyle.Render(relPath))

				switch {
				case op.IsNoOp:
					line += "  " + renameNoOpStyle.Render("(no-op)")
				case op.IsCaseOnly:
					line += "  " + renameCaseOnlyStyle.Render("(case rename)")
				}

				fmt.Fprintln(out, line)
			}
		}

		fmt.Fprintln(out)
	}

	// Summary counts.
	totalMoves := 0
	totalNoOps := 0
	for _, ap := range plan.Albums {
		for _, op := range ap.Moves {
			if op.IsNoOp {
				totalNoOps++
			} else {
				totalMoves++
			}
		}
	}

	noOpLabel := "no-ops"
	if totalNoOps == 1 {
		noOpLabel = "no-op"
	}
	summaryText := fmt.Sprintf(
		"%d albums · %d moves · %d %s · %d warnings",
		len(plan.Albums), totalMoves, totalNoOps, noOpLabel, len(allWarnings),
	)
	fmt.Fprintln(out, renameRuleStyle.Render(strings.Repeat("─", len(summaryText))))
	fmt.Fprintln(out, renameBoldStyle.Render(summaryText))
}

// artistGroup clusters all AlbumPlans for a single artist together with the
// display bucket character ("a"–"z" or "0").
type artistGroup struct {
	artist string
	bucket string
	albums []planner.AlbumPlan
}

// renameGroupByArtist clusters the albums by AlbumArtist and returns them
// sorted: letter-bucket groups a–z first, then "0", with albums within each
// group sorted by AlbumName.
func renameGroupByArtist(albums []planner.AlbumPlan) []artistGroup {
	m := make(map[string]*artistGroup)
	for _, ap := range albums {
		if _, ok := m[ap.AlbumArtist]; !ok {
			m[ap.AlbumArtist] = &artistGroup{
				artist: ap.AlbumArtist,
				bucket: renameBucket(ap.AlbumArtist),
			}
		}
		m[ap.AlbumArtist].albums = append(m[ap.AlbumArtist].albums, ap)
	}

	groups := make([]artistGroup, 0, len(m))
	for _, g := range m {
		sort.Slice(g.albums, func(i, j int) bool {
			return g.albums[i].AlbumName < g.albums[j].AlbumName
		})
		groups = append(groups, *g)
	}

	sort.Slice(groups, func(i, j int) bool {
		bi, bj := groups[i].bucket, groups[j].bucket
		// "0" bucket sorts after all letter buckets.
		if bi == "0" && bj != "0" {
			return false
		}
		if bj == "0" && bi != "0" {
			return true
		}
		if bi != bj {
			return bi < bj
		}
		return groups[i].artist < groups[j].artist
	})

	return groups
}

// renameBucket returns the single-character display bucket for an already-
// sanitized artist name: "a"–"z" for letter-initial artists, "0" for all
// others (digit-initial, empty, or manual-override values starting with a
// non-letter rune).
func renameBucket(artist string) string {
	if len(artist) == 0 {
		return "0"
	}
	r, _ := utf8.DecodeRuneInString(artist)
	if unicode.IsLetter(r) {
		return string(r)
	}
	return "0"
}
