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

package devicesync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mfinelli/musicrename/internal/target"
	"github.com/mfinelli/musicrename/internal/transcode"
)

// PrepareVideo produces a target-ready copy of the video file at src,
// written to dst, via [transcode.TranscodeVideo] using settings. dst's
// parent directory is created if it doesn't already exist; dst is
// overwritten if it does.
//
// Unlike [PrepareTrack], there is no passthrough case to check at all:
// Rockbox's MPEGplayer plugin only decodes one specific format (MPEG-2
// video / MP3 audio, muxed as an MPEG Program Stream), so every video is
// always transcoded, regardless of its source container/codec. No tags are
// migrated and no artwork is embedded either unlike PrepareTrack's audio case,
// Rockbox's video plugin doesn't read either one.
func PrepareVideo(ctx context.Context, src, dst string, settings target.VideoTranscodeSettings) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(dst), err)
	}
	if err := transcode.TranscodeVideo(ctx, src, dst, settings); err != nil {
		return fmt.Errorf("transcoding %s: %w", src, err)
	}
	return nil
}
