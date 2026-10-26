/*
 * Copyright © 2020-2026 Mario Finelli
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
	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/exitstatus"
)

var rootCmd = &cobra.Command{
	Use:   "mrr",
	Short: "Normalize and organize a local music library",
	Long: `musicrename is a CLI tool for normalizing a local music library into a
consistent, sanitized directory hierarchy based on internal metadata tags.

Files are organized as:
  [first letter]/[artist]/[year] [album]/[track] [title].ext

Artist, album, and title strings are transliterated to ASCII, lowercased, and
stripped of non-alphanumeric characters before use.

Intended workflow:
  mrr rename   # move files into place
  mrr lyrics   # fetch and embed lyrics
  mrr check    # audit the result
  mrr sums     # generate md5 checksums`,
	CompletionOptions: cobra.CompletionOptions{HiddenDefaultCmd: true},
	Version:           "4.5.0",
}

// Execute runs the command line and returns the exit status for the process to
// end with. A command that has something to say about its status returns it,
// so that everything it deferred has run by the time the process ends.
//
// cobra reports the errors that commands return, except those that a command
// has said it has reported (see quietExit).
func Execute() int {
	if err := rootCmd.Execute(); err != nil {
		return exitstatus.Code(err)
	}
	return 0
}

// quietExit returns an error that ends the command with the status, which cobra
// does not report: it cannot tell an error that has been reported from one that
// has not, so it is told, here, that it is not to. Nothing else about how it
// reports errors is changed.
func quietExit(code int) error {
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true
	return exitstatus.New(code)
}

// exitFindings is what a command that audits something returns when it found
// something, after it has printed what it found. It makes the process exit with
// a non-zero status, which is how the check commands say that there are findings,
// and nothing more: it is not an error to report.
//
// It is returned, and not acted on where it is found with os.Exit, so that the
// command unwinds as it would for any other error, and what it deferred runs.
func exitFindings() error {
	return quietExit(1)
}
