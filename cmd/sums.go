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
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/hasher"
	"github.com/mfinelli/musicrename/internal/metadata"
)

// Lipgloss styles for sums output. renameHeaderStyle, renameAlbumStyle,
// renameRuleStyle, and renameBoldStyle are reused from rename.go; they are in
// the same package and carry the same visual meaning here.
var (
	sumsCheckStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green
	sumsSkipStyle  = lipgloss.NewStyle().Faint(true)
)

var sumsCmd = &cobra.Command{
	Use:   "sums [path]",
	Short: "Generate sums.md5 checksums for an album or library",
	Long: `Computes MD5 checksums for all files in an album directory and writes
them to sums.md5 in a format compatible with 'md5sum -c'.

If path directly contains audio files it is treated as a single album root and
only that album is processed. Otherwise path is treated as a library root and
all albums within it are processed recursively.

If path is omitted it defaults to the current working directory.

In single-album mode, an existing sums.md5 is always an error unless --force
is passed. In library mode, albums that already have a sums.md5 are silently
skipped; pass --force to regenerate them all.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSums,
}

func init() {
	sumsCmd.Flags().Bool("force", false, "Overwrite existing sums.md5 files")
	rootCmd.AddCommand(sumsCmd)
}

// sumsAudioExts mirrors the audio extension set used by the metadata package.
// Duplicated here to keep the detection logic self-contained without importing
// an unexported variable.
var sumsAudioExts = map[string]bool{".flac": true, ".mp3": true, ".m4a": true}

func runSums(cmd *cobra.Command, args []string) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", dir, err)
	}

	force, _ := cmd.Flags().GetBool("force")
	out := cmd.OutOrStdout()
	isTTY := isatty.IsTerminal(os.Stdout.Fd())

	isAlbum, err := sumsIsAlbumRoot(absDir)
	if err != nil {
		return fmt.Errorf("reading %s: %w", absDir, err)
	}

	if isAlbum {
		return runSumsAlbum(out, absDir, force, isTTY)
	}
	return runSumsLibrary(out, absDir, force, isTTY)
}

// sumsIsAlbumRoot reports whether dir directly contains at least one audio
// file, matching the same heuristic used by the metadata scanner.
func sumsIsAlbumRoot(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if !e.IsDir() && sumsAudioExts[strings.ToLower(filepath.Ext(e.Name()))] {
			return true, nil
		}
	}
	return false, nil
}

// runSumsAlbum generates sums.md5 for a single album directory. It refuses to
// proceed if sums.md5 already exists unless force is true.
func runSumsAlbum(out io.Writer, dir string, force, isTTY bool) error {
	sumsPath := filepath.Join(dir, hasher.SumsFilename)
	if _, err := os.Stat(sumsPath); err == nil && !force {
		return fmt.Errorf(
			"%s already exists; use --force to regenerate",
			sumsPath,
		)
	}

	fmt.Fprintln(out, renameHeaderStyle.Render("Hashing files..."))
	fmt.Fprintln(out)

	var count int
	progress := func(rel string) {
		count++
		if isTTY {
			fmt.Fprintf(out, "\r\033[K  → %s", rel)
		}
	}

	if err := hasher.Hash(dir, progress); err != nil {
		if isTTY {
			fmt.Fprint(out, "\r\033[K")
		}
		return fmt.Errorf("hashing %s: %w", dir, err)
	}

	if isTTY {
		fmt.Fprint(out, "\r\033[K")
	}

	fmt.Fprintln(out, sumsCheckStyle.Render(
		fmt.Sprintf("✓  sums.md5 written — %d %s", count, pluralFiles(count)),
	))
	return nil
}

// runSumsLibrary generates sums.md5 for every album found under dir. Albums
// that already have a sums.md5 are skipped unless force is true.
func runSumsLibrary(out io.Writer, dir string, force, isTTY bool) error {
	albums, err := metadata.ScanLibrary(dir)
	if err != nil {
		return fmt.Errorf("scanning library: %w", err)
	}

	// Sort by path for deterministic, reproducible output order.
	sort.Slice(albums, func(i, j int) bool {
		return albums[i].RootPath < albums[j].RootPath
	})

	fmt.Fprintln(out, renameHeaderStyle.Render("Generating checksums..."))
	fmt.Fprintln(out)

	if len(albums) == 0 {
		fmt.Fprintln(out, "No albums found.")
		return nil
	}

	var generated, skipped int

	for _, album := range albums {
		relPath, err := filepath.Rel(dir, album.RootPath)
		if err != nil {
			relPath = album.RootPath
		}

		// Print the album path before processing so the user can see
		// which album is active while hashing runs below it.
		fmt.Fprintln(out, "  "+renameAlbumStyle.Render(relPath))

		sumsPath := filepath.Join(album.RootPath, hasher.SumsFilename)
		if _, err := os.Stat(sumsPath); err == nil && !force {
			fmt.Fprintln(out, "    "+sumsSkipStyle.Render("— skipped"))
			skipped++
			continue
		}

		var count int
		progress := func(rel string) {
			count++
			if isTTY {
				// \r overwrites the current line (below the album name
				// line) so each filename replaces the previous one.
				fmt.Fprintf(out, "\r\033[K    → %s", rel)
			}
		}

		if err := hasher.Hash(album.RootPath, progress); err != nil {
			if isTTY {
				fmt.Fprint(out, "\r\033[K")
			}
			return fmt.Errorf("hashing %s: %w", relPath, err)
		}

		if isTTY {
			fmt.Fprint(out, "\r\033[K")
		}

		fmt.Fprintln(out, "    "+sumsCheckStyle.Render(
			fmt.Sprintf("✓  %d %s", count, pluralFiles(count)),
		))
		generated++
	}

	fmt.Fprintln(out)
	summaryText := fmt.Sprintf(
		"%d albums · %d generated · %d skipped",
		len(albums), generated, skipped,
	)
	fmt.Fprintln(out, renameRuleStyle.Render(strings.Repeat("─", len(summaryText))))
	fmt.Fprintln(out, renameBoldStyle.Render(summaryText))
	return nil
}

// pluralFiles returns "file" or "files" depending on n.
func pluralFiles(n int) string {
	if n == 1 {
		return "file"
	}
	return "files"
}
