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

package video

import (
	"path/filepath"
	"slices"

	"go.senan.xyz/taglib"

	"github.com/mfinelli/musicrename/internal/hasher"
)

// derivedAudioTagDriftMessage compares audioPath's current tags against
// what WriteDerivedAudioTags would write from nfo, returning a
// human-readable finding if they differ, or "" if they match. Only the four
// tags WriteDerivedAudioTags actually owns (ownedAudioTagKeys, from
// derived_audio_tags.go) are compared so a foreign tag like
// REPLAYGAIN_TRACK_GAIN drifting (it never does on its own) isn't this
// function's concern.
//
// want[key] and tags[key] are compared directly via slices.Equal rather than
// branching on whether nfo sets that field at all: a missing key on either
// side reads as nil, and nil correctly compares unequal to a stale
// leftover value (no separate "field was cleared" case needs handling).
func derivedAudioTagDriftMessage(audioPath string, nfo NFO) (string, error) {
	tags, err := taglib.ReadTags(audioPath)
	if err != nil {
		return "", err
	}

	want := map[string][]string{
		taglib.Title:  {nfo.Title},
		taglib.Artist: {nfo.Artist},
	}
	if nfo.Album != "" {
		want[taglib.Album] = []string{nfo.Album}
	}
	if nfo.Year != "" {
		want[taglib.Date] = []string{nfo.Year}
	}

	for key := range ownedAudioTagKeys {
		if !slices.Equal(want[key], tags[key]) {
			return "derived audio tags do not match musicvideo.nfo; run 'video extract-audio --retag'", nil
		}
	}
	return "", nil
}

// derivedAudioContentDriftMessage compares audio.src.md5's recorded video
// hash (as of the last extraction) against sums.md5's *currently recorded*
// entry for the video: a plain string comparison, no hashing performed
// here. Each of the ways this can be unverifiable (no audio.src.md5 at
// all, no sums.md5, sums.md5 missing the video's entry) gets its own
// distinct message rather than being collapsed into a generic "can't
// verify" or silently treated as fresh.
func derivedAudioContentDriftMessage(dir, videoPath string) (string, error) {
	srcSums, srcExisted, err := hasher.ReadSums(dir, AudioSrcSumsFilename)
	if err != nil {
		return "", err
	}
	if !srcExisted {
		return "derived audio exists but no audio.src.md5 sidecar was found", nil
	}

	videoBase := filepath.Base(videoPath)
	recordedHash, ok := srcSums[videoBase]
	if !ok {
		return "audio.src.md5 exists but has no entry for the current video filename; can't verify", nil
	}

	currentSums, currentExisted, err := hasher.ReadSums(dir, hasher.SumsFilename)
	if err != nil {
		return "", err
	}
	if !currentExisted {
		return "can't verify derived audio freshness: sums.md5 is missing; run 'video sums'", nil
	}
	currentHash, ok := currentSums[videoBase]
	if !ok {
		return "can't verify derived audio freshness: sums.md5 has no entry for the video; run 'video sums'", nil
	}

	if recordedHash != currentHash {
		return "derived audio may be stale (source video content changed); run 'video extract-audio --force'", nil
	}
	return "", nil
}
