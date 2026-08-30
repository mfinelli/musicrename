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

// Package artwork resizes primary album art for a sync target.
package artwork

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png" // registers the PNG decoder with image.Decode
	"os"
	"path/filepath"

	xdraw "golang.org/x/image/draw"
)

// jpegQuality is the fixed encode quality used for all resized artwork.
// Dimension is the controlling constraint; output file size is whatever
// falls out of dimension and this quality setting, not an independent target.
const jpegQuality = 85

// Resize decodes the image in src JPEG or PNG (whichever primary artwork
// format is actually present), scales it down to fit within
// maxDimension x maxDimension pixels while preserving aspect ratio, and
// re-encodes it as JPEG at a fixed quality.
//
// An image already within maxDimension in both dimensions is never
// upscaled (its existing dimensions are kept). If it's also already a
// JPEG, the original bytes are returned completely unchanged rather than
// decoding and re-encoding. PNG source is still converted even when
// already within bounds, since that's a genuine one-time format
// conversion this project's artwork handling requires (Rockbox/embedding
// support), not a resize with no reason to touch the bytes so the same
// logic doesn't apply to it. However an unusually large (in bytes, not
// dimensions) already-small JPEG is copied at its full size rather than
// getting a smaller file out of the quality-85 re-encode it would otherwise
// have gotten.
func Resize(src []byte, maxDimension int) ([]byte, error) {
	if maxDimension <= 0 {
		return nil, fmt.Errorf("maxDimension must be positive, got %d", maxDimension)
	}

	img, format, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("decoding image: %w", err)
	}

	bounds := img.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()
	dstW, dstH := fitDimensions(srcW, srcH, maxDimension)

	if format == "jpeg" && dstW == srcW && dstH == srcH {
		out := make([]byte, len(src))
		copy(out, src)
		return out, nil
	}

	// A JPEG-encoded destination has no alpha channel; flatten onto white
	// first so a source PNG with transparency doesn't end up with
	// black (the zero value) where it used to be transparent.
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, xdraw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, fmt.Errorf("encoding jpeg: %w", err)
	}
	return buf.Bytes(), nil
}

// ResizeFile reads the image at srcPath, resizes it per [Resize], and
// writes the result to dstPath, creating dstPath's parent directory if
// necessary and overwriting dstPath if it already exists.
func ResizeFile(srcPath, dstPath string, maxDimension int) error {
	src, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", srcPath, err)
	}

	out, err := Resize(src, maxDimension)
	if err != nil {
		return fmt.Errorf("resizing %s: %w", srcPath, err)
	}

	dir := filepath.Dir(dstPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	if err := os.WriteFile(dstPath, out, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", dstPath, err)
	}
	return nil
}

// fitDimensions returns the largest (w, h) no larger than max in either
// dimension that preserves the srcW:srcH aspect ratio, without ever
// upscaling (if srcW and srcH already both fit within max, they are
// returned unchanged).
func fitDimensions(srcW, srcH, max int) (w, h int) {
	if srcW <= max && srcH <= max {
		return srcW, srcH
	}

	if srcW >= srcH {
		w = max
		h = srcH * max / srcW
	} else {
		h = max
		w = srcW * max / srcH
	}
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return w, h
}
