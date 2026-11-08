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

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/metadata"
	"github.com/mfinelli/musicrename/internal/spectrum"
	"github.com/mfinelli/musicrename/internal/style"
)

var spectrumCmd = &cobra.Command{
	Use:   "spectrum [path]",
	Short: "Check whether lossless audio is really a lossy transcode",
	Long: `Looks for signs that audio stored in a lossless container (FLAC, or ALAC in
an .m4a) was really produced by a lossy encoder such as MP3 or AAC and only
later converted. Lossy encoders discard everything above a cutoff frequency;
converting the result to FLAC does not bring it back, so the cutoff stays
visible as a sharp edge in the file's frequency spectrum. This command measures
the spectrum and looks for that edge. It also reports hi-res files with nothing
above CD bandwidth (upsampled) and wide files that only use 16 bits.

If path is an audio file, that track is analyzed. If path is a directory that
directly contains audio files, every track in it is analyzed. A library root is
not accepted: this is meant to be run on one album or track at a time.

Each track is reported as:
  ok           nothing suggests it is anything but what it claims to be
  suspect      shows a feature lossy encoding causes, but genuine lossless
               sources can have it too; look at the spectrogram
  likely fake  strong evidence of a transcode or an upsample
  inconclusive too little audio to judge

This is a heuristic, not a proof. A high-bitrate lossy encode can pass, and a
lossless file with an unusually sharp lowpass can be flagged.

With --png, a spectrogram image of each track is written into the given
directory, for a final judgment by eye. By default ffmpeg draws it. With
--annotated the image is drawn from the same measurements as the verdict, with
the detected cutoff marked on it (and, for hi-res files, the top of CD
bandwidth). By default each annotated image is scaled to its own loudness; add
--fixed-scale to use the same color scale for every image, so that images of
different tracks can be compared directly. Nothing is ever written to the album
itself unless you point --png there.

Exits with a non-zero status when any track is suspect or likely fake, enabling
use in scripts. If path is omitted it defaults to the current directory.`,
	Args: cobra.MaximumNArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// Either an audio file or a directory is valid here, so fall back
		// to normal completion rather than filtering to audio extensions.
		return nil, cobra.ShellCompDirectiveDefault
	},
	RunE: runSpectrum,
}

func init() {
	spectrumCmd.Flags().String("png", "", "Write a spectrogram PNG for each track into `dir`")
	spectrumCmd.Flags().Bool("annotated", false, "Draw the spectrogram from the analysis, with the detected cutoff marked (requires --png)")
	spectrumCmd.Flags().Bool("fixed-scale", false, "Use the same color scale for every annotated image, so they can be compared (requires --annotated)")
	spectrumCmd.Flags().BoolP("verbose", "v", false, "Show the technical detail behind each verdict")
	spectrumCmd.RegisterFlagCompletionFunc("png", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveFilterDirs
	})
	rootCmd.AddCommand(spectrumCmd)
}

func runSpectrum(cmd *cobra.Command, args []string) error {
	start := time.Now()

	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", path, err)
	}

	pngDir, _ := cmd.Flags().GetString("png")
	annotated, _ := cmd.Flags().GetBool("annotated")
	fixedScale, _ := cmd.Flags().GetBool("fixed-scale")
	verbose, _ := cmd.Flags().GetBool("verbose")
	out := cmd.OutOrStdout()

	if annotated && pngDir == "" {
		return fmt.Errorf("--annotated requires --png")
	}
	if fixedScale && !annotated {
		return fmt.Errorf("--fixed-scale requires --annotated")
	}

	tracks, isAlbum, err := spectrumTracks(absPath)
	if err != nil {
		return err
	}

	if pngDir != "" {
		if err := os.MkdirAll(pngDir, 0o755); err != nil {
			return fmt.Errorf("could not create %q: %w", pngDir, err)
		}
	}

	if isAlbum {
		lipgloss.Fprintln(out, style.Header.Render("Analyzing album..."))
		fmt.Fprintln(out)
		lipgloss.Fprintln(out, "  "+style.Name.Render(filepath.Base(absPath)))
	} else {
		lipgloss.Fprintln(out, style.Header.Render("Analyzing track..."))
		fmt.Fprintln(out)
	}

	ctx := cmd.Context()
	th := spectrum.DefaultThresholds()
	var analyzed, flagged, skipped, failed int

	for _, track := range tracks {
		name := filepath.Base(track)

		res, err := spectrum.Analyze(ctx, track, th)

		var notLossless *spectrum.NotLosslessError
		switch {
		case errors.As(err, &notLossless):
			if !isAlbum {
				return fmt.Errorf("%q uses the lossy codec %q; there is nothing to check",
					name, notLossless.Codec)
			}
			skipped++
			lipgloss.Fprintln(out, "  "+style.Faint.Render(
				fmt.Sprintf("%s  skipped: %s is lossy", name, notLossless.Codec)))
			continue

		case err != nil:
			if !isAlbum {
				return fmt.Errorf("analyzing %q: %w", name, err)
			}
			failed++
			lipgloss.Fprintln(out, "  "+name)
			lipgloss.Fprintln(out, "    "+style.Error.Render("✗ could not analyze: "+err.Error()))
			continue
		}

		analyzed++
		if res.Verdict.Flagged() {
			flagged++
		}
		spectrumPrintTrack(out, name, res, verbose)

		if pngDir != "" {
			spectrumWritePNG(ctx, out, track, pngDir, annotated, fixedScale, res, th)
		}
	}

	fmt.Fprintln(out)
	checkPrintSummary(out, analyzed, "track", flagged, time.Since(start))
	if skipped > 0 {
		lipgloss.Fprintln(out, style.Faint.Render(
			fmt.Sprintf("%d lossy %s skipped", skipped, pluralFiles(skipped))))
	}

	if failed > 0 {
		return fmt.Errorf("%d %s could not be analyzed", failed, pluralTracks(failed))
	}
	if flagged > 0 {
		return exitFindings()
	}
	return nil
}

// spectrumTracks resolves path to the audio files to analyze. A file is a
// single track; a directory that directly contains audio is an album, whose
// tracks are returned in name order. isAlbum distinguishes the two. A
// directory with no audio directly inside (a library root) is an error.
func spectrumTracks(path string) (tracks []string, isAlbum bool, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, false, fmt.Errorf("could not access %q: %w", path, err)
	}

	if !info.IsDir() {
		ext := strings.ToLower(filepath.Ext(path))
		if !metadata.IsAudioExt(ext) {
			return nil, false, fmt.Errorf(
				"%q is not a supported audio file (expected one of: %s)",
				filepath.Base(path), metadata.DottedExtList(metadata.AudioExtensions),
			)
		}
		return []string{path}, false, nil
	}

	isAlbum, err = sumsIsAlbumRoot(path)
	if err != nil {
		return nil, false, fmt.Errorf("reading %s: %w", path, err)
	}
	if !isAlbum {
		return nil, false, fmt.Errorf(
			"%s contains no audio files; spectrum analyzes one track or one album at a time, not a library",
			path,
		)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, false, fmt.Errorf("reading %s: %w", path, err)
	}
	for _, e := range entries {
		if !e.IsDir() && metadata.IsAudioExt(filepath.Ext(e.Name())) {
			tracks = append(tracks, filepath.Join(path, e.Name()))
		}
	}
	return tracks, true, nil
}

// spectrumPrintTrack prints one track's result: its name and format, a verdict
// line, any further findings, and (with verbose) the detail behind them.
func spectrumPrintTrack(out io.Writer, name string, res *spectrum.Result, verbose bool) {
	lipgloss.Fprintln(out, "  "+name+"  "+style.Faint.Render(res.Info.Describe()))

	var verdict string
	switch res.Verdict {
	case spectrum.VerdictOK:
		verdict = style.OK.Render("✓ " + res.Verdict.String())
	case spectrum.VerdictSuspect:
		verdict = style.Warn.Render("⚠ " + res.Verdict.String())
	case spectrum.VerdictLikelyFake:
		verdict = style.Error.Render("✗ " + res.Verdict.String())
	default:
		verdict = style.Faint.Render("– " + res.Verdict.String())
	}
	lipgloss.Fprintln(out, "    "+verdict+" — "+res.Summary())

	for _, more := range res.More() {
		lipgloss.Fprintln(out, "      + "+more)
	}

	if verbose {
		for _, line := range res.Verbose() {
			lipgloss.Fprintln(out, "      "+style.Faint.Render(line))
		}
	}
}

// spectrumWritePNG writes track's spectrogram into dir, named after the track:
// ffmpeg's picture by default, or, if annotated, one drawn from res with the
// detected cutoff marked. A failure is only a warning: the analysis it
// illustrates has already succeeded and been reported.
func spectrumWritePNG(ctx context.Context, out io.Writer, track, dir string, annotated, fixedScale bool, res *spectrum.Result, th spectrum.Thresholds) {
	base := filepath.Base(track)
	dst := filepath.Join(dir, strings.TrimSuffix(base, filepath.Ext(base))+".png")

	var err error
	if annotated {
		err = spectrum.WriteAnnotatedSpectrogram(ctx, track, dst, res, th,
			spectrum.AnnotateOptions{FixedScale: fixedScale})
	} else {
		err = spectrum.WriteSpectrogram(ctx, track, dst)
	}
	if err != nil {
		lipgloss.Fprintln(out, "    "+style.Warn.Render("⚠ could not write spectrogram: "+err.Error()))
		return
	}
	lipgloss.Fprintln(out, "    "+style.Faint.Render("↳ spectrogram: "+dst))
}
