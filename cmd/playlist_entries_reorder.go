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
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/completion"
	"github.com/mfinelli/musicrename/internal/metadata"
	"github.com/mfinelli/musicrename/internal/playlist"
)

var (
	reorderCursorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true)  // blue
	reorderGrabbedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true) // bright yellow
	reorderGrabbedLine  = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
)

// reorderChromeLines is how many lines of the terminal View() spends on
// things other than the entry list itself (title, blank lines, help line),
// subtracted from the terminal height to get how many entry rows actually
// fit on screen at once.
const reorderChromeLines = 4

var playlistEntriesReorderCmd = &cobra.Command{
	Use:   "reorder <playlist>",
	Short: "Interactively reorder a library-wide playlist's entries",
	Long: `Opens an interactive editor listing playlist's current entries. Move the
cursor with the arrow keys (or j/k, Home/End, PgUp/PgDn); press space to
"grab" the entry under the cursor, then the same movement keys move it
instead of the cursor, shifting everything between the old and new position
by one to make room; press space again to release it. Press enter to save
the new order, or esc/ctrl+c/q to cancel without changing anything.

Filenames are shown immediately; track metadata loads in the background 
afterward, filling in as each file is read but you can reorder freely while 
it's still loading.

playlist must already exist.`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completion.PlaylistArg,
	RunE:              runPlaylistEntriesReorder,
}

func init() {
	playlistEntriesCmd.AddCommand(playlistEntriesReorderCmd)
}

func runPlaylistEntriesReorder(cmd *cobra.Command, args []string) error {
	path, err := filepath.Abs(args[0])
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", args[0], err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		return fmt.Errorf("%s: %w", path, statErr)
	}
	absRoot := playlist.LibraryRootRootFor(path)

	gp, err := playlist.ReadGlobalPlaylist(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	out := cmd.OutOrStdout()
	if len(gp.Entries) < 2 {
		fmt.Fprintln(out, "Nothing to reorder (fewer than two entries).")
		return nil
	}

	m := newReorderModel(path, gp.Entries)
	defer m.cancel() // safety net; the quit keys below already cancel proactively
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return fmt.Errorf("running reorder editor: %w", err)
	}

	result, ok := final.(*reorderModel)
	if !ok {
		return fmt.Errorf("internal error: unexpected reorder editor result type")
	}
	if !result.confirmed {
		fmt.Fprintln(out, "Cancelled; no changes made.")
		return nil
	}

	warning, err := playlist.SetEntries(absRoot, path, result.rels())
	if err != nil {
		return err
	}
	if warning != "" {
		lipgloss.Fprintln(out, renameWarningStyle.Render("⚠ "+warning))
	}
	fmt.Fprintf(out, "Saved new order for %d entries\n", len(result.entries))
	return nil
}

// reorderRow is one playlist entry as displayed in the reorder editor.
//
// id is a stable identity assigned once, in [newReorderModel], and never
// changed afterward (distinct from the row's *position* in
// [reorderModel.entries], which changes freely as the user reorders it).
// This is what lets a [tagLoadedMsg] arriving from the background loader
// find the right row via [reorderModel.posByID] no matter how much
// reordering happened while it was in flight, and is also what makes it
// safe for the background loader to never touch [reorderModel.entries] at
// all (see [reorderModel.Init]): loading reads a private, one-time
// snapshot of (id, rel) pairs instead, so there's nothing for a concurrent
// reorder in the main update loop to race against.
//
// label starts as the bare filename and is replaced once the background
// loader resolves this row's tags.
type reorderRow struct {
	id      int
	rel     string
	label   string
	missing bool
}

// reorderModel is a bubbletea model for the interactive reorder editor.
// huh has no drag-reorder shape, so this is hand-built directly on
// bubbletea rather than composed from huh fields, mirroring the pattern
// DESIGN.md earmarked for this feature: lazy background tag loading via
// [metadata.Reader.ReadTrack], a grab-and-move interaction instead of a
// checklist.
type reorderModel struct {
	path    string
	entries []reorderRow
	// posByID[id] is entries' row with that id's current index. Indexed
	// directly by id (ids are assigned 0..len(entries)-1 and never
	// change), not a map, since it's touched on every reorder and every
	// background-load result.
	posByID   []int
	cursor    int
	grabbed   bool
	scrollTop int
	height    int // visible entry rows; refined by the first tea.WindowSizeMsg
	tagMsgs   chan tagLoadedMsg
	confirmed bool
	// ctx/cancel bound the background loader's lifetime to this model's:
	// cancelled the moment the user quits (any of enter/esc/ctrl+c/q), so
	// a playlist with thousands of unresolved entries doesn't keep opening
	// files.
	ctx    context.Context
	cancel context.CancelFunc
}

// tagLoadedMsg reports one entry's resolved tags, arriving asynchronously
// from the background loader started in [reorderModel.Init]. id, not a
// positional index, is what identifies which row this result belongs to.
type tagLoadedMsg struct {
	id      int
	label   string
	missing bool
}

// tagsDoneMsg signals that every entry's tags have been loaded (or
// attempted); the background loader's channel has closed.
type tagsDoneMsg struct{}

func newReorderModel(path string, rels []string) *reorderModel {
	entries := make([]reorderRow, len(rels))
	posByID := make([]int, len(rels))
	for i, rel := range rels {
		entries[i] = reorderRow{id: i, rel: rel, label: filepath.Base(rel)}
		posByID[i] = i
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &reorderModel{
		path:    path,
		entries: entries,
		posByID: posByID,
		height:  20, // a sane default before the first tea.WindowSizeMsg arrives
		ctx:     ctx,
		cancel:  cancel,
	}
}

// rels returns the entries' rel paths in their current (possibly
// reordered) order.
func (m *reorderModel) rels() []string {
	rels := make([]string, len(m.entries))
	for i, e := range m.entries {
		rels[i] = e.rel
	}
	return rels
}

func (m *reorderModel) Init() tea.Cmd {
	libraryRootRoot := playlist.LibraryRootRootFor(m.path)
	m.tagMsgs = make(chan tagLoadedMsg)

	// A private, one-time snapshot of (id, rel) pairs for the background
	// goroutine to read but not m.entries itself, which the main update
	// loop is free to reorder concurrently for as long as this load is
	// still in flight. Nothing else ever touches this snapshot, so
	// there's no race on it either.
	type idRel struct {
		id  int
		rel string
	}
	snapshot := make([]idRel, len(m.entries))
	for i, e := range m.entries {
		snapshot[i] = idRel{id: e.id, rel: e.rel}
	}

	// Checks m.ctx before opening each file, and again on the send itself
	// (which would otherwise block forever once the user quits and
	// nothing is calling waitForTag anymore): a bare "stop when nobody's
	// reading" plus process-exit is not a real guarantee, and the goal is
	// for quitting to actually stop the loader promptly, not merely
	// happen to look that way because the process usually exits right
	// after.
	go func() {
		defer close(m.tagMsgs)
		reader := metadata.NewReader()
		for _, s := range snapshot {
			select {
			case <-m.ctx.Done():
				return
			default:
			}

			row := playlist.ResolveEntryRow(libraryRootRoot, s.rel, reader)

			select {
			case m.tagMsgs <- tagLoadedMsg{id: s.id, label: row.Label, missing: row.Missing}:
			case <-m.ctx.Done():
				return
			}
		}
	}()

	return waitForTag(m.tagMsgs)
}

// waitForTag returns a tea.Cmd that blocks for exactly one message from
// ch, translating a closed channel into tagsDoneMsg. Update re-issues this
// after every tagLoadedMsg it receives, chaining forward one message at a
// time until the background loader finishes (the standard bubbletea
// idiom for streaming incremental results from a background goroutine
// into the update loop without blocking it).
func waitForTag(ch chan tagLoadedMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return tagsDoneMsg{}
		}
		return msg
	}
}

func (m *reorderModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = max(msg.Height-reorderChromeLines, 1)
		m.ensureVisible()

	case tagLoadedMsg:
		if msg.id >= 0 && msg.id < len(m.posByID) {
			pos := m.posByID[msg.id]
			m.entries[pos].label = msg.label
			m.entries[pos].missing = msg.missing
		}
		return m, waitForTag(m.tagMsgs)

	case tagsDoneMsg:
		// Every entry has been resolved (or attempted); nothing further
		// to do here (this case exists to document that explicitly,
		// since it would otherwise fall through the switch silently).

	case tea.KeyPressMsg:
		switch msg.String() {
		case "up", "k":
			m.moveTo(m.cursor - 1)
		case "down", "j":
			m.moveTo(m.cursor + 1)
		case "pgup":
			m.moveTo(m.cursor - m.height)
		case "pgdown":
			m.moveTo(m.cursor + m.height)
		case "home":
			m.moveTo(0)
		case "end":
			m.moveTo(len(m.entries) - 1)
		case "space":
			m.grabbed = !m.grabbed
		case "enter":
			m.confirmed = true
			m.cancel()
			return m, tea.Quit
		case "esc", "ctrl+c", "q":
			m.confirmed = false
			m.cancel()
			return m, tea.Quit
		}
	}

	return m, nil
}

// moveTo moves the cursor to idx, clamped to the valid entry range. If
// grabbed, the entry currently under the cursor moves along with it:
// every entry strictly between the old and new position shifts over by
// one to make room, preserving each shifted entry's own relative order which
// is the same operation whether the cursor moves by one (an adjacent swap,
// via the arrow keys) or jumps several places at once (Home/End/PgUp/
// PgDn), so every movement key routes through this one function. Every
// row whose position actually changes gets its posByID entry updated to
// match, so an in-flight background load result still lands correctly.
func (m *reorderModel) moveTo(idx int) {
	if idx < 0 {
		idx = 0
	}
	if idx >= len(m.entries) {
		idx = len(m.entries) - 1
	}
	if idx == m.cursor {
		return
	}

	if m.grabbed {
		entry := m.entries[m.cursor]
		if idx < m.cursor {
			copy(m.entries[idx+1:m.cursor+1], m.entries[idx:m.cursor])
		} else {
			copy(m.entries[m.cursor:idx], m.entries[m.cursor+1:idx+1])
		}
		m.entries[idx] = entry

		lo, hi := idx, m.cursor
		if lo > hi {
			lo, hi = hi, lo
		}
		for i := lo; i <= hi; i++ {
			m.posByID[m.entries[i].id] = i
		}
	}

	m.cursor = idx
	m.ensureVisible()
}

// ensureVisible adjusts scrollTop, if needed, so the cursor stays within
// the visible window (called after every cursor move and whenever the
// window is resized).
func (m *reorderModel) ensureVisible() {
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

func (m *reorderModel) View() tea.View {
	var b strings.Builder

	b.WriteString(renameHeaderStyle.Render(fmt.Sprintf("Reorder %s", filepath.Base(m.path))))
	b.WriteString("\n\n")

	end := min(m.scrollTop+m.height, len(m.entries))

	for i := m.scrollTop; i < end; i++ {
		e := m.entries[i]

		label := e.label
		if e.missing {
			label = renameWarningStyle.Render("⚠  " + label)
		}

		marker := "  "
		switch {
		case i == m.cursor && m.grabbed:
			marker = reorderGrabbedStyle.Render("◆ ")
		case i == m.cursor:
			marker = reorderCursorStyle.Render("> ")
		}

		line := marker + label
		if i == m.cursor && m.grabbed {
			line = reorderGrabbedLine.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	help := "↑/↓ move · space grab · enter save · esc cancel"
	if m.grabbed {
		help = "↑/↓ move entry · space release · enter save · esc cancel"
	}
	b.WriteString(renameSourceStyle.Render(help))

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}
