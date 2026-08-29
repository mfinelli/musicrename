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

// Package transcode shells out to ffmpeg to convert an audio file's codec
// for a sync target, mirroring the yt-dlp shell-out pattern already used for
// music video fetching (internal/video) rather than linking a codec library
// directly.
//
// This package intentionally does nothing with tags or artwork.
package transcode

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/mfinelli/musicrename/internal/target"
)

// runner abstracts the actual ffmpeg invocation so Audio's surrounding
// logic can be tested without requiring a real ffmpeg binary. The
// production implementation (execRunner) shells out to the real binary;
// tests supply a fake.
type runner interface {
	Run(ctx context.Context, args []string) error
}

// execRunner shells out to the real ffmpeg binary on PATH.
type execRunner struct{}

func (execRunner) Run(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	// ffmpeg's own progress output is forwarded directly to the terminal
	// rather than captured; Audio does not depend on parsing it.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Audio transcodes the audio file at src into dst using params (the
// per-target codec/quality settings internal/target.EncodeParams). dst's
// parent directory must already exist; if dst already exists it is
// overwritten.
//
// Tags and artwork are intentionally left out of this call entirely.
// Metadata is stripped during transcode (-map_metadata -1) and any
// embedded picture stream is dropped (-vn) rather than trusting ffmpeg's
// format-conversion heuristics to carry them over. Both are migrated
// afterward as separate steps, using this project's existing tag/artwork
// mechanism (go.senan.xyz/taglib, already used everywhere else tags are
// read or written): every tag this tool recognizes migrates through the same
// normalized representation it already uses everywhere, rather than depending
// on however completely ffmpeg's Vorbis-comment-to-ID3v2 mapping happens to
// cover this project's tag vocabulary, and there is no risk of ffmpeg carrying
// over a stale, unresized embedded picture that the later artwork step
// would then need to detect and overwrite.
func Audio(ctx context.Context, src, dst string, params target.EncodeParams) error {
	return transcode(ctx, execRunner{}, src, dst, params)
}

// transcode contains Audio's logic against an injectable runner so it can
// be exercised in tests without a real ffmpeg binary.
func transcode(ctx context.Context, r runner, src, dst string, params target.EncodeParams) error {
	args := []string{
		"-y", // overwrite dst if it already exists
		"-i", src,
		"-map_metadata", "-1", // strip all copied metadata; migrated separately
		"-vn", // drop any embedded picture stream; re-embedded separately
		"-c:a", params.Codec,
	}
	args = append(args, params.Args...)
	args = append(args, dst)

	if err := r.Run(ctx, args); err != nil {
		return fmt.Errorf("ffmpeg: %w", err)
	}
	return nil
}
