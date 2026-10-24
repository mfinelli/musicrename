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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// StreamInfo describes the audio stream being analyzed.
type StreamInfo struct {
	// Codec is ffprobe's codec name, e.g. "flac" or "alac".
	Codec string
	// SampleRate is in Hz.
	SampleRate int
	// Channels is the number of channels.
	Channels int
	// ChannelLayout is ffprobe's layout name (e.g. "stereo", "5.1"); it
	// may be empty.
	ChannelLayout string
	// BitsPerSample is the declared sample size, or 0 if unknown.
	BitsPerSample int
}

// NotLosslessError is returned by Analyze for a file whose codec is lossy (or
// unrecognized). Looking for evidence of lossy encoding in a file that is
// openly lossy has no meaning.
type NotLosslessError struct {
	Codec string
}

func (e *NotLosslessError) Error() string {
	return fmt.Sprintf("codec %q is not lossless", e.Codec)
}

// IsLosslessCodec reports whether an ffprobe codec name is one of the
// lossless codecs this package can usefully analyze.
func IsLosslessCodec(codec string) bool {
	switch codec {
	case "flac", "alac", "wavpack", "ape", "tta", "truehd", "mlp":
		return true
	}
	return strings.HasPrefix(codec, "pcm_")
}

// ffmpegTag matches the "[flac @ 0x55af3b6a2c80] " prefix ffmpeg puts on a
// message from one of its components.
var ffmpegTag = regexp.MustCompile(`^\[[^\]]*@ 0x[0-9a-f]+\]\s*`)

// firstLine returns the first non-empty line of ffmpeg or ffprobe's stderr
// output, without the component prefix. Their complaints about a bad file run
// to many lines, mostly repeating one another with memory addresses; the first
// is the one that says what went wrong, and a report listing many tracks needs
// each failure to fit on a line.
func firstLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return ffmpegTag.ReplaceAllString(line, "")
		}
	}
	return ""
}

// lfeIndex returns the channel index of the low-frequency-effects channel for
// an ffprobe layout name, or -1 if the layout has none (or is unknown). In
// FFmpeg's channel order the LFE channel is third in a 2.1 layout and fourth
// in every larger layout that has one (5.1, 7.1, and so on).
//
// The LFE channel is deliberately lowpassed at roughly 120 Hz, so it has no
// meaningful high-frequency content to examine and must not be allowed to
// stand in for the other channels.
func lfeIndex(layout string) int {
	if len(layout) < 3 || layout[0] < '0' || layout[0] > '9' || layout[1:3] != ".1" {
		return -1
	}
	if layout[0] == '2' {
		return 2
	}
	return 3
}

// probeRunner abstracts the ffprobe invocation so Probe's parsing can be
// tested without a real ffprobe binary. It mirrors the equivalent seam in
// internal/transcode.
type probeRunner interface {
	Run(ctx context.Context, args []string) (stdout string, err error)
}

// execProbeRunner shells out to the real ffprobe binary on PATH.
type execProbeRunner struct{}

func (execProbeRunner) Run(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "ffprobe", args...)

	// Unlike internal/transcode, ffprobe's stderr is captured rather than
	// forwarded: this package analyzes many files in a row, and a raw error
	// line interleaved with the report is less useful than the same text
	// attached to the error for the file it belongs to.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		if msg := firstLine(stderr.String()); msg != "" {
			err = fmt.Errorf("%w: %s", err, msg)
		}
	}
	return string(out), err
}

// Probe reads the first audio stream's properties from path using ffprobe.
func Probe(ctx context.Context, path string) (StreamInfo, error) {
	return probe(ctx, execProbeRunner{}, path)
}

func probe(ctx context.Context, r probeRunner, path string) (StreamInfo, error) {
	args := []string{
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=codec_name,sample_rate,channels,channel_layout,bits_per_raw_sample",
		"-of", "json",
		path,
	}

	out, err := r.Run(ctx, args)
	if err != nil {
		return StreamInfo{}, fmt.Errorf("ffprobe: %w", err)
	}

	var parsed struct {
		Streams []struct {
			CodecName        string `json:"codec_name"`
			SampleRate       string `json:"sample_rate"`
			Channels         int    `json:"channels"`
			ChannelLayout    string `json:"channel_layout"`
			BitsPerRawSample string `json:"bits_per_raw_sample"`
		} `json:"streams"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return StreamInfo{}, fmt.Errorf("parsing ffprobe output: %w", err)
	}
	if len(parsed.Streams) == 0 {
		return StreamInfo{}, fmt.Errorf("no audio stream in %s", path)
	}

	s := parsed.Streams[0]
	rate, err := strconv.Atoi(s.SampleRate)
	if err != nil || rate <= 0 {
		return StreamInfo{}, fmt.Errorf("ffprobe reported no usable sample rate for %s", path)
	}
	if s.Channels <= 0 {
		return StreamInfo{}, fmt.Errorf("ffprobe reported no channels for %s", path)
	}

	// ffprobe prints "N/A" (or nothing) when it does not know the depth.
	bitsPer, _ := strconv.Atoi(s.BitsPerRawSample)

	return StreamInfo{
		Codec:         s.CodecName,
		SampleRate:    rate,
		Channels:      s.Channels,
		ChannelLayout: s.ChannelLayout,
		BitsPerSample: bitsPer,
	}, nil
}

// source abstracts how decoded audio is obtained so Analyze's surrounding
// logic can be tested without a real ffmpeg binary.
type source interface {
	// Open returns the file's audio as interleaved 32-bit little-endian
	// floats at the file's own sample rate and channel count. Close must
	// be called; it reports a decode failure the stream's contents alone
	// may not reveal.
	Open(ctx context.Context, path string) (io.ReadCloser, error)
}

// execSource decodes with the real ffmpeg binary on PATH.
type execSource struct{}

func (execSource) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-v", "error",
		"-nostdin",
		"-i", path,
		"-map", "0:a:0",
		// No -ar or -ac: the audio is decoded at its native sample rate
		// and channel count, since resampling or downmixing would
		// itself reshape the spectrum being measured.
		"-f", "f32le",
		"-acodec", "pcm_f32le",
		"-",
	)

	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("ffmpeg: %w", err)
	}

	return &cmdReader{cmd: cmd, out: out, stderr: stderr}, nil
}

// cmdReader reads a running command's stdout and, on Close, waits for the
// command and reports a failed exit.
type cmdReader struct {
	cmd    *exec.Cmd
	out    io.Reader
	stderr *bytes.Buffer
	eof    bool
}

func (c *cmdReader) Read(p []byte) (int, error) {
	n, err := c.out.Read(p)
	if errors.Is(err, io.EOF) {
		c.eof = true
	}
	return n, err
}

func (c *cmdReader) Close() error {
	if !c.eof {
		// Stopped before the end of the stream: don't wait for a decode
		// nobody is reading.
		_ = c.cmd.Process.Kill()
	}
	err := c.cmd.Wait()
	if c.eof && err != nil {
		msg := firstLine(c.stderr.String())
		if msg == "" {
			return fmt.Errorf("ffmpeg: %w", err)
		}
		return fmt.Errorf("ffmpeg: %w: %s", err, msg)
	}
	return nil
}
