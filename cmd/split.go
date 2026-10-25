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
	"time"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/pcm"
	"github.com/mfinelli/musicrename/internal/riplog"
	"github.com/mfinelli/musicrename/internal/style"
)

var splitCmd = &cobra.Command{
	Use:   "split --check log [dir]",
	Short: "Check that split tracks are a faithful split of a ripped image",
	Long: `Checks that the tracks an image rip was split into hold exactly what was
ripped. The log is the EAC log of the rip, with its cue sheet beside it, and dir
is the directory of the split tracks, which defaults to the current directory, so
the usual way to run it is from inside the directory the album was split into.

The tracks are named as the workflow names them:

  01 Title.flac     a single disc
  1-01 Title.flac   disc 1 of several, which share the directory (the disc is
                    taken from the log's name, as in "Album (disc 1).log")
  00 Title.flac     audio before track 1, a hidden track or a pregap

Checked:
  - the log's checksum, since the log is what the tracks are compared with
  - a file for every track of the disc (and for 00 only if the disc has audio
    before track 1), and no other file for any of them
  - each track is CD audio: 44.1 kHz, 16-bit, stereo
  - the tracks, in order, decode to the log's Copy CRC, so that they hold the
    audio EAC ripped; and each matches the MD5 its own FLAC stores, so that none
    is damaged
  - each track is as long as the cue sheet says, with pregaps kept either at the
    end of the track before or at the start of the track they lead into, since a
    split can be cut a few samples off at every boundary and still hold exactly
    the audio of the rip

The audio is decoded with ffmpeg, which takes a few seconds for an album.

Each check ends up as ok, a note, a warning, or an error. Only the checks that
did not pass are listed unless -v is given. The exit status is non-zero when
there is an error, and with --strict when there is a warning.`,
	Args: cobra.RangeArgs(1, 2),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 1 {
			return nil, cobra.ShellCompDirectiveFilterDirs
		}
		return nil, cobra.ShellCompDirectiveDefault
	},
	RunE: runSplit,
}

func init() {
	splitCmd.Flags().Bool("check", false, "Check an existing split against the rip's log (all this does so far)")
	splitCmd.Flags().Bool("strict", false, "Treat warnings as failures for the exit status")
	splitCmd.Flags().BoolP("verbose", "v", false, "Show every check, not only those that did not pass")
	// Checking is the only thing the command does yet. It is a flag so that the
	// command can split too, later, without changing how it is run to check.
	_ = splitCmd.MarkFlagRequired("check")
	rootCmd.AddCommand(splitCmd)
}

func runSplit(cmd *cobra.Command, args []string) error {
	start := time.Now()

	logPath, err := filepath.Abs(args[0])
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", args[0], err)
	}
	if info, err := os.Stat(logPath); err != nil {
		return fmt.Errorf("could not access %q: %w", args[0], err)
	} else if info.IsDir() || !strings.EqualFold(filepath.Ext(logPath), ".log") {
		return fmt.Errorf("%q is not a .log file", filepath.Base(logPath))
	}

	dir := "."
	if len(args) > 1 {
		dir = args[1]
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", dir, err)
	}
	if info, err := os.Stat(absDir); err != nil {
		return fmt.Errorf("could not access %q: %w", dir, err)
	} else if !info.IsDir() {
		return fmt.Errorf("%q is not a directory", dir)
	}

	strict, _ := cmd.Flags().GetBool("strict")
	verbose, _ := cmd.Flags().GetBool("verbose")
	out := cmd.OutOrStdout()

	lipgloss.Fprintln(out, style.Header.Render("Checking split tracks..."))
	fmt.Fprintln(out)

	l, res, err := riplog.CheckSplit(cmd.Context(), logPath, absDir, pcm.Decoder{})
	if err != nil {
		lipgloss.Fprintln(out, "  "+filepath.Base(absDir))
		lipgloss.Fprintln(out, "    "+style.Error.Render("✗ "+err.Error()))
		fmt.Fprintln(out)
		checkPrintSummary(out, 1, "split", 1, time.Since(start))
		os.Exit(1)
	}

	lipgloss.Fprintln(out, "  "+filepath.Base(absDir)+"  "+
		style.Faint.Render("against "+filepath.Base(logPath)+" ("+l.Describe()+")"))
	for _, c := range res.Checks {
		if c.Status == riplog.StatusPass && !verbose {
			continue
		}
		lipgloss.Fprintln(out, "    "+riplogCheckLine(c))
	}
	if !verbose {
		lipgloss.Fprintln(out, "    "+style.OK.Render("✓")+" "+res.PassSummary())
	}

	fmt.Fprintln(out)
	checkPrintSummary(out, 1, "split", res.Count(riplog.StatusWarning)+res.Count(riplog.StatusError), time.Since(start))

	if res.Fails(strict) {
		os.Exit(1)
	}
	return nil
}
