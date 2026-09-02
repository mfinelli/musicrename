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
package replaygain

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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

// Compute runs rsgain against path in Custom Mode, computing and writing
// REPLAYGAIN_TRACK_GAIN/REPLAYGAIN_TRACK_PEAK tags directly (rsgain writes
// them itself). Track gain only (no -a): every file this computes  ReplayGain
// for is a single standalone derived-audio track, not part of an  album this
// tool manages as a unit.
func Compute(ctx context.Context, path string) error {
	return compute(ctx, execRunner{}, path)
}

func compute(ctx context.Context, r runner, path string) error {
	args := []string{
		"custom",
		"-s", "i", // scan and write ReplayGain 2.0 tags
		"-l", targetLoudness,
		path,
	}

	if err := r.Run(ctx, args); err != nil {
		return fmt.Errorf("rsgain: %w", err)
	}
	return nil
}
