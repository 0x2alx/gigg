package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/0x2alx/gigg/internal/store"
)

// attachKey makes scout a key that can attach (the env's runner keys predate the scope).
func attachKey(e *env, name string) cred {
	e.t.Helper()
	body := fmt.Sprintf(`{"name":"attach","user_id":%d,"reach":["%s"],"scopes":["workspace:read","gigs:read","gigs:work","comments:read","attachments:write"],"allowed_listeners":["internal"]}`,
		e.ids[name+".user"], name)
	code, out, _ := e.req("POST", "/keys", body, e.ada())
	e.ok(code, out, 201)
	var kc KeyCreated
	_ = json.Unmarshal(out, &kc)
	return cred{token: kc.Token, listener: "internal"}
}

func upload(e *env, c cred, gig int64, filename string, data []byte, extra string) (int, store.Attachment, []byte) {
	e.t.Helper()
	c.ct = "application/octet-stream"
	q := url.Values{"filename": {filename}}.Encode() + extra
	code, out, _ := e.req("POST", fmt.Sprintf("/gigs/%d/attachments?%s", gig, q), string(data), c)
	var att store.Attachment
	_ = json.Unmarshal(out, &att)
	return code, att, out
}

func pngOf(w, h int, alpha bool) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			a := uint8(255)
			if alpha && x < w/2 {
				a = 0
			}
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 90, a})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func TestAttachments(t *testing.T) {
	e := newEnv(t)
	k := attachKey(e, "scout")
	gig := e.ids["scout.gig"]

	// an image: stored as is, measured, with a preview no larger than 480 px
	code, img, out := upload(e, k, gig, "../../shot.png", pngOf(1200, 600, false), "&label=Board+after")
	e.ok(code, out, 201)
	if img.Kind != "file" || *img.Filename != "shot.png" || *img.Mime != "image/png" || !img.Inline || !img.HasThumb ||
		*img.Width != 1200 || *img.Height != 600 || img.Label != "Board after" {
		t.Fatalf("image: %s", out)
	}
	code, body, hdr := e.req("GET", fmt.Sprintf("/attachments/%d/thumb", img.ID), "", k)
	e.ok(code, body, 200)
	if hdr.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("opaque preview type: %s", hdr.Get("Content-Type"))
	}
	code, body, hdr = e.req("GET", fmt.Sprintf("/attachments/%d/file", img.ID), "", k)
	e.ok(code, body, 200)
	if hdr.Get("Content-Type") != "image/png" || !strings.HasPrefix(hdr.Get("Content-Disposition"), "inline") ||
		!strings.Contains(hdr.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("image headers: %v", hdr)
	}
	tag := hdr.Get("ETag")
	if code, _, _ := e.reqH("GET", fmt.Sprintf("/attachments/%d/file", img.ID), "", k, map[string]string{"If-None-Match": tag}); code != 304 {
		t.Fatalf("etag: %d", code)
	}
	// a preview of a transparent image keeps its transparency (PNG)
	code, clear, out := upload(e, k, gig, "logo.png", pngOf(64, 64, true), "")
	e.ok(code, out, 201)
	_, _, hdr = e.req("GET", fmt.Sprintf("/attachments/%d/thumb", clear.ID), "", k)
	if hdr.Get("Content-Type") != "image/png" {
		t.Fatalf("transparent preview type: %s", hdr.Get("Content-Type"))
	}

	// Markdown is shown (as text, the page renders it); HTML named .png, SVG and HTML are downloads
	code, md, out := upload(e, k, gig, "report.md", []byte("# Done\n\nAll good."), "")
	e.ok(code, out, 201)
	if *md.Mime != "text/markdown" || !md.Inline || md.HasThumb {
		t.Fatalf("markdown: %s", out)
	}
	_, body, hdr = e.req("GET", fmt.Sprintf("/attachments/%d/file", md.ID), "", k)
	if hdr.Get("Content-Type") != "text/plain; charset=utf-8" || string(body) != "# Done\n\nAll good." {
		t.Fatalf("markdown served: %v %q", hdr, body)
	}
	for _, c := range []struct{ name, data, mime string }{
		{"evil.png", "<html><script>alert(1)</script></html>", "text/html"},
		{"pic.svg", `<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`, "image/svg+xml"},
		{"page.html", "hello", "text/html"},
		{"data.bin", "\x00\x01\x02binary", "application/octet-stream"},
	} {
		code, att, out := upload(e, k, gig, c.name, []byte(c.data), "")
		e.ok(code, out, 201)
		if *att.Mime != c.mime || att.Inline {
			t.Fatalf("%s: %s", c.name, out)
		}
		_, _, hdr := e.req("GET", fmt.Sprintf("/attachments/%d/file", att.ID), "", k)
		if hdr.Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(hdr.Get("Content-Disposition"), "attachment") {
			t.Fatalf("%s served inline: %v", c.name, hdr)
		}
	}
	// ?download=1 makes even an image a download
	_, _, hdr = e.req("GET", fmt.Sprintf("/attachments/%d/file?download=true", img.ID), "", k)
	if !strings.HasPrefix(hdr.Get("Content-Disposition"), `attachment; filename=shot.png`) {
		t.Fatalf("download: %v", hdr.Get("Content-Disposition"))
	}

	// links and commits
	code, out, _ = e.req("POST", fmt.Sprintf("/gigs/%d/attachments/link", gig), `{"kind":"link","value":"https://example.com/pr/4","label":"PR"}`, k)
	e.ok(code, out, 201)
	code, out, _ = e.req("POST", fmt.Sprintf("/gigs/%d/attachments/link", gig), `{"kind":"commit","value":"ABC1234DEF"}`, k)
	e.ok(code, out, 201)
	if !strings.Contains(string(out), `"value":"abc1234def"`) {
		t.Fatalf("commit: %s", out)
	}
	for _, bad := range []string{`{"kind":"link","value":"javascript:alert(1)"}`, `{"kind":"commit","value":"main"}`, `{"kind":"link","value":"ftp://x"}`} {
		if code, out, _ := e.req("POST", fmt.Sprintf("/gigs/%d/attachments/link", gig), bad, k); code != 422 {
			t.Fatalf("%s: %d %s", bad, code, out)
		}
	}
	if code, _, out := upload(e, k, gig, "empty.txt", nil, ""); code != 422 && code != 400 {
		t.Fatalf("empty file: %d %s", code, out)
	}

	// the list (with the env's own link), and the gig's count
	code, out, _ = e.req("GET", fmt.Sprintf("/gigs/%d/attachments", gig), "", e.ada())
	e.ok(code, out, 200)
	var list attachmentsOut
	_ = json.Unmarshal(out, &list.Body)
	if len(list.Body.Items) != 10 || list.Body.FileMax != store.DefaultAttachFileMax || list.Body.Used == 0 {
		t.Fatalf("list: %d items, %s", len(list.Body.Items), out)
	}
	_, out, _ = e.req("GET", fmt.Sprintf("/gigs/%d", gig), "", e.ada())
	if !strings.Contains(string(out), `"attachment_count":10`) {
		t.Fatalf("count: %s", out)
	}

	// who may: a viewer sees but can't attach; another workspace sees nothing; a key needs the scope
	bob := e.loginAs("bob", "bob's long password")
	if code, _, _ := e.req("GET", fmt.Sprintf("/attachments/%d/file", img.ID), "", bob); code != 200 {
		t.Fatalf("viewer reads: %d", code)
	}
	bob.ct = "application/octet-stream"
	if code, _, _ := e.reqH("POST", fmt.Sprintf("/gigs/%d/attachments?filename=x.txt", gig), "hi", bob, map[string]string{"X-Gigg-CSRF": "1"}); code != 403 {
		t.Fatalf("viewer attached: %d", code)
	}
	if code, _, _ := e.req("GET", fmt.Sprintf("/attachments/%d/file", img.ID), "", e.key("atlas")); code != 404 {
		t.Fatalf("other workspace: %d", code)
	}
	if code, _, _ := e.req("GET", fmt.Sprintf("/attachments/%d", img.ID), "", e.key("atlas")); code != 404 {
		t.Fatalf("other workspace meta: %d", code)
	}
	if code, _, out := upload(e, e.key("scout"), gig, "x.txt", []byte("hi"), ""); code != 403 || !strings.Contains(string(out), "attachments:write") {
		t.Fatalf("no scope: %d %s", code, out)
	}

	// limits: a smaller file cap and quota (admins only), then over each
	if code, _, _ := e.req("PUT", fmt.Sprintf("/workspaces/%d/attachment-limits", e.ids["scout.ws"]), `{"file_max":100,"quota":null}`, k); code != 403 {
		t.Fatalf("non-admin set limits: %d", code)
	}
	code, out, _ = e.reqH("PUT", fmt.Sprintf("/workspaces/%d/attachment-limits", e.ids["scout.ws"]), `{"file_max":100,"quota":null}`, e.ada(), map[string]string{"X-Gigg-CSRF": "1"})
	e.ok(code, out, 200)
	if code, _, out := upload(e, k, gig, "big.txt", bytes.Repeat([]byte("a"), 101), ""); code != 413 || !strings.Contains(string(out), "100 bytes") {
		t.Fatalf("over the file cap: %d %s", code, out)
	}
	code, out, _ = e.reqH("PUT", fmt.Sprintf("/workspaces/%d/attachment-limits", e.ids["scout.ws"]), fmt.Sprintf(`{"file_max":null,"quota":%d}`, list.Body.Used+10), e.ada(), map[string]string{"X-Gigg-CSRF": "1"})
	e.ok(code, out, 200)
	if code, _, out := upload(e, k, gig, "fits.txt", []byte("0123456789"), ""); code != 201 {
		t.Fatalf("fits the quota: %d %s", code, out)
	}
	if code, _, out := upload(e, k, gig, "full.txt", []byte("x"), ""); code != 413 || !strings.Contains(string(out), "full") {
		t.Fatalf("over the quota: %d %s", code, out)
	}
	if code, _, _ := e.reqH("PUT", fmt.Sprintf("/workspaces/%d/attachment-limits", e.ids["scout.ws"]), `{"file_max":999999999999}`, e.ada(), map[string]string{"X-Gigg-CSRF": "1"}); code != 422 {
		t.Fatalf("file cap over the ceiling: %d", code)
	}

	// removing: your own yes; someone else's needs Edit gigs (scout is a member: has it)
	code, out, _ = e.req("DELETE", fmt.Sprintf("/attachments/%d", md.ID), "", k)
	e.ok(code, out, 204)
	if code, _, _ := e.req("GET", fmt.Sprintf("/attachments/%d", md.ID), "", k); code != 404 {
		t.Fatalf("deleted still there: %d", code)
	}

	// the trash hides them; a restore brings them back
	csrf := map[string]string{"X-Gigg-CSRF": "1"}
	code, out, _ = e.reqH("DELETE", fmt.Sprintf("/gigs/%d", gig), "", e.ada(), csrf)
	e.ok(code, out, 200)
	if code, _, _ := e.req("GET", fmt.Sprintf("/attachments/%d/file", img.ID), "", e.ada()); code != 404 {
		t.Fatalf("trashed gig's file: %d", code)
	}
	_, out, _ = e.req("GET", fmt.Sprintf("/gigs/%d/attachments", gig), "", e.ada())
	if !strings.Contains(string(out), `"items":[]`) {
		t.Fatalf("trashed gig's list: %s", out)
	}
	if code, _, _ := upload(e, k, gig, "late.txt", []byte("x"), ""); code != 409 {
		t.Fatalf("attach to trashed: %d", code)
	}
	code, out, _ = e.reqH("POST", fmt.Sprintf("/gigs/%d/restore", gig), "", e.ada(), csrf)
	e.ok(code, out, 200)
	if code, _, _ := e.req("GET", fmt.Sprintf("/attachments/%d/file", img.ID), "", e.ada()); code != 200 {
		t.Fatalf("restored gig's file: %d", code)
	}
}

// TestAttachmentRunID: a runner's file names its run; a run of another gig is refused.
func TestAttachmentRunID(t *testing.T) {
	e := newEnv(t)
	k := attachKey(e, "scout")
	gig, run := e.ids["scout.gig"], e.ids["scout.run"]
	code, att, out := upload(e, k, gig, "log.txt", []byte("ok"), fmt.Sprintf("&run_id=%d", run))
	e.ok(code, out, 201)
	if att.RunID == nil || *att.RunID != run {
		t.Fatalf("run id: %s", out)
	}
	if code, _, out := upload(e, k, gig, "log.txt", []byte("ok"), fmt.Sprintf("&run_id=%d", e.ids["atlas.run"])); code != 422 {
		t.Fatalf("foreign run: %d %s", code, out)
	}
}

func (e *env) loginAs(user, pw string) cred {
	e.t.Helper()
	code, body, hdr := e.req("POST", "/auth/login", `{"username":"`+user+`","password":"`+pw+`"}`, cred{listener: "main"})
	e.ok(code, body, 200)
	m := regexp.MustCompile(`__Host-gigg_session=([^;]+)`).FindStringSubmatch(hdr.Get("Set-Cookie"))
	return cred{cookie: m[1], listener: "main", origin: "https://board.example"}
}
