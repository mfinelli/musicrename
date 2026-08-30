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
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/video"
)

var videoRenameCmd = &cobra.Command{
	Use:   "rename [video-root]",
	Short: "Reconcile video locations with their musicvideo.nfo metadata",
	Long: `Scans video-root for videos, reads each musicvideo.nfo, and moves any
video along with its nfo, info.txt (if present), and sums.md5 (if present)
whose current location no longer matches its Artist/Title. This picks up
drift from 'video edit' changing artist/title, a video that was placed (or
its nfo created) by hand, or a bucket-override/sanitization rule changing.

A video directory with no musicvideo.nfo, or with more than one video file,
is skipped with a warning rather than guessed at.

If video-root is omitted it defaults to the current working directory.

With --dry-run, prints the planned moves without touching any files.

sums.md5 always travels along with a moved video directory. If the video's
filename also changed (title-driven, so it can differ from the directory
move) and sums.md5 exists, that one entry's filename is updated
in place. Pass --skip-md5 to still move sums.md5 but leave its content alone.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runVideoRename,
}

func init() {
	videoRenameCmd.Flags().Bool("dry-run", false, "Print planned moves without making any changes")
	videoRenameCmd.Flags().Bool("skip-md5", false, "Move sums.md5 along with the directory but do not update its entries")
	videoCmd.AddCommand(videoRenameCmd)
}

func runVideoRename(cmd *cobra.Command, args []string) error {
	root := "."
	if len(args) > 0 {
		root = args[0]
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("could not resolve video root %q: %w", root, err)
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")

	plan, err := video.Scan(absRoot)
	if err != nil {
		return fmt.Errorf("scanning video root: %w", err)
	}

	out := cmd.OutOrStdout()

	if dryRun {
		printVideoDryRun(out, plan)
		return nil
	}

	printVideoRunPlan(out, plan)

	skipMD5, _ := cmd.Flags().GetBool("skip-md5")

	// On an interactive terminal, print a \r progress line for each move so
	// the user has live feedback during execution. On a non-TTY (redirected
	// output, etc) the callback is nil and no progress is written.
	var progress func(video.RenameMove)
	if isatty.IsTerminal(os.Stdout.Fd()) {
		progress = func(m video.RenameMove) {
			fmt.Fprintf(out, "\r\033[K  → %s", m.Title)
		}
	}

	result, err := video.Execute(plan, absRoot, skipMD5, progress)
	if err != nil {
		return fmt.Errorf("executing moves: %w", err)
	}

	if progress != nil {
		fmt.Fprint(out, "\r\033[K")
	}

	printVideoRunSummary(out, plan, result.Warnings)
	return nil
}

// printVideoRunPlan writes a condensed overview to out before execution
// begins: artists with move counts, but no per-video detail.
func printVideoRunPlan(out io.Writer, plan *video.RenamePlan) {
	fmt.Fprintln(out, renameHeaderStyle.Render("Renaming videos..."))
	fmt.Fprintln(out)

	if len(plan.Moves) == 0 {
		fmt.Fprintln(out, "No videos found.")
		return
	}

	for _, g := range videoGroupByArtist(plan.Moves) {
		fmt.Fprintln(out, renameArtistStyle.Render(g.bucket+" / "+g.artist))
		moves, noOps := videoMoveCounts(g.moves)
		fmt.Fprintln(out, "  "+renameSourceStyle.Render(
			fmt.Sprintf("· %d move(s), %d no-op(s)", moves, noOps),
		))
	}
	fmt.Fprintln(out)
}

// printVideoDryRun writes the complete dry-run plan to out, grouped by
// artist. Warnings are shown at the top; a summary line appears at the
// bottom.
func printVideoDryRun(out io.Writer, plan *video.RenamePlan) {
	fmt.Fprintln(out, renameHeaderStyle.Render("Dry run: no files will be moved."))
	fmt.Fprintln(out)

	printVideoWarnings(out, plan.Warnings)

	if len(plan.Moves) == 0 {
		fmt.Fprintln(out, "No videos found.")
		return
	}

	for _, g := range videoGroupByArtist(plan.Moves) {
		fmt.Fprintln(out, renameArtistStyle.Render(g.bucket+" / "+g.artist))

		for _, m := range g.moves {
			arrow := renameArrowStyle.Render("→")
			// Bucket/artist are already shown in the group header above, so
			// only the title-level directory name (the new leaf) needs
			// to be shown here.
			line := fmt.Sprintf("  %s  %s  %s",
				renameSourceStyle.Render(m.OldDir), arrow,
				renameNewPathStyle.Render(filepath.Base(m.NewDir)))

			switch {
			case m.IsNoOp:
				line += "  " + renameNoOpStyle.Render("(no-op)")
			case m.IsCaseOnly:
				line += "  " + renameCaseOnlyStyle.Render("(case rename)")
			}

			fmt.Fprintln(out, line)
		}
		fmt.Fprintln(out)
	}

	moves, noOps := videoMoveCounts(plan.Moves)
	printVideoSummaryLine(out, moves, noOps, len(plan.Warnings))
}

// printVideoRunSummary writes all warnings (scan-phase and execute-phase
// combined) followed by the summary line to out.
func printVideoRunSummary(out io.Writer, plan *video.RenamePlan, execWarnings []string) {
	allWarnings := append(append([]string{}, plan.Warnings...), execWarnings...)
	printVideoWarnings(out, allWarnings)

	moves, noOps := videoMoveCounts(plan.Moves)
	printVideoSummaryLine(out, moves, noOps, len(allWarnings))
}

func printVideoWarnings(out io.Writer, warnings []string) {
	if len(warnings) == 0 {
		return
	}
	fmt.Fprintln(out, renameWarningStyle.Render(fmt.Sprintf("⚠  %d warning(s)", len(warnings))))
	for _, w := range warnings {
		fmt.Fprintln(out, "   "+renameWarningStyle.Render(w))
	}
	fmt.Fprintln(out)
}

// printVideoSummaryLine renders the rule and the summary line that appears
// at the bottom of both dry-run and live-run output.
func printVideoSummaryLine(out io.Writer, moves, noOps, warnings int) {
	noOpLabel := "no-ops"
	if noOps == 1 {
		noOpLabel = "no-op"
	}
	summaryText := fmt.Sprintf("%d moves · %d %s · %d warnings", moves, noOps, noOpLabel, warnings)
	fmt.Fprintln(out, renameRuleStyle.Render(strings.Repeat("─", len(summaryText))))
	fmt.Fprintln(out, renameBoldStyle.Render(summaryText))
}

// videoMoveCounts returns the number of real moves and no-op moves.
func videoMoveCounts(moves []video.RenameMove) (real, noOps int) {
	for _, m := range moves {
		if m.IsNoOp {
			noOps++
		} else {
			real++
		}
	}
	return
}

// videoArtistGroup clusters RenameMoves for a single artist together with
// the display bucket character ("a"-"z" or "0").
type videoArtistGroup struct {
	bucket string
	artist string
	moves  []video.RenameMove
}

// videoGroupByArtist clusters moves by (bucket, artist) and returns them
// sorted: letter-bucket groups a-z first, then "0", with moves within each
// group sorted by Title. Mirrors rename.go's renameGroupByArtist.
func videoGroupByArtist(moves []video.RenameMove) []videoArtistGroup {
	m := make(map[string]*videoArtistGroup)
	for _, mv := range moves {
		key := mv.Bucket + "/" + mv.Artist
		if _, ok := m[key]; !ok {
			m[key] = &videoArtistGroup{bucket: mv.Bucket, artist: mv.Artist}
		}
		m[key].moves = append(m[key].moves, mv)
	}

	groups := make([]videoArtistGroup, 0, len(m))
	for _, g := range m {
		sort.Slice(g.moves, func(i, j int) bool { return g.moves[i].Title < g.moves[j].Title })
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
