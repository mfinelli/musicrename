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

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefinitionsMatchNames(t *testing.T) {
	t.Run("every name in Names has a Definition", func(t *testing.T) {
		for _, n := range Names {
			_, ok := DefinitionFor(n)
			assert.True(t, ok, "expected a Definition for %q", n)
		}
	})

	t.Run("every Definition corresponds to a name in Names", func(t *testing.T) {
		for name := range definitions {
			assert.True(t, Valid(name), "definitions has an entry for unrecognized target %q", name)
		}
	})

	t.Run("counts match (no duplicate or missing entries either way)", func(t *testing.T) {
		assert.Equal(t, len(Names), len(definitions))
	})
}

func TestDefinitionFor(t *testing.T) {
	t.Run("returns false for an unrecognized target", func(t *testing.T) {
		_, ok := DefinitionFor("chromecast")
		assert.False(t, ok)
	})

	t.Run("ipod falls back to aac for anything outside its accepted set", func(t *testing.T) {
		def, ok := DefinitionFor("ipod")
		require.True(t, ok)
		assert.Equal(t, FormatAAC, def.TranscodeFormat)
		assert.False(t, def.EmbedArt)
	})

	t.Run("sdcard transcodes to mp3 and embeds art", func(t *testing.T) {
		def, ok := DefinitionFor("sdcard")
		require.True(t, ok)
		assert.Equal(t, FormatMP3, def.TranscodeFormat)
		assert.True(t, def.EmbedArt)
	})

	t.Run("ipod supports video with real WinFF-derived transcode settings", func(t *testing.T) {
		def, ok := DefinitionFor("ipod")
		require.True(t, ok)
		assert.True(t, def.SupportsVideo)
		assert.Equal(t, 400, def.Video.VideoBitrateKbps)
		assert.Equal(t, 128, def.Video.AudioBitrateKbps)
		assert.Equal(t, VideoScale{Width: 320, Height: 240}, def.Video.Fullscreen)
		assert.Equal(t, VideoScale{Width: 320, Height: 176}, def.Video.Widescreen)
	})

	t.Run("sdcard does not support video", func(t *testing.T) {
		def, ok := DefinitionFor("sdcard")
		require.True(t, ok)
		assert.False(t, def.SupportsVideo)
	})

	t.Run("every target with SupportsVideo has real (non-zero) video settings", func(t *testing.T) {
		for name := range definitions {
			def, _ := DefinitionFor(name)
			if !def.SupportsVideo {
				continue
			}
			assert.NotZero(t, def.Video.VideoBitrateKbps, "target %q supports video but has no video bitrate set", name)
			assert.NotZero(t, def.Video.AudioBitrateKbps, "target %q supports video but has no audio bitrate set", name)
			assert.NotZero(t, def.Video.Fullscreen, "target %q supports video but has no fullscreen scale set", name)
			assert.NotZero(t, def.Video.Widescreen, "target %q supports video but has no widescreen scale set", name)
		}
	})
}

func TestDefinitionAccepts(t *testing.T) {
	t.Run("ipod accepts flac, mp3, m4a, opus, and ogg", func(t *testing.T) {
		def, ok := DefinitionFor("ipod")
		require.True(t, ok)
		assert.True(t, def.Accepts(".flac"))
		assert.True(t, def.Accepts(".mp3"))
		assert.True(t, def.Accepts(".m4a"))
		assert.True(t, def.Accepts(".opus"))
		assert.True(t, def.Accepts(".ogg"))
		assert.False(t, def.Accepts(".wav"))
	})

	t.Run("sdcard accepts only mp3", func(t *testing.T) {
		def, ok := DefinitionFor("sdcard")
		require.True(t, ok)
		assert.True(t, def.Accepts(".mp3"))
		assert.False(t, def.Accepts(".flac"))
		assert.False(t, def.Accepts(".m4a"))
	})

	t.Run("zero-value Definition accepts nothing", func(t *testing.T) {
		var def Definition
		assert.False(t, def.Accepts(".mp3"))
	})
}

func TestEncodeParamsFor(t *testing.T) {
	t.Run("mp3 is defined", func(t *testing.T) {
		params, ok := EncodeParamsFor(FormatMP3)
		require.True(t, ok)
		assert.Equal(t, "libmp3lame", params.Codec)
		assert.Equal(t, ".mp3", params.Ext)
		assert.NotEmpty(t, params.Args)
	})

	t.Run("aac is defined", func(t *testing.T) {
		params, ok := EncodeParamsFor(FormatAAC)
		require.True(t, ok)
		assert.Equal(t, "aac", params.Codec)
		assert.Equal(t, ".m4a", params.Ext)
		assert.NotEmpty(t, params.Args)
	})

	t.Run("an unrecognized format returns false", func(t *testing.T) {
		_, ok := EncodeParamsFor(AudioFormat("wav"))
		assert.False(t, ok)
	})

	t.Run("the zero value AudioFormat is unrecognized", func(t *testing.T) {
		_, ok := EncodeParamsFor(AudioFormat(""))
		assert.False(t, ok)
	})

	t.Run("every non-empty TranscodeFormat used by a target has encode params", func(t *testing.T) {
		for name := range definitions {
			def, _ := DefinitionFor(name)
			if def.TranscodeFormat == "" {
				continue
			}
			_, ok := EncodeParamsFor(def.TranscodeFormat)
			assert.True(t, ok, "target %q references undefined format %q", name, def.TranscodeFormat)
		}
	})
}
