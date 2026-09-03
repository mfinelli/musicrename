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

// Package completion holds shell-completion (cobra ValidArgsFunction and
// flag-completion) logic shared across multiple cmd/ commands.
package completion

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/mfinelli/musicrename/internal/playlist"
)

// PlaylistArg is the shared ValidArgsFunction for every command whose first
// positional argument is a specific library-wide playlist file
// (playlists/*.m3u8) rather than a library-root-root directory: the
// playlist entries family, playlist sort, playlist targets, and sync
// navidrome delete/pull/push.
func PlaylistArg(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return []string{playlist.Extension}, cobra.ShellCompDirectiveFilterFileExt
}

// CommaSeparated completes a single comma-separated flag value or
// positional argument drawn from candidates, e.g. typing "ipod,sd"
// completes to "ipod,sdcard". Cobra always hands the whole current value to
// a completion func (not just the token after the last comma), so this
// splits on the last comma itself: everything before it is kept verbatim as
// the returned strings' prefix, names already listed there are excluded
// from further suggestions (no point re-offering "ipod" once it's already
// picked), and only the trailing partial segment is matched against
// candidates.
//
// Shared by playlist create --targets, playlist targets --set (both over
// target.Names), and playlist sort's [fields] argument (over
// playlist.ValidSortFieldNames).
func CommaSeparated(candidates []string) func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		prefix := ""
		partial := toComplete
		already := make(map[string]bool)

		if idx := strings.LastIndex(toComplete, ","); idx >= 0 {
			prefix = toComplete[:idx+1]
			partial = toComplete[idx+1:]
			for _, s := range strings.Split(toComplete[:idx], ",") {
				already[s] = true
			}
		}

		var out []string
		for _, c := range candidates {
			if already[c] || !strings.HasPrefix(c, partial) {
				continue
			}
			out = append(out, prefix+c)
		}

		// NoSpace: the shell shouldn't insert a trailing space after
		// completing one name, since a comma (and another name) may
		// follow. NoFileComp: this is never a filename.
		return out, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
	}
}
