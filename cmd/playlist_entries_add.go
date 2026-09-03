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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/completion"
	"github.com/mfinelli/musicrename/internal/devicesync"
	"github.com/mfinelli/musicrename/internal/metadata"
	"github.com/mfinelli/musicrename/internal/playlist"
	"github.com/mfinelli/musicrename/internal/video"
)

var playlistEntriesAddCmd = &cobra.Command{
	Use:   "add <playlist> [path]...",
	Short: "Append one or more tracks to a library-wide playlist",
	Long: `Appends each given path, in order, to playlist's entry list, then rewrites
the file.

Each path may be given relative to the current working directory or as an
absolute path; either way it's resolved and stored relative to the library
root (derived from playlist's own location under playlists/).

A path that doesn't resolve to a real file under the library root is
skipped and reported.

With no path arguments, opens an interactive directory browser instead of
requiring paths up front: arrow keys (or j/k) move the cursor, left/h/
backspace goes up a level, right/l/enter descends into a directory or, if it
is an album, opens a checkbox picker for its tracks. Inside the videos
library root specifically, entering an artist instead opens a checkbox
picker of that artist's videos (but only for videos that have derived audio).
Press / to filter the current directory's listing by substring. Nothing is
written until you leave the browser: esc/q at the top level saves everything
staged across the whole session while ctrl+c discards all changes.

playlist must already exist (use 'playlist create' to scaffold a new one
first).`,
	Args: cobra.MinimumNArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return completion.PlaylistArg(cmd, args, toComplete)
		}
		// Subsequent [path]... arguments are library files (audio tracks,
		// or derived-audio files under the video root), not restricted to
		// a single extension set, so fall back to normal completion.
		return nil, cobra.ShellCompDirectiveDefault
	},
	RunE: runPlaylistEntriesAdd,
}

func init() {
	playlistEntriesCmd.AddCommand(playlistEntriesAddCmd)
}

func runPlaylistEntriesAdd(cmd *cobra.Command, args []string) error {
	path, err := filepath.Abs(args[0])
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", args[0], err)
	}
	absRoot := playlist.LibraryRootRootFor(path)

	if len(args) == 1 {
		return runPlaylistEntriesAddInteractive(cmd, absRoot, path)
	}

	relPaths := make([]string, 0, len(args)-1)
	for _, raw := range args[1:] {
		abs, err := filepath.Abs(raw)
		if err != nil {
			return fmt.Errorf("could not resolve path %q: %w", raw, err)
		}
		rel, err := filepath.Rel(absRoot, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%s is outside the library root (%s)", raw, absRoot)
		}
		relPaths = append(relPaths, rel)
	}

	added, warnings, err := playlist.AddEntries(absRoot, path, relPaths)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	for _, a := range added {
		fmt.Fprintln(out, "  + "+a)
	}
	for _, w := range warnings {
		fmt.Fprintln(out, renameWarningStyle.Render("⚠ "+w))
	}
	fmt.Fprintf(out, "%d added, %d warning(s)\n", len(added), len(warnings))

	if len(added) > 0 {
		if gp, gerr := playlist.ReadGlobalPlaylist(path); gerr == nil {
			printSortReminder(out, path, gp)
		}
	}
	return nil
}

// entriesAddChromeLines mirrors reorderChromeLines: lines View() spends on
// things other than the directory listing itself.
const entriesAddChromeLines = 5

// entriesAddMode distinguishes the browser's two screens within one
// session: navigating directories, or picking tracks from an album via an
// embedded huh checklist.
type entriesAddMode int

const (
	entriesAddModeBrowse entriesAddMode = iota
	entriesAddModeAlbum
)

func runPlaylistEntriesAddInteractive(cmd *cobra.Command, absRoot, path string) error {
	if _, statErr := os.Stat(path); statErr != nil {
		return fmt.Errorf("%s: %w", path, statErr)
	}

	gp, err := playlist.ReadGlobalPlaylist(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	m, err := newEntriesAddModel(absRoot, path, gp.Entries)
	if err != nil {
		return fmt.Errorf("starting browser: %w", err)
	}

	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return fmt.Errorf("running entries browser: %w", err)
	}

	result, ok := final.(*entriesAddModel)
	if !ok {
		return fmt.Errorf("internal error: unexpected entries browser result type")
	}

	out := cmd.OutOrStdout()
	if result.aborted {
		fmt.Fprintln(out, "Aborted; no changes made.")
		return nil
	}

	newEntries := result.selection.FinalEntries()
	warning, err := playlist.SetEntries(absRoot, path, newEntries)
	if err != nil {
		return err
	}
	if warning != "" {
		fmt.Fprintln(out, renameWarningStyle.Render("⚠ "+warning))
	}
	fmt.Fprintf(out, "Playlist now has %d entries\n", len(newEntries))

	if result.selection.HasNewEntries() {
		printSortReminder(out, path, gp)
	}
	return nil
}

// printSortReminder prints a low-key note if gp has a #SORT: directive
// stored, since neither entries add mode ever re-sorts automatically
// after appending. Doing so safely would mean resolving every entry's
// tags just to check whether the playlist is already in that order; and it
// isn't even safe in principle, since #SORT: only records the last explicit
// sort criteria used, not a live guarantee the playlist is still in that
// order (a `playlist entries reorder` session never touches it, so blindly
// reapplying it here could silently discard hand-curation work).
func printSortReminder(out io.Writer, path string, gp *playlist.GlobalPlaylist) {
	if !gp.HasSort || len(gp.Sort) == 0 {
		return
	}
	fmt.Fprintf(out, "Note: this playlist has #SORT:%s stored; run 'playlist sort %s' to reapply it.\n",
		strings.Join(gp.Sort, ","), path)
}

// entriesAddModel is a bubbletea model spanning both the directory browser
// and, when an album is opened, an embedded huh checklist; one model so
// staged selections and the current browse position both persist across
// visiting several albums in one session.
type entriesAddModel struct {
	libraryRootRoot string
	playlistPath    string
	selection       *playlist.BrowseSelection

	mode entriesAddMode

	// browse state
	currentDir  string
	allEntries  []string // unfiltered directory names
	dirEntries  []string // allEntries, or its filtered subset
	cursor      int
	scrollTop   int
	height      int
	filtering   bool
	filterQuery string
	errMsg      string

	// album state
	form           *huh.Form
	albumSelection []string // bound to the active form's MultiSelect value
	albumRelPaths  []string // every candidate rel path shown in the active form

	aborted bool
}

func newEntriesAddModel(libraryRootRoot, playlistPath string, original []string) (*entriesAddModel, error) {
	m := &entriesAddModel{
		libraryRootRoot: libraryRootRoot,
		playlistPath:    playlistPath,
		selection:       playlist.NewBrowseSelection(original),
		height:          20, // a sane default before the first tea.WindowSizeMsg arrives
	}
	if err := m.loadDir(libraryRootRoot); err != nil {
		return nil, err
	}
	return m, nil
}

// loadDir lists dir's browsable children into allEntries/dirEntries and
// resets cursor/scroll/filter state for the new listing. At
// libraryRootRoot itself, the listing is every library root
// (devicesync.LibraryRoots, excluding the reserved playlists siblings and
// dotfiles) plus the videos sibling, appended separately rather
// than by changing LibraryRoots (which stays correct as-is for its other
// callers, e.g. device sync). Videos holds no real "album" tracks the
// normal way, but a video's derived audio file is a legitimate playlist entry,
// so it needs to be reachable here even though it's still excluded from every
// LibraryRoots-driven operation elsewhere. At any deeper level,
// playlist.ListSubdirectories (every subdirectory except dotfiles). Neither
// case reads any file's tags (this is a plain directory listing, nothing
// more).
func (m *entriesAddModel) loadDir(dir string) error {
	var names []string
	var err error
	if dir == m.libraryRootRoot {
		names, err = devicesync.LibraryRoots(dir)
		if err == nil {
			if _, statErr := os.Stat(filepath.Join(dir, "videos")); statErr == nil {
				names = append(names, "videos")
				sort.Strings(names)
			}
		}
	} else {
		names, err = playlist.ListSubdirectories(dir)
	}
	if err != nil {
		return err
	}

	m.currentDir = dir
	m.allEntries = names
	m.cursor = 0
	m.scrollTop = 0
	m.filtering = false
	m.filterQuery = ""
	m.errMsg = ""
	m.applyFilter()
	return nil
}

func (m *entriesAddModel) applyFilter() {
	m.dirEntries = playlist.FilterNames(m.allEntries, m.filterQuery)
}

// up moves to the parent directory, clamped at libraryRootRoot (a no-op
// there). A read error on the parent leaves the current listing in place
// (reported via errMsg) rather than navigating into a directory that
// can't actually be listed.
func (m *entriesAddModel) up() {
	if m.currentDir == m.libraryRootRoot {
		return
	}
	parent := filepath.Dir(m.currentDir)
	if err := m.loadDir(parent); err != nil {
		m.errMsg = err.Error()
	}
}

// enter opens the selected directory: descends into it, or (if it directly
// contains audio files) opens the album checklist for it instead (see
// openAlbum). Inside the videos library root specifically, entering an
// artist directory (one level below a bucket letter, i.e. m.currentDir's
// parent is libraryRootRoot/videos) instead opens an aggregated checklist
// of that artist's videos that have a derived audio file since a video's leaf
// directory only ever has exactly one possible entry, checking each one
// individually one directory at a time would be far more friction than it'
// s worth. A read error is reported via errMsg rather than navigating into an
// unreadable directory.
func (m *entriesAddModel) enter() tea.Cmd {
	if len(m.dirEntries) == 0 {
		return nil
	}
	full := filepath.Join(m.currentDir, m.dirEntries[m.cursor])

	if filepath.Dir(m.currentDir) == filepath.Join(m.libraryRootRoot, "videos") {
		return m.openArtistVideos(full)
	}

	isAlbum, err := sumsIsAlbumRoot(full)
	if err != nil {
		m.errMsg = err.Error()
		return nil
	}
	if isAlbum {
		return m.openAlbum(full)
	}

	if err := m.loadDir(full); err != nil {
		m.errMsg = err.Error()
	}
	return nil
}

// openAlbum reads dir's tracks (tags included; this is the one point in
// the whole session where any file is actually opened, scoped to just this
// one album) and builds a huh MultiSelect form for them, pre-checked against
// the current staged selection. The form becomes the active sub-model until
// it completes or is cancelled.
func (m *entriesAddModel) openAlbum(dir string) tea.Cmd {
	albums, err := metadata.ProcessLibrary(dir)
	if err != nil {
		m.errMsg = err.Error()
		return nil
	}
	if len(albums) == 0 {
		m.errMsg = fmt.Sprintf("%s does not appear to be a readable album directory", dir)
		return nil
	}
	album := albums[0]

	sortedTracks := sortTracksForDisplay(album.Tracks)
	hasMultiDisc := albumHasMultiDisc(album.Tracks)

	relPaths := make([]string, 0, len(sortedTracks))
	options := make([]huh.Option[string], 0, len(sortedTracks))
	for _, t := range sortedTracks {
		rel, relErr := filepath.Rel(m.libraryRootRoot, t.Path)
		if relErr != nil {
			m.errMsg = relErr.Error()
			return nil
		}
		relPaths = append(relPaths, rel)
		label := formatTrackLabel(t, hasMultiDisc, album.ResolvedArtist)
		options = append(options, huh.NewOption(label, rel).Selected(m.selection.IsSelected(rel)))
	}

	m.albumRelPaths = relPaths
	m.albumSelection = nil
	field := huh.NewMultiSelect[string]().
		Title(fmt.Sprintf("Select tracks from %s", filepath.Base(dir))).
		Options(options...).
		Value(&m.albumSelection)

	m.form = huh.NewForm(huh.NewGroup(field))
	m.mode = entriesAddModeAlbum
	return m.form.Init()
}

// openArtistVideos builds a huh MultiSelect, via videoOptionsForArtist, of
// every video under artistDir that has a derived audio file, resolving each
// one to its derived audio file's path (a video with no derived audio yet, or
// the anomalous multiple-derived-audio-files state, is silently omitted rather
// than surfaced here since video check already owns flagging that specific
// problem).
//
// Never navigates m.currentDir at all (mirroring openAlbum, which also
// builds directly on top of the current listing). Completing or
// cancelling the form returns to the bucket-level artist listing, without
// ever having "visually" entered artistDir.
func (m *entriesAddModel) openArtistVideos(artistDir string) tea.Cmd {
	relPaths, options, err := videoOptionsForArtist(m.libraryRootRoot, artistDir, m.selection.IsSelected,
		func(videoPath string) (string, bool) {
			audioFiles, aerr := video.DerivedAudioFiles(videoPath)
			if aerr != nil || len(audioFiles) != 1 {
				return "", false // none yet, or the ambiguous multiple-files state
			}
			return audioFiles[0], true
		},
	)
	if err != nil {
		m.errMsg = err.Error()
		return nil
	}
	if len(options) == 0 {
		m.errMsg = fmt.Sprintf("%s has no videos with extracted audio yet", artistDir)
		return nil
	}

	m.albumRelPaths = relPaths
	m.albumSelection = nil
	field := huh.NewMultiSelect[string]().
		Title(fmt.Sprintf("Select videos from %s", filepath.Base(artistDir))).
		Options(options...).
		Value(&m.albumSelection)

	m.form = huh.NewForm(huh.NewGroup(field))
	m.mode = entriesAddModeAlbum
	return m.form.Init()
}

func (m *entriesAddModel) Init() tea.Cmd {
	return nil
}

func (m *entriesAddModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// ctrl+c always aborts the whole session immediately, in either mode,
	// regardless of whatever huh's own internal key handling might
	// otherwise do with it (intercepted here, before ever reaching
	// m.form.Update, so this is never ambiguous).
	if km, ok := msg.(tea.KeyMsg); ok && km.String() == "ctrl+c" {
		m.aborted = true
		return m, tea.Quit
	}

	if m.mode == entriesAddModeAlbum {
		return m.updateAlbum(msg)
	}
	return m.updateBrowse(msg)
}

func (m *entriesAddModel) updateAlbum(msg tea.Msg) (tea.Model, tea.Cmd) {
	newModel, cmd := m.form.Update(msg)
	if f, ok := newModel.(*huh.Form); ok {
		m.form = f
	}

	switch m.form.State {
	case huh.StateCompleted:
		selectedAfter := make(map[string]bool, len(m.albumSelection))
		for _, s := range m.albumSelection {
			selectedAfter[s] = true
		}
		m.selection.Apply(m.albumRelPaths, selectedAfter)
		m.mode = entriesAddModeBrowse
		return m, nil
	case huh.StateAborted:
		// Reachable only via esc within the checklist itself (ctrl+c
		// never reaches m.form.Update at all): discard this album's
		// edits and return to browsing, not the whole session.
		m.mode = entriesAddModeBrowse
		return m, nil
	}

	return m, cmd
}

func (m *entriesAddModel) updateBrowse(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height - entriesAddChromeLines
		if m.height < 1 {
			m.height = 1
		}
		m.ensureVisible()

	case tea.KeyMsg:
		if m.filtering {
			return m.updateFilterInput(msg)
		}

		switch msg.String() {
		case "up", "k":
			m.moveCursor(-1)
		case "down", "j":
			m.moveCursor(1)
		case "left", "h", "backspace":
			m.up()
		case "right", "l", "enter":
			return m, m.enter()
		case "/":
			m.filtering = true
		case "esc", "q":
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m *entriesAddModel) updateFilterInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.filtering = false
		m.cursor = 0
		m.scrollTop = 0
	case "esc":
		m.filtering = false
		m.filterQuery = ""
		m.applyFilter()
		m.cursor = 0
		m.scrollTop = 0
	case "backspace":
		if len(m.filterQuery) > 0 {
			m.filterQuery = m.filterQuery[:len(m.filterQuery)-1]
			m.applyFilter()
		}
	default:
		if len(msg.Runes) > 0 {
			m.filterQuery += string(msg.Runes)
			m.applyFilter()
		}
	}
	return m, nil
}

func (m *entriesAddModel) moveCursor(delta int) {
	if len(m.dirEntries) == 0 {
		return
	}
	next := m.cursor + delta
	if next < 0 {
		next = 0
	}
	if next >= len(m.dirEntries) {
		next = len(m.dirEntries) - 1
	}
	m.cursor = next
	m.ensureVisible()
}

func (m *entriesAddModel) ensureVisible() {
	if m.cursor < m.scrollTop {
		m.scrollTop = m.cursor
	}
	if m.cursor >= m.scrollTop+m.height {
		m.scrollTop = m.cursor - m.height + 1
	}
	if m.scrollTop < 0 {
		m.scrollTop = 0
	}
}

func (m *entriesAddModel) View() string {
	if m.mode == entriesAddModeAlbum {
		return m.form.View()
	}

	var b strings.Builder

	rel, err := filepath.Rel(m.libraryRootRoot, m.currentDir)
	if err != nil || rel == "." {
		rel = ""
	}
	header := "Add tracks: " + filepath.Base(m.playlistPath)
	if rel != "" {
		header += "  ·  " + rel
	}
	b.WriteString(renameHeaderStyle.Render(header))
	b.WriteString("\n")
	b.WriteString(renameSourceStyle.Render(fmt.Sprintf("%d staged", m.selection.StagedCount())))
	b.WriteString("\n\n")

	if m.filtering {
		b.WriteString("/" + m.filterQuery + "█\n\n")
	} else if m.filterQuery != "" {
		b.WriteString(renameSourceStyle.Render("filter: " + m.filterQuery))
		b.WriteString("\n\n")
	}

	if m.errMsg != "" {
		b.WriteString(renameWarningStyle.Render("⚠ " + m.errMsg))
		b.WriteString("\n\n")
	}

	if len(m.dirEntries) == 0 {
		b.WriteString(renameSourceStyle.Render("  (empty)"))
		b.WriteString("\n")
	}

	end := m.scrollTop + m.height
	if end > len(m.dirEntries) {
		end = len(m.dirEntries)
	}
	for i := m.scrollTop; i < end; i++ {
		marker := "  "
		if i == m.cursor {
			marker = reorderCursorStyle.Render("> ")
		}
		b.WriteString(marker + m.dirEntries[i] + "/\n")
	}

	b.WriteString("\n")
	help := "↑/↓ move · ←/h up · →/enter open · / filter · esc/q save & quit · ctrl+c discard"
	b.WriteString(renameSourceStyle.Render(help))

	return b.String()
}
