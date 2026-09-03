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

import "github.com/spf13/cobra"

var syncSdcardCmd = &cobra.Command{
	Use:   "sdcard <device-path> [library-root]",
	Short: "Sync the library to an SD card (MP3-only, embedded artwork)",
	Long: `Syncs library-root (defaults to the current directory) to device-path,
an SD card's mounted filesystem.

Only MP3 is accepted; anything else (FLAC, M4A) is transcoded. Album
artwork is resized to fit within 500px and embedded directly into each
track's tags (this target never ships external artwork) and an artwork
change alone re-embeds every already-synced track in that album, even if
none of the audio itself changed.

Computes what needs to be added, regenerated, and deleted, checks the
device has enough free space, then prompts for confirmation before making
any changes (deletions only ever affect the device copy, not the source
files). --yes skips the prompt; --dry-run shows the plan without touching
anything; --verbose itemizes every change instead of just the summary
counts.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		// The actual work is identical to sync ipod's (only the target
		// name differs) so it's shared via runSyncDevice (defined
		// alongside sync ipod's command in sync_ipod.go) rather than
		// duplicated here.
		return runSyncDevice(cmd, "sdcard", args)
	},
}

func init() {
	syncSdcardCmd.Flags().Bool("dry-run", false, "Show the plan without changing anything")
	syncSdcardCmd.Flags().Bool("yes", false, "Skip the confirmation prompt")
	syncSdcardCmd.Flags().Bool("verbose", false, "Itemize every change instead of just the summary counts")
	syncSdcardCmd.Flags().Bool("no-video", false, "No effect on this target (no video support)")
	syncSdcardCmd.Flags().Bool("video-only", false, "Errors: this target has no video support")
	syncCmd.AddCommand(syncSdcardCmd)
}
