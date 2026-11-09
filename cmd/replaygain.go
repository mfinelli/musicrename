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
	"errors"
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

	"github.com/mfinelli/musicrename/internal/hasher"
	"github.com/mfinelli/musicrename/internal/metadata"
	"github.com/mfinelli/musicrename/internal/replaygain"
	"github.com/mfinelli/musicrename/internal/style"
)

var replaygainCmd = &cobra.Command{
	Use:   "replaygain [path]",
	Short: "Calculate and write ReplayGain tags for an album or library",
	Long: `Calculates ReplayGain with rsgain (which must be on PATH) and writes the
REPLAYGAIN_TRACK_GAIN, REPLAYGAIN_TRACK_PEAK, REPLAYGAIN_ALBUM_GAIN, and
REPLAYGAIN_ALBUM_PEAK tags to every audio file of each album.

If path directly contains audio files it is treated as a single album and only
that album is processed. Otherwise path is treated as a library root and all
albums within it are processed recursively. path defaults to the current
directory. Album values are calculated over every track of an album together,
so a single file is not accepted: pass the album's directory.

If an album has a sums.md5, the entries of the files that changed are updated,
and only those (pass --skip-md5 to leave it alone).`,
	Args: cobra.MaximumNArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// Either an album or a library root is valid here, so fall back to
		// normal completion.
		return nil, cobra.ShellCompDirectiveDefault
	},
	RunE: runReplaygain,
}

func init() {
	replaygainCmd.Flags().Bool("skip-md5", false, "Do not update sums.md5 even if it exists")
	rootCmd.AddCommand(replaygainCmd)
}

// replaygainRun holds what the album and library modes have in common.
type replaygainRun struct {
	out     io.Writer
	skipMD5 bool
	isTTY   bool
	start   time.Time

	albums  int // albums scanned successfully
	tracks  int // tracks in them
	changed int // files whose content changed
	failed  int // albums that could not be scanned
}

func runReplaygain(cmd *cobra.Command, args []string) error {
	start := time.Now()

	target := "."
	if len(args) > 0 {
		target = args[0]
	}

	abs, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", target, err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("accessing %s: %w", abs, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is a file: album gain is calculated over a whole album, so pass its directory", abs)
	}

	skipMD5, _ := cmd.Flags().GetBool("skip-md5")
	r := &replaygainRun{
		out:     cmd.OutOrStdout(),
		skipMD5: skipMD5,
		isTTY:   isatty.IsTerminal(os.Stdout.Fd()),
		start:   start,
	}
	ctx := context.Background()

	isAlbum, err := metadata.IsAlbumRoot(abs)
	if err != nil {
		return fmt.Errorf("reading %s: %w", abs, err)
	}
	if isAlbum {
		return r.album(ctx, abs)
	}
	return r.library(ctx, abs)
}

// album handles album mode: a directory containing audio files directly.
func (r *replaygainRun) album(ctx context.Context, dir string) error {
	paths, err := metadata.AudioFiles(dir)
	if err != nil {
		return err
	}

	r.heading()
	r.scan(ctx, dir, filepath.Base(dir), paths)
	return r.finish()
}

// library handles library mode: a directory containing album subdirectories.
func (r *replaygainRun) library(ctx context.Context, root string) error {
	albums, err := metadata.LibraryAlbums(root)
	if err != nil {
		return err
	}

	r.heading()

	if len(albums) == 0 {
		fmt.Fprintln(r.out, "No albums found.")
		return nil
	}

	for _, album := range albums {
		r.scan(ctx, album.RootPath, album.Rel, album.Paths)
	}
	return r.finish()
}

func (r *replaygainRun) heading() {
	lipgloss.Fprintln(r.out, style.Header.Render("Scanning ReplayGain..."))
	fmt.Fprintln(r.out)
}

// clearProgress erases the progress line, on a terminal.
func (r *replaygainRun) clearProgress() {
	if r.isTTY {
		fmt.Fprint(r.out, "\r\033[K")
	}
}

// scan scans the album in dir, whose audio files are paths, and writes a line
// for it, named name. A failure is reported and counted, and doesn't stop the
// albums after it.
func (r *replaygainRun) scan(ctx context.Context, dir, name string, paths []string) {
	// An album takes a while to measure, so say which one it is on, on a
	// terminal.
	if r.isTTY {
		fmt.Fprintf(r.out, "\r\033[K  → %s", name)
	}

	res, err := replaygain.ScanAlbum(ctx, paths)
	if err == nil && !r.skipMD5 {
		// UpdateFile does nothing without a sums.md5 and rehashes only the
		// file it is given, so the other entries keep what was recorded.
		for _, p := range res.Changed {
			if err = hasher.UpdateFile(dir, filepath.Base(p)); err != nil {
				err = fmt.Errorf("updating %s: %w", hasher.SumsFilename, err)
				break
			}
		}
	}

	r.clearProgress()
	if err != nil {
		r.failed++
		lipgloss.Fprintln(r.out, "  "+style.Error.Render("!")+"  "+style.Name.Render(name)+"  "+style.Error.Render(err.Error()))
		return
	}

	r.albums++
	r.tracks += len(paths)
	r.changed += len(res.Changed)

	detail := fmt.Sprintf("%d %s · album gain %s · album peak %s · ", len(paths), pluralTracks(len(paths)), res.AlbumGain, res.AlbumPeak)
	if len(res.Changed) == 0 {
		detail += "unchanged"
	} else {
		detail += fmt.Sprintf("%d changed", len(res.Changed))
	}
	lipgloss.Fprintln(r.out, "  "+style.OK.Render("✓")+"  "+style.Name.Render(name)+"  "+style.Faint.Render(detail))
}

// finish writes the rule and summary line, like the other commands, and turns
// albums that couldn't be scanned into an error, after everything that could be
// scanned has been.
func (r *replaygainRun) finish() error {
	fmt.Fprintln(r.out)

	text := fmt.Sprintf("%d albums · %d tracks · %d files changed · %d failed · %s",
		r.albums+r.failed, r.tracks, r.changed, r.failed, time.Since(r.start).Round(10*time.Millisecond))
	lipgloss.Fprintln(r.out, style.Rule.Render(strings.Repeat("─", utf8.RuneCountInString(text))))
	lipgloss.Fprintln(r.out, style.Bold.Render(text))

	switch {
	case r.failed == 1:
		return errors.New("1 album could not be scanned")
	case r.failed > 1:
		return fmt.Errorf("%d albums could not be scanned", r.failed)
	}
	return nil
}
