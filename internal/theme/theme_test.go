package theme

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseColour(t *testing.T) {
	for in, want := range map[string]string{
		`"#abc"`: "#AABBCC", `"abcd"`: "#AABBCC", `"#1a2b3c"`: "#1A2B3C", `"FF1A2B3C"`: "#1A2B3C", `"#80FFFFFF"`: "#FFFFFF",
		`4280034592`: "#1C2520", // 0xFF1C2520 as an ARGB integer
	} {
		var v any
		d := json.NewDecoder(strings.NewReader(in))
		d.UseNumber()
		_ = d.Decode(&v)
		got, ok := ParseColour(v)
		if !ok || got != want {
			t.Errorf("%s -> %q %v, want %s", in, got, ok, want)
		}
	}
	for _, bad := range []any{"blue-ish", "#12345", "", nil, true, -1.0} {
		if _, ok := ParseColour(bad); ok {
			t.Errorf("%v parsed", bad)
		}
	}
}

// The real app exports must survive Sane unchanged (byte-for-byte after re-indenting).
func TestRoundTripAppExports(t *testing.T) {
	files, _ := filepath.Glob("../../testdata/themes/*.json")
	if len(files) < 4 {
		t.Fatalf("fixtures: %v", files)
	}
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		out, err := Sane(raw)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		var a, b bytes.Buffer
		_ = json.Indent(&a, out, "", "    ")
		_ = json.Compact(&b, raw)
		var c bytes.Buffer
		_ = json.Indent(&c, b.Bytes(), "", "    ")
		if a.String() != c.String() {
			t.Errorf("%s changed:\n%s\nvs\n%s", f, a.String(), c.String())
		}
	}
}

func TestSaneClampsAndDrops(t *testing.T) {
	in := `{"name":"  ` + strings.Repeat("x", 50) + `  ","dark":true,"background":"#000","surface":"#111","surfaceVariant":"#222",
	"border":"#333","accent":"#444","onAccent":"#555","text":"#666","mutedText":"#777","danger":"#888","secondary":null,
	"font":"Roboto Slab","fontWeight":"Heavy","textScalePct":500,"cornerDp":-3,"borderDp":2.6,"evil":"<script>",
	"widget":{"opacityPct":150,"font":null}}`
	out, err := Sane([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{`"name":"` + strings.Repeat("x", 40) + `"`, `"fontWeight":"Regular"`, `"textScalePct":130`, `"cornerDp":0`, `"borderDp":3`, `"opacityPct":100`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in %s", want, s)
		}
	}
	if strings.Contains(s, "evil") || strings.Contains(s, "secondary") {
		t.Errorf("kept unknown key or null secondary: %s", s)
	}
	for _, bad := range []string{`{"dark":true}`, `[]`, `{"name":"x","dark":true,"background":"red"}`,
		strings.Replace(in, `"Roboto Slab"`, `"x;}body{"`, 1)} {
		if _, err := Sane([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	if ValidID("nightwalker") || ValidID("Graphite") || ValidID("a/b") || !ValidID("custom-1731400000") {
		t.Error("id rules")
	}
}

// TestSaneExtras: the gigg-only keys (control shape, board column colours) survive when valid and
// are dropped when not, so the apps' exports stay unchanged and junk can't reach the CSS.
func TestSaneExtras(t *testing.T) {
	base := `{"name":"X","dark":true,"background":"#000000","surface":"#111111","surfaceVariant":"#222222",
	"border":"#333333","accent":"#444444","onAccent":"#555555","text":"#666666","mutedText":"#777777","danger":"#888888",
	"font":"Inter","fontWeight":"Medium","textScalePct":90,"cornerDp":8,"borderDp":1`
	out, err := Sane([]byte(base + `,"controlShape":"pill","laneColor":"#abcdef","laneBorder":"#123456","logoColor":"#ff8800"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"controlShape":"pill"`, `"laneColor":"#ABCDEF"`, `"laneBorder":"#123456"`, `"logoColor":"#FF8800"`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("missing %s in %s", want, out)
		}
	}
	out, err = Sane([]byte(base + `,"controlShape":"blob","laneColor":"red;}","laneBorder":null}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "controlShape") || strings.Contains(string(out), "lane") {
		t.Errorf("kept invalid extras: %s", out)
	}
}
