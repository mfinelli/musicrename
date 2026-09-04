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
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"go.senan.xyz/taglib"

	"github.com/mfinelli/musicrename/internal/lyrics"
	"github.com/mfinelli/musicrename/internal/metadata"
)

// Lipgloss styles for lyrics output. Header, album, rule, bold, and warning
// styles are reused from rename.go; they are in the same package.
var (
	lyricsEmbeddedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green
	lyricsSkippedStyle  = lipgloss.NewStyle().Faint(true)                     // dim
	lyricsNotFoundStyle = lipgloss.NewStyle().Faint(true)                     // dim
	lyricsFailedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")) // red
)

var lyricsCmd = &cobra.Command{
	Use:   "lyrics [path]",
	Short: "Fetch and embed lyrics from LRCLIB",
	Long: `Fetches lyrics from the LRCLIB public API and embeds them into audio
file tags. Supports FLAC, MP3, and M4A files.

For FLAC files, synced (LRC) lyrics are stored in the LYRICS tag and plain
text in UNSYNCEDLYRICS. For MP3 and M4A files, plain text lyrics are stored
in the LYRICS tag (USLT / ©lyr); synced-only results are skipped for these
formats.

If path is an audio file, only that file is processed (track mode). If path
directly contains audio files it is treated as a single album root. Otherwise
path is treated as a library root and all albums within it are processed
recursively.

If path is omitted it defaults to the current working directory.

Existing lyrics tags are never overwritten unless --force is passed.`,
	Args: cobra.MaximumNArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// Either an audio file or a directory is valid here, so fall back
		// to normal completion rather than filtering to audio extensions.
		return nil, cobra.ShellCompDirectiveDefault
	},
	RunE: runLyrics,
}

func init() {
	lyricsCmd.Flags().Bool("force", false, "Re-fetch and overwrite existing lyrics")
	rootCmd.AddCommand(lyricsCmd)
}

func runLyrics(cmd *cobra.Command, args []string) error {
	start := time.Now()

	target := "."
	if len(args) > 0 {
		target = args[0]
	}

	abs, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", target, err)
	}

	force, _ := cmd.Flags().GetBool("force")
	out := cmd.OutOrStdout()
	ctx := context.Background()

	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("accessing %s: %w", abs, err)
	}

	if !info.IsDir() {
		ext := strings.ToLower(filepath.Ext(abs))
		if !metadata.IsAudioExt(ext) {
			return fmt.Errorf("%s is not a supported audio file (expected one of: %s)",
				abs, metadata.DottedExtList(metadata.AudioExtensions))
		}
		return runLyricsTrack(ctx, out, abs, force, start)
	}

	isAlbum, err := lyricsIsAlbumRoot(abs)
	if err != nil {
		return fmt.Errorf("reading %s: %w", abs, err)
	}

	if isAlbum {
		return runLyricsAlbum(ctx, out, abs, force, start)
	}
	return runLyricsLibrary(ctx, out, abs, force, start)
}

// lyricsIsAlbumRoot reports whether dir directly contains at least one audio file.
func lyricsIsAlbumRoot(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if !e.IsDir() && metadata.IsAudioExt(strings.ToLower(filepath.Ext(e.Name()))) {
			return true, nil
		}
	}
	return false, nil
}

// runLyricsTrack handles track mode: a single audio file.
func runLyricsTrack(ctx context.Context, out io.Writer, path string, force bool, start time.Time) error {
	lipgloss.Fprintln(out, renameHeaderStyle.Render("Fetching lyrics..."))
	fmt.Fprintln(out)

	tracks, warnings := buildTrackInfos([]string{path})
	for _, w := range warnings {
		lipgloss.Fprintln(out, "  "+renameWarningStyle.Render("⚠  "+w))
	}

	summary, err := lyrics.Fetch(ctx, tracks, force, lyricsProgressCallback(out, "  "))
	if err != nil {
		return err
	}

	fmt.Fprintln(out)
	printLyricsSummaryLine(out, summary, time.Since(start))
	return nil
}

// runLyricsAlbum handles album mode: a directory containing audio files directly.
func runLyricsAlbum(ctx context.Context, out io.Writer, dir string, force bool, start time.Time) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading %s: %w", dir, err)
	}

	var paths []string
	for _, e := range entries {
		if !e.IsDir() && metadata.IsAudioExt(strings.ToLower(filepath.Ext(e.Name()))) {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(paths)

	lipgloss.Fprintln(out, renameHeaderStyle.Render("Fetching lyrics..."))
	fmt.Fprintln(out)

	tracks, warnings := buildTrackInfos(paths)
	for _, w := range warnings {
		lipgloss.Fprintln(out, "  "+renameWarningStyle.Render("⚠  "+w))
	}

	summary, err := lyrics.Fetch(ctx, tracks, force, lyricsProgressCallback(out, "  "))
	if err != nil {
		return err
	}

	fmt.Fprintln(out)
	printLyricsSummaryLine(out, summary, time.Since(start))
	return nil
}

// runLyricsLibrary handles library mode: a directory containing album subdirectories.
func runLyricsLibrary(ctx context.Context, out io.Writer, dir string, force bool, start time.Time) error {
	albums, err := metadata.ScanLibrary(dir)
	if err != nil {
		return fmt.Errorf("scanning library: %w", err)
	}

	sort.Slice(albums, func(i, j int) bool {
		return albums[i].RootPath < albums[j].RootPath
	})

	lipgloss.Fprintln(out, renameHeaderStyle.Render("Fetching lyrics..."))
	fmt.Fprintln(out)

	if len(albums) == 0 {
		fmt.Fprintln(out, "No albums found.")
		return nil
	}

	var total lyrics.Summary

	for _, album := range albums {
		relPath, err := filepath.Rel(dir, album.RootPath)
		if err != nil {
			relPath = album.RootPath
		}
		lipgloss.Fprintln(out, "  "+renameAlbumStyle.Render(relPath))

		paths := make([]string, 0, len(album.Tracks))
		for _, t := range album.Tracks {
			paths = append(paths, t.Path)
		}
		sort.Strings(paths)

		tracks, warnings := buildTrackInfos(paths)
		for _, w := range warnings {
			lipgloss.Fprintln(out, "    "+renameWarningStyle.Render("⚠  "+w))
		}

		summary, err := lyrics.Fetch(ctx, tracks, force, lyricsProgressCallback(out, "    "))
		if err != nil {
			return fmt.Errorf("processing %s: %w", relPath, err)
		}

		total.Embedded += summary.Embedded
		total.Skipped += summary.Skipped
		total.NotFound += summary.NotFound
		total.Failed += summary.Failed

		fmt.Fprintln(out)
	}

	printLyricsSummaryLine(out, total, time.Since(start))
	return nil
}

// lyricsProgressCallback returns a progress function that prints each track's
// outcome to out with a distinct sigil and colour for each status.
func lyricsProgressCallback(out io.Writer, indent string) func(string, lyrics.LyricStatus) {
	return func(path string, status lyrics.LyricStatus) {
		name := filepath.Base(path)
		var line string
		switch status {
		case lyrics.StatusEmbedded:
			line = lyricsEmbeddedStyle.Render("✓  " + name)
		case lyrics.StatusSkipped:
			line = lyricsSkippedStyle.Render("—  " + name + "  (already has lyrics)")
		case lyrics.StatusNotFound:
			line = lyricsNotFoundStyle.Render("✗  " + name + "  (not found)")
		case lyrics.StatusFailed:
			line = lyricsFailedStyle.Render("!  " + name + "  (failed)")
		}
		fmt.Fprintln(out, indent+line)
	}
}

// printLyricsSummaryLine renders the rule and summary line at the bottom of
// every lyrics run, consistent with the rename and sums commands.
func printLyricsSummaryLine(out io.Writer, s lyrics.Summary, elapsed time.Duration) {
	total := s.Embedded + s.Skipped + s.NotFound + s.Failed
	summaryText := fmt.Sprintf(
		"%d tracks · %d embedded · %d skipped · %d not found · %d failed · %s",
		total, s.Embedded, s.Skipped, s.NotFound, s.Failed, elapsed.Round(10*time.Millisecond),
	)
	lipgloss.Fprintln(out, renameRuleStyle.Render(strings.Repeat("─", len(summaryText))))
	lipgloss.Fprintln(out, renameBoldStyle.Render(summaryText))
}

// buildTrackInfos opens each audio file with go-taglib to read both tags and
// audio properties (for duration), returning a TrackInfo slice ready for
// lyrics.Fetch. Files that cannot be opened are skipped; a warning is
// appended for each failure.
func buildTrackInfos(paths []string) ([]lyrics.TrackInfo, []string) {
	tracks := make([]lyrics.TrackInfo, 0, len(paths))
	var warnings []string

	for _, path := range paths {
		f, err := taglib.OpenReadOnly(path)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("could not read %s: %v", filepath.Base(path), err))
			continue
		}

		tags := f.Tags()
		props := f.Properties()
		f.Close()

		getFirst := func(key string) string {
			if vals, ok := tags[key]; ok && len(vals) > 0 {
				return vals[0]
			}
			return ""
		}

		// Use track-level ARTIST for LRCLIB queries; ALBUMARTIST is a fallback
		// for compilations where the track artist is the relevant performer.
		artist := getFirst(taglib.Artist)
		if artist == "" {
			artist = getFirst(taglib.AlbumArtist)
		}

		tracks = append(tracks, lyrics.TrackInfo{
			Path:     path,
			Title:    getFirst(taglib.Title),
			Artist:   artist,
			Album:    getFirst(taglib.Album),
			Duration: props.Length,
		})
	}

	return tracks, warnings
}
