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

package playlist

// BrowseSelection tracks staged entry selections during an interactive
// `playlist entries add` browsing session: which of the playlist's
// original entries remain selected, plus any newly staged entries in the
// order first added. Entirely in-memory bookkeeping (no filesystem access
// of any kind) so a session can visit as many albums as it likes before
// anything is actually written.
type BrowseSelection struct {
	original    []string
	originalSet map[string]bool
	selected    map[string]bool
	addedOrder  []string
}

// NewBrowseSelection seeds a session from a playlist's current entries:
// every one of them starts selected, matching what's already there before
// anything is touched.
func NewBrowseSelection(original []string) *BrowseSelection {
	originalSet := make(map[string]bool, len(original))
	selected := make(map[string]bool, len(original))
	for _, o := range original {
		originalSet[o] = true
		selected[o] = true
	}
	return &BrowseSelection{
		original:    original,
		originalSet: originalSet,
		selected:    selected,
	}
}

// IsSelected reports whether rel is currently staged to be in the final
// playlist.
func (s *BrowseSelection) IsSelected(rel string) bool {
	return s.selected[rel]
}

// StagedCount is how many entries are currently selected in total, across
// both original and newly added ones.
func (s *BrowseSelection) StagedCount() int {
	n := 0
	for _, v := range s.selected {
		if v {
			n++
		}
	}
	return n
}

// Apply diffs selectedAfter (a completed album checklist's final selected
// set) against the current staged state, for exactly candidates (that
// album's tracks, the only ones the checklist could have changed) and
// anything outside candidates is left untouched, even if it happens to
// also appear in selectedAfter for some other reason.
//
// A newly staged entry (not already in the playlist this session started
// with) is appended to the staging order the first time it's selected,
// and removed from that order if later unstaged again; an original entry
// being unstaged and later restaged never touches that order at all,
// since its position is already accounted for by where it sits in the
// original playlist (see FinalEntries).
func (s *BrowseSelection) Apply(candidates []string, selectedAfter map[string]bool) {
	for _, rel := range candidates {
		was := s.selected[rel]
		now := selectedAfter[rel]
		if was == now {
			continue
		}
		s.selected[rel] = now

		if s.originalSet[rel] {
			continue
		}
		if now {
			s.addedOrder = append(s.addedOrder, rel)
		} else {
			for i, a := range s.addedOrder {
				if a == rel {
					s.addedOrder = append(s.addedOrder[:i], s.addedOrder[i+1:]...)
					break
				}
			}
		}
	}
}

// FinalEntries combines original, selected, and addedOrder into the
// playlist's final entry list: original entries preserved in their
// original order (minus anything explicitly unstaged this session),
// followed by newly staged entries in the order first staged.
func (s *BrowseSelection) FinalEntries() []string {
	final := make([]string, 0, len(s.original)+len(s.addedOrder))
	for _, o := range s.original {
		if s.selected[o] {
			final = append(final, o)
		}
	}
	final = append(final, s.addedOrder...)
	return final
}

// HasNewEntries reports whether at least one entry not already in the
// original playlist was staged this session: distinct from StagedCount,
// which also counts untouched original entries, and true regardless of
// whether any original entries were also unstaged in the same session.
func (s *BrowseSelection) HasNewEntries() bool {
	return len(s.addedOrder) > 0
}
