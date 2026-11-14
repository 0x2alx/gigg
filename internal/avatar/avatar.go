// Package avatar turns an uploaded picture into the only form gigg stores and serves: a 256x256 PNG,
// centre-cropped. Re-encoding drops metadata and anything hidden in the original file.
package avatar

import (
	"bytes"
	"errors"
	"image"
	"image/png"

	_ "image/gif"  // decoders
	_ "image/jpeg" // (registered by import)

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// Limits on uploads, checked before decoding the pixels.
const (
	MaxBytes = 2 << 20
	MaxSide  = 4096
	Size     = 256
)

// Normalize validates and re-encodes an image to a Size x Size PNG.
func Normalize(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > MaxBytes {
		return nil, errors.New("pictures are up to 2 MB")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("not a PNG, JPEG, WebP or GIF image")
	}
	if cfg.Width < 16 || cfg.Height < 16 || cfg.Width > MaxSide || cfg.Height > MaxSide {
		return nil, errors.New("pictures are 16 to 4096 pixels a side")
	}
	_ = format
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("the image couldn't be read")
	}
	b := src.Bounds()
	side := b.Dx()
	if b.Dy() < side {
		side = b.Dy()
	}
	// image.Rect would swap the corners into order (and lose the offset); build the rectangle as is
	at := image.Pt(b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2)
	crop := image.Rectangle{Min: at, Max: at.Add(image.Pt(side, side))}
	dst := image.NewRGBA(image.Rect(0, 0, Size, Size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, crop, draw.Over, nil)
	var out bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, dst); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
