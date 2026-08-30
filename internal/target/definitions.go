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

package target

import "slices"

// AudioFormat identifies a target audio format independent of the specific
// ffmpeg codec/encoder used to produce it so a target's Definition only ever
// needs to say "I want mp3," not remember specific encoder flags. The zero
// value ("") means "no format" / "never transcodes," used as
// [Definition.TranscodeFormat]'s default.
type AudioFormat string

// FormatMP3 is the MP3 audio format, encoded via libmp3lame.
const FormatMP3 AudioFormat = "mp3"

// EncodeParams are the actual ffmpeg codec and arguments used to produce a
// given AudioFormat: the one place codec-level specifics (which encoder,
// which quality setting) live, so adding or tuning a format never requires
// touching any target's Definition.
type EncodeParams struct {
	// Codec is the ffmpeg -c:a value, e.g. "libmp3lame".
	Codec string
	// Args are additional ffmpeg arguments controlling encode quality,
	// e.g. ["-q:a", "0"] (V0).
	Args []string
	// Ext is the output file extension, including the leading dot.
	Ext string
}

// encodeParams maps each supported AudioFormat to its real ffmpeg encode
// settings.
var encodeParams = map[AudioFormat]EncodeParams{
	FormatMP3: {Codec: "libmp3lame", Args: []string{"-q:a", "0"}, Ext: ".mp3"},
}

// EncodeParamsFor returns the encode parameters for format. ok is false
// for an unrecognized format.
func EncodeParamsFor(format AudioFormat) (params EncodeParams, ok bool) {
	params, ok = encodeParams[format]
	return params, ok
}

// Definition is a target's full sync policy: which source audio formats it
// accepts unchanged, what format anything else gets transcoded to, and how it
// wants artwork delivered.
type Definition struct {
	// AcceptedFormats are source extensions (lowercase, including the
	// leading dot) copied through to this target unchanged.
	AcceptedFormats []string
	// TranscodeFormat is the target format anything outside
	// AcceptedFormats gets transcoded to. The zero value means this
	// target never transcodes (every source format it might encounter
	// must already be in AcceptedFormats).
	TranscodeFormat AudioFormat
	// ArtMaxDimension is the max width/height, in pixels, resized artwork
	// is constrained to for this target.
	ArtMaxDimension int
	// EmbedArt, if true, means artwork is embedded in each audio file
	// instead of shipped as an external folder.jpg/png. Since the
	// external file becomes redundant once art travels with the audio
	// file itself, sync does not copy it to a target with EmbedArt set.
	EmbedArt bool
}

// definitions maps each valid target name (see Names, Valid) to its full
// sync policy. Every name in Names must have an entry here, and vice
// versa (enforced by TestDefinitionsMatchNames).
var definitions = map[string]Definition{
	"ipod": {
		AcceptedFormats: []string{".flac", ".mp3", ".m4a"},
		// No TranscodeFormat: Rockbox plays every format this tool
		// already manages, so ipod never needs to transcode anything.
		ArtMaxDimension: 400, // iPod screen is 320x240; a little headroom
		EmbedArt:        false,
	},
	"sdcard": {
		AcceptedFormats: []string{".mp3"},
		TranscodeFormat: FormatMP3,
		ArtMaxDimension: 500,
		EmbedArt:        true,
	},
}

// DefinitionFor returns target's full sync policy. ok is false for an
// unrecognized target (callers should already have validated the target
// name via Valid before relying on this).
func DefinitionFor(target string) (def Definition, ok bool) {
	def, ok = definitions[target]
	return def, ok
}

// Accepts reports whether ext (lowercase, including the leading dot) is in
// d's accepted-formats set (i.e., whether a source file with this
// extension should be copied through to this target unchanged rather than
// transcoded).
func (d Definition) Accepts(ext string) bool {
	return slices.Contains(d.AcceptedFormats, ext)
}
