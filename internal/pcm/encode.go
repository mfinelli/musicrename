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

package pcm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
)

// Tag is one Vorbis comment of a FLAC file: TITLE=Something is Tag{"TITLE",
// "Something"}. Both are UTF-8, as the format has it.
type Tag struct {
	Key, Value string
}

// EncodeError is a failure of ffmpeg to encode audio to a file.
type EncodeError struct {
	// Path is the file that was to be written.
	Path string
	// Message is what ffmpeg said.
	Message string
}

func (e *EncodeError) Error() string {
	return fmt.Sprintf("could not write %s: %s", e.Path, e.Message)
}

// stopReader is a stream that stops its decode when it is closed.
type stopReader struct {
	*io.PipeReader
	cancel context.CancelFunc
}

func (s *stopReader) Close() error {
	s.cancel()
	return s.PipeReader.Close()
}

// Stream decodes a file to raw CD audio, as Decode does, for the caller to read
// from: 16-bit little-endian samples, left then right. Reading it to its end
// without an error means the file decoded cleanly; if it does not, the error it
// gives is the *DecodeError for the file, or ErrNoFFmpeg. Closing it before then
// stops the decode.
func (Decoder) Stream(ctx context.Context, path string) io.ReadCloser {
	ctx, cancel := context.WithCancel(ctx)
	return &stopReader{PipeReader: stream(ctx, []string{path}), cancel: cancel}
}

// errReader remembers the error that ended what it read, which is not io.EOF.
type errReader struct {
	r   io.Reader
	err error
}

func (e *errReader) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) && e.err == nil {
		e.err = err
	}
	return n, err
}

// Encode writes the audio read from r to a FLAC file at path, with the tags,
// replacing a file that is there. r gives raw CD audio, as Stream does, and is read
// to its end, which is the end of the audio.
//
// The file has only the tags given, and no mention of ffmpeg: the tags are what
// the caller chose to say of it. Nothing is converted, so decoding the file gives
// back what was read, sample for sample. If r fails, the error it failed with is
// returned, and the file is not to be used.
func (Decoder) Encode(ctx context.Context, r io.Reader, path string, tags []Tag) error {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return ErrNoFFmpeg
	}

	args := []string{
		"-v", "error",
		"-y",
		"-f", "s16le", "-ar", "44100", "-ac", "2",
		"-i", "pipe:0",
		"-map_metadata", "-1",
		"-c:a", "flac",
		// No encoder tag, and no version of ffmpeg in the vendor string.
		"-fflags", "+bitexact",
	}
	for _, t := range tags {
		args = append(args, "-metadata", t.Key+"="+t.Value)
	}
	// The format is given, since the name of a file that is not finished may not
	// say it, and "file:" keeps a name from being read as a URL.
	args = append(args, "-f", "flac", "file:"+path)

	in := &errReader{r: r}
	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	cmd.Stdin = in
	stderr := &headBuffer{limit: 2048}
	cmd.Stderr = stderr

	err = cmd.Run()

	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case in.err != nil:
		// What went wrong is what r failed with, and not what ffmpeg then made of
		// the audio there was.
		return in.err
	}
	if msg := message(stderr.String()); msg != "" {
		return &EncodeError{Path: path, Message: msg}
	}
	if err != nil {
		return &EncodeError{Path: path, Message: err.Error()}
	}
	return nil
}
