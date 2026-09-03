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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mfinelli/musicrename/internal/hasher"
	"github.com/mfinelli/musicrename/internal/replaygain"
	"github.com/mfinelli/musicrename/internal/transcode"
)

// ExtractAudioOptions selects which of ExtractAudio's three modes runs. The
// zero value is a fresh extraction. Retag and Force are mutually exclusive.
type ExtractAudioOptions struct {
	// Retag rewrites only the derived audio file's tags from nfo (without
	// re-encoding, recomputing ReplayGain, or refreshing audio.src.md5,
	// since the video's actual content is assumed unchanged (only nfo
	// drifted, e.g., after a `video edit`). Requires an existing derived
	// audio file and errors if none exists yet.
	Retag bool

	// Force fully re-extracts regardless of current state: remux, retag,
	// recompute ReplayGain, and refresh audio.src.md5 for when the source
	// video's actual content changed (a re-fetch, a replaced upload).
	Force bool
}

// ExtractAudio produces (or refreshes) videoPath's derived audio file from
// nfo, returning the derived file's path on success.
//
// nfo.Title and nfo.Artist are required (mirroring WriteDerivedAudioTags'
// check, surfaced here before any work begins rather than after a remux).
//
// More than one existing derived audio file for videoPath (DerivedAudioFiles
// returning more than one match (for example a previous re-extraction's stale
// file wasn't cleaned up, or manual tampering) is always a hard error,
// regardless of opts: this is exactly the kind of state the curator should
// notice and resolve deliberately, not something even --force should silently
// paper over by guessing which one to keep.
func ExtractAudio(ctx context.Context, videoPath string, nfo NFO, opts ExtractAudioOptions) (string, error) {
	if opts.Retag && opts.Force {
		return "", fmt.Errorf("retag and force are mutually exclusive")
	}
	if nfo.Title == "" || nfo.Artist == "" {
		return "", fmt.Errorf("%s: musicvideo.nfo is missing title or artist", videoPath)
	}

	dir := filepath.Dir(videoPath)

	existing, err := DerivedAudioFiles(videoPath)
	if err != nil {
		return "", fmt.Errorf("finding existing derived audio for %s: %w", videoPath, err)
	}
	if len(existing) > 1 {
		return "", fmt.Errorf(
			"%s: multiple existing derived audio files (%s); resolve manually before continuing",
			videoPath, strings.Join(existing, ", "),
		)
	}

	if opts.Retag {
		if len(existing) == 0 {
			return "", fmt.Errorf("%s: no existing derived audio to retag; run without --retag first", videoPath)
		}
		return existing[0], retagDerivedAudio(existing[0], dir, nfo)
	}

	if !opts.Force && len(existing) == 1 {
		return "", fmt.Errorf(
			"%s: derived audio already exists (%s); use --retag to update tags or --force to re-extract",
			videoPath, existing[0],
		)
	}

	return extractDerivedAudio(ctx, videoPath, dir, existing, nfo)
}

// retagDerivedAudio rewrites path's tags from nfo and refreshes its
// sums.md5 entry. No re-encode, no ReplayGain recompute, no audio.src.md5
// touch (path's audio content itself is assumed unchanged).
func retagDerivedAudio(path, dir string, nfo NFO) error {
	if err := WriteDerivedAudioTags(path, nfo); err != nil {
		return err
	}
	if err := hasher.UpdateFile(dir, filepath.Base(path)); err != nil {
		return fmt.Errorf("updating sums.md5 for %s: %w", path, err)
	}
	return nil
}

// extractDerivedAudio runs the full pipeline (probe, remux, tag, ReplayGain)
// producing videoPath's derived audio file, then (only once that new file
// is confirmed fully written and processed) removes any stale-extension
// leftover from existing (a previous extraction whose source video had a
// different audio codec). Cleanup explicitly happens last: if any earlier
// step fails, a pre-existing derived file (however stale) is left intact
// rather than the video ending up with no derived audio at all.
func extractDerivedAudio(ctx context.Context, videoPath, dir string, existing []string, nfo NFO) (string, error) {
	codec, err := transcode.ProbeAudioCodec(ctx, videoPath)
	if err != nil {
		return "", err
	}
	ext, ok := transcode.ExtensionForCodec(codec)
	if !ok {
		return "", fmt.Errorf("%s: unrecognized audio codec %q, cannot extract", videoPath, codec)
	}

	stem := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	dstPath := filepath.Join(dir, stem+ext)

	if err := transcode.RemuxAudio(ctx, videoPath, dstPath); err != nil {
		return "", err
	}
	if err := WriteDerivedAudioTags(dstPath, nfo); err != nil {
		return "", err
	}
	if err := replaygain.Compute(ctx, dstPath); err != nil {
		return "", err
	}
	if err := hasher.UpdateFile(dir, filepath.Base(dstPath)); err != nil {
		return "", fmt.Errorf("updating sums.md5 for %s: %w", dstPath, err)
	}

	videoHash, err := hasher.HashFile(videoPath)
	if err != nil {
		return "", fmt.Errorf("hashing %s: %w", videoPath, err)
	}
	sidecar := map[string]string{filepath.Base(videoPath): videoHash}
	if err := hasher.WriteSums(dir, AudioSrcSumsFilename, sidecar); err != nil {
		return "", fmt.Errorf("writing %s: %w", AudioSrcSumsFilename, err)
	}

	for _, old := range existing {
		if old == dstPath {
			continue // same name+ext: RemuxAudio's -y already overwrote it in place
		}
		if err := os.Remove(old); err != nil {
			return "", fmt.Errorf("removing stale derived audio %s: %w", old, err)
		}
		if err := hasher.RemoveFile(dir, filepath.Base(old)); err != nil {
			return "", fmt.Errorf("removing %s from sums.md5: %w", old, err)
		}
	}

	return dstPath, nil
}
