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
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/hasher"
	"github.com/mfinelli/musicrename/internal/metadata"
	"github.com/mfinelli/musicrename/internal/playlist"
	"github.com/mfinelli/musicrename/internal/target"
)

var playlistSelectCmd = &cobra.Command{
	Use:   "select <target> [album-path]",
	Short: "Interactively edit an album's target selection manifest",
	Long: `Presents a checkbox list of every track in an album and writes the
checked selection to {target}.m3u8 (e.g. ipod.m3u8), the album-local
selection manifest.

Tracks already listed in an existing manifest are pre-checked. A manifest
entry that no longer matches any track currently found in the album is shown
too (as a bare filename with a warning marker) rather than silently
dropped; unchecking it removes it the same way unchecking a real track would.

If every track ends up unchecked, the manifest file is deleted rather than
left behind empty.

If the album already has a sums.md5, it is updated to match via a targeted,
single-file update. Pass --skip-md5 to leave sums.md5 untouched entirely.

target must be one of: ` + strings.Join(target.Names, ", ") + `

If album-path is omitted it defaults to the current working directory.`,
	Args: cobra.RangeArgs(1, 2),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return target.Names, cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveDefault
	},
	RunE: runPlaylistSelect,
}

func init() {
	playlistSelectCmd.Flags().Bool("skip-md5", false, "Do not update sums.md5 even if it exists")
	playlistCmd.AddCommand(playlistSelectCmd)
}

func runPlaylistSelect(cmd *cobra.Command, args []string) error {
	targetName := args[0]
	if !target.Valid(targetName) {
		return fmt.Errorf(
			"unknown target %q; valid targets are: %s",
			targetName, strings.Join(target.Names, ", "),
		)
	}

	albumPath := "."
	if len(args) > 1 {
		albumPath = args[1]
	}
	absAlbum, err := filepath.Abs(albumPath)
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", albumPath, err)
	}

	isAlbum, err := sumsIsAlbumRoot(absAlbum)
	if err != nil {
		return fmt.Errorf("reading %s: %w", absAlbum, err)
	}
	if !isAlbum {
		return fmt.Errorf("%s does not directly contain any audio files", absAlbum)
	}

	skipMD5, _ := cmd.Flags().GetBool("skip-md5")
	out := cmd.OutOrStdout()

	albums, err := metadata.ProcessLibrary(absAlbum)
	if err != nil {
		return fmt.Errorf("reading album: %w", err)
	}
	if len(albums) == 0 {
		return fmt.Errorf("%s does not appear to be a readable album directory", absAlbum)
	}
	album := albums[0]

	existingNames, err := playlist.ReadManifest(absAlbum, targetName)
	if err != nil {
		return fmt.Errorf("reading existing %s: %w", playlist.ManifestFilename(targetName), err)
	}
	existingSet := make(map[string]bool, len(existingNames))
	for _, n := range existingNames {
		existingSet[n] = true
	}

	sortedTracks := sortTracksForDisplay(album.Tracks)
	hasMultiDisc := albumHasMultiDisc(album.Tracks)

	type row struct {
		filename string
		label    string
	}
	var rows []row

	for _, t := range sortedTracks {
		filename := filepath.Base(t.Path)
		rows = append(rows, row{
			filename: filename,
			label:    formatTrackLabel(t, hasMultiDisc, album.ResolvedArtist),
		})
		delete(existingSet, filename)
	}

	// Anything left in existingSet was in the manifest but doesn't match any
	// track found in the album: surface it rather than silently dropping
	// it. Iterate the original manifest order (not map order) for stable
	// output, appended after every real track.
	for _, n := range existingNames {
		if existingSet[n] {
			rows = append(rows, row{
				filename: n,
				label: renameWarningStyle.Render(
					"⚠  " + n + "  (missing — no matching track found)",
				),
			})
			delete(existingSet, n) // guard against duplicate lines in a malformed manifest
		}
	}

	if len(rows) == 0 {
		fmt.Fprintln(out, "No tracks found in this album.")
		return nil
	}

	options := make([]huh.Option[string], 0, len(rows))
	for _, r := range rows {
		options = append(options, huh.NewOption(r.label, r.filename).Selected(existingNamesContains(existingNames, r.filename)))
	}

	var selected []string
	field := huh.NewMultiSelect[string]().
		Title(fmt.Sprintf("Select tracks for %s (%s)", targetName, filepath.Base(absAlbum))).
		Options(options...).
		Value(&selected)

	if err := huh.NewForm(huh.NewGroup(field)).Run(); err != nil {
		return fmt.Errorf("prompting: %w", err)
	}

	selectedSet := make(map[string]bool, len(selected))
	for _, s := range selected {
		selectedSet[s] = true
	}

	// Preserve display order (real tracks in disc/track order, then any
	// stale entries) rather than relying on the order huh happens to return
	// selections in.
	final := make([]string, 0, len(rows))
	for _, r := range rows {
		if selectedSet[r.filename] {
			final = append(final, r.filename)
		}
	}

	if err := playlist.WriteManifest(absAlbum, targetName, final); err != nil {
		return fmt.Errorf("writing %s: %w", playlist.ManifestFilename(targetName), err)
	}

	manifestName := playlist.ManifestFilename(targetName)
	if !skipMD5 {
		if _, err := os.Stat(filepath.Join(absAlbum, hasher.SumsFilename)); err == nil {
			if len(final) == 0 {
				if err := hasher.RemoveFile(absAlbum, manifestName); err != nil {
					return fmt.Errorf("updating %s: %w", hasher.SumsFilename, err)
				}
			} else {
				if err := hasher.UpdateFile(absAlbum, manifestName); err != nil {
					return fmt.Errorf("updating %s: %w", hasher.SumsFilename, err)
				}
			}
		}
	}

	if len(final) == 0 {
		fmt.Fprintln(out, sumsCheckStyle.Render(
			fmt.Sprintf("✓  %s removed (no tracks selected)", manifestName),
		))
	} else {
		fmt.Fprintln(out, sumsCheckStyle.Render(
			fmt.Sprintf("✓  %s written — %d %s selected", manifestName, len(final), pluralTracks(len(final))),
		))
	}

	return nil
}

// sortTracksForDisplay returns a copy of tracks sorted by (DiscNumber,
// TrackNumber). A nil TrackNumber sorts as 0, consistent with the planner's
// treatment of an absent tag (internal/planner). The input slice is never
// mutated.
func sortTracksForDisplay(tracks []*metadata.Track) []*metadata.Track {
	sorted := make([]*metadata.Track, len(tracks))
	copy(sorted, tracks)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].DiscNumber != sorted[j].DiscNumber {
			return sorted[i].DiscNumber < sorted[j].DiscNumber
		}
		ti, tj := 0, 0
		if sorted[i].TrackNumber != nil {
			ti = *sorted[i].TrackNumber
		}
		if sorted[j].TrackNumber != nil {
			tj = *sorted[j].TrackNumber
		}
		return ti < tj
	})
	return sorted
}

// albumHasMultiDisc reports whether tracks span two or more distinct
// positive DISCNUMBER values, mirroring the same heuristic used by the
// planner (internal/planner) for filename generation.
func albumHasMultiDisc(tracks []*metadata.Track) bool {
	discs := make(map[int]bool)
	for _, t := range tracks {
		if t.DiscNumber > 0 {
			discs[t.DiscNumber] = true
		}
	}
	return len(discs) > 1
}

// formatTrackLabel builds the checkbox label for a single track: track
// number (and disc number, when the album has more than one disc), title
// (falling back to the filename stem when the TITLE tag is absent and the
// track's artist only when it differs from the album's resolved artist.
func formatTrackLabel(t *metadata.Track, hasMultiDisc bool, albumArtist string) string {
	title := t.Title
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(t.Path), filepath.Ext(t.Path))
	}

	trackNum := 0
	if t.TrackNumber != nil {
		trackNum = *t.TrackNumber
	}

	var b strings.Builder
	if hasMultiDisc {
		fmt.Fprintf(&b, "%d-%02d  %s", t.DiscNumber, trackNum, title)
	} else {
		fmt.Fprintf(&b, "%02d  %s", trackNum, title)
	}

	if t.Artist != "" && t.Artist != albumArtist {
		fmt.Fprintf(&b, "  —  %s", t.Artist)
	}

	return b.String()
}

// existingNamesContains reports whether name is present in names. Used to
// determine each checkbox's initial pre-selected state.
func existingNamesContains(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// pluralTracks returns "track" or "tracks" depending on n.
func pluralTracks(n int) string {
	if n == 1 {
		return "track"
	}
	return "tracks"
}
