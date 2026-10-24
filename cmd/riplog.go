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
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/musicbrainz"
	"github.com/mfinelli/musicrename/internal/riplog"
	"github.com/mfinelli/musicrename/internal/style"
)

// riplogLongTemplate is the command's help text. The %s is the table of
// required settings, which is built from the profile itself (see
// riplogRequirements) so that the help cannot disagree with what is checked.
const riplogLongTemplate = `Reads the settings an Exact Audio Copy rip was made with back out of its log and
checks them. EAC goes ahead with a wrong setting without complaint, and the
audio usually still sounds fine, so this is how a mistake gets noticed.

path is a .log file, or a directory whose .log files are all checked (a
multi-disc rip has one per disc). Each log is expected to have its cue sheet,
its FLAC image and a musicbrainz.txt beside it. Run it on the original rip,
before the files are checksummed and the image is split. It defaults to the
current directory.

These settings must have the values shown. The names are the ones EAC uses in
the log:

%s

Also checked:
  - the drive's read offset and overread setting, against what is known for it
  - FLAC.EXE as the encoder, with -V among its options
  - no normalization, matching test and copy CRCs, and no reported errors
  - AccurateRip and CUETools DB results (a disc they do not know is a warning)
  - the cue sheet (same name, .cue): same tracks as the log, every INDEX 01 at
    the log's start sector, and a matching disc ID
  - the FLAC image the cue sheet names: that it is CD audio, and that it holds
    exactly as many samples as the log's table of contents says the disc has
  - that the disc is one the MusicBrainz release in musicbrainz.txt has: the
    disc ID computed from the log is looked up there (--offline skips this)
  - that the log ends with its checksum (present and well formed, not verified)

A required setting the log does not state is an error, not a pass.

Only image rips (one disc, one file, with a cue sheet) from English-language EAC
1.x are supported. Any other log is reported as unsupported, which counts as an
error because it cannot be verified.

Each check ends up as ok, a note, a warning, or an error. Only the checks that
did not pass are listed unless -v is given. The exit status is non-zero when any
log has an error, and with --strict when any has a warning.`

var riplogCmd = &cobra.Command{
	Use:   "riplog [path]",
	Short: "Check an EAC rip log for the correct settings",
	Long:  riplogLong(),
	Args:  cobra.MaximumNArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// Either a log file or a directory is valid here, so fall back to
		// normal completion.
		return nil, cobra.ShellCompDirectiveDefault
	},
	RunE: runRiplog,
}

// riplogLong returns the help text with the required settings filled in.
func riplogLong() string {
	return fmt.Sprintf(riplogLongTemplate, riplogRequirements())
}

// riplogRequirements formats the profile's required settings as an indented
// two-column table: the setting's name and its required value.
func riplogRequirements() string {
	reqs := riplog.Requirements()

	width := 0
	for _, r := range reqs {
		width = max(width, len(r.Label))
	}

	lines := make([]string, len(reqs))
	for i, r := range reqs {
		lines[i] = fmt.Sprintf("  %-*s  %s", width, r.Label, r.Want)
	}
	return strings.Join(lines, "\n")
}

func init() {
	riplogCmd.Flags().Bool("strict", false, "Treat warnings as failures for the exit status")
	riplogCmd.Flags().Bool("offline", false, "Do not look the disc up on MusicBrainz")
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
	offline, _ := cmd.Flags().GetBool("offline")
	out := cmd.OutOrStdout()

	logs, err := riplogTargets(absPath)
	if err != nil {
		return err
	}

	if len(logs) == 1 {
		lipgloss.Fprintln(out, style.Header.Render("Checking rip log..."))
	} else {
		lipgloss.Fprintln(out, style.Header.Render("Checking rip logs..."))
	}
	fmt.Fprintln(out)

	// One lookup serves every log, so a multi-disc rip asks MusicBrainz about
	// its release once. A nil one is no network.
	var mb riplog.Lookup
	if !offline {
		mb = musicbrainz.NewLookup()
	}

	var findings int
	failed := false
	for _, logPath := range logs {
		n, fails := riplogCheckOne(cmd.Context(), out, logPath, mb, strict, verbose)
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
func riplogCheckOne(ctx context.Context, out io.Writer, logPath string, mb riplog.Lookup, strict, verbose bool) (findings int, fails bool) {
	name := filepath.Base(logPath)

	l, res, err := riplog.CheckFile(ctx, logPath, mb)
	if err != nil {
		lipgloss.Fprintln(out, "  "+name)
		lipgloss.Fprintln(out, "    "+style.Error.Render("✗ "+err.Error()))
		return 1, true
	}

	lipgloss.Fprintln(out, "  "+name+"  "+style.Faint.Render(l.Describe()))
	for _, c := range res.Checks {
		if c.Status == riplog.StatusPass && !verbose {
			continue
		}
		lipgloss.Fprintln(out, "    "+riplogCheckLine(c))
	}
	if !verbose {
		lipgloss.Fprintln(out, "    "+style.OK.Render("✓")+" "+res.PassSummary())
	}

	return res.Count(riplog.StatusWarning) + res.Count(riplog.StatusError), res.Fails(strict)
}

// riplogCheckLine formats one check as a sigil, its name and its detail.
func riplogCheckLine(c riplog.Check) string {
	var sigil string
	switch c.Status {
	case riplog.StatusPass:
		sigil = style.OK.Render("✓")
	case riplog.StatusWarning:
		sigil = style.Warn.Render("⚠")
	case riplog.StatusError:
		sigil = style.Error.Render("✗")
	default:
		sigil = style.Faint.Render("·")
	}

	detail := c.Detail
	if c.Status == riplog.StatusPass || c.Status == riplog.StatusInfo {
		detail = style.Faint.Render(detail)
	}
	return sigil + " " + c.Name + ": " + detail
}
