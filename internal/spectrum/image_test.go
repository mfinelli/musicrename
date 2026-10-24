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
	"context"
	"errors"
	"image"
	_ "image/png" // registers the PNG decoder for image.DecodeConfig
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/testutil"
)

type fakeImageRunner struct {
	args []string
	err  error
}

func (f *fakeImageRunner) Run(_ context.Context, args []string) error {
	f.args = args
	return f.err
}

func TestWriteSpectrogramArguments(t *testing.T) {
	fr := &fakeImageRunner{}

	err := writeSpectrogram(context.Background(), fr, "in.flac", "out.png")

	require.NoError(t, err)
	assert.Equal(t, []string{
		"-y",
		"-v", "error",
		"-nostdin",
		"-i", "in.flac",
		"-filter_complex", "[0:a:0]showspectrumpic=s=1280x720:legend=1[v]",
		"-map", "[v]",
		"-frames:v", "1",
		"out.png",
	}, fr.args)

	t.Run("failure is wrapped", func(t *testing.T) {
		err := writeSpectrogram(context.Background(), &fakeImageRunner{err: errors.New("boom")}, "a", "b.png")
		assert.ErrorContains(t, err, "ffmpeg: boom")
	})
}

func TestWriteSpectrogramRealFile(t *testing.T) {
	dir := t.TempDir()
	src := testutil.MakeNoiseFlac(t, dir, "track.flac", 44100, 4)
	dst := filepath.Join(dir, "track.png")

	err := WriteSpectrogram(context.Background(), src, dst)

	require.NoError(t, err)

	f, err := os.Open(dst)
	require.NoError(t, err)
	defer f.Close()
	cfg, format, err := image.DecodeConfig(f)
	require.NoError(t, err)
	assert.Equal(t, "png", format)
	// ffmpeg adds axes and a legend around the requested plot area.
	assert.GreaterOrEqual(t, cfg.Width, imageWidth)
	assert.GreaterOrEqual(t, cfg.Height, imageHeight)

	t.Run("overwrites an existing file", func(t *testing.T) {
		require.NoError(t, WriteSpectrogram(context.Background(), src, dst))
	})

	t.Run("an unreadable source is an error with ffmpeg's message", func(t *testing.T) {
		err := WriteSpectrogram(context.Background(), filepath.Join(dir, "nope.flac"), filepath.Join(dir, "x.png"))
		assert.ErrorContains(t, err, "No such file or directory")
	})

	t.Run("a missing destination directory is an error", func(t *testing.T) {
		err := WriteSpectrogram(context.Background(), src, filepath.Join(dir, "no-such-dir", "x.png"))
		assert.Error(t, err)
	})
}
