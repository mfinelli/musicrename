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

package artwork

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeTestImage returns w x h image bytes, solid red, encoded as either
// "jpeg" or "png".
func makeTestImage(t *testing.T, w, h int, format string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{R: 255, A: 255}}, image.Point{}, draw.Src)

	var buf bytes.Buffer
	switch format {
	case "jpeg":
		require.NoError(t, jpeg.Encode(&buf, img, nil))
	case "png":
		require.NoError(t, png.Encode(&buf, img))
	default:
		t.Fatalf("unknown format %q", format)
	}
	return buf.Bytes()
}

func decodeDimensions(t *testing.T, data []byte) (w, h int) {
	t.Helper()
	img, format, err := image.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, "jpeg", format, "output must always be JPEG regardless of source format")
	b := img.Bounds()
	return b.Dx(), b.Dy()
}

func TestFitDimensions(t *testing.T) {
	t.Run("landscape image wider than max", func(t *testing.T) {
		w, h := fitDimensions(1000, 500, 400)
		assert.Equal(t, 400, w)
		assert.Equal(t, 200, h)
	})

	t.Run("portrait image taller than max", func(t *testing.T) {
		w, h := fitDimensions(500, 1000, 400)
		assert.Equal(t, 200, w)
		assert.Equal(t, 400, h)
	})

	t.Run("square image larger than max", func(t *testing.T) {
		w, h := fitDimensions(800, 800, 400)
		assert.Equal(t, 400, w)
		assert.Equal(t, 400, h)
	})

	t.Run("already within bounds: unchanged, never upscaled", func(t *testing.T) {
		w, h := fitDimensions(200, 100, 400)
		assert.Equal(t, 200, w)
		assert.Equal(t, 100, h)
	})

	t.Run("exactly at the bound", func(t *testing.T) {
		w, h := fitDimensions(400, 400, 400)
		assert.Equal(t, 400, w)
		assert.Equal(t, 400, h)
	})

	t.Run("extremely thin image never rounds down to zero", func(t *testing.T) {
		w, h := fitDimensions(10000, 1, 400)
		assert.Equal(t, 400, w)
		assert.GreaterOrEqual(t, h, 1)
	})
}

func TestResize(t *testing.T) {
	t.Run("scales a large landscape jpeg down, preserving aspect ratio", func(t *testing.T) {
		src := makeTestImage(t, 1000, 500, "jpeg")
		out, err := Resize(src, 400)
		require.NoError(t, err)

		w, h := decodeDimensions(t, out)
		assert.Equal(t, 400, w)
		assert.Equal(t, 200, h)
	})

	t.Run("scales a large portrait png down, preserving aspect ratio", func(t *testing.T) {
		src := makeTestImage(t, 300, 900, "png")
		out, err := Resize(src, 400)
		require.NoError(t, err)

		w, h := decodeDimensions(t, out)
		assert.Equal(t, 133, w) // 300 * 400 / 900, integer division
		assert.Equal(t, 400, h)
	})

	t.Run("a png already within bounds is not upscaled but is still converted to jpeg", func(t *testing.T) {
		src := makeTestImage(t, 100, 100, "png")
		out, err := Resize(src, 400)
		require.NoError(t, err)

		w, h := decodeDimensions(t, out)
		assert.Equal(t, 100, w)
		assert.Equal(t, 100, h)
	})

	t.Run("a jpeg already within bounds is returned completely unchanged, not re-encoded", func(t *testing.T) {
		src := makeTestImage(t, 100, 100, "jpeg")
		out, err := Resize(src, 400)
		require.NoError(t, err)

		assert.Equal(t, src, out, "an already-fitting JPEG source must never be lossily re-encoded")
	})

	t.Run("a jpeg exceeding bounds is still resized and re-encoded, not passed through", func(t *testing.T) {
		src := makeTestImage(t, 1000, 500, "jpeg")
		out, err := Resize(src, 400)
		require.NoError(t, err)

		assert.NotEqual(t, src, out, "a genuine resize must still touch the bytes")
		w, h := decodeDimensions(t, out)
		assert.Equal(t, 400, w)
		assert.Equal(t, 200, h)
	})

	t.Run("errors on invalid image data", func(t *testing.T) {
		_, err := Resize([]byte("not an image"), 400)
		assert.Error(t, err)
	})

	t.Run("errors on a non-positive maxDimension", func(t *testing.T) {
		src := makeTestImage(t, 100, 100, "jpeg")
		_, err := Resize(src, 0)
		assert.Error(t, err)
		_, err = Resize(src, -1)
		assert.Error(t, err)
	})
}

func TestResizeFile(t *testing.T) {
	t.Run("reads, resizes, and writes, creating parent directories", func(t *testing.T) {
		dir := t.TempDir()
		srcPath := filepath.Join(dir, "folder.png")
		require.NoError(t, os.WriteFile(srcPath, makeTestImage(t, 1000, 500, "png"), 0644))

		dstPath := filepath.Join(dir, "nested", "sub", "ipod.jpg")
		require.NoError(t, ResizeFile(srcPath, dstPath, 400))

		out, err := os.ReadFile(dstPath)
		require.NoError(t, err)
		w, h := decodeDimensions(t, out)
		assert.Equal(t, 400, w)
		assert.Equal(t, 200, h)
	})

	t.Run("overwrites an existing destination", func(t *testing.T) {
		dir := t.TempDir()
		srcPath := filepath.Join(dir, "folder.jpg")
		require.NoError(t, os.WriteFile(srcPath, makeTestImage(t, 100, 100, "jpeg"), 0644))

		dstPath := filepath.Join(dir, "ipod.jpg")
		require.NoError(t, os.WriteFile(dstPath, []byte("stale content"), 0644))

		require.NoError(t, ResizeFile(srcPath, dstPath, 400))

		out, err := os.ReadFile(dstPath)
		require.NoError(t, err)
		assert.NotEqual(t, "stale content", string(out))
	})

	t.Run("errors when the source file does not exist", func(t *testing.T) {
		dir := t.TempDir()
		err := ResizeFile(filepath.Join(dir, "missing.jpg"), filepath.Join(dir, "out.jpg"), 400)
		assert.Error(t, err)
	})
}
