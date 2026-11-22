package media

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"testing"

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

func ftyp(brand string) []byte {
	return append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p'}, []byte(brand+"\x00\x00\x00\x00mif1miaf")...)
}

// TestSniffMedia: more image types, video and audio, from their bytes.
func TestSniffMedia(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 30, 20))
	var b, tf bytes.Buffer
	_ = bmp.Encode(&b, img)
	_ = tiff.Encode(&tf, img, nil)
	for _, c := range []struct {
		name string
		data []byte
		mime string
		w    int
	}{
		{"a.bmp", b.Bytes(), "image/bmp", 30},
		{"scan.tif", tf.Bytes(), "image/tiff", 30},
		{"a.avif", ftyp("avif"), "image/avif", 0},
		{"IMG_1.HEIC", ftyp("heic"), "image/heic", 0},
		{"clip.mp4", ftyp("isom"), "video/mp4", 0},
		{"clip.mov", ftyp("qt  "), "video/quicktime", 0},
		{"song.m4a", ftyp("M4A "), "audio/mp4", 0},
		{"clip.webm", []byte{0x1a, 0x45, 0xdf, 0xa3, 0x9f, 0x42, 0x86, 0x81, 0x01}, "video/webm", 0},
		{"a.mp3", []byte("ID3\x04\x00\x00\x00\x00\x00\x00rest"), "audio/mpeg", 0},
		{"a.wav", append([]byte("RIFF\x24\x00\x00\x00WAVEfmt "), make([]byte, 16)...), "audio/wav", 0},
		{"a.ogg", append([]byte("OggS\x00\x02"), make([]byte, 20)...), "audio/ogg", 0},
	} {
		i := Sniff(c.data, c.name)
		if i.Mime != c.mime || i.Width != c.w {
			t.Errorf("%s: %+v, want %s (width %d)", c.name, i, c.mime, c.w)
		}
	}
	// a TIFF can't show in browsers but still gets a preview
	if _, _, err := Thumb(tf.Bytes(), Sniff(tf.Bytes(), "a.tif")); err != nil {
		t.Errorf("tiff preview: %v", err)
	}
}

func TestSniff(t *testing.T) {
	var p bytes.Buffer
	_ = png.Encode(&p, image.NewRGBA(image.Rect(0, 0, 20, 10)))
	var g bytes.Buffer
	_ = gif.Encode(&g, image.NewPaletted(image.Rect(0, 0, 8, 8), []color.Color{color.Black, color.White}), nil)
	for _, c := range []struct {
		name string
		data []byte
		mime string
	}{
		{"shot.png", p.Bytes(), "image/png"},
		{"shot.txt", p.Bytes(), "image/png"}, // the bytes decide, not the name
		{"a.gif", g.Bytes(), "image/gif"},
		{"notes.md", []byte("# hi"), "text/markdown"},
		{"notes.MARKDOWN", []byte("# hi"), "text/markdown"},
		{"log.txt", []byte("line\n"), "text/plain"},
		{"noext", []byte("plain words"), "text/plain"},
		{"x.md", []byte("<!DOCTYPE html><html>"), "text/html"},
		{"x.svg", []byte("<svg/>"), "image/svg+xml"},
		{"x.js", []byte("alert(1)"), "text/javascript"},
		{"doc.pdf", []byte("%PDF-1.7\n\x00\x01"), "application/pdf"},
		{"blob", []byte{0, 1, 2, 3}, "application/octet-stream"},
		{"latin1.txt", []byte{'c', 'a', 'f', 0xe9}, "application/octet-stream"},
	} {
		if got := Sniff(c.data, c.name).Mime; got != c.mime {
			t.Errorf("%s: %s, want %s", c.name, got, c.mime)
		}
	}
	if i := Sniff(p.Bytes(), "x"); i.Width != 20 || i.Height != 10 {
		t.Errorf("size: %+v", i)
	}
}

func TestThumb(t *testing.T) {
	var p bytes.Buffer
	_ = png.Encode(&p, image.NewRGBA(image.Rect(0, 0, 1000, 250))) // transparent
	out, mime, err := Thumb(p.Bytes(), Sniff(p.Bytes(), "a.png"))
	if err != nil || mime != "image/png" {
		t.Fatalf("%v %s", err, mime)
	}
	cfg, _ := png.DecodeConfig(bytes.NewReader(out))
	if cfg.Width != ThumbSide || cfg.Height != 120 {
		t.Fatalf("scaled to %dx%d", cfg.Width, cfg.Height)
	}
	if _, _, err := Thumb(p.Bytes(), Info{Mime: "image/png", Width: 5000, Height: 5000}); err != ErrNoThumb {
		t.Fatalf("too big: %v", err)
	}
}
