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

// Package replaygain shells out to rsgain to compute and write
// ReplayGain 2.0 tags. Unlike every other tag write in this project,
// rsgain writes the REPLAYGAIN_* tags directly itself, not via go-taglib.
//
// Every scan uses the same settings (a -18 LUFS target and true peaks), so that
// files scanned by different parts of the tool, a standalone track from a video
// or the tracks of an album, are tagged the same way.
package replaygain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"go.senan.xyz/taglib"

	"github.com/mfinelli/musicrename/internal/hasher"
)

// targetLoudness is the ReplayGain 2.0 reference level (EBU R128/ITU-R
// BS.1770, -18 LUFS). Passed explicitly on every invocation rather than
// relying on rsgain's default which keeps the actual value correct and
// intentional regardless of what a future rsgain version might default to.
const targetLoudness = "-18"

// runner abstracts the actual rsgain invocation, mirroring
// internal/transcode's role for ffmpeg (so Compute's surrounding logic can
// be tested without a real rsgain binary).
type runner interface {
	Run(ctx context.Context, args []string) error
}

// execRunner shells out to the real rsgain binary on PATH.
type execRunner struct{}

func (execRunner) Run(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, "rsgain", args...)
	// rsgain's scan/status output is forwarded directly to the
	// terminal rather than captured, matching internal/transcode's Audio
	// (Compute does not depend on parsing it).
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// args builds the rsgain arguments for scanning paths in Custom Mode and
// writing the tags (-s i), at the target loudness, with true peaks (-t). album
// asks for album gain and peak as well (-a), which rsgain calculates over all of
// paths together; quiet turns off rsgain's status output (-q).
func args(paths []string, album, quiet bool) []string {
	a := []string{"custom"}
	if album {
		a = append(a, "-a")
	}
	a = append(a,
		"-s", "i", // scan and write ReplayGain 2.0 tags
		"-l", targetLoudness,
		"-t", // true peak, not sample peak
	)
	if quiet {
		a = append(a, "-q")
	}
	return append(a, paths...)
}

// Compute runs rsgain against path in Custom Mode, computing and writing
// REPLAYGAIN_TRACK_GAIN/REPLAYGAIN_TRACK_PEAK tags directly (rsgain writes
// them itself). Track gain only (no -a): every file this computes  ReplayGain
// for is a single standalone derived-audio track, not part of an  album this
// tool manages as a unit. Albums go through ScanAlbum.
func Compute(ctx context.Context, path string) error {
	return compute(ctx, execRunner{}, path)
}

func compute(ctx context.Context, r runner, path string) error {
	if err := r.Run(ctx, args([]string{path}, false, false)); err != nil {
		return fmt.Errorf("rsgain: %w", err)
	}
	return nil
}

// AlbumResult is what ScanAlbum found out about one album.
type AlbumResult struct {
	// Changed lists the files whose content is different after the scan, in
	// the order they were given. A scan of an album that is already tagged
	// correctly changes none, and one that changes the album gain changes every
	// file, since each carries it.
	//
	// This is by content and not by modification time because rsgain rewrites
	// a file whose tags are already right with identical content, which only
	// changes its modification time.
	Changed []string

	// AlbumGain and AlbumPeak are the album values the files now carry, as
	// written (for example "-7.31 dB" and "0.987654"), read from the first
	// file. They are empty if it has none.
	AlbumGain, AlbumPeak string
}

// ScanAlbum scans paths together as one album, writing REPLAYGAIN_TRACK_GAIN,
// REPLAYGAIN_TRACK_PEAK, REPLAYGAIN_ALBUM_GAIN and REPLAYGAIN_ALBUM_PEAK to
// each file. Every file is always measured, so the album values are those of
// the album as it is now. The result says which files ended up with different
// content, so that anything recording the files' checksums can be brought up to
// date for exactly those.
//
// paths must all be of the one album: they are what album gain is calculated
// over.
func ScanAlbum(ctx context.Context, paths []string) (AlbumResult, error) {
	return scanAlbum(ctx, execRunner{}, paths)
}

func scanAlbum(ctx context.Context, r runner, paths []string) (AlbumResult, error) {
	if len(paths) == 0 {
		return AlbumResult{}, errors.New("no files to scan")
	}

	before, err := checksums(paths)
	if err != nil {
		return AlbumResult{}, err
	}

	if err := r.Run(ctx, args(paths, true, true)); err != nil {
		return AlbumResult{}, fmt.Errorf("rsgain: %w", err)
	}

	after, err := checksums(paths)
	if err != nil {
		return AlbumResult{}, err
	}

	var res AlbumResult
	for i, p := range paths {
		if before[i] != after[i] {
			res.Changed = append(res.Changed, p)
		}
	}

	tags, err := taglib.ReadTags(paths[0])
	if err != nil {
		return AlbumResult{}, fmt.Errorf("reading tags from %s: %w", paths[0], err)
	}
	if v := tags["REPLAYGAIN_ALBUM_GAIN"]; len(v) > 0 {
		res.AlbumGain = v[0]
	}
	if v := tags["REPLAYGAIN_ALBUM_PEAK"]; len(v) > 0 {
		res.AlbumPeak = v[0]
	}
	return res, nil
}

// checksums returns the MD5 of each of paths, in order.
func checksums(paths []string) ([]string, error) {
	sums := make([]string, len(paths))
	for i, p := range paths {
		sum, err := hasher.HashFile(p)
		if err != nil {
			return nil, err
		}
		sums[i] = sum
	}
	return sums, nil
}
