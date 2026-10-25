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
	"strings"

	"github.com/mfinelli/musicrename/internal/pcm"
)

// Decoder fingerprints audio files. pcm.Decoder, which decodes with ffmpeg, is
// one; tests use a fake.
type Decoder interface {
	// Fingerprint decodes the files, in order, as one stretch of audio and
	// fingerprints it. It returns a *pcm.DecodeError for audio that does not
	// decode cleanly, and pcm.ErrNoFFmpeg if there is nothing to decode with.
	Fingerprint(ctx context.Context, paths ...string) (pcm.Fingerprint, error)
}

// Options are what CheckFile may use besides the files themselves. A field left
// nil is something that is not available, and the check that needs it is noted
// as not made.
type Options struct {
	// MusicBrainz looks up the release in musicbrainz.txt; nil for no network.
	MusicBrainz Lookup
	// Audio decodes the image to check its contents; nil to leave that out.
	Audio Decoder
}

// imageFile is the FLAC image a cue sheet names, once it is known to be one.
type imageFile struct {
	path string
	info streamInfo
}

// checkAudio confirms the image holds the audio EAC read. The log's Copy CRC is a
// CRC-32 of the audio EAC ripped, before it was encoded, and FLAC loses nothing,
// so decoding the image must give audio with that CRC. It also compares what it
// decodes with what the FLAC records of itself, its length and the MD5 of its
// audio, which is what shows damage to the file.
//
// It is only done for CD audio, the one format the decoder reproduces exactly,
// and only if the log's CRC is the plain one: with "Null samples used in CRC
// calculations" off, EAC leaves the silent samples out of it, which is not what
// is hashed here.
func checkAudio(ctx context.Context, r *Result, l *Log, img imageFile, dec Decoder) {
	const id, name = "audio", "Audio"

	if dec == nil {
		r.add(id, name, StatusInfo, "the image was not decoded, so its audio was not checked")
		return
	}
	if v, _ := l.Setting("Null samples used in CRC calculations"); !strings.EqualFold(v, "Yes") {
		r.add(id, name, StatusInfo,
			"the log's CRC leaves out null samples, which is not reproduced, so the audio was not compared with it")
		return
	}
	if l.Range.CopyCRC == "" {
		r.add(id, name, StatusInfo, "the log has no Copy CRC to compare the audio with")
		return
	}

	fp, err := dec.Fingerprint(ctx, img.path)
	var decodeErr *pcm.DecodeError
	switch {
	case errors.Is(err, pcm.ErrNoFFmpeg):
		r.add(id, name, StatusWarning, "ffmpeg was not found, so the audio was not checked")
		return
	case errors.As(err, &decodeErr):
		r.add(id, name, StatusError, "the FLAC does not decode cleanly: %s", decodeErr.Message)
		return
	case err != nil:
		r.add(id, name, StatusWarning, "could not decode the image, so its audio was not checked: %v", err)
		return
	}

	// What the file says of itself. A difference is damage: the file is not what
	// the encoder wrote.
	var damage []string
	if want := img.info.TotalSamples; want != 0 && uint64(fp.Samples()) != want {
		damage = append(damage, fmt.Sprintf("it decodes to %d samples, not the %d in its header", fp.Samples(), want))
	}
	if img.info.HasMD5() && fp.MD5 != img.info.MD5 {
		damage = append(damage, "its audio does not match the MD5 in its header")
	}

	var problems []string
	if len(damage) > 0 {
		problems = append(problems, "the FLAC is damaged: "+strings.Join(damage, ", and "))
	}
	if !strings.EqualFold(fp.CRC(), l.Range.CopyCRC) {
		problems = append(problems, fmt.Sprintf(
			"the audio's CRC is %s, not the log's Copy CRC %s: it is not the audio EAC ripped, or it was changed after",
			fp.CRC(), l.Range.CopyCRC))
	}
	if len(problems) > 0 {
		r.add(id, name, StatusError, "%s", strings.Join(problems, "; "))
		return
	}

	detail := fmt.Sprintf("CRC %s matches the log's Copy CRC", fp.CRC())
	if img.info.HasMD5() {
		detail += ", and the audio matches the MD5 in the FLAC's header"
	} else {
		detail += " (the FLAC stores no MD5 to compare with)"
	}
	r.add(id, name, StatusPass, "%s", detail)
}
