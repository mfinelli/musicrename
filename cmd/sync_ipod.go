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
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/devicesync"
)

var syncIpodCmd = &cobra.Command{
	Use:   "ipod <device-path> [library-root]",
	Short: "Sync the library to an iPod (FLAC/MP3/M4A passthrough, external artwork)",
	Long: `Syncs library-root (defaults to the current directory) to device-path,
an iPod's mounted filesystem.

FLAC, MP3, and M4A tracks are copied through unchanged; nothing is
transcoded for this target. Album artwork is resized to fit within 400px
and shipped as an external folder.jpg alongside the tracks (this target
never embeds artwork).

Computes what needs to be added, regenerated, and deleted, checks the
device has enough free space, then prompts for confirmation before making
any changes (deletions only ever affect the device copy, not the source
files). --yes skips the prompt; --dry-run shows the plan without touching
anything; --verbose itemizes every change instead of just the summary
counts.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSyncDevice(cmd, "ipod", args)
	},
}

func init() {
	syncIpodCmd.Flags().Bool("dry-run", false, "Show the plan without changing anything")
	syncIpodCmd.Flags().Bool("yes", false, "Skip the confirmation prompt")
	syncIpodCmd.Flags().Bool("verbose", false, "Itemize every change instead of just the summary counts")
	syncCmd.AddCommand(syncIpodCmd)
}

// runSyncDevice is the shared implementation behind every per-target
// device sync command's RunE (sync ipod here, sync sdcard in
// sync_sdcard.go). It lives here rather than in its own file since the
// actual planning/execution logic it calls is already in
// internal/devicesync (Plan, Execute); this function and the print
// helpers below it are purely the CLI-layer concerns (argument/flag
// handling, the confirmation prompt, and terminal output), which is a
// small enough, command-specific amount of code to define once here and call
// from sync_sdcard.go, rather than a dedicated shared file.
func runSyncDevice(cmd *cobra.Command, targetName string, args []string) error {
	devicePathArg := args[0]
	libraryRootArg := "."
	if len(args) > 1 {
		libraryRootArg = args[1]
	}

	devicePath, err := filepath.Abs(devicePathArg)
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", devicePathArg, err)
	}
	libraryRoot, err := filepath.Abs(libraryRootArg)
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", libraryRootArg, err)
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	skipConfirm, _ := cmd.Flags().GetBool("yes")
	verbose, _ := cmd.Flags().GetBool("verbose")
	out := cmd.OutOrStdout()

	fmt.Fprintln(out, renameHeaderStyle.Render(fmt.Sprintf("Planning sync to %s...", targetName)))

	plan, err := devicesync.Plan(libraryRoot, devicePath, targetName)
	if err != nil {
		return fmt.Errorf("planning sync: %w", err)
	}

	counts := devicesync.CountChanges(plan.Diff)
	if counts.Total() == counts.Skip {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Already up to date.")
		printSyncWarnings(out, plan.Warnings)
		return nil
	}

	fmt.Fprintln(out)
	printPlanSummary(out, counts, plan.Capacity)

	if !dryRun && !plan.Capacity.Sufficient() {
		return fmt.Errorf(
			"not enough free space on %s: need ~%s, have %s available (%s would be freed by deletions)",
			devicePath, devicesync.FormatBytes(plan.Capacity.NeededBytes),
			devicesync.FormatBytes(plan.Capacity.AvailableBytes), devicesync.FormatBytes(plan.Capacity.FreedBytes),
		)
	}

	if dryRun {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Dry run — nothing will be changed.")
		if verbose {
			fmt.Fprintln(out)
			printPlanDetail(out, plan.Diff)
		}
		printSyncWarnings(out, plan.Warnings)
		return nil
	}

	if !skipConfirm {
		confirmed := false
		title := fmt.Sprintf(
			"Sync to %s? %d to add, %d to regenerate, %d to delete.",
			targetName, counts.Add, counts.Regenerate, counts.Delete,
		)
		if err := huh.NewConfirm().
			Title(title).
			Description("Deletions only affect the device copy; source files are never touched.").
			Affirmative("Sync").
			Negative("Cancel").
			Value(&confirmed).
			Run(); err != nil {
			return fmt.Errorf("prompting: %w", err)
		}
		if !confirmed {
			fmt.Fprintln(out, "Cancelled.")
			return nil
		}
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, renameHeaderStyle.Render("Syncing..."))

	result, err := devicesync.Execute(context.Background(), libraryRoot, devicePath, targetName, plan.Diff, false)
	if err != nil {
		return fmt.Errorf("syncing: %w", err)
	}
	warnings := append(plan.Warnings, result.Warnings...)

	fmt.Fprintln(out)
	if verbose {
		printExecuteDetail(out, result)
		fmt.Fprintln(out)
	}
	fmt.Fprintln(out, sumsCheckStyle.Render(fmt.Sprintf(
		"✓  %d created, %d updated, %d deleted",
		len(result.Created), len(result.Updated), len(result.Deleted),
	)))
	printSyncWarnings(out, warnings)

	return nil
}

// printPlanSummary renders the counts-and-bytes overview shown before any
// confirmation prompt, in both dry-run and real-run modes.
func printPlanSummary(out io.Writer, c devicesync.ChangeCounts, capacity *devicesync.CapacityReport) {
	fmt.Fprintf(out, "%d to add, %d to regenerate, %d to delete, %d already up to date\n",
		c.Add, c.Regenerate, c.Delete, c.Skip)
	fmt.Fprintf(out, "Needs ~%s · frees ~%s · %s available on device\n",
		devicesync.FormatBytes(capacity.NeededBytes), devicesync.FormatBytes(capacity.FreedBytes),
		devicesync.FormatBytes(capacity.AvailableBytes))
}

// printPlanDetail itemizes every add/regenerate/delete entry in diff, for
// --verbose dry-run output. Skip entries are never itemized — there's
// nothing to say about a file that isn't changing.
func printPlanDetail(out io.Writer, diff *devicesync.DiffResult) {
	for _, change := range diff.Changes {
		switch change.Action {
		case devicesync.ActionAdd:
			fmt.Fprintln(out, "  "+sumsCheckStyle.Render("+ "+syncEntryLabel(change.Entry))+" (would add)")
		case devicesync.ActionRegenerate:
			fmt.Fprintln(out, "  "+checkFindingStyle.Render("~ "+syncEntryLabel(change.Entry))+" (would regenerate)")
		case devicesync.ActionDelete:
			fmt.Fprintln(out, "  "+renameWarningStyle.Render("- "+syncEntryLabel(change.Entry))+" (would delete)")
		}
	}
}

// printExecuteDetail itemizes what Execute actually did, for --verbose
// real-run output.
func printExecuteDetail(out io.Writer, result *devicesync.ExecuteResult) {
	for _, entry := range result.Created {
		fmt.Fprintln(out, "  "+sumsCheckStyle.Render("+ "+syncEntryLabel(entry))+" (created)")
	}
	for _, entry := range result.Updated {
		fmt.Fprintln(out, "  "+checkFindingStyle.Render("~ "+syncEntryLabel(entry))+" (updated)")
	}
	for _, entry := range result.Deleted {
		fmt.Fprintln(out, "  "+renameWarningStyle.Render("- "+syncEntryLabel(entry))+" (deleted)")
	}
}

// syncEntryLabel renders a DesiredEntry as a single displayable path.
func syncEntryLabel(entry devicesync.DesiredEntry) string {
	return filepath.Join(entry.Root, entry.Rel)
}

// printSyncWarnings renders an aggregated warning list (from planning and,
// once a real sync has run, execution too), or nothing at all when there
// are none.
func printSyncWarnings(out io.Writer, warnings []string) {
	if len(warnings) == 0 {
		return
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "%d warning(s):\n", len(warnings))
	for _, w := range warnings {
		fmt.Fprintln(out, "  "+renameWarningStyle.Render("⚠ "+w))
	}
}
