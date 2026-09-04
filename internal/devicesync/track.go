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

// Package devicesync produces target-ready track copies and runs the full
// device sync reconciliation. It manages the per-track piece ([PrepareTrack])
// (passthrough copy or transcode, tag migration, and artwork embedding) which
// the  reconciliation algorithm and capacity planning both sit on top of.
package devicesync

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.senan.xyz/taglib"

	"github.com/mfinelli/musicrename/internal/target"
	"github.com/mfinelli/musicrename/internal/transcode"
)

// PrepareTrack produces a target-ready copy of the audio file at src,
// written to dst: a plain byte-for-byte copy if src's extension is already
// accepted by def ([target.Definition.Accepts]), or a transcode
// ([transcode.Audio]) into def's TranscodeFormat otherwise. tick, if
// non-nil, is called periodically with byte-copy progress (but only on
// the passthrough path; there's nothing to report through tick on a
// transcode because ffmpeg's output covers that on its own, so tick is
// simply unused there).
// dst's parent directory is created if it doesn't already exist; dst is
// overwritten if it does.
//
// Tags are migrated (the same mechanism `check`/`inspect`/`lyrics` already
// use, replacing dst's tags entirely, [taglib.Clear], rather than merging)
// only on the transcode path. A transcode needs tags written regardless, since
// internal/transcode.Audio strips them outright (-map_metadata -1) and the
// destination is already a different file by construction (there is no
// byte-identity property to protect there).
//
// If def.EmbedArt and art is non-nil, art (already resized to def's
// ArtMaxDimension by the caller; this function does not resize) is
// embedded into dst via [taglib.WriteImage] regardless of whether this was
// a passthrough or a transcode. This is not the same concern as the tags
// case above: for any target with EmbedArt set (currently only `sdcard`),
// on-device bytes are never meant to be identical to source in the first
// place; embedding is the whole point of that target, derived-file handling
// (a `{target}.src.md5` sidecar, not a direct hash comparison) already
// accounts for that, unlike the passthrough case. art is ignored entirely for
// a target that doesn't embed artwork.
func PrepareTrack(ctx context.Context, src, dst string, def target.Definition, art []byte, tick func(copied, total int64)) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(dst), err)
	}

	ext := strings.ToLower(filepath.Ext(src))
	if def.Accepts(ext) {
		if err := copyFile(src, dst, tick); err != nil {
			return fmt.Errorf("copying %s: %w", src, err)
		}
	} else {
		params, ok := target.EncodeParamsFor(def.TranscodeFormat)
		if !ok {
			return fmt.Errorf(
				"%s: no encode params defined for format %q", src, def.TranscodeFormat,
			)
		}
		if err := transcode.Audio(ctx, src, dst, params); err != nil {
			return fmt.Errorf("transcoding %s: %w", src, err)
		}

		tags, err := taglib.ReadTags(src)
		if err != nil {
			return fmt.Errorf("reading tags from %s: %w", src, err)
		}
		if err := taglib.WriteTags(dst, tags, taglib.Clear); err != nil {
			return fmt.Errorf("writing tags to %s: %w", dst, err)
		}
	}

	if def.EmbedArt && art != nil {
		if err := taglib.WriteImage(dst, art); err != nil {
			return fmt.Errorf("embedding artwork in %s: %w", dst, err)
		}
	}

	return nil
}

// copyFile copies src to dst byte-for-byte, for PrepareTrack's passthrough
// case (src's format is already accepted by the target, so no transcode is
// needed). tick, if non-nil, is called periodically (throttled to
// copyProgressInterval, always including one final call once every byte
// is written) with bytes copied so far and the total.
func copyFile(src, dst string, tick func(copied, total int64)) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	var w io.Writer = out
	if tick != nil {
		info, statErr := in.Stat()
		if statErr != nil {
			return statErr
		}
		w = &copyProgressWriter{w: out, total: info.Size(), tick: tick}
	}

	if _, err := io.Copy(w, in); err != nil {
		return err
	}
	// Explicit Close (in addition to the deferred one above) so a
	// write-flush failure is actually caught and returned, rather than
	// silently discarded the way the deferred call's error would be.
	return out.Close()
}

// copyProgressInterval throttles copyProgressWriter's tick calls so a
// large file copied through many small io.Copy buffer writes doesn't
// flood the caller (a 32MB file at Go's default 32kB copy buffer is
// otherwise ~1000 Write calls).
const copyProgressInterval = 100 * time.Millisecond

// copyProgressWriter wraps an io.Writer, invoking tick with the running
// byte count after each underlying Write but throttled to at most once per
// copyProgressInterval, except the final call (copied == total), which
// always fires regardless of timing so the caller reliably sees 100%.
type copyProgressWriter struct {
	w        io.Writer
	total    int64
	copied   int64
	tick     func(copied, total int64)
	lastTick time.Time
}

func (c *copyProgressWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.copied += int64(n)
	if c.copied == c.total || time.Since(c.lastTick) >= copyProgressInterval {
		c.tick(c.copied, c.total)
		c.lastTick = time.Now()
	}
	return n, err
}
