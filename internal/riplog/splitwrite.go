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

package riplog

import (
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/mfinelli/musicrename/internal/pcm"
	"github.com/mfinelli/musicrename/internal/sanitize"
)

// Splitter does the work of making a split: it decodes the image, which a
// TrackDecoder does for the check, and writes the tracks.
type Splitter interface {
	TrackDecoder
	// Stream decodes a FLAC file to raw CD audio, which reads without an error
	// only if it decoded cleanly. See pcm.Decoder.Stream.
	Stream(ctx context.Context, path string) io.ReadCloser
	// Encode writes raw CD audio, read to its end, to a FLAC file with the tags.
	// See pcm.Decoder.Encode.
	Encode(ctx context.Context, r io.Reader, path string, tags []pcm.Tag) error
}

// bytesPerCDSample is the size of one sample of CD audio, which is 16 bits for
// each of the two channels.
const bytesPerCDSample = 4

// hiddenTrackTitle is what a hidden track, which the cue sheet has no title for, is
// called.
const hiddenTrackTitle = "Hidden track"

// splitPiece is one file of a split: a track of the disc, or the audio before it.
type splitPiece struct {
	number  int // 0 for the audio before track 1
	samples int64
	name    string // the name of the file
	tags    []pcm.Tag
}

// Split makes a split of the image of the rip the log at logPath describes, into
// the FLAC files of its tracks in dir, which is made if it is not there. The
// tracks are cut where the cue sheet puts them, with each pregap at the end of
// the track before it, which is where a split is checked to have them. Audio
// before track 1 is part of track 1, or a track 00, as opts says (see
// HiddenTrack). Each track is tagged with what the cue sheet says of it, and the
// MusicBrainz release if the rip has one.
//
// Nothing is written unless it is right, and what is written is checked. The log
// must be as EAC wrote it, and the image the one it names, of the length the disc
// has; the audio of the image must have the CRC the log records, if that can be
// compared, which is found out before any track is put in place; and no file that
// is to be made may be there already. The tracks are written under names that are
// not those of tracks, and only when all are written, and the audio is as it should
// be, are they renamed. Then the split is checked, as CheckSplit does, and what it
// finds is in the result. If anything goes wrong before that, the result says why,
// and there is nothing left of the attempt.
//
// It returns the log, which describes the disc, and the findings.
func Split(ctx context.Context, logPath, dir string, sp Splitter, opts SplitOptions) (*Log, *Result, error) {
	l, err := readLog(logPath)
	if err != nil {
		return nil, nil, err
	}

	// refuse is what is done when it cannot be done: there is nothing written.
	refuse := func(r *Result, format string, args ...any) (*Log, *Result, error) {
		r.add("split-write", "Split", StatusError, "nothing was written: "+format, args...)
		return l, r, nil
	}

	r := &Result{}
	checkChecksum(r, l)
	if r.Failed() {
		return refuse(r, "the log is not as EAC wrote it")
	}
	c := splitCue(r, logPath)
	if c == nil {
		return refuse(r, "the cue sheet cannot be used")
	}

	ripDir := filepath.Dir(logPath)
	image := splitImage(ripDir, c)
	if image == "" {
		return refuse(r, "the cue sheet does not name one FLAC image that is beside the log")
	}
	if problem := checkImageForSplit(l, c, image); problem != "" {
		return refuse(r, "%s", problem)
	}
	if same, err := sameDirectory(ripDir, dir); err == nil && same {
		return refuse(r, "the tracks are not to be written into the directory of the rip, which is its archive")
	}

	disc := discNumber(logPath)
	pre, _ := preAudioFrames(l, c)
	separate := opts.HiddenTrack.Separate(pre)
	release := releaseIDOf(ripDir)

	pieces, err := planPieces(l, c, disc, separate, release)
	if err != nil {
		return refuse(r, "%v", err)
	}
	var there []string
	for _, p := range pieces {
		if _, err := os.Lstat(filepath.Join(dir, p.name)); err == nil {
			there = append(there, p.name)
		}
	}
	if len(there) > 0 {
		return refuse(r, "%d %s %s already there: %s", len(there), plural(int64(len(there)), "file"), isAre(len(there)), listLimited(there))
	}
	made := missingDirectories(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return refuse(r, "%s could not be made: %v", dir, err)
	}

	// From here on, there are files, and perhaps directories, to take away if it
	// does not come to an end.
	var written []string
	done := false
	defer func() {
		if done {
			return
		}
		for _, path := range written {
			os.Remove(path)
		}
		for _, d := range made {
			os.Remove(d) // only if it is empty, which it is if it was not there
		}
	}()

	crc, err := writePieces(ctx, sp, image, dir, pieces, &written)
	if err != nil {
		if ctx.Err() != nil {
			return l, nil, ctx.Err()
		}
		return refuse(r, "%s", splitFailure(err))
	}

	// The audio is what EAC ripped, if the log's CRC can say so.
	crcStatus := StatusPass
	crcDetail := fmt.Sprintf("the image has the CRC the log records, %s", crc)
	if why := crcNotComparable(l); why != "" {
		crcStatus = StatusInfo
		crcDetail = fmt.Sprintf("the CRC of the image was not compared with the log's, as %s", why)
	} else if !strings.EqualFold(crc, l.Range.CopyCRC) {
		return refuse(r, "the image decodes to CRC %s, and the log's Copy CRC is %s, so it is not what EAC ripped", crc, l.Range.CopyCRC)
	}

	// All the audio is right, so the files are put in place.
	var finals []string
	for i, p := range pieces {
		final := filepath.Join(dir, p.name)
		if err := os.Rename(written[i], final); err != nil {
			for _, f := range finals {
				os.Remove(f)
			}
			return refuse(r, "%s could not be put in place: %v", p.name, err)
		}
		finals = append(finals, final)
	}
	done = true

	_, post, err := CheckSplit(ctx, logPath, dir, sp, opts)
	if err != nil {
		return l, nil, err
	}

	out := &Result{}
	out.add("split-write", "Split", StatusPass, "%d %s written to %s", len(pieces), plural(int64(len(pieces)), "track"), dir)
	out.add("split-crc", "Image", crcStatus, "%s", crcDetail)
	if release == "" {
		out.add("split-release", "MusicBrainz release", StatusInfo,
			"there is no usable %s beside the log, so the tracks are not tagged with the release", releaseFile)
	}
	out.Checks = append(out.Checks, post.Checks...)
	return l, out, nil
}

// checkImageForSplit says what is wrong with an image that is to be split, or
// returns "" if nothing is: it must be CD audio, and as long as the disc.
func checkImageForSplit(l *Log, c *Cue, image string) string {
	si, err := readStreamInfo(image)
	switch {
	case errors.Is(err, errNotFLAC):
		return fmt.Sprintf("%s is %v", filepath.Base(image), err)
	case err != nil:
		return fmt.Sprintf("%s cannot be read: %v", filepath.Base(image), err)
	case si.SampleRate != cdSampleRate || si.Channels != cdChannels || si.BitsPerSample != cdBits:
		return fmt.Sprintf("%s is %s, %d-bit, %s, and not CD audio", filepath.Base(image),
			rateName(si.SampleRate), si.BitsPerSample, channelsName(si.Channels))
	}

	want := imageSamples(l, c)
	if want == 0 {
		return "the cue sheet and the log do not say how long the image is"
	}
	if int64(si.TotalSamples) != want {
		return fmt.Sprintf("%s holds %d samples, and the disc has %d", filepath.Base(image), si.TotalSamples, want)
	}
	return ""
}

// missingDirectories returns the directories that are not there on the way to dir,
// and dir if it is not, deepest first, which is what making it would make.
func missingDirectories(dir string) []string {
	var missing []string
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(d); err == nil {
			break
		}
		missing = append(missing, d)
	}
	return missing
}

// sameDirectory reports whether two paths are the same directory, if the second
// is there.
func sameDirectory(a, b string) (bool, error) {
	ia, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	ib, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(ia, ib), nil
}

// writePieces decodes the image once, and writes each piece of it to a file of
// its own, under a name that is not that of a track. It records the files it makes
// in written, in order, as it makes them, so that they can be taken away. It
// returns the CRC-32 of the whole of the audio, which is only right if all of it
// was there and there was no more.
func writePieces(ctx context.Context, sp Splitter, image, dir string, pieces []splitPiece, written *[]string) (crc string, err error) {
	rc := sp.Stream(ctx, image)
	defer rc.Close()

	hash := crc32.NewIEEE()
	// What the image fails with is what went wrong, however an encoder takes it.
	in := &errCapture{r: io.TeeReader(rc, hash)}

	for _, p := range pieces {
		path := filepath.Join(dir, "."+p.name+".partial")
		*written = append(*written, path)

		piece := &countingReader{r: io.LimitReader(in, p.samples*bytesPerCDSample)}
		err := sp.Encode(ctx, piece, path, p.tags)
		switch {
		case in.err != nil:
			return "", in.err
		case err != nil:
			return "", err
		}
		if piece.n != p.samples*bytesPerCDSample {
			return "", fmt.Errorf("the image ends before %s does: it is %d samples short", p.name, p.samples-piece.n/bytesPerCDSample)
		}
	}

	// Nothing may be left, or the pieces are not the whole of what the disc has.
	var extra [1]byte
	n, err := in.Read(extra[:])
	switch {
	case n > 0:
		return "", errors.New("the image holds more audio than the cue sheet and the log say the disc has")
	case err != nil && !errors.Is(err, io.EOF):
		return "", err
	}
	return fmt.Sprintf("%08X", hash.Sum32()), nil
}

// errCapture remembers the error that ended what it read, which is not io.EOF.
type errCapture struct {
	r   io.Reader
	err error
}

func (e *errCapture) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) && e.err == nil {
		e.err = err
	}
	return n, err
}

// countingReader counts what is read through it.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// splitFailure says why a split could not be made, from what went wrong.
func splitFailure(err error) string {
	var decodeErr *pcm.DecodeError
	var encodeErr *pcm.EncodeError
	switch {
	case errors.Is(err, pcm.ErrNoFFmpeg):
		return "ffmpeg was not found"
	case errors.As(err, &decodeErr):
		return fmt.Sprintf("the image does not decode cleanly: %s", decodeErr.Message)
	case errors.As(err, &encodeErr):
		return encodeErr.Error()
	}
	return err.Error()
}

// planPieces works out the files of a split: where the audio is cut, what each is
// called, and what it is tagged with. The audio before track 1 comes first, if it
// is a track of its own.
func planPieces(l *Log, c *Cue, disc int, separate bool, release string) ([]splitPiece, error) {
	lengths, err := expectedLengths(l, c, separate, false)
	if err != nil {
		return nil, err
	}

	byNumber := map[int]CueTrack{}
	for _, ct := range c.Tracks {
		byNumber[ct.Number] = ct
	}

	var numbers []int
	if separate {
		numbers = append(numbers, 0)
	}
	for _, t := range l.TOC {
		numbers = append(numbers, t.Number)
	}

	pieces := make([]splitPiece, 0, len(numbers))
	for _, n := range numbers {
		ct := byNumber[n]
		title := ct.Title
		if n == 0 {
			title = hiddenTrackTitle
		}

		prefix := trackLabel(disc, n) + " "
		stem := sanitize.PathComponent(title, sanitize.TrackOverride, sanitize.TrackTitleLimit(prefix, ".flac"))
		if stem == "" {
			stem = sanitize.PathComponent("Track "+strconv.Itoa(n), sanitize.TrackOverride, sanitize.TrackTitleLimit(prefix, ".flac"))
		}

		pieces = append(pieces, splitPiece{
			number:  n,
			samples: lengths[n],
			name:    prefix + stem + ".flac",
			tags:    tagsFor(c, ct, n, disc, title, release),
		})
	}
	return pieces, nil
}

// tagsFor is what a track is tagged with: what the cue sheet says of it and the
// album, and where it is on the disc, and so what Picard needs to find it a place
// on the release. Whatever is empty is left out. All of it is UTF-8, as the cue
// sheet's text was made when it was read.
//
// Only so much is written because Picard keeps tags it has nothing to say about,
// and what the cue sheet has beyond this, such as the genre and the year, is from
// a database that is often wrong. The comment EAC adds is not written at all.
func tagsFor(c *Cue, ct CueTrack, number, disc int, title, release string) []pcm.Tag {
	artist := ct.Performer
	if artist == "" {
		artist = c.Performer
	}
	var discTag string
	if disc > 0 {
		discTag = strconv.Itoa(disc)
	}

	var tags []pcm.Tag
	for _, t := range []pcm.Tag{
		{Key: "TITLE", Value: title},
		{Key: "ARTIST", Value: artist},
		{Key: "ALBUM", Value: c.Title},
		{Key: "ALBUMARTIST", Value: c.Performer},
		{Key: "TRACKNUMBER", Value: strconv.Itoa(number)},
		{Key: "DISCNUMBER", Value: discTag},
		{Key: "ISRC", Value: ct.ISRC},
		{Key: "MUSICBRAINZ_ALBUMID", Value: release},
	} {
		if v := tagText(t.Value); v != "" {
			tags = append(tags, pcm.Tag{Key: t.Key, Value: v})
		}
	}
	return tags
}

// tagText makes text fit to be a tag: valid UTF-8, with a line break or any other
// control character as a space, and no space around it or twice in a row.
func tagText(s string) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// releaseIDOf reads the MusicBrainz release the rip's disc is on from the file the
// workflow saves its URL in, beside the log. It is "" if there is no file, or it
// does not hold one.
func releaseIDOf(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, releaseFile))
	if err != nil {
		return ""
	}
	// It is "" if the text is not a release.
	id, _ := parseReleaseURL(string(data))
	return id
}
