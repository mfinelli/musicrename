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
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/lyrics"
	"github.com/mfinelli/musicrename/internal/metadata"
	"github.com/mfinelli/musicrename/internal/style"
)

var lyricsDiffCmd = &cobra.Command{
	Use:   "diff [path]",
	Short: "Compare embedded lyrics with what a refetch from LRCLIB would write",
	Long: `Compares the lyrics embedded in audio files with what "mrr lyrics --force"
would write to them now, without changing anything, so that a refetch doesn't
have to be done blind.

Each file is looked up on LRCLIB the way lyrics does it, and only what a refetch
would write is compared: the plain lyrics and, for FLAC files, the synced lyrics
and the word-synced Lyricsfile. Synced lyrics are compared as normalized text,
apart from their timing, so an upstream retiming shows as a retiming instead of
every line having changed. The Lyricsfile is compared by what it says, not as
text, since it is mostly timings; --raw also shows a text diff of it.

If path is an audio file it is compared in full detail (track mode). Otherwise
every album under path is compared, and only the tracks that differ are listed;
-v adds the details of each. path defaults to the current directory. A library
costs a request per track at the pace LRCLIB asks for, so it takes a while.

Each track is marked: ✓ identical, ~ changed, + word-sync available or new
lyrics, ✗ not found, ! failed. How the LRC or the YAML is formatted doesn't
count as a difference.

--url compares a single file with a specific LRCLIB track, as lyrics --url does.`,
	Args: cobra.MaximumNArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// Either an audio file or a directory is valid here, so fall back
		// to normal completion rather than filtering to audio extensions.
		return nil, cobra.ShellCompDirectiveDefault
	},
	RunE: runLyricsDiff,
}

func init() {
	lyricsDiffCmd.Flags().String("url", "", "Compare with a specific LRCLIB track by URL (track mode only)")
	lyricsDiffCmd.Flags().Bool("full-context", false, "Show the whole lyrics in each diff, not only the lines around changes")
	lyricsDiffCmd.Flags().Bool("raw", false, "Also show a text diff of the Lyricsfile documents")
	lyricsDiffCmd.Flags().Bool("skip-lyricsfile", false, "Compare as lyrics --skip-lyricsfile would write")
	lyricsDiffCmd.Flags().BoolP("verbose", "v", false, "Show the details of each differing track, and list retimed lines")
	lyricsCmd.AddCommand(lyricsDiffCmd)
}

// lyricsDiffPalette is how lyrics diff output is styled, built from the shared
// styles: added text green, removed text red, and the words that changed within
// a line in reverse video.
func lyricsDiffPalette() lyrics.Palette {
	render := func(st lipgloss.Style) func(string) string {
		return func(s string) string { return st.Render(s) }
	}
	return lyrics.Palette{
		Add:     render(style.OK),
		Del:     render(style.Error),
		AddWord: render(style.OK.Reverse(true)),
		DelWord: render(style.Error.Reverse(true)),
		OK:      render(style.OK),
		Warn:    render(style.Warn),
		Err:     render(style.Error),
		Name:    render(style.Name),
		Faint:   render(style.Faint),
		Bold:    render(style.Bold),
	}
}

// lyricsDiffRun holds what the modes of lyrics diff have in common.
type lyricsDiffRun struct {
	out     io.Writer
	opts    lyrics.DiffOptions
	verbose bool
	pal     lyrics.Palette
	isTTY   bool
	start   time.Time

	// total counts every track compared, across albums.
	total lyrics.DiffSummary
}

func runLyricsDiff(cmd *cobra.Command, args []string) error {
	start := time.Now()

	target := "."
	if len(args) > 0 {
		target = args[0]
	}

	abs, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", target, err)
	}

	rawURL, _ := cmd.Flags().GetString("url")
	fullContext, _ := cmd.Flags().GetBool("full-context")
	showRaw, _ := cmd.Flags().GetBool("raw")
	skipLyricsfile, _ := cmd.Flags().GetBool("skip-lyricsfile")
	verbose, _ := cmd.Flags().GetBool("verbose")

	opts := lyrics.DiffOptions{
		Context:        lyrics.DefaultContext,
		Raw:            showRaw,
		SkipLyricsfile: skipLyricsfile,
	}
	if fullContext {
		opts.Context = lyrics.FullContext
	}

	r := &lyricsDiffRun{
		out:     cmd.OutOrStdout(),
		opts:    opts,
		verbose: verbose,
		pal:     lyricsDiffPalette(),
		isTTY:   isatty.IsTerminal(os.Stdout.Fd()),
		start:   start,
	}
	ctx := context.Background()

	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("accessing %s: %w", abs, err)
	}

	if rawURL != "" {
		if info.IsDir() {
			return fmt.Errorf("--url is only valid with a single track, not a directory (%s)", abs)
		}
		if err := lyricsDiffCheckAudio(abs); err != nil {
			return err
		}
		id, err := lyrics.ParseID(rawURL)
		if err != nil {
			return fmt.Errorf("--url: %w", err)
		}
		return r.trackByID(ctx, abs, id)
	}

	if !info.IsDir() {
		if err := lyricsDiffCheckAudio(abs); err != nil {
			return err
		}
		return r.track(ctx, abs)
	}

	isAlbum, err := metadata.IsAlbumRoot(abs)
	if err != nil {
		return fmt.Errorf("reading %s: %w", abs, err)
	}
	if isAlbum {
		return r.album(ctx, abs)
	}
	return r.library(ctx, abs)
}

// lyricsDiffCheckAudio reports an error unless path is a supported audio file.
func lyricsDiffCheckAudio(path string) error {
	ext := strings.ToLower(filepath.Ext(path))
	if !metadata.IsAudioExt(ext) {
		return fmt.Errorf("%s is not a supported audio file (expected one of: %s)",
			path, metadata.DottedExtList(metadata.AudioExtensions))
	}
	return nil
}

// track handles track mode: one file, in full detail.
func (r *lyricsDiffRun) track(ctx context.Context, path string) error {
	r.heading()

	_, err := r.compare(ctx, []string{path}, "  ", "", true)
	if err != nil {
		return err
	}

	fmt.Fprintln(r.out)
	printLyricsDiffSummaryLine(r.out, r.total, time.Since(r.start))
	return nil
}

// trackByID handles track mode with --url: one file against a specific record.
func (r *lyricsDiffRun) trackByID(ctx context.Context, path string, id int) error {
	r.heading()

	d, err := lyrics.DiffByID(ctx, path, id, r.opts)
	if err != nil {
		return err
	}
	r.total.Add(d.Status)
	r.printTrack(d, "  ", true)

	fmt.Fprintln(r.out)
	printLyricsDiffSummaryLine(r.out, r.total, time.Since(r.start))
	return nil
}

// album handles album mode: a directory containing audio files directly.
func (r *lyricsDiffRun) album(ctx context.Context, dir string) error {
	paths, err := metadata.AudioFiles(dir)
	if err != nil {
		return err
	}

	r.heading()

	if _, err := r.compare(ctx, paths, "  ", "", false); err != nil {
		r.finish()
		return err
	}

	r.finish()
	return nil
}

// library handles library mode: a directory containing album subdirectories.
func (r *lyricsDiffRun) library(ctx context.Context, dir string) error {
	albums, err := metadata.LibraryAlbums(dir)
	if err != nil {
		return err
	}

	r.heading()

	if len(albums) == 0 {
		fmt.Fprintln(r.out, "No albums found.")
		return nil
	}

	for _, album := range albums {
		printed, err := r.compare(ctx, album.Paths, "    ", album.Rel, false)
		if err != nil {
			// Show how far it got before reporting why it stopped.
			r.finish()
			return fmt.Errorf("processing %s: %w", album.Rel, err)
		}
		if printed {
			fmt.Fprintln(r.out)
		}
	}

	printLyricsDiffSummaryLine(r.out, r.total, time.Since(r.start))
	return nil
}

// finish ends an album-mode run, with a blank line and the summary.
func (r *lyricsDiffRun) finish() {
	fmt.Fprintln(r.out)
	printLyricsDiffSummaryLine(r.out, r.total, time.Since(r.start))
}

func (r *lyricsDiffRun) heading() {
	lipgloss.Fprintln(r.out, style.Header.Render("Comparing lyrics..."))
	fmt.Fprintln(r.out)
}

// clearProgress erases the progress line, on a terminal.
func (r *lyricsDiffRun) clearProgress() {
	if r.isTTY {
		fmt.Fprint(r.out, "\r\033[K")
	}
}

// compare compares the lyrics of paths with LRCLIB and writes what differs as
// the results arrive, counting every track in r.total. Tracks that are
// identical are only counted, unless alwaysDetails is set (track mode), which
// also shows every track in full detail; otherwise details are shown for the
// tracks that differ if -v was given. heading, if not empty, is the album's
// name, written once before the first track listed. It reports whether
// anything was written under that heading.
func (r *lyricsDiffRun) compare(ctx context.Context, paths []string, indent, heading string, alwaysDetails bool) (bool, error) {
	tracks, warnings := buildTrackInfos(paths)

	pending := heading
	flushHeading := func() {
		if pending != "" {
			lipgloss.Fprintln(r.out, "  "+style.Name.Render(pending))
			pending = ""
		}
	}

	printed := false
	for _, w := range warnings {
		flushHeading()
		lipgloss.Fprintln(r.out, indent+style.Warn.Render("⚠  "+w))
		printed = true
	}

	_, err := lyrics.Diff(ctx, tracks, r.opts, func(d lyrics.TrackDiff) {
		r.total.Add(d.Status)

		r.clearProgress()
		if alwaysDetails || d.Status != lyrics.DiffSame {
			flushHeading()
			r.printTrack(d, indent, alwaysDetails || r.verbose)
			printed = true
		}
		// A run over a library is slow and mostly silent, so say what it is
		// up to, on a terminal.
		if r.isTTY {
			fmt.Fprintf(r.out, "\r\033[K  → %s", filepath.Base(d.Path))
		}
	})
	r.clearProgress()
	return printed, err
}

// printTrack writes the line for one track and, if details is set, what was
// found, indented beneath it.
func (r *lyricsDiffRun) printTrack(d lyrics.TrackDiff, indent string, details bool) {
	line := indent + lyrics.StatusMarker(d, r.pal) + "  " + style.Name.Render(filepath.Base(d.Path))
	if headline := lyrics.Headline(d); headline != "" {
		line += "  " + style.Faint.Render(headline)
	}
	lipgloss.Fprintln(r.out, line)

	if !details {
		return
	}

	// The details are styled text, which only lipgloss's writers strip of
	// color when the output isn't a terminal, so they are rendered to a
	// buffer and written through one.
	var buf strings.Builder
	lyrics.RenderDetails(&buf, d, r.pal, indent+"   ", lyrics.RenderOptions{ListRetimed: r.verbose})
	lipgloss.Fprint(r.out, buf.String())
}

// printLyricsDiffSummaryLine renders the rule and summary line at the bottom of
// every lyrics diff run, like the other lyrics modes.
func printLyricsDiffSummaryLine(out io.Writer, s lyrics.DiffSummary, elapsed time.Duration) {
	text := fmt.Sprintf(
		"%d tracks · %d identical · %d changed · %d word-sync available · %d new · %d not found · %d failed · %s",
		s.Total(), s.Same, s.Changed, s.WordSync, s.New, s.NotFound, s.Failed, elapsed.Round(10*time.Millisecond),
	)
	lipgloss.Fprintln(out, style.Rule.Render(strings.Repeat("─", utf8.RuneCountInString(text))))
	lipgloss.Fprintln(out, style.Bold.Render(text))
}
