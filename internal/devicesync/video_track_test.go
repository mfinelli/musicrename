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

package devicesync

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/target"
	"github.com/mfinelli/musicrename/internal/testutil"
)

func testVideoSettings() target.VideoTranscodeSettings {
	return target.VideoTranscodeSettings{
		VideoBitrateKbps: 400,
		AudioBitrateKbps: 128,
		Fullscreen:       target.VideoScale{Width: 320, Height: 240},
		Widescreen:       target.VideoScale{Width: 320, Height: 176},
	}
}

func TestPrepareVideo(t *testing.T) {
	t.Run("produces a real MPEG-2/MPEG-PS file, creating dst's parent directory", func(t *testing.T) {
		dir := t.TempDir()
		src := testutil.MakeVideoFile(t, dir, "src.mp4", 640, 360, "libx264", "aac")
		dst := filepath.Join(dir, "nested", "further", "dst.mpg")

		err := PrepareVideo(context.Background(), src, dst, testVideoSettings())
		require.NoError(t, err)
		assert.FileExists(t, dst)

		outStr := testutil.ProbeText(t, dst, "stream=codec_name,codec_type")
		assert.Contains(t, outStr, "codec_name=mpeg2video")
		assert.Contains(t, outStr, "codec_name=mp3")
	})

	t.Run("overwrites an existing dst", func(t *testing.T) {
		dir := t.TempDir()
		src := testutil.MakeVideoFile(t, dir, "src.mp4", 320, 240, "libx264", "aac")
		dst := filepath.Join(dir, "dst.mpg")
		require.NoError(t, PrepareVideo(context.Background(), src, dst, testVideoSettings()))

		err := PrepareVideo(context.Background(), src, dst, testVideoSettings())
		require.NoError(t, err)
		assert.FileExists(t, dst)
	})

	t.Run("propagates a transcode failure (e.g. a refused aspect ratio)", func(t *testing.T) {
		dir := t.TempDir()
		src := testutil.MakeVideoFile(t, dir, "portrait.mp4", 360, 640, "libx264", "aac")
		dst := filepath.Join(dir, "dst.mpg")

		err := PrepareVideo(context.Background(), src, dst, testVideoSettings())
		assert.Error(t, err)
	})
}
