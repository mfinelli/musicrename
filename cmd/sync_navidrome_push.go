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
	"io"
	"os"
	"path/filepath"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/completion"
	"github.com/mfinelli/musicrename/internal/navidrome"
	"github.com/mfinelli/musicrename/internal/navidromesync"
)

var syncNavidromePushCmd = &cobra.Command{
	Use:   "push [playlist]",
	Short: "Push playlists from playlists/ to Navidrome",
	Long: `Pushes local playlists (playlists/) to the Navidrome server
configured via 'musicrename login', overwriting remote content with whatever
the local file now says.

Every entry is resolved to a Navidrome song ID by searching the server's
catalog (an entry that fails to resolve is skipped with a warning rather than
failing the whole push).

A local file with no #NAVIDROME-ID yet is created remotely and has the new
ID written back into the file. An already-correlated file has its remote
name and full track list replaced to match local exactly (in order); if the
remote side already matches, nothing is sent.

With no argument, pushes every file under playlists/. With playlist given,
pushes just that one.

Before pushing, a library scan is triggered and awaited so Navidrome's view
of the filesystem is current; --skip-scan bypasses this when it's already
known to be fresh.

--dry-run prints what would happen without changing anything, locally or
remotely.`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completion.PlaylistArg,
	RunE:              runSyncNavidromePush,
}

func init() {
	syncNavidromePushCmd.Flags().Bool("dry-run", false, "Print what would happen without changing anything")
	syncNavidromePushCmd.Flags().Bool("skip-scan", false, "Skip triggering a library scan before pushing")
	syncNavidromeCmd.AddCommand(syncNavidromePushCmd)
}

func runSyncNavidromePush(cmd *cobra.Command, args []string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	skipScan, _ := cmd.Flags().GetBool("skip-scan")
	out := cmd.OutOrStdout()

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

	if !skipScan {
		lipgloss.Fprintln(out, renameHeaderStyle.Render("Checking library is up to date..."))
		if err := navidrome.Scan(client, navidrome.DefaultScanPollInterval, scanProgressPrinter(out)); err != nil {
			return fmt.Errorf("scanning library: %w", err)
		}
		clearProgressLine(out)
		fmt.Fprintln(out, "Library scan complete.")
		fmt.Fprintln(out)
	}

	var (
		result *navidromesync.PushResult
		root   string
	)

	if len(args) == 1 {
		playlistPath, err := filepath.Abs(args[0])
		if err != nil {
			return fmt.Errorf("could not resolve path %q: %w", args[0], err)
		}
		root = filepath.Dir(filepath.Dir(playlistPath)) // best-effort for display only

		fmt.Fprintf(out, "Pushing %s to %s...\n", args[0], creds.URL)
		result, err = navidromesync.PushOne(client, playlistPath, dryRun)
		if err != nil {
			return err
		}
	} else {
		root, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("determining current directory: %w", err)
		}

		fmt.Fprintf(out, "Pushing playlists to %s...\n", creds.URL)
		result, err = navidromesync.PushAll(client, root, dryRun)
		if err != nil {
			return err
		}
	}

	printPushResult(out, root, result, dryRun)
	return nil
}

// printPushResult renders a navidromesync.PushResult, following the same
// conventions as printPullResult.
func printPushResult(out io.Writer, root string, result *navidromesync.PushResult, dryRun bool) {
	label := func(path string) string {
		if rel, err := filepath.Rel(root, path); err == nil {
			return rel
		}
		return path
	}

	createdVerb, updatedVerb := "created", "updated"
	if dryRun {
		createdVerb, updatedVerb = "would create", "would update"
	}

	for _, p := range result.Created {
		lipgloss.Fprintln(out, "  "+sumsCheckStyle.Render("+ "+label(p))+" ("+createdVerb+")")
	}
	for _, p := range result.Updated {
		lipgloss.Fprintln(out, "  "+checkFindingStyle.Render("~ "+label(p))+" ("+updatedVerb+")")
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "%d created, %d updated, %d unchanged\n",
		len(result.Created), len(result.Updated), len(result.Unchanged))

	if len(result.Warnings) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "%d warning(s):\n", len(result.Warnings))
		for _, w := range result.Warnings {
			lipgloss.Fprintln(out, "  "+renameWarningStyle.Render("⚠ "+w))
		}
	}
}
