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
	"os"

	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/lyrics"
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
  mrr rename       # move files into place
  mrr replaygain   # calculate ReplayGain tags
  mrr lyrics       # fetch and embed lyrics
  mrr check        # audit the result
  mrr sums         # generate md5 checksums`,
	CompletionOptions: cobra.CompletionOptions{HiddenDefaultCmd: true},
	Version:           "4.8.0",
}

func init() {
	// The version literal above is the only place the release tooling
	// updates, so LRCLIB's User-Agent takes it from here rather than
	// keeping its own copy.
	lyrics.Version = rootCmd.Version
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
