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

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/devicesync"
	"github.com/mfinelli/musicrename/internal/target"
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

Also syncs selected video, transcoding each as necessary. --no-video skips
video entirely; --video-only skips audio and syncs only video.

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
	syncIpodCmd.Flags().Bool("no-video", false, "Skip video, sync audio only")
	syncIpodCmd.Flags().Bool("video-only", false, "Skip audio, sync video only")
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
	noVideo, _ := cmd.Flags().GetBool("no-video")
	videoOnly, _ := cmd.Flags().GetBool("video-only")
	out := cmd.OutOrStdout()

	if noVideo && videoOnly {
		return fmt.Errorf("--no-video and --video-only are mutually exclusive")
	}

	def, ok := target.DefinitionFor(targetName)
	if !ok {
		return fmt.Errorf("unrecognized target %q", targetName)
	}
	if videoOnly && !def.SupportsVideo {
		return fmt.Errorf("target %q does not support video; --video-only doesn't apply", targetName)
	}

	lipgloss.Fprintln(out, renameHeaderStyle.Render(fmt.Sprintf("Planning sync to %s...", targetName)))

	// audioPlan/videoPlan are computed independently (audio unless
	// --video-only, video only when the target actually supports it and
	// --no-video wasn't given) and then combined via MergePlans, which
	// already handles a nil video plan by returning audio unchanged so
	// this merge call covers every combination (both, audio-only via
	// --no-video, audio-only because the target simply has no video
	// support at all, or --video-only) without needing its own branch per
	// case.
	var audioPlan, videoPlan *devicesync.PlanResult
	if !videoOnly {
		audioPlan, err = devicesync.Plan(libraryRoot, devicePath, targetName)
		if err != nil {
			return fmt.Errorf("planning sync: %w", err)
		}
	}
	if def.SupportsVideo && !noVideo {
		videoPlan, err = devicesync.VideoPlan(libraryRoot, devicePath, targetName)
		if err != nil {
			return fmt.Errorf("planning video sync: %w", err)
		}
	}

	var plan *devicesync.PlanResult
	switch {
	case audioPlan != nil:
		plan = devicesync.MergePlans(audioPlan, videoPlan)
	case videoPlan != nil:
		plan = videoPlan
	default:
		// Unreachable given the validation above (videoOnly requires
		// SupportsVideo, and !videoOnly always computes audioPlan) but
		// guarded rather than risk a nil dereference below if that
		// invariant is ever broken by a future edit.
		return fmt.Errorf("internal error: nothing was planned")
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
	lipgloss.Fprintln(out, renameHeaderStyle.Render("Syncing..."))

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
	lipgloss.Fprintln(out, sumsCheckStyle.Render(fmt.Sprintf(
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
// nothing to say about a file that isn't changing. When diff contains a
// mix of audio and video entries, they're itemized under separate
// "Audio:"/"Video:" headers rather than interleaved in one flat list. A
// plain video sync (--video-only, or a target with no audio changes at all)
// still renders as one flat list, without an empty "Audio:" header above
// nothing.
func printPlanDetail(out io.Writer, diff *devicesync.DiffResult) {
	audio, video := partitionChangesByRoot(diff.Changes)
	if len(video) == 0 {
		printChangeList(out, audio)
		return
	}
	if len(audio) > 0 {
		lipgloss.Fprintln(out, renameHeaderStyle.Render("Audio:"))
		printChangeList(out, audio)
		fmt.Fprintln(out)
	}
	lipgloss.Fprintln(out, renameHeaderStyle.Render("Video:"))
	printChangeList(out, video)
}

func printChangeList(out io.Writer, changes []devicesync.PlannedChange) {
	for _, change := range changes {
		switch change.Action {
		case devicesync.ActionAdd:
			lipgloss.Fprintln(out, "  "+sumsCheckStyle.Render("+ "+syncEntryLabel(change.Entry))+" (would add)")
		case devicesync.ActionRegenerate:
			lipgloss.Fprintln(out, "  "+checkFindingStyle.Render("~ "+syncEntryLabel(change.Entry))+" (would regenerate)")
		case devicesync.ActionDelete:
			lipgloss.Fprintln(out, "  "+renameWarningStyle.Render("- "+syncEntryLabel(change.Entry))+" (would delete)")
		}
	}
}

// partitionChangesByRoot splits changes into audio and video, purely by
// each entry's own Root ("videos" is always video, reserved everywhere
// else in this project for exactly that meaning and is never a real audio
// library root name).
func partitionChangesByRoot(changes []devicesync.PlannedChange) (audio, video []devicesync.PlannedChange) {
	for _, c := range changes {
		if c.Entry.Root == "videos" {
			video = append(video, c)
		} else {
			audio = append(audio, c)
		}
	}
	return audio, video
}

// printExecuteDetail itemizes what Execute actually did, for --verbose
// real-run output it splits into "Audio:"/"Video:" sections the same way
// printPlanDetail does, and for the same reason.
func printExecuteDetail(out io.Writer, result *devicesync.ExecuteResult) {
	audioCreated, videoCreated := partitionEntriesByRoot(result.Created)
	audioUpdated, videoUpdated := partitionEntriesByRoot(result.Updated)
	audioDeleted, videoDeleted := partitionEntriesByRoot(result.Deleted)

	if len(videoCreated)+len(videoUpdated)+len(videoDeleted) == 0 {
		printExecuteEntries(out, result.Created, result.Updated, result.Deleted)
		return
	}
	if len(audioCreated)+len(audioUpdated)+len(audioDeleted) > 0 {
		lipgloss.Fprintln(out, renameHeaderStyle.Render("Audio:"))
		printExecuteEntries(out, audioCreated, audioUpdated, audioDeleted)
		fmt.Fprintln(out)
	}
	lipgloss.Fprintln(out, renameHeaderStyle.Render("Video:"))
	printExecuteEntries(out, videoCreated, videoUpdated, videoDeleted)
}

func printExecuteEntries(out io.Writer, created, updated, deleted []devicesync.DesiredEntry) {
	for _, entry := range created {
		lipgloss.Fprintln(out, "  "+sumsCheckStyle.Render("+ "+syncEntryLabel(entry))+" (created)")
	}
	for _, entry := range updated {
		lipgloss.Fprintln(out, "  "+checkFindingStyle.Render("~ "+syncEntryLabel(entry))+" (updated)")
	}
	for _, entry := range deleted {
		lipgloss.Fprintln(out, "  "+renameWarningStyle.Render("- "+syncEntryLabel(entry))+" (deleted)")
	}
}

// partitionEntriesByRoot is partitionChangesByRoot's equivalent for a
// flat []DesiredEntry list (ExecuteResult's Created/Updated/Deleted
// shape, rather than DiffResult's []PlannedChange).
func partitionEntriesByRoot(entries []devicesync.DesiredEntry) (audio, video []devicesync.DesiredEntry) {
	for _, e := range entries {
		if e.Root == "videos" {
			video = append(video, e)
		} else {
			audio = append(audio, e)
		}
	}
	return audio, video
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
		lipgloss.Fprintln(out, "  "+renameWarningStyle.Render("⚠ "+w))
	}
}
