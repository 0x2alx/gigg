package media

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"testing"
)

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
