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
	Short: "Compare the audio of two FLAC files",
	Long: `Compares the audio of two FLAC files, which can be tracks or images, and says
whether it is the same: for instance, whether two rips of one disc made with
different drives hold the same audio, or whether a track in the library is the
one in an image. It compares what the files decode to, so how they were encoded,
and their tags, do not matter.

If it is not the same, it says how many samples differ and where, and whether the
differences are only in the first or last few samples. If nearly every sample
differs because one is the other moved along (which is what a wrong drive offset
does, or a track cut with its pregap in another place), it says by how many
samples, and what is left once that is allowed for.

Both files must be CD audio in FLAC (44.1 kHz, 16-bit, stereo), since anything
else would be compared as a conversion of what was stored.`,
	Args: cobra.ExactArgs(2),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) >= 2 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return []string{"flac"}, cobra.ShellCompDirectiveFilterFileExt
	},
	RunE: runCompare,
}

func init() {
	compareCmd.Flags().BoolP("verbose", "v", false, "Show every check, not only those that did not pass")
	rootCmd.AddCommand(compareCmd)
}

// compareFile resolves one of the arguments to an absolute path, and checks that
// it is a FLAC file.
func compareFile(arg string) (string, error) {
	abs, err := filepath.Abs(arg)
	if err != nil {
		return "", fmt.Errorf("could not resolve path %q: %w", arg, err)
	}

	info, err := os.Stat(abs)
	switch {
	case err != nil:
		return "", fmt.Errorf("could not access %q: %w", arg, err)
	case info.IsDir():
		return "", fmt.Errorf("%q is a directory: compare takes two FLAC files", arg)
	case strings.EqualFold(filepath.Ext(abs), ".log"):
		return "", fmt.Errorf("%q is a log: comparing rips is not supported yet, compare the FLAC files", arg)
	case !strings.EqualFold(filepath.Ext(abs), ".flac"):
		return "", fmt.Errorf("%q is not a .flac file", arg)
	}
	return abs, nil
}

func runCompare(cmd *cobra.Command, args []string) error {
	start := time.Now()

	a, err := compareFile(args[0])
	if err != nil {
		return err
	}
	b, err := compareFile(args[1])
	if err != nil {
		return err
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	out := cmd.OutOrStdout()

	lipgloss.Fprintln(out, style.Header.Render("Comparing audio..."))
	fmt.Fprintln(out)
	lipgloss.Fprintln(out, "  "+style.Bold.Render("A")+"  "+style.Name.Render(a))
	lipgloss.Fprintln(out, "  "+style.Bold.Render("B")+"  "+style.Name.Render(b))

	res := riplog.CompareFiles(cmd.Context(), a, b, pcm.Decoder{})
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
		os.Exit(1)
	}
	return nil
}
