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

	"go.senan.xyz/taglib"

	"github.com/mfinelli/musicrename/internal/target"
	"github.com/mfinelli/musicrename/internal/transcode"
)

// PrepareTrack produces a target-ready copy of the audio file at src,
// written to dst: a plain byte-for-byte copy if src's extension is already
// accepted by def ([target.Definition.Accepts]), or a transcode
// ([transcode.Audio]) into def's TranscodeFormat otherwise.
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
func PrepareTrack(ctx context.Context, src, dst string, def target.Definition, art []byte) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(dst), err)
	}

	ext := strings.ToLower(filepath.Ext(src))
	if def.Accepts(ext) {
		if err := copyFile(src, dst); err != nil {
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
// needed).
func copyFile(src, dst string) error {
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

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	// Explicit Close (in addition to the deferred one above) so a
	// write-flush failure is actually caught and returned, rather than
	// silently discarded the way the deferred call's error would be.
	return out.Close()
}
