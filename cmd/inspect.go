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
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"go.senan.xyz/taglib"

	"github.com/mfinelli/musicrename/internal/sanitize"
)

// inspectLabelWidth is the column at which tag values begin, measured in
// runes. Wide enough for "Album Artist:" (12 chars) plus one space.
const inspectLabelWidth = 14

// Lipgloss styles for inspect output.
var (
	inspectFaintStyle    = lipgloss.NewStyle().Faint(true)
	inspectBoldStyle     = lipgloss.NewStyle().Bold(true)
	inspectFilenameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6")) // cyan, matches renameAlbumStyle
)

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

	var syncedLyrics, unsyncedLyrics string
	switch ext {
	case ".flac":
		syncedLyrics = getFirst(taglib.Lyrics)
		unsyncedLyrics = getFirst("UNSYNCEDLYRICS")
	default: // .mp3, .m4a: plain text is stored in the LYRICS tag
		unsyncedLyrics = getFirst(taglib.Lyrics)
	}

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

	// Header.
	fmt.Fprintln(out, renameHeaderStyle.Render("Inspecting..."))
	fmt.Fprintln(out)

	// File line: cyan filename + faint format badge, indented.
	formatLabel := strings.ToUpper(strings.TrimPrefix(ext, "."))
	fmt.Fprintln(out,
		"  "+
			inspectFilenameStyle.Render(filepath.Base(path))+
			"  "+
			inspectFaintStyle.Render(formatLabel),
	)
	fmt.Fprintln(out)

	// Metadata fields, indented two spaces.
	inspectPrintTagField(out, "Title", title, cleanTitle)
	inspectPrintTagField(out, "Artist", artist, cleanArtist)
	inspectPrintTagField(out, "Album Artist", albumArtist, cleanAlbumArtist)
	inspectPrintTagField(out, "Album", album, cleanAlbum)
	fmt.Fprintln(out)

	yearDisplay := inspectDash(year)
	if rawDate != "" && rawDate != year {
		yearDisplay = fmt.Sprintf("%s  %s", year, inspectFaintStyle.Render("(DATE: "+strconv.Quote(rawDate)+")"))
	}
	inspectPrintField(out, "Year", yearDisplay)
	inspectPrintField(out, "Track", inspectDash(trackNum))
	inspectPrintField(out, "Disc", inspectDash(discNum))
	fmt.Fprintln(out)

	if ext == ".flac" {
		inspectPrintField(out, "Synced", inspectLyricsPreview(syncedLyrics))
	}
	inspectPrintField(out, "Unsynced", inspectLyricsPreview(unsyncedLyrics))

	return nil
}

// inspectPrintTagField prints a bold label + raw value line and, when the
// value is non-empty, a faint sanitized line beneath it.
func inspectPrintTagField(out io.Writer, label, raw string, clean sanitize.Result) {
	inspectPrintField(out, label, inspectDash(raw))
	if raw != "" {
		inspectPrintSanitized(out, clean)
	}
}

// inspectPrintField writes "  Label:  value" with the label in bold. Because
// lipgloss bold adds ANSI escape bytes that are not counted by %-*s, padding
// is computed from the unstyled label length and applied manually.
func inspectPrintField(out io.Writer, label, value string) {
	styled := inspectBoldStyle.Render(label + ":")
	// Pad based on rune count of the plain label + colon so that ANSI bytes
	// in the styled version don't throw off the column alignment.
	plainWidth := utf8.RuneCountInString(label + ":")
	pad := ""
	if inspectLabelWidth > plainWidth {
		pad = strings.Repeat(" ", inspectLabelWidth-plainWidth)
	}
	fmt.Fprintf(out, "  %s%s%s\n", styled, pad, value)
}

// inspectPrintSanitized writes the faint "↳ value  [manual override]" line
// beneath each text metadata field, aligned to the value column.
func inspectPrintSanitized(out io.Writer, result sanitize.Result) {
	// Indent by 2 (global indent) + inspectLabelWidth to align with values.
	indent := strings.Repeat(" ", 2+inspectLabelWidth)
	line := "↳ " + result.Value
	if result.ManualOverride {
		line += "  [manual override]"
	}
	fmt.Fprintln(out, indent+inspectFaintStyle.Render(line))
}

// inspectDash returns s unchanged, or "—" if s is empty.
func inspectDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// inspectLyricsPreview returns a truncated first content line from a lyrics
// string, or "—" if the string is empty. Up to 50 runes of the first
// non-empty line are shown; longer lines are suffixed with "…".
func inspectLyricsPreview(s string) string {
	if s == "" {
		return "—"
	}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		runes := []rune(line)
		if len(runes) > 50 {
			return string(runes[:50]) + "…"
		}
		return line
	}
	return "—"
}
