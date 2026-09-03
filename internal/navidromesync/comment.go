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
	"sort"
	"strings"
)

// commentDirectivesPattern matches a musicrename-managed suffix appended to
// a Navidrome playlist's comment field, e.g. "Great road trip mix
// [musicrename:sort=artist,album;targets=ipod,sdcard]". A suffix rather
// than owning the whole field lets a human still write an ordinary
// description in the same comment; only this trailing bracketed segment is
// ever read or rewritten by musicrename. Anchored to the end of the
// string, non-greedy on the human-text group so a comment that happens to
// contain other bracketed text elsewhere isn't misread.
//
// The bracket's inner content is a single semicolon-separated list of
// key=value directives (not one bracket per directive) matched here as
// one opaque group and split apart in [parseCommentDirectives], since a
// semicolon or an unescaped bracket can never legitimately appear inside a
// target name or sort field name (both are drawn from small, fixed,
// punctuation-free vocabularies), so no escaping is needed to keep the two
// directives' comma-separated value lists unambiguous within one bracket.
var commentDirectivesPattern = regexp.MustCompile(`(?s)^(.*?)\s*\[musicrename:([^\]]*)\]\s*$`)

// commentDirectiveOrder is every recognized comment directive key, in the
// fixed alphabetical order [composeComment] always writes them in so that we
// have deterministic output regardless of which are present, mirroring how
// target *values* are already alphabetized on write. Adding a third
// directive means inserting its key here in alphabetical position, not
// just appending, and adding its case to composeComment's key-by-key
// section below.
var commentDirectiveOrder = []string{"sort", "targets"}

// commentDirectives holds every musicrename-managed directive that can
// appear in a Navidrome playlist's comment suffix. Field names and Has*
// pairing intentionally mirror [playlist.GlobalPlaylist]'s Targets/
// HasTargets and Sort/HasSort so the two stay directly comparable and a
// caller can move values between them without translation.
type commentDirectives struct {
	Targets    []string
	HasTargets bool
	Sort       []string
	HasSort    bool
}

// parseCommentDirectives splits a Navidrome comment into its human-authored
// portion and any musicrename-managed directives found in its suffix.
// Directive keys may appear in any order within the bracket because only
// [composeComment]'s output is canonically ordered: this parser stays
// liberal about what it accepts (a hand-edited comment, or a suffix
// written by a future version of musicrename with its own key order,
// should still parse correctly) while [composeComment] is strict about
// what it produces. An unrecognized key within the bracket is silently
// ignored rather than rejected outright, for the same forward-compatibility
// reason. A key with no "=" is likewise ignored.
//
// A given key's Has* is false, and its value slice nil, when that key
// isn't present in the suffix at all which is distinct from present-but-empty
// (e.g. "targets="), which is an explicit, deliberate empty list. This
// mirrors the same absent/present-but-empty distinction
// [playlist.GlobalPlaylist] makes for its own local directives.
func parseCommentDirectives(comment string) (human string, d commentDirectives) {
	m := commentDirectivesPattern.FindStringSubmatch(comment)
	if m == nil {
		return comment, commentDirectives{}
	}

	human = m[1]
	for kv := range strings.SplitSeq(m[2], ";") {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}
		key, raw, found := strings.Cut(kv, "=")
		if !found {
			continue
		}

		values := splitCommentList(raw)
		switch strings.TrimSpace(key) {
		case "targets":
			d.Targets = values
			d.HasTargets = true
		case "sort":
			d.Sort = values
			d.HasSort = true
		}
	}
	return human, d
}

// splitCommentList splits one directive's raw comma-separated value into
// its parts, trimmed. Always non-nil (even for an empty raw), so a caller
// can distinguish "key present, empty value" from "key absent entirely"
// purely via the corresponding Has* field, without also checking for nil.
func splitCommentList(raw string) []string {
	values := []string{}
	if raw == "" {
		return values
	}
	for p := range strings.SplitSeq(raw, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			values = append(values, p)
		}
	}
	return values
}

// composeComment rebuilds a Navidrome comment from a human-authored prefix
// and a set of directives (the inverse of [parseCommentDirectives]).
// Present directives are always written in [commentDirectiveOrder], so two
// calls with the same logical content produce byte-identical output
// regardless of the order their fields were set in which is what lets
// push.go compare a freshly-composed comment against one recomposed from
// the (possibly differently-ordered, e.g. hand-edited) remote comment
// without a spurious "different" result.
//
// Targets' values are alphabetized here because order carries no meaning
// for a set. Sort's values are never reordered: order there is precedence,
// the exact thing the directive exists to remember, and alphabetizing it
// would silently corrupt it which matches [playlist.WriteGlobalPlaylist]'s
// identical treatment of the local #SORT: directive.
//
// If no directive in d has its Has* set, the suffix is omitted entirely:
// this is how a local directive being removed (via `playlist targets
// --clear`, or the last-remembered #SORT: disappearing some other way)
// reconciles onto the remote side: push simply stops appending anything
// for it, leaving only whatever human text was already there.
func composeComment(human string, d commentDirectives) string {
	human = strings.TrimSpace(human)

	parts := make([]string, 0, len(commentDirectiveOrder))
	for _, key := range commentDirectiveOrder {
		switch key {
		case "sort":
			if d.HasSort {
				parts = append(parts, "sort="+strings.Join(d.Sort, ","))
			}
		case "targets":
			if d.HasTargets {
				sorted := append([]string(nil), d.Targets...)
				sort.Strings(sorted)
				parts = append(parts, "targets="+strings.Join(sorted, ","))
			}
		}
	}

	if len(parts) == 0 {
		return human
	}

	suffix := fmt.Sprintf("[musicrename:%s]", strings.Join(parts, ";"))
	if human == "" {
		return suffix
	}
	return human + " " + suffix
}
