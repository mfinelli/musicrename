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

package spectrum

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// The spectrogram is drawn at this size before ffmpeg adds the axes and
// legend around it, so the finished image is somewhat larger. Wide enough to
// read a whole track's detail at a glance, small enough to open quickly.
const (
	imageWidth  = 1280
	imageHeight = 720
)

// imageRunner abstracts the ffmpeg invocation so WriteSpectrogram's argument
// construction can be tested without a real ffmpeg binary.
type imageRunner interface {
	Run(ctx context.Context, args []string) error
}

// execImageRunner shells out to the real ffmpeg binary on PATH.
type execImageRunner struct{}

func (execImageRunner) Run(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if msg := firstLine(stderr.String()); msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

// WriteSpectrogram renders a spectrogram of the audio file at src (a frequency
// versus time picture of the whole track, with labelled axes and a dB legend)
// to a PNG at dst, using ffmpeg's showspectrumpic filter. dst's parent
// directory must already exist, and dst should end in .png; an existing file
// is overwritten.
//
// The picture is drawn by ffmpeg independently of this package's own
// analysis. It is meant for a person to look at and judge, in the way they
// would look at one in an audio editor: a lossy encode shows up as a hard
// ceiling with nothing above it. Compare the cutoff in the report with the
// frequency axis.
func WriteSpectrogram(ctx context.Context, src, dst string) error {
	return writeSpectrogram(ctx, execImageRunner{}, src, dst)
}

func writeSpectrogram(ctx context.Context, r imageRunner, src, dst string) error {
	// The audio stream is selected explicitly. Without that, ffmpeg can be
	// confused by a file that also carries embedded cover art (a video
	// stream) when deciding what to feed the filter and what to write.
	graph := fmt.Sprintf("[0:a:0]showspectrumpic=s=%dx%d:legend=1[v]", imageWidth, imageHeight)

	args := []string{
		"-y",
		"-v", "error",
		"-nostdin",
		"-i", src,
		"-filter_complex", graph,
		"-map", "[v]",
		"-frames:v", "1",
		dst,
	}

	if err := r.Run(ctx, args); err != nil {
		return fmt.Errorf("ffmpeg: %w", err)
	}
	return nil
}
