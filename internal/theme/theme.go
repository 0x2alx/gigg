// Package theme sanitises themes in gigg's theme JSON format (the format the web UI imports and
// exports) before they are stored. Themes arrive through the API and MCP, so the server applies the
// format's rules itself: colours parsed and re-emitted as #RRGGBB (opaque), numbers clamped, names
// trimmed, fonts validated, unknown keys dropped, `widget` kept as given (inert JSON).
package theme

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Builtin ids are reserved: a custom theme can't use them.
var Builtin = map[string]bool{"clumsy-clanker": true, "nightwalker": true, "daywalker": true, "petty-officer": true, "high-contrast": true, "neon": true, "carbon": true}

// Retired ids map to current ones.
var Retired = map[string]string{"graphite": "nightwalker", "daylight": "daywalker", "signature": "clumsy-clanker"}

var idRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)
var fontRe = regexp.MustCompile(`^[A-Za-z0-9 ]{1,60}$`)

// ValidID reports a usable custom theme id.
func ValidID(id string) bool {
	return idRe.MatchString(id) && !Builtin[strings.ToLower(id)] && Retired[strings.ToLower(id)] == ""
}

var colourKeys = []string{"background", "surface", "surfaceVariant", "border", "accent", "onAccent", "text", "mutedText", "danger"}

// ParseColour accepts #RGB, #RGBA, #RRGGBB, #AARRGGBB (alpha first), with or without '#', any case,
// or a bare ARGB integer; it returns #RRGGBB (upper case, alpha dropped: themes are opaque).
func ParseColour(v any) (string, bool) {
	switch c := v.(type) {
	case float64:
		if c < 0 || c > 0xFFFFFFFF || c != float64(int64(c)) {
			return "", false
		}
		return fmt.Sprintf("#%06X", int64(c)&0xFFFFFF), true
	case json.Number:
		n, err := strconv.ParseInt(string(c), 10, 64)
		if err != nil || n < 0 || n > 0xFFFFFFFF {
			return "", false
		}
		return fmt.Sprintf("#%06X", n&0xFFFFFF), true
	case string:
		h := strings.TrimPrefix(strings.TrimSpace(c), "#")
		if !regexp.MustCompile(`^[0-9A-Fa-f]+$`).MatchString(h) {
			return "", false
		}
		h = strings.ToUpper(h)
		switch len(h) {
		case 3:
			return "#" + string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]}), true
		case 4: // #RGBA
			return "#" + string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]}), true
		case 6:
			return "#" + h, true
		case 8: // #AARRGGBB
			return "#" + h[2:], true
		}
	}
	return "", false
}

var weights = map[string]bool{"Light": true, "Regular": true, "Medium": true, "Bold": true}

// Sane validates and normalises a complete theme object (as the UI sends it after its own import
// merge). It returns canonical JSON in the export key order.
func Sane(raw []byte) ([]byte, error) {
	if len(raw) > 32*1024 {
		return nil, errors.New("theme too large")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, errors.New("a theme is a JSON object")
	}
	var b bytes.Buffer
	b.WriteString("{")
	sep := func() {
		if b.Len() > 1 {
			b.WriteString(",")
		}
	}
	put := func(k string, v any) {
		sep()
		kb, _ := json.Marshal(k)
		vb, _ := json.Marshal(v)
		b.Write(kb)
		b.WriteString(":")
		b.Write(vb)
	}
	name, _ := m["name"].(string)
	name = strings.TrimSpace(name)
	if r := []rune(name); len(r) > 40 {
		name = string(r[:40])
	}
	if name == "" {
		name = "My theme"
	}
	put("name", name)
	dark, ok := m["dark"].(bool)
	if !ok {
		return nil, errors.New("dark must be true or false")
	}
	put("dark", dark)
	for _, k := range colourKeys {
		c, ok := ParseColour(m[k])
		if !ok {
			return nil, fmt.Errorf("%s isn't a colour", k)
		}
		put(k, c)
	}
	if v, present := m["secondary"]; present && v != nil {
		c, ok := ParseColour(v)
		if !ok {
			return nil, errors.New("secondary isn't a colour")
		}
		put("secondary", c)
	}
	font, _ := m["font"].(string)
	if !fontRe.MatchString(font) {
		return nil, errors.New("font: 1-60 letters, digits and spaces")
	}
	put("font", font)
	fw, _ := m["fontWeight"].(string)
	if !weights[fw] {
		fw = "Regular"
	}
	put("fontWeight", fw)
	put("textScalePct", clamp(m["textScalePct"], 85, 130, 100))
	put("cornerDp", clamp(m["cornerDp"], 0, 28, 12))
	put("borderDp", clamp(m["borderDp"], 0, 3, 1))
	// widget: kept byte-for-byte (key order included) so it round-trips to the apps; rebuilt only
	// when its opacity needs clamping.
	var rawm map[string]json.RawMessage
	_ = json.Unmarshal(raw, &rawm)
	if wr, ok := rawm["widget"]; ok && len(wr) > 0 && wr[0] == '{' {
		if w, ok := m["widget"].(map[string]any); ok {
			if op, ok := w["opacityPct"]; ok && op != nil && clamp(op, 0, 100, 100) != clamp(op, -1<<31, 1<<31, 100) {
				w["opacityPct"] = clamp(op, 0, 100, 100)
				put("widget", w)
			} else {
				var c bytes.Buffer
				if json.Compact(&c, wr) == nil {
					sep()
					b.WriteString(`"widget":`)
					b.Write(c.Bytes())
				}
			}
		}
	}
	b.WriteString("}")
	return b.Bytes(), nil
}

func clamp(v any, lo, hi, def int64) int64 {
	var n int64
	switch x := v.(type) {
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return def
		}
		n = int64(f + 0.5)
	case float64:
		n = int64(x + 0.5)
	default:
		return def
	}
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}
