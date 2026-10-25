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

// Package pcm decodes CD audio and fingerprints it: its length, a CRC-32 and an
// MD5 of the raw samples. Two files with the same fingerprint hold the same
// audio, whatever their compression or tags, which is how a ripped image is
// compared with what EAC read, and one rip with another.
//
// The audio is decoded with ffmpeg as 16-bit little-endian samples at the
// file's own rate and channel count, with nothing converted, so for CD audio
// (16-bit stereo at 44.1 kHz) what is hashed is exactly the samples that were
// ripped. A file in another format would be converted to 16 bits, and what is
// hashed would be the conversion: callers check the format first.
//
// ffmpeg is not trusted to say whether the audio decoded well. It can exit
// successfully, with a full-length result, after a damaged frame, and stays
// silent about it unless asked to check each frame's CRC. So anything it writes
// to its error output counts as a problem, and the fingerprint is compared by
// callers with what the audio is supposed to be, which catches what ffmpeg does
// not.
package pcm

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os/exec"
	"regexp"
	"strings"
)

// bytesPerSample is the size of one sample of CD audio: two channels of 16 bits.
const bytesPerSample = 4

// Fingerprint identifies a stretch of audio.
type Fingerprint struct {
	// Bytes is how much audio there is, as raw 16-bit samples.
	Bytes int64
	// CRC32 is the standard CRC-32 (the IEEE polynomial, as in zlib and gzip) of
	// the samples. It is what Exact Audio Copy writes in its logs as the Copy
	// CRC, when it counts null samples.
	CRC32 uint32
	// MD5 is the MD5 of the samples, which is what FLAC stores in its header.
	MD5 [md5.Size]byte
}

// Samples is how many samples of CD audio there are, each of the two channels'
// value at one instant counting as one, which is how a disc's length is given.
func (f Fingerprint) Samples() int64 { return f.Bytes / bytesPerSample }

// CRC returns the CRC-32 as Exact Audio Copy writes it: eight upper-case hex
// digits.
func (f Fingerprint) CRC() string { return fmt.Sprintf("%08X", f.CRC32) }

// MD5Hex returns the MD5 as 32 lower-case hex digits.
func (f Fingerprint) MD5Hex() string { return hex.EncodeToString(f.MD5[:]) }

// ErrNoFFmpeg is returned when there is no ffmpeg to decode with, which says
// nothing about the audio.
var ErrNoFFmpeg = errors.New("ffmpeg was not found")

// DecodeError reports audio that did not decode cleanly: a file that is not
// audio, one that is damaged, or one that is cut short.
type DecodeError struct {
	// Path is the file that did not decode.
	Path string
	// Message is what ffmpeg said, or how it failed.
	Message string
}

func (e *DecodeError) Error() string {
	return fmt.Sprintf("%s does not decode cleanly: %s", e.Path, e.Message)
}

// Decoder fingerprints audio with the ffmpeg on PATH.
type Decoder struct{}

// Fingerprint decodes the files, in order, as one continuous stretch of audio and
// fingerprints it, so that the tracks of an album give the fingerprint of the
// image they were split from. It stops at the first file that does not decode
// cleanly and returns a *DecodeError for it, with the fingerprint of the audio
// before the problem; or ErrNoFFmpeg if there is no ffmpeg; or the context's
// error if it was cancelled.
func (Decoder) Fingerprint(ctx context.Context, paths ...string) (Fingerprint, error) {
	crc, sum := crc32.NewIEEE(), md5.New()
	w := &countWriter{w: io.MultiWriter(crc, sum)}

	fp := func() Fingerprint {
		f := Fingerprint{Bytes: w.n, CRC32: crc.Sum32()}
		sum.Sum(f.MD5[:0])
		return f
	}

	for _, path := range paths {
		if err := decode(ctx, path, w); err != nil {
			return fp(), err
		}
	}
	return fp(), nil
}

// decode writes the decoded audio of the file at path to w.
func decode(ctx context.Context, path string, w io.Writer) error {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-v", "error",
		"-nostdin",
		// Ask for each frame's own CRC to be checked. Without it a damaged
		// frame is decoded into the wrong samples without a word.
		"-err_detect", "crccheck",
		// "file:" keeps a name such as "a:b.flac" from being read as a URL.
		"-i", "file:"+path,
		"-map", "0:a:0",
		// No -ar, -ac or -sample_fmt: nothing is converted (see the package
		// comment).
		"-f", "s16le",
		"-",
	)
	cmd.Stdout = w
	stderr := &headBuffer{limit: 2048}
	cmd.Stderr = stderr

	err := cmd.Run()

	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, exec.ErrNotFound):
		return ErrNoFFmpeg
	}
	// ffmpeg is run to report errors only, so anything it says is one, even if
	// it then exits successfully.
	msg := message(stderr.String())
	switch {
	case msg != "":
		return &DecodeError{Path: path, Message: msg}
	case err != nil:
		return &DecodeError{Path: path, Message: err.Error()}
	}
	return nil
}

// reLogPrefix is the "[flac @ 0x55d85d2f3340] " that ffmpeg puts before a
// message, naming the component and where it sits in memory, which is no use to
// anyone reading it.
var reLogPrefix = regexp.MustCompile(`^\[[^\]]*\]\s*`)

// noTimestamp is what ffmpeg prints, as the position of a problem, when it does
// not know it: the smallest 64-bit integer. A real position is a sample number
// and says where in the audio the problem is, but this says nothing.
const noTimestamp = " at PTS -9223372036854775808"

// message returns the first line of ffmpeg's error output, without its prefix,
// which is the most specific of what it says; or "" if there is none.
func message(stderr string) string {
	for _, line := range strings.Split(stderr, "\n") {
		line = reLogPrefix.ReplaceAllString(strings.TrimSpace(line), "")
		if line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), noTimestamp)); line != "" {
			return line
		}
	}
	return ""
}

// countWriter counts what is written through it.
type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// headBuffer keeps the first limit bytes written to it and discards the rest, so
// a command that says a great deal cannot use up memory.
type headBuffer struct {
	limit int
	buf   []byte
}

func (h *headBuffer) Write(p []byte) (int, error) {
	if room := h.limit - len(h.buf); room > 0 {
		h.buf = append(h.buf, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

func (h *headBuffer) String() string { return string(h.buf) }
