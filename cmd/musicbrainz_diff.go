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

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"go.senan.xyz/taglib"

	"github.com/mfinelli/musicrename/internal/metadata"
	"github.com/mfinelli/musicrename/internal/musicbrainz"
)

var musicbrainzDiffCmd = &cobra.Command{
	Use:   "diff [album-path]",
	Short: "Compare an album's MusicBrainz release against its last known snapshot",
	Long: `Reads the MUSICBRAINZ_ALBUMID tag from album-path's first track (by
track number; falls back to the first track in directory order if none has
one) and fetches that release's current data from MusicBrainz, comparing it
against the snapshot recorded the last time diff ran for this album
(musicbrainz.json.gz). Every field that changed is reported.

If album-path is omitted it defaults to the current working directory.
Either way it must be a single album directory (one directly containing
audio files). A library root or an artist directory is rejected because the 
MusicBrainz project specifically asks not to run this kind of check across an 
entire library.

With no prior snapshot, the current data is recorded as the baseline and
nothing is reported (because there is nothing to compare it against yet).

--dry-run shows the same comparison without updating the snapshot, so a
later run (dry or not) reports the identical diff again.`,
	Args: cobra.MaximumNArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// album-path is always a directory
		return nil, cobra.ShellCompDirectiveFilterDirs
	},
	RunE: runMusicbrainzDiff,
}

func init() {
	musicbrainzDiffCmd.Flags().Bool("dry-run", false, "Show the diff without recording a new snapshot")
	musicbrainzCmd.AddCommand(musicbrainzDiffCmd)
}

func runMusicbrainzDiff(cmd *cobra.Command, args []string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", path, err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("could not access %q: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%q is not a directory; musicbrainz diff operates on a whole album", path)
	}

	album, err := metadata.ProcessAlbum(absPath)
	if err != nil {
		return err
	}

	track := musicbrainzFirstTrack(album)
	if track == nil {
		return fmt.Errorf("%s has no audio tracks", absPath)
	}

	mbid, err := readMusicBrainzAlbumID(track.Path)
	if err != nil {
		return err
	}
	if mbid == "" {
		return fmt.Errorf("%s has no MUSICBRAINZ_ALBUMID tag", track.Path)
	}

	out := cmd.OutOrStdout()
	lipgloss.Fprintln(out, renameHeaderStyle.Render(fmt.Sprintf("Checking MusicBrainz for %s...", filepath.Base(absPath))))
	fmt.Fprintln(out)

	result, err := musicbrainz.Check(context.Background(), mbid, absPath, dryRun)
	if err != nil {
		return fmt.Errorf("checking musicbrainz: %w", err)
	}

	printMusicbrainzDiffResult(out, result, dryRun)
	return nil
}

// musicbrainzFirstTrack returns album's track with the lowest positive
// TrackNumber, mirroring Album.ResolveAlbumArtist's selection rule; if no
// track has a positive TrackNumber, the first track in directory order is
// used instead.
func musicbrainzFirstTrack(album *metadata.Album) *metadata.Track {
	if len(album.Tracks) == 0 {
		return nil
	}

	sorted := make([]*metadata.Track, len(album.Tracks))
	copy(sorted, album.Tracks)
	sort.SliceStable(sorted, func(i, j int) bool {
		ni, nj := 0, 0
		if sorted[i].TrackNumber != nil {
			ni = *sorted[i].TrackNumber
		}
		if sorted[j].TrackNumber != nil {
			nj = *sorted[j].TrackNumber
		}
		return ni < nj
	})

	for _, t := range sorted {
		if t.TrackNumber != nil && *t.TrackNumber > 0 {
			return t
		}
	}
	return album.Tracks[0]
}

// readMusicBrainzAlbumID reads the MUSICBRAINZ_ALBUMID tag from path,
// returning "" (not an error) if the tag is absent.
func readMusicBrainzAlbumID(path string) (string, error) {
	file, err := taglib.OpenReadOnly(path)
	if err != nil {
		return "", fmt.Errorf("opening %s: %w", path, err)
	}
	defer file.Close()

	if vals := file.Tags()[taglib.MusicBrainzAlbumID]; len(vals) > 0 {
		return vals[0], nil
	}
	return "", nil
}

// printMusicbrainzDiffResult renders a musicbrainz.CheckResult to out.
func printMusicbrainzDiffResult(out io.Writer, result *musicbrainz.CheckResult, dryRun bool) {
	if result.FirstCheck {
		lipgloss.Fprintln(out, sumsCheckStyle.Render("✓  no prior snapshot; recorded current data as the baseline"))
		return
	}

	if len(result.Changes) == 0 {
		lipgloss.Fprintln(out, sumsCheckStyle.Render("✓  no changes since the last check"))
		return
	}

	changeLabel := "changes"
	if len(result.Changes) == 1 {
		changeLabel = "change"
	}
	suffix := ""
	if dryRun {
		suffix = " (dry run; snapshot not updated)"
	}
	lipgloss.Fprintln(out, renameBoldStyle.Render(
		fmt.Sprintf("%d %s since the last check%s:", len(result.Changes), changeLabel, suffix),
	))
	for _, c := range result.Changes {
		fmt.Fprintf(out, "  - %s\n", c)
	}
}
