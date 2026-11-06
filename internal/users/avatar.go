// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"time"

	"golang.org/x/sync/semaphore"
)

// Limits of profile pictures (AUTH-009, decision M1).
const (
	MaxAvatarBytes  = 1 << 20
	maxAvatarPixels = 4096
	avatarSize      = 512
)

// A small file can decode to much: 4096 × 4096 pixels of 16 bits and alpha take
// 128 MiB from 130 KiB of PNG, and any role may upload one (T-025). So what is
// being decoded is bounded by its pixels, not by the size of the file. A
// progressive JPEG needs the most per pixel.
const (
	avatarDecodeBudget  = 128 << 20
	avatarBytesPerPixel = 8
	jpegBytesPerPixel   = 16
	avatarDecodeWait    = 5 * time.Second
)

var avatarDecoding = semaphore.NewWeighted(avatarDecodeBudget)

// ErrBusy means too many pictures are being decoded at the moment.
var ErrBusy = errors.New("too many pictures in work")

// processAvatar checks an upload by its content and returns a re-encoded,
// downscaled image with its media type. Re-encoding drops all metadata
// such as EXIF; only PNG and JPEG are accepted.
func processAvatar(ctx context.Context, data []byte) ([]byte, string, error) {
	if len(data) > MaxAvatarBytes {
		return nil, "", &InvalidError{Detail: "a profile picture has at most 1 MiB"}
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") {
		return nil, "", &InvalidError{Detail: "a profile picture is a PNG or JPEG image"}
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxAvatarPixels || cfg.Height > maxAvatarPixels {
		return nil, "", &InvalidError{Detail: "a profile picture has at most 4096 × 4096 pixels"}
	}
	perPixel := int64(avatarBytesPerPixel)
	if format == "jpeg" {
		perPixel = jpegBytesPerPixel
	}
	weight := min(int64(cfg.Width)*int64(cfg.Height)*perPixel, avatarDecodeBudget)
	wait, cancel := context.WithTimeout(ctx, avatarDecodeWait)
	defer cancel()
	if err := avatarDecoding.Acquire(wait, weight); err != nil {
		return nil, "", ErrBusy
	}
	defer avatarDecoding.Release(weight)
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", &InvalidError{Detail: "the image cannot be read"}
	}
	img = downscale(img, avatarSize)
	var out bytes.Buffer
	if format == "jpeg" {
		err = jpeg.Encode(&out, img, &jpeg.Options{Quality: 85})
		return out.Bytes(), "image/jpeg", err
	}
	err = png.Encode(&out, img)
	return out.Bytes(), "image/png", err
}

// downscale shrinks img to fit into size × size by averaging the source
// pixels of each target pixel; smaller images are copied unchanged.
func downscale(img image.Image, size int) *image.NRGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	tw, th := w, h
	if w > size || h > size {
		if w >= h {
			tw, th = size, max(1, h*size/w)
		} else {
			tw, th = max(1, w*size/h), size
		}
	}
	dst := image.NewNRGBA(image.Rect(0, 0, tw, th))
	for y := range th {
		y0, y1 := b.Min.Y+y*h/th, b.Min.Y+max((y+1)*h/th, y*h/th+1)
		for x := range tw {
			x0, x1 := b.Min.X+x*w/tw, b.Min.X+max((x+1)*w/tw, x*w/tw+1)
			var r, g, bl, a, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					c := color.NRGBAModel.Convert(img.At(sx, sy)).(color.NRGBA)
					r, g, bl, a = r+uint64(c.R), g+uint64(c.G), bl+uint64(c.B), a+uint64(c.A)
					n++
				}
			}
			dst.SetNRGBA(x, y, color.NRGBA{R: uint8(r / n), G: uint8(g / n), B: uint8(bl / n), A: uint8(a / n)}) //nolint:gosec // averages of 8-bit values
		}
	}
	return dst
}
