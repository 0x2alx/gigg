package avatar

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// A tall and a wide picture, each with a white square in the middle and coloured bands at the
// ends: the centre crop keeps the white square in the middle and drops the bands.
func TestNormalizeCentreCrops(t *testing.T) {
	for _, sz := range []image.Point{{400, 600}, {600, 400}} {
		src := image.NewRGBA(image.Rect(0, 0, sz.X, sz.Y))
		for y := 0; y < sz.Y; y++ {
			for x := 0; x < sz.X; x++ {
				c := color.RGBA{0, 0, 200, 255}
				if (sz.Y > sz.X && y < 100) || (sz.X > sz.Y && x < 100) {
					c = color.RGBA{200, 0, 0, 255} // band that must be cropped away
				}
				if x > sz.X/2-40 && x < sz.X/2+40 && y > sz.Y/2-40 && y < sz.Y/2+40 {
					c = color.RGBA{255, 255, 255, 255}
				}
				src.Set(x, y, c)
			}
		}
		var in bytes.Buffer
		_ = png.Encode(&in, src)
		out, err := Normalize(in.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(out))
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds().Dx() != Size || img.Bounds().Dy() != Size {
			t.Fatalf("%v: size %v", sz, img.Bounds())
		}
		if r, g, b, _ := img.At(Size/2, Size/2).RGBA(); r>>8 < 240 || g>>8 < 240 || b>>8 < 240 {
			t.Errorf("%v: centre is not the white square", sz)
		}
		if r, _, _, _ := img.At(2, 2).RGBA(); r>>8 > 50 {
			t.Errorf("%v: the cropped-away band is still in the corner", sz)
		}
	}
}
