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
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/playlist"
	"github.com/mfinelli/musicrename/internal/target"
	"github.com/mfinelli/musicrename/internal/video"
)

var videoSelectCmd = &cobra.Command{
	Use:   "select <target> [video-root]",
	Short: "Interactively choose which videos sync to a device target",
	Long: `Opens an interactive directory browser scoped to video-root, for choosing
which videos are selected for target's video sync: arrow keys (or j/k) move 
the cursor, left/h/backspace goes up a level, right/l/enter descends into a 
bucket letter or, from there, opens a checkbox picker of that artist's videos. 
Press / to filter the current directory's listing by substring. Nothing is 
written until you leave the browser: esc/q at the top level saves the selection 
to video-root/target.m3u8 while ctrl+c discards all changes.

target must support video; this refuses outright for one that doesn't.

If video-root is omitted it defaults to the current working directory.`,
	Args: cobra.RangeArgs(1, 2),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			// Only video-capable targets are valid here; target must
			// support video, matching runVideoSelect's own check below.
			return target.VideoCapableNames, cobra.ShellCompDirectiveNoFileComp
		}
		// video-root: fall back to normal directory/file completion.
		return nil, cobra.ShellCompDirectiveDefault
	},
	RunE: runVideoSelect,
}

func init() {
	videoCmd.AddCommand(videoSelectCmd)
}

func runVideoSelect(cmd *cobra.Command, args []string) error {
	targetName := args[0]
	if !target.Valid(targetName) {
		return fmt.Errorf("unrecognized target %q (expected one of: %s)",
			targetName, strings.Join(target.Names, ", "))
	}
	def, _ := target.DefinitionFor(targetName)
	if !def.SupportsVideo {
		return fmt.Errorf("target %q does not support video", targetName)
	}

	root := "."
	if len(args) > 1 {
		root = args[1]
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("could not resolve video root %q: %w", root, err)
	}
	if _, statErr := os.Stat(absRoot); statErr != nil {
		return fmt.Errorf("%s: %w", absRoot, statErr)
	}

	original, err := playlist.ReadManifest(absRoot, targetName)
	if err != nil {
		return fmt.Errorf("reading existing selection: %w", err)
	}

	m, err := newVideoSelectModel(absRoot, targetName, original)
	if err != nil {
		return fmt.Errorf("starting browser: %w", err)
	}

	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return fmt.Errorf("running video selection browser: %w", err)
	}

	result, ok := final.(*videoSelectModel)
	if !ok {
		return fmt.Errorf("internal error: unexpected video selection browser result type")
	}

	out := cmd.OutOrStdout()
	if result.aborted {
		fmt.Fprintln(out, "Aborted; no changes made.")
		return nil
	}

	newEntries := result.selection.FinalEntries()
	if err := playlist.WriteManifest(absRoot, targetName, newEntries); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s now has %d selected video(s)\n", playlist.ManifestFilename(targetName), len(newEntries))
	return nil
}

// videoSelectChromeLines mirrors entriesAddChromeLines: lines View() spends
// on things other than the directory listing itself.
const videoSelectChromeLines = 5

// videoSelectMode distinguishes the browser's two screens within one
// session: navigating directories, or picking videos from an artist via an
// embedded huh checklist.
type videoSelectMode int

const (
	videoSelectModeBrowse videoSelectMode = iota
	videoSelectModeArtist
)

// videoSelectModel is a bubbletea model spanning both the directory
// browser and, when an artist is opened, an embedded huh checklist (one
// model so staged selections and the current browse position both persist
// across visiting several artists in one session). Structurally close to
// entriesAddModel (cmd/playlist_entries_add.go), but simpler: the videos
// tree (video-root/bucket/artist/title, one level shallower than audio's
// bucket/artist/album/track) never needs the "multiple library roots at the
// top level" or "is this an album" branching entriesAddModel has to handle;
// entering an artist directory always opens the checklist.
type videoSelectModel struct {
	videoRoot  string
	targetName string
	selection  *playlist.BrowseSelection

	currentDir  string
	allEntries  []string
	dirEntries  []string
	cursor      int
	scrollTop   int
	height      int
	filtering   bool
	filterQuery string
	errMsg      string

	form            *huh.Form
	artistSelection []string // bound to the active form's MultiSelect value
	artistRelPaths  []string // every candidate rel path shown in the active form

	mode    videoSelectMode
	aborted bool
}

func newVideoSelectModel(videoRoot, targetName string, original []string) (*videoSelectModel, error) {
	m := &videoSelectModel{
		videoRoot:  videoRoot,
		targetName: targetName,
		selection:  playlist.NewBrowseSelection(original),
		height:     20, // a sane default before the first tea.WindowSizeMsg arrives
	}
	if err := m.loadDir(videoRoot); err != nil {
		return nil, err
	}
	return m, nil
}

// loadDir lists dir's browsable children (playlist.ListSubdirectories,
// every subdirectory except dotfiles) into allEntries/dirEntries and
// resets cursor/scroll/filter state for the new listing. Unlike
// entriesAddModel.loadDir, there's no special top-level case: every
// directory in this browser, videoRoot included, is listed the same way.
func (m *videoSelectModel) loadDir(dir string) error {
	names, err := playlist.ListSubdirectories(dir)
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

func (m *videoSelectModel) applyFilter() {
	m.dirEntries = playlist.FilterNames(m.allEntries, m.filterQuery)
}

// up moves to the parent directory, clamped at videoRoot (a no-op there).
// A read error on the parent leaves the current listing in place (reported
// via errMsg) rather than navigating into a directory that can't actually
// be listed.
func (m *videoSelectModel) up() {
	if m.currentDir == m.videoRoot {
		return
	}
	parent := filepath.Dir(m.currentDir)
	if err := m.loadDir(parent); err != nil {
		m.errMsg = err.Error()
	}
}

// enter opens the selected directory: descends into it, unless
// m.currentDir is already a direct child of videoRoot (i.e. a bucket
// letter, about to enter an artist) — in which case it opens that
// artist's video checklist instead (openArtist), never descending
// visually into the artist directory at all. A read error is reported via
// errMsg rather than navigating into an unreadable directory.
func (m *videoSelectModel) enter() tea.Cmd {
	if len(m.dirEntries) == 0 {
		return nil
	}
	full := filepath.Join(m.currentDir, m.dirEntries[m.cursor])

	if m.currentDir != m.videoRoot {
		return m.openArtist(full)
	}

	if err := m.loadDir(full); err != nil {
		m.errMsg = err.Error()
	}
	return nil
}

// openArtist builds a huh MultiSelect, via videoOptionsForArtist
// (cmd/video_browse.go, shared with `playlist entries add`), of every
// video under artistDir without filtering, unlike playlist entries add's
// equivalent, since every video (whether or not it has a derived audio
// file yet) is eligible for video sync selection.
func (m *videoSelectModel) openArtist(artistDir string) tea.Cmd {
	relPaths, options, err := videoOptionsForArtist(m.videoRoot, artistDir, m.selection.IsSelected,
		func(videoPath string) (string, bool) {
			return videoPath, true
		},
	)
	if err != nil {
		m.errMsg = err.Error()
		return nil
	}
	if len(options) == 0 {
		m.errMsg = fmt.Sprintf("%s has no videos", artistDir)
		return nil
	}

	m.artistRelPaths = relPaths
	m.artistSelection = nil
	field := huh.NewMultiSelect[string]().
		Title(fmt.Sprintf("Select videos from %s", filepath.Base(artistDir))).
		Options(options...).
		Value(&m.artistSelection)

	m.form = huh.NewForm(huh.NewGroup(field))
	m.mode = videoSelectModeArtist
	return m.form.Init()
}

func (m *videoSelectModel) Init() tea.Cmd {
	return nil
}

func (m *videoSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// ctrl+c always aborts the whole session immediately, in either mode,
	// regardless of whatever huh's own internal key handling might
	// otherwise do with it (intercepted here, before ever reaching
	// m.form.Update, so this is never ambiguous).
	if km, ok := msg.(tea.KeyMsg); ok && km.String() == "ctrl+c" {
		m.aborted = true
		return m, tea.Quit
	}

	if m.mode == videoSelectModeArtist {
		return m.updateArtist(msg)
	}
	return m.updateBrowse(msg)
}

func (m *videoSelectModel) updateArtist(msg tea.Msg) (tea.Model, tea.Cmd) {
	newModel, cmd := m.form.Update(msg)
	if f, ok := newModel.(*huh.Form); ok {
		m.form = f
	}

	switch m.form.State {
	case huh.StateCompleted:
		selectedAfter := make(map[string]bool, len(m.artistSelection))
		for _, s := range m.artistSelection {
			selectedAfter[s] = true
		}
		m.selection.Apply(m.artistRelPaths, selectedAfter)
		m.mode = videoSelectModeBrowse
		return m, nil
	case huh.StateAborted:
		// Reachable only via esc within the checklist itself (ctrl+c
		// never reaches m.form.Update at all): discard this artist's
		// edits and return to browsing, not the whole session.
		m.mode = videoSelectModeBrowse
		return m, nil
	}

	return m, cmd
}

func (m *videoSelectModel) updateBrowse(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height - videoSelectChromeLines
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

func (m *videoSelectModel) updateFilterInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (m *videoSelectModel) moveCursor(delta int) {
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

func (m *videoSelectModel) ensureVisible() {
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

func (m *videoSelectModel) View() string {
	if m.mode == videoSelectModeArtist {
		return m.form.View()
	}

	var b strings.Builder

	rel, err := filepath.Rel(m.videoRoot, m.currentDir)
	if err != nil || rel == "." {
		rel = ""
	}
	header := "Select videos: " + m.targetName
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

// videoOptionsForArtist builds huh MultiSelect options for every video
// directly under artistDir: each of artistDir's subdirectories is one video's
// leaf directory), labeled by title alone (every option here is already known
// to be by the same artist).
//
// Shared between `playlist entries add`'s openArtistVideos (resolving each
// video to its derived audio file, only for videos that have exactly one)
// and `video select`'s equivalent (resolving to the video file itself,
// unfiltered). Resolve decides both whether a given video is eligible at
// all and which path represents it in the final list; ok=false skips that
// video entirely, the same as a video with no readable nfo title does.
func videoOptionsForArtist(
	root, artistDir string,
	isSelected func(rel string) bool,
	resolve func(videoPath string) (selectPath string, ok bool),
) (relPaths []string, options []huh.Option[string], err error) {
	titleDirs, err := playlist.ListSubdirectories(artistDir)
	if err != nil {
		return nil, nil, err
	}

	for _, name := range titleDirs {
		titleDir := filepath.Join(artistDir, name)

		videoPath, verr := video.SoleVideoFile(titleDir)
		if verr != nil {
			continue // no video, or more than one: not this browser's problem to flag
		}
		selectPath, ok := resolve(videoPath)
		if !ok {
			continue
		}
		nfo, nerr := video.ReadNFO(titleDir)
		if nerr != nil || strings.TrimSpace(nfo.Title) == "" {
			continue
		}

		rel, relErr := filepath.Rel(root, selectPath)
		if relErr != nil {
			return nil, nil, relErr
		}
		relPaths = append(relPaths, rel)
		options = append(options, huh.NewOption(nfo.Title, rel).Selected(isSelected(rel)))
	}
	return relPaths, options, nil
}
