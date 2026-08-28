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

package navidromesync

import (
	"fmt"
	"regexp"
	"strings"
)

// commentTargetsPattern matches a musicrename-managed #TARGETS: suffix
// appended to a Navidrome playlist's comment field, e.g. "Great road trip mix
// [musicrename:targets=ipod,sdcard]". A suffix rather than owning the whole
// field lets a human still write an ordinary description in the same comment;
// only this trailing bracketed segment is ever read or rewritten by
// musicrename. Anchored to the end of the string, non-greedy on the human-text
// group so a comment that happens to contain other bracketed text elsewhere
// isn't misread.
var commentTargetsPattern = regexp.MustCompile(`(?s)^(.*?)\s*\[musicrename:targets=([^\]]*)\]\s*$`)

// parseCommentTargets splits a Navidrome comment into its human-authored
// portion and any musicrename-managed targets suffix.
//
// hasTargets is false, and targets nil, when no such suffix is present at
// all which is distinct from a present-but-empty suffix
// ("[musicrename:targets=]"), which is an explicit, deliberate empty list.
// This mirrors the same absent/present-but-empty distinction
// [playlist.GlobalPlaylist] makes for the local #TARGETS: directive, so
// the two stay directly comparable.
func parseCommentTargets(comment string) (human string, targets []string, hasTargets bool) {
	m := commentTargetsPattern.FindStringSubmatch(comment)
	if m == nil {
		return comment, nil, false
	}

	human = m[1]
	raw := m[2]
	if raw == "" {
		return human, []string{}, true
	}

	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			targets = append(targets, p)
		}
	}
	if targets == nil {
		targets = []string{}
	}
	return human, targets, true
}

// composeComment rebuilds a Navidrome comment from a human-authored prefix
// and a targets list — the inverse of parseCommentTargets.
//
// If hasTargets is false, the suffix is omitted entirely: this is how a
// local #TARGETS: directive being removed reconciles onto the remote
// side — push simply stops appending a suffix, leaving only whatever
// human text was already there.
func composeComment(human string, targets []string, hasTargets bool) string {
	human = strings.TrimSpace(human)
	if !hasTargets {
		return human
	}

	suffix := fmt.Sprintf("[musicrename:targets=%s]", strings.Join(targets, ","))
	if human == "" {
		return suffix
	}
	return human + " " + suffix
}
