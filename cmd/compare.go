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

var compareCmd = &cobra.Command{
	Use:   "compare a b",
	Short: "Compare the audio of two FLAC files, or of two rips of a disc",
	Long: `Compares the audio of two FLAC files, which can be tracks or images, and says
whether it is the same: for instance, whether a track in the library is the one in
an image. It compares what the files decode to, so how they were encoded,
and their tags, do not matter.

If it is not the same, it says how many samples differ and where, and whether the
differences are only in the first or last few samples. If nearly every sample
differs because one is the other moved along (which is what a wrong drive offset
does, or a track cut with its pregap in another place), it says by how many
samples, and what is left once that is allowed for.

Both files must be CD audio in FLAC (44.1 kHz, 16-bit, stereo), since anything
else would be compared as a conversion of what was stored.

Given the logs of two rips of one disc instead, it compares the images they
name (the cue sheet beside each log) and uses what the logs say about the
drives. A drive that cannot read the first or last few samples of a disc
leaves silence there, so two correct rips can differ slightly.`,
	Args: cobra.ExactArgs(2),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) >= 2 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return []string{"flac", "log"}, cobra.ShellCompDirectiveFilterFileExt
	},
	RunE: runCompare,
}

func init() {
	compareCmd.Flags().BoolP("verbose", "v", false, "Show every check, not only those that did not pass")
	rootCmd.AddCommand(compareCmd)
}

// compareFile resolves one of the arguments to an absolute path, and checks that
// it is a FLAC file or a log, which it reports.
func compareFile(arg string) (path string, isLog bool, err error) {
	abs, err := filepath.Abs(arg)
	if err != nil {
		return "", false, fmt.Errorf("could not resolve path %q: %w", arg, err)
	}

	info, err := os.Stat(abs)
	switch {
	case err != nil:
		return "", false, fmt.Errorf("could not access %q: %w", arg, err)
	case info.IsDir():
		return "", false, fmt.Errorf("%q is a directory: compare takes two FLAC files or two logs", arg)
	case strings.EqualFold(filepath.Ext(abs), ".log"):
		return abs, true, nil
	case !strings.EqualFold(filepath.Ext(abs), ".flac"):
		return "", false, fmt.Errorf("%q is not a .flac file or a .log", arg)
	}
	return abs, false, nil
}

func runCompare(cmd *cobra.Command, args []string) error {
	start := time.Now()

	a, aLog, err := compareFile(args[0])
	if err != nil {
		return err
	}
	b, bLog, err := compareFile(args[1])
	if err != nil {
		return err
	}
	if aLog != bLog {
		return fmt.Errorf("compare takes two FLAC files or two logs, not one of each")
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	out := cmd.OutOrStdout()

	heading := "Comparing audio..."
	if aLog {
		heading = "Comparing rips..."
	}
	lipgloss.Fprintln(out, style.Header.Render(heading))
	fmt.Fprintln(out)
	lipgloss.Fprintln(out, "  "+style.Bold.Render("A")+"  "+style.Name.Render(a))
	lipgloss.Fprintln(out, "  "+style.Bold.Render("B")+"  "+style.Name.Render(b))

	var res *riplog.Result
	if aLog {
		if res, err = riplog.CompareRips(cmd.Context(), a, b, pcm.Decoder{}); err != nil {
			return err
		}
	} else {
		res = riplog.CompareFiles(cmd.Context(), a, b, pcm.Decoder{})
	}
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
	checkPrintSummary(out, 1, "comparison", res.Count(riplog.StatusWarning)+res.Count(riplog.StatusError), time.Since(start))

	if res.Failed() {
		return exitFindings()
	}
	return nil
}
