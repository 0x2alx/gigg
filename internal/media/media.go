// Package media decides what an uploaded file really is, from its bytes rather than the name or type
// the client claims, and makes the small previews of images.
package media

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"path/filepath"
	"strings"
	"unicode/utf8"

	_ "image/gif" // decoders (registered by import)

	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// Info is what Sniff found.
type Info struct {
	Mime          string // the stored type; store.InlineMime says which are shown in the page
	Width, Height int    // images
}

// Thumbnail limits: images up to MaxPixels get a preview at most ThumbSide pixels on the long side.
// Decoding costs 4 bytes a pixel, so the cap keeps one decode near 64 MiB.
const (
	MaxPixels = 4096 * 4096
	ThumbSide = 480
)

// download-only names: shown as text these would be harmless, but a browser opening the file on its
// own would run them, so they never get an inline type
var activeExt = map[string]string{
	".html": "text/html", ".htm": "text/html", ".xhtml": "application/xhtml+xml", ".svg": "image/svg+xml",
	".svgz": "image/svg+xml", ".xml": "application/xml", ".js": "text/javascript", ".mjs": "text/javascript",
}

// Sniff looks at the bytes (and, for text, the file name's extension) and returns the type to store.
func Sniff(data []byte, filename string) Info {
	ext := strings.ToLower(filepath.Ext(filename))
	if cfg, format, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		switch format {
		case "png", "jpeg", "gif", "webp", "bmp", "tiff":
			return Info{Mime: "image/" + format, Width: cfg.Width, Height: cfg.Height}
		}
	}
	if m := isoMedia(data); m != "" {
		return Info{Mime: m}
	}
	detected := http.DetectContentType(data)
	switch {
	case strings.HasPrefix(detected, "video/webm"):
		return Info{Mime: "video/webm"}
	case strings.HasPrefix(detected, "audio/mpeg"):
		return Info{Mime: "audio/mpeg"}
	case strings.HasPrefix(detected, "audio/wave"):
		return Info{Mime: "audio/wav"}
	case strings.HasPrefix(detected, "application/ogg"):
		return Info{Mime: "audio/ogg"}
	}
	if m, ok := activeExt[ext]; ok {
		return Info{Mime: m}
	}
	if strings.HasPrefix(detected, "text/html") || strings.HasPrefix(detected, "text/xml") {
		return Info{Mime: "text/html"}
	}
	if isText(data) {
		switch ext {
		case ".md", ".markdown", ".mdown", ".mkd":
			return Info{Mime: "text/markdown"}
		}
		return Info{Mime: "text/plain"}
	}
	if i := strings.IndexByte(detected, ';'); i >= 0 {
		detected = detected[:i]
	}
	if detected == "" || strings.HasPrefix(detected, "text/") {
		detected = "application/octet-stream"
	}
	return Info{Mime: detected}
}

// isoMedia reads the brand of an ISO base media file (the "ftyp" box): AVIF and HEIF images, MP4
// video and M4A audio. "" when it is none of those.
func isoMedia(data []byte) string {
	if len(data) < 12 || string(data[4:8]) != "ftyp" {
		return ""
	}
	switch brand := string(data[8:12]); brand {
	case "avif", "avis":
		return "image/avif"
	case "heic", "heix", "hevc", "hevx", "heim", "heis", "mif1", "msf1":
		return "image/heic"
	case "M4A ", "M4B ":
		return "audio/mp4"
	case "qt  ":
		return "video/quicktime"
	case "isom", "iso2", "iso3", "iso4", "iso5", "iso6", "mp41", "mp42", "avc1", "M4V ", "dash", "mmp4", "MSNV", "f4v ":
		return "video/mp4"
	}
	return ""
}

// isText: valid UTF-8 without NUL bytes.
func isText(data []byte) bool {
	return len(data) > 0 && utf8.Valid(data) && bytes.IndexByte(data, 0) < 0
}

// ErrNoThumb: the image is too big to decode for a preview, or not readable.
var ErrNoThumb = errors.New("no preview")

// Thumb makes a preview of an image Sniff accepted: JPEG for opaque images, PNG when there is
// transparency. The first frame of an animated GIF.
func Thumb(data []byte, info Info) ([]byte, string, error) {
	if info.Width < 1 || info.Height < 1 || info.Width*info.Height > MaxPixels {
		return nil, "", ErrNoThumb
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", ErrNoThumb
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > ThumbSide || h > ThumbSide {
		if w >= h {
			h, w = max(1, h*ThumbSide/w), ThumbSide
		} else {
			w, h = max(1, w*ThumbSide/h), ThumbSide
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	var out bytes.Buffer
	if dst.Opaque() {
		if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 82}); err != nil {
			return nil, "", err
		}
		return out.Bytes(), "image/jpeg", nil
	}
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&out, dst); err != nil {
		return nil, "", err
	}
	return out.Bytes(), "image/png", nil
}
