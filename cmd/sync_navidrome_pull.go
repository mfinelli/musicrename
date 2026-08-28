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
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/navidrome"
	"github.com/mfinelli/musicrename/internal/navidromesync"
)

var syncNavidromePullCmd = &cobra.Command{
	Use:   "pull [playlist]",
	Short: "Pull playlists from Navidrome into playlists/",
	Long: `Pulls playlists from the Navidrome server configured via 'musicrename
login' into the library-wide playlists/ tree, overwriting local content with
whatever Navidrome currently holds.

With no argument, pulls every playlist: local files already correlated by
#NAVIDROME-ID are updated in place, a remote playlist never seen before gets
a new local file, and a local file whose correlated playlist no longer exists
remotely is deleted to match.

With playlist given (a path to an existing, already-correlated local
playlist file), pulls just that one. If the server confirms that playlist no
longer exists, the local file is deleted to match rather than treated as an
error.

Before pulling, a library scan is triggered and awaited so Navidrome's view
of the filesystem is current; --skip-scan bypasses this when it's already
known to be fresh.

--dry-run prints what would happen without changing any files.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSyncNavidromePull,
}

func init() {
	syncNavidromePullCmd.Flags().Bool("dry-run", false, "Print what would happen without changing any files")
	syncNavidromePullCmd.Flags().Bool("skip-scan", false, "Skip triggering a library scan before pulling")
	syncNavidromeCmd.AddCommand(syncNavidromePullCmd)
}

func runSyncNavidromePull(cmd *cobra.Command, args []string) error {
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
		fmt.Fprintln(out, renameHeaderStyle.Render("Checking library is up to date..."))
		if err := navidrome.Scan(client, navidrome.DefaultScanPollInterval, scanProgressPrinter(out)); err != nil {
			return fmt.Errorf("scanning library: %w", err)
		}
		clearProgressLine(out)
		fmt.Fprintln(out, "Library scan complete.")
		fmt.Fprintln(out)
	}

	var (
		result *navidromesync.PullResult
		root   string
	)

	if len(args) == 1 {
		playlistPath, err := filepath.Abs(args[0])
		if err != nil {
			return fmt.Errorf("could not resolve path %q: %w", args[0], err)
		}
		root = filepath.Dir(filepath.Dir(playlistPath)) // best-effort for display only

		fmt.Fprintf(out, "Pulling %s from %s...\n", args[0], creds.URL)
		result, err = navidromesync.PullOne(client, playlistPath, dryRun)
		if err != nil {
			return err
		}
	} else {
		root, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("determining current directory: %w", err)
		}

		fmt.Fprintf(out, "Fetching playlists from %s...\n", creds.URL)
		result, err = navidromesync.PullAll(client, root, dryRun)
		if err != nil {
			return err
		}
	}

	printPullResult(out, root, result, dryRun)
	return nil
}

// scanProgressPrinter returns a navidrome.ScanProgress callback that
// renders a \r-updating status line when out is an interactive terminal,
// and does nothing on a non-TTY (redirected output, a log file, etc.) —
// matching the existing progress convention used by rename/video rename.
func scanProgressPrinter(out io.Writer) func(navidrome.ScanProgress) {
	if !isatty.IsTerminal(os.Stdout.Fd()) {
		return nil
	}
	return func(p navidrome.ScanProgress) {
		fmt.Fprintf(out, "\r\033[K  still scanning (%s elapsed, %d items)...", p.Elapsed.Round(time.Second), p.Count)
	}
}

// clearProgressLine erases whatever scanProgressPrinter last wrote, if
// anything (a no-op, harmless \r\033[K, on a non-TTY where nothing was
// ever written).
func clearProgressLine(out io.Writer) {
	if isatty.IsTerminal(os.Stdout.Fd()) {
		fmt.Fprint(out, "\r\033[K")
	}
}

// printPullResult renders a navidromesync.PullResult: a summary line plus
// itemized created/updated/deleted paths (relative to root when possible,
// for readability), followed by any warnings.
func printPullResult(out io.Writer, root string, result *navidromesync.PullResult, dryRun bool) {
	label := func(path string) string {
		if rel, err := filepath.Rel(root, path); err == nil {
			return rel
		}
		return path
	}

	verb := func(created bool) string {
		if dryRun {
			if created {
				return "would create"
			}
			return "would update"
		}
		if created {
			return "created"
		}
		return "updated"
	}

	for _, p := range result.Created {
		fmt.Fprintln(out, "  "+sumsCheckStyle.Render("+ "+label(p))+" ("+verb(true)+")")
	}
	for _, p := range result.Updated {
		fmt.Fprintln(out, "  "+checkFindingStyle.Render("~ "+label(p))+" ("+verb(false)+")")
	}
	for _, p := range result.Deleted {
		removedVerb := "removed"
		if dryRun {
			removedVerb = "would remove"
		}
		fmt.Fprintln(out, "  "+renameWarningStyle.Render("- "+label(p))+" ("+removedVerb+")")
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "%d created, %d updated, %d unchanged, %d deleted\n",
		len(result.Created), len(result.Updated), len(result.Unchanged), len(result.Deleted))

	if len(result.Warnings) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "%d warning(s):\n", len(result.Warnings))
		for _, w := range result.Warnings {
			fmt.Fprintln(out, "  "+renameWarningStyle.Render("⚠ "+w))
		}
	}
}
