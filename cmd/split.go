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
	Use:   "split [path] dir",
	Short: "Split the image of a rip into tracks, or check a split",
	Long: `Splits the image of an Exact Audio Copy rip into one FLAC file per track, in dir,
or, with --check, checks that the tracks it was split into hold exactly what
was ripped.

path is a rip's log (its cue sheet and image are beside it) or a directory with
logs in it, defaulting to the current working directory. Every log in a
directory is split in turn, so the discs of a multi-disc album all end up in
the same final directory. To write into the current directory, pass "."
explicitly.

Each track is cut where the cue sheet puts it, with each pregap at the end of
the previous track. Audio before track 1 is part of track 1 if it is a pregap,
(up to five seconds) and a track of its own, 00, if it is longer. Pass
--hidden-track to override one way or the other as necessary. The tracks are
tagged with what the cue sheet records, always as UTF-8. Additionally, the
MusicBrainz release in musicbrainz.txt is tagged if there is one beside the
log.

The tracks are named as the workflow names them:

  01 title.flac     a single disc
  1-01 title.flac   disc 1 of several, which share the directory (the disc is
                    taken from the log's name, as in "Album (disc 1).log")
  00 title.flac     audio before track 1, a hidden track or a pregap
  1-00 title.flac   the same, on a disc of several

Checked:
  - the log's checksum, since the log is what the tracks are compared with
  - a file for every track of the disc (and for 00 only if the disc has audio
    before track 1), and no other file for any of them
  - each track is CD audio: 44.1 kHz, 16-bit, stereo
  - the tracks, in order, decode to the log's Copy CRC, so that they hold the
    audio EAC ripped (if not, and the rip's image is beside the log, the first
    and last difference from it are found, by track and place in it); and each
    matches the MD5 its own FLAC stores, so that none is damaged
  - each track is as long as the cue sheet says, with each pregap at the end of
    the track before it (at the start of the track it leads into is a warning),
    since a split can be cut a few samples off at every boundary and still hold
    exactly the audio of the rip

The audio is decoded and encoded with ffmpeg, which takes a few seconds for an
album.

Each check ends up as ok, a note, a warning, or an error. Only the checks that
did not pass are listed unless -v is given. The exit status is non-zero when
there is an error, and with --strict when there is a warning.`,
	Args: cobra.RangeArgs(0, 2),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 1 {
			return nil, cobra.ShellCompDirectiveFilterDirs
		}
		if len(args) > 1 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveDefault
	},
	RunE: runSplit,
}

func init() {
	splitCmd.Flags().Bool("check", false, "Check an existing split against the rip's log, instead of making one")
	splitCmd.Flags().String("hidden-track", "auto", "What audio before track 1 is: auto, yes (a track 00) or no (part of track 1)")
	splitCmd.RegisterFlagCompletionFunc("hidden-track", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return riplog.HiddenTrackValues(), cobra.ShellCompDirectiveNoFileComp
	})
	splitCmd.Flags().Bool("strict", false, "Treat warnings as failures for the exit status")
	splitCmd.Flags().BoolP("verbose", "v", false, "Show every check, not only those that did not pass")
	rootCmd.AddCommand(splitCmd)
}

// splitLogs resolves the argument that names a rip, which is a log, or a directory
// with logs in it, to the logs. It is the current directory if there is no argument.
func splitLogs(arg string) ([]string, error) {
	if arg == "" {
		arg = "."
	}
	abs, err := filepath.Abs(arg)
	if err != nil {
		return nil, fmt.Errorf("could not resolve path %q: %w", arg, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("could not access %q: %w", arg, err)
	}

	if !info.IsDir() {
		if !strings.EqualFold(filepath.Ext(abs), ".log") {
			return nil, fmt.Errorf("%q is not a .log file", filepath.Base(abs))
		}
		return []string{abs}, nil
	}

	logs, err := riplog.FindLogs(abs)
	if err != nil {
		return nil, fmt.Errorf("could not read %q: %w", arg, err)
	}
	if len(logs) == 0 {
		return nil, fmt.Errorf("there is no .log file in %q", arg)
	}
	return logs, nil
}

func runSplit(cmd *cobra.Command, args []string) error {
	start := time.Now()

	check, _ := cmd.Flags().GetBool("check")
	hiddenFlag, _ := cmd.Flags().GetString("hidden-track")
	hidden, err := riplog.ParseHiddenTrack(hiddenFlag)
	if err != nil {
		return fmt.Errorf("invalid --hidden-track: %w", err)
	}

	// With --check the arguments are the rip, and the directory of the tracks, which
	// is the current one. To split they are the rip, which is the current directory,
	// and the directory to put the tracks in, which has to be said.
	var ripArg, dirArg string
	switch {
	case check && len(args) > 0:
		ripArg = args[0]
		dirArg = "."
		if len(args) > 1 {
			dirArg = args[1]
		}
	case check:
		dirArg = "."
	case len(args) == 0:
		return errors.New("the directory for the tracks is needed: pass . for the current one")
	case len(args) == 1:
		dirArg = args[0]
	default:
		ripArg, dirArg = args[0], args[1]
	}

	logs, err := splitLogs(ripArg)
	if err != nil {
		return err
	}
	absDir, err := filepath.Abs(dirArg)
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", dirArg, err)
	}
	if check {
		if info, err := os.Stat(absDir); err != nil {
			return fmt.Errorf("could not access %q: %w", dirArg, err)
		} else if !info.IsDir() {
			return fmt.Errorf("%q is not a directory", dirArg)
		}
	}

	strict, _ := cmd.Flags().GetBool("strict")
	verbose, _ := cmd.Flags().GetBool("verbose")
	out := cmd.OutOrStdout()
	opts := riplog.SplitOptions{HiddenTrack: hidden}

	heading := "Splitting..."
	if check {
		heading = "Checking split tracks..."
	}
	lipgloss.Fprintln(out, style.Header.Render(heading))
	fmt.Fprintln(out)

	failures := 0
	for _, logPath := range logs {
		var (
			l   *riplog.Log
			res *riplog.Result
		)
		if check {
			l, res, err = riplog.CheckSplit(cmd.Context(), logPath, absDir, pcm.Decoder{}, opts)
		} else {
			l, res, err = riplog.Split(cmd.Context(), logPath, absDir, pcm.Decoder{}, opts)
		}
		if err != nil {
			if cmd.Context().Err() != nil {
				return cmd.Context().Err()
			}
			lipgloss.Fprintln(out, "  "+filepath.Base(logPath))
			lipgloss.Fprintln(out, "    "+style.Error.Render("✗ "+err.Error()))
			failures++
			continue
		}

		if check {
			lipgloss.Fprintln(out, "  "+filepath.Base(absDir)+"  "+
				style.Faint.Render("against "+filepath.Base(logPath)+" ("+l.Describe()+")"))
		} else {
			lipgloss.Fprintln(out, "  "+filepath.Base(logPath)+"  "+style.Faint.Render("("+l.Describe()+")"))
		}
		for _, c := range res.Checks {
			// What a split made is the point of it, so it is always said.
			if c.Status == riplog.StatusPass && !verbose && c.ID != "split-write" {
				continue
			}
			lipgloss.Fprintln(out, "    "+riplogCheckLine(c))
		}
		if !verbose {
			lipgloss.Fprintln(out, "    "+style.OK.Render("✓")+" "+res.PassSummary())
		}
		if res.Fails(strict) {
			failures++
		}
	}

	fmt.Fprintln(out)
	checkPrintSummary(out, len(logs), "split", failures, time.Since(start))

	if failures > 0 {
		os.Exit(1)
	}
	return nil
}
