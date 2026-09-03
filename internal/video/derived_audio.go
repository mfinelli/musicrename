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
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mfinelli/musicrename/internal/transcode"
)

// AudioSrcSumsFilename is the derived-audio content-drift sidecar: records
// the source video's hash as of extraction time, in the same md5sum-compatible
// format sums.md5 itself uses (via [hasher.WriteSums]/[hasher.ReadSums]).
// Structurally it's the same idea as the on-device {target}.src.md5 sidecar
// ([target.SrcSumsFilename]), just fixed to one name rather than parameterized
// by target, since there's exactly one derived audio file per video directory
// rather than one per sync target.
const AudioSrcSumsFilename = "audio.src.md5"

// DerivedAudioFiles returns every file directly inside videoPath's directory
// that is a candidate derived audio file for it i.e., same base filename stem
// as videoPath, with an extension transcode.RemuxAudio could have produced.
//
// Ordinarily this is zero or one file. More than one is a meaningful
// condition (a previous extraction's file wasn't cleaned up after the
// source video's audio codec changed across a re-extraction, or manual
// tampering) rather than something to collapse into a single result here:
// callers that need to detect it (extraction's stale-file cleanup, "video
// check"'s multiple-sidecar finding) inspect the returned slice's length
// directly rather than this function guessing which one is "the" real one.
func DerivedAudioFiles(videoPath string) ([]string, error) {
	dir := filepath.Dir(videoPath)
	stem := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var matches []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		name := e.Name()
		ext := filepath.Ext(name)
		if strings.TrimSuffix(name, ext) != stem {
			continue
		}
		if !transcode.IsDerivedAudioExt(strings.ToLower(ext)) {
			continue
		}

		matches = append(matches, filepath.Join(dir, name))
	}

	sort.Strings(matches)
	return matches, nil
}
