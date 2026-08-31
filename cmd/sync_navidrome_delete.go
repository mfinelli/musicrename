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
	"errors"
	"fmt"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/navidrome"
	"github.com/mfinelli/musicrename/internal/navidromesync"
	"github.com/mfinelli/musicrename/internal/playlist"
)

var syncNavidromeDeleteCmd = &cobra.Command{
	Use:   "delete <playlist>",
	Short: "Permanently delete one playlist, locally and on Navidrome",
	Long: `Deletes a single local playlist file's correlated remote playlist: 
reads its #NAVIDROME-ID, deletes that playlist on the Navidrome server 
configured via 'musicrename login', then removes the local file. This is the 
only sanctioned way to perform a real, intended deletion (pull/push's 
deletions are self-healing reactions to what the other side already did).

playlist must already exist locally and have a #NAVIDROME-ID (i.e., it's
been pushed at least once) or this command errors immediately, without
deleting anything, otherwise.

No library scan is triggered first; nothing here depends on the library's 
current filesystem state.

Prompts for confirmation before doing anything, since this is destructive
both locally and remotely and cannot be undone; --yes skips the prompt.`,
	Args: cobra.ExactArgs(1),
	RunE: runSyncNavidromeDelete,
}

func init() {
	syncNavidromeDeleteCmd.Flags().Bool("yes", false, "Skip the confirmation prompt")
	syncNavidromeCmd.AddCommand(syncNavidromeDeleteCmd)
}

func runSyncNavidromeDelete(cmd *cobra.Command, args []string) error {
	skipConfirm, _ := cmd.Flags().GetBool("yes")
	out := cmd.OutOrStdout()

	playlistPath, err := filepath.Abs(args[0])
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", args[0], err)
	}

	// Read this up front, before touching the network at all: a playlist
	// with no #NAVIDROME-ID has nothing to delete remotely, and there's no
	// reason to authenticate just to discover that.
	gp, err := playlist.ReadGlobalPlaylist(playlistPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", args[0], err)
	}
	if !gp.HasNavidromeID {
		return fmt.Errorf("%s has no #NAVIDROME-ID; nothing to delete remotely", args[0])
	}

	if !skipConfirm {
		label := gp.Name
		if label == "" {
			label = args[0]
		}

		confirmed := false
		if err := huh.NewConfirm().
			Title(fmt.Sprintf("Permanently delete %q from Navidrome and remove %s locally?", label, args[0])).
			Description("This cannot be undone.").
			Affirmative("Delete").
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

	creds, err := navidrome.LoadCredentials()
	if err != nil {
		if errors.Is(err, navidrome.ErrNotLoggedIn) {
			return err
		}
		return fmt.Errorf("loading credentials: %w", err)
	}

	client, err := navidrome.NewClient(*creds)
	if err != nil {
		return err
	}

	warning, err := navidromesync.DeleteOne(client, playlistPath)
	if err != nil {
		return err
	}
	if warning != "" {
		fmt.Fprintln(out, renameWarningStyle.Render("⚠ "+warning))
	}

	fmt.Fprintf(out, "Deleted %s.\n", args[0])
	return nil
}
