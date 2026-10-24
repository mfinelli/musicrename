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
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/riplog"
)

// Lipgloss styles for riplog output. The pass and warning colors are shared
// with check (checkOKStyle, checkFindingStyle); only the colors unique to this
// command are defined here.
var (
	riplogErrorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9")) // bright red
	riplogFaintStyle = lipgloss.NewStyle().Faint(true)
)

var riplogCmd = &cobra.Command{
	Use:   "riplog [path]",
	Short: "Check an EAC rip log for the correct settings",
	Long: `Checks an Exact Audio Copy rip log against the settings a rip should have been
made with. A wrong setting does not stop a rip from completing, and often gives
a file that sounds fine, so the log is the one place such a mistake can be
caught afterwards.

If path is a .log file, that log is checked. If path is a directory, every .log
file directly inside it is checked (a multi-disc rip has one per disc). Run it
on the original rip, before the files are checksummed and the image is split.
If path is omitted it defaults to the current directory.

It checks that the rip was made in secure mode with accurate stream and the
audio cache defeated, without C2 pointers, normalization, or removal of silent
blocks; that the drive's read offset is the one known for it; that the test and
copy passes were both made and agree; that no errors were reported; that
AccurateRip and the CUETools database (if present) confirmed the rip; and that
the audio was encoded with FLAC with verification on.

Each check is shown as ok, a note, a warning, or an error. By default only the
checks that did not pass are listed; use -v to see them all.

Only image rips (one disc, one file, with a cue sheet) from English-language
EAC 1.x are supported. A log of any other kind is reported as unsupported, which
counts as an error, since it cannot be verified.

Exits with a non-zero status when any log has an error, enabling use in scripts.
Warnings (such as a disc that AccurateRip does not know) do not by themselves
fail the run; with --strict they do.`,
	Args: cobra.MaximumNArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// Either a log file or a directory is valid here, so fall back to
		// normal completion.
		return nil, cobra.ShellCompDirectiveDefault
	},
	RunE: runRiplog,
}

func init() {
	riplogCmd.Flags().Bool("strict", false, "Treat warnings as failures for the exit status")
	riplogCmd.Flags().BoolP("verbose", "v", false, "Show every check, not only those that did not pass")
	rootCmd.AddCommand(riplogCmd)
}

func runRiplog(cmd *cobra.Command, args []string) error {
	start := time.Now()

	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", path, err)
	}

	strict, _ := cmd.Flags().GetBool("strict")
	verbose, _ := cmd.Flags().GetBool("verbose")
	out := cmd.OutOrStdout()

	logs, err := riplogTargets(absPath)
	if err != nil {
		return err
	}

	if len(logs) == 1 {
		lipgloss.Fprintln(out, renameHeaderStyle.Render("Checking rip log..."))
	} else {
		lipgloss.Fprintln(out, renameHeaderStyle.Render("Checking rip logs..."))
	}
	fmt.Fprintln(out)

	var findings int
	failed := false
	for _, logPath := range logs {
		n, fails := riplogCheckOne(out, logPath, strict, verbose)
		findings += n
		failed = failed || fails
	}

	fmt.Fprintln(out)
	checkPrintSummary(out, len(logs), "log", findings, time.Since(start))

	if failed {
		os.Exit(1)
	}
	return nil
}

// riplogTargets resolves path to the logs to check: the file itself, or the
// .log files directly inside a directory.
func riplogTargets(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("could not access %q: %w", path, err)
	}

	if !info.IsDir() {
		if !strings.EqualFold(filepath.Ext(path), ".log") {
			return nil, fmt.Errorf("%q is not a .log file", filepath.Base(path))
		}
		return []string{path}, nil
	}

	logs, err := riplog.FindLogs(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if len(logs) == 0 {
		return nil, fmt.Errorf("no .log files in %s", path)
	}
	return logs, nil
}

// riplogCheckOne checks and reports one log. It returns the number of findings
// (errors and warnings) it contributes to the summary and whether the log makes
// the run fail.
func riplogCheckOne(out io.Writer, logPath string, strict, verbose bool) (findings int, fails bool) {
	name := filepath.Base(logPath)

	data, err := os.ReadFile(logPath)
	if err != nil {
		lipgloss.Fprintln(out, "  "+name)
		lipgloss.Fprintln(out, "    "+riplogErrorStyle.Render("✗ could not read: "+err.Error()))
		return 1, true
	}

	l, err := riplog.ParseLog(data)
	if err != nil {
		lipgloss.Fprintln(out, "  "+name)
		var unsupported *riplog.UnsupportedError
		if errors.As(err, &unsupported) {
			lipgloss.Fprintln(out, "    "+riplogErrorStyle.Render("✗ "+err.Error()))
		} else {
			lipgloss.Fprintln(out, "    "+riplogErrorStyle.Render("✗ could not read the log: "+err.Error()))
		}
		return 1, true
	}

	res := riplog.Verify(l)

	lipgloss.Fprintln(out, "  "+name+"  "+riplogFaintStyle.Render(l.Describe()))
	for _, c := range res.Checks {
		if c.Status == riplog.StatusPass && !verbose {
			continue
		}
		lipgloss.Fprintln(out, "    "+riplogCheckLine(c))
	}
	if !verbose {
		lipgloss.Fprintln(out, "    "+checkOKStyle.Render("✓")+" "+res.PassSummary())
	}

	return res.Count(riplog.StatusWarning) + res.Count(riplog.StatusError), res.Fails(strict)
}

// riplogCheckLine formats one check as a sigil, its name and its detail.
func riplogCheckLine(c riplog.Check) string {
	var sigil string
	switch c.Status {
	case riplog.StatusPass:
		sigil = checkOKStyle.Render("✓")
	case riplog.StatusWarning:
		sigil = checkFindingStyle.Render("⚠")
	case riplog.StatusError:
		sigil = riplogErrorStyle.Render("✗")
	default:
		sigil = riplogFaintStyle.Render("·")
	}

	detail := c.Detail
	if c.Status == riplog.StatusPass || c.Status == riplog.StatusInfo {
		detail = riplogFaintStyle.Render(detail)
	}
	return sigil + " " + c.Name + ": " + detail
}
