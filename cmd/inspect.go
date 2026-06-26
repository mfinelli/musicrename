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
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"go.senan.xyz/taglib"

	"github.com/mfinelli/musicrename/internal/sanitize"
)

// inspectLabelWidth is the column at which tag values begin. It is wide enough
// to accommodate the longest label ("Album Artist:") plus one space.
const inspectLabelWidth = 14

var inspectCmd = &cobra.Command{
	Use:   "inspect <file>",
	Short: "Display detected and sanitized metadata for an audio file",
	Long: `Reads the metadata tags from an audio file and prints them alongside their
sanitized equivalents (the values that would be used when renaming).`,
	Args: cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// Restrict file completion to supported audio extensions.
		return []string{"flac", "mp3", "m4a"}, cobra.ShellCompDirectiveFilterFileExt
	},
	RunE: runInspect,
}

func init() {
	rootCmd.AddCommand(inspectCmd)
}

func runInspect(cmd *cobra.Command, args []string) error {
	path := args[0]

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".flac", ".mp3", ".m4a":
		// supported
	default:
		return fmt.Errorf(
			"%q is not a supported audio file (expected .flac, .mp3, or .m4a)",
			filepath.Base(path),
		)
	}

	file, err := taglib.OpenReadOnly(path)
	if err != nil {
		return fmt.Errorf("could not open %q: %w", path, err)
	}
	defer file.Close()

	tags := file.Tags()
	if tags == nil {
		return fmt.Errorf("no tags found in %q", filepath.Base(path))
	}

	// getFirst returns the first value for a tag key, or empty string.
	getFirst := func(key string) string {
		if vals, ok := tags[key]; ok && len(vals) > 0 {
			return vals[0]
		}
		return ""
	}

	title := getFirst(taglib.Title)
	artist := getFirst(taglib.Artist)
	albumArtist := getFirst(taglib.AlbumArtist)
	album := getFirst(taglib.Album)
	rawDate := getFirst(taglib.Date)
	trackNum := getFirst(taglib.TrackNumber)
	discNum := getFirst(taglib.DiscNumber)

	// Extract the four-character year from a potentially full ISO-8601 date.
	year := ""
	if rawDate != "" {
		year = strings.SplitN(rawDate, "-", 2)[0]
	}

	// Sanitize the text fields that feed into directory and file names.
	cleanTitle := sanitize.CleanStringResult(title, sanitize.TrackOverride)
	cleanArtist := sanitize.CleanStringResult(artist, sanitize.ArtistOverride)
	cleanAlbumArtist := sanitize.CleanStringResult(albumArtist, sanitize.ArtistOverride)
	cleanAlbum := sanitize.CleanStringResult(album, sanitize.AlbumOverride)

	out := cmd.OutOrStdout()

	// header
	formatLabel := strings.ToUpper(strings.TrimPrefix(ext, "."))
	inspectPrintField(out, "File", fmt.Sprintf("%s  (%s)", filepath.Base(path), formatLabel))
	fmt.Fprintln(out)

	// text metadata
	inspectPrintTagField(out, "Title", title, cleanTitle)
	inspectPrintTagField(out, "Artist", artist, cleanArtist)
	inspectPrintTagField(out, "Album Artist", albumArtist, cleanAlbumArtist)
	inspectPrintTagField(out, "Album", album, cleanAlbum)
	fmt.Fprintln(out)

	// other metadata
	yearDisplay := inspectDash(year)
	if rawDate != "" && rawDate != year {
		yearDisplay = fmt.Sprintf("%s  (DATE: %q)", year, rawDate)
	}
	inspectPrintField(out, "Year", yearDisplay)
	inspectPrintField(out, "Track", inspectDash(trackNum))
	inspectPrintField(out, "Disc", inspectDash(discNum))

	return nil
}

// inspectPrintTagField prints a raw tag value and, when the value is non-empty,
// a dim second line showing its sanitized equivalent.
func inspectPrintTagField(out io.Writer, label, raw string, clean sanitize.Result) {
	inspectPrintField(out, label, inspectDash(raw))
	if raw != "" {
		inspectPrintSanitized(out, clean)
	}
}

// inspectPrintField writes a single label+value line, padding the label to
// inspectLabelWidth so values align across all fields.
func inspectPrintField(out io.Writer, label, value string) {
	fmt.Fprintf(out, "%-*s%s\n", inspectLabelWidth, label+":", value)
}

// inspectPrintSanitized writes the dim "↳ value  [manual override]" line that
// appears beneath each text metadata field.
func inspectPrintSanitized(out io.Writer, result sanitize.Result) {
	indent := strings.Repeat(" ", inspectLabelWidth)
	line := "↳ " + result.Value
	if result.ManualOverride {
		line += "  [manual override]"
	}
	fmt.Fprintln(out, indent+inspectDim(line))
}

// inspectDim wraps s in ANSI dim escape codes.
func inspectDim(s string) string {
	return "\033[2m" + s + "\033[0m"
}

// inspectDash returns s unchanged, or "—" if s is empty.
func inspectDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
