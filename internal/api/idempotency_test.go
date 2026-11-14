package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/0x2alx/gigg/internal/store"
)

// reqH is e.req with extra request headers.
func (e *env) reqH(method, path, body string, c cred, hdr map[string]string) (int, []byte, http.Header) {
	e.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, "https://board.example"+Prefix+path, rd)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		r.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.cookie != "" {
		r.AddCookie(&http.Cookie{Name: "__Host-gigg_session", Value: c.cookie})
	}
	if c.origin != "" {
		r.Header.Set("Origin", c.origin)
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	r = r.WithContext(WithInfo(r.Context(), RequestInfo{Listener: c.listener, RequestID: "test"}))
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w.Code, w.Body.Bytes(), w.Header()
}

func (e *env) commentCount(task int64) int {
	e.t.Helper()
	var n int
	if err := e.st.R.QueryRow(`SELECT COUNT(*) FROM comments WHERE task_id = ?`, task).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func TestIdempotencyReplayAndMismatch(t *testing.T) {
	e := newEnv(t)
	task := e.ids["scout.task"]
	path := fmt.Sprintf("/tasks/%d/comments", task)
	idem := map[string]string{"Idempotency-Key": "retry-1"}
	before := e.commentCount(task)

	code, body1, h1 := e.reqH("POST", path, `{"body_md":"once"}`, e.key("scout"), idem)
	e.ok(code, body1, 201)
	if h1.Get("Idempotent-Replayed") != "" {
		t.Fatal("first request marked as replayed")
	}
	code, body2, h2 := e.reqH("POST", path, `{"body_md":"once"}`, e.key("scout"), idem)
	e.ok(code, body2, 201)
	if h2.Get("Idempotent-Replayed") != "true" || string(body1) != string(body2) {
		t.Fatalf("replay: header %q\n%s\n%s", h2.Get("Idempotent-Replayed"), body1, body2)
	}
	if h2.Get("ETag") != h1.Get("ETag") || !strings.HasPrefix(h2.Get("Content-Type"), "application/json") {
		t.Fatalf("replayed headers: etag %q/%q ct %q", h1.Get("ETag"), h2.Get("ETag"), h2.Get("Content-Type"))
	}
	if n := e.commentCount(task); n != before+1 {
		t.Fatalf("comments %d, want %d (replay must not run the handler)", n, before+1)
	}

	// same key, different body / different path: 422
	code, out, _ := e.reqH("POST", path, `{"body_md":"twice"}`, e.key("scout"), idem)
	if code != 422 || !strings.Contains(string(out), "idempotency_mismatch") {
		t.Fatalf("mismatch: %d %s", code, out)
	}
	code, out, _ = e.reqH("POST", path+"?x=1", `{"body_md":"once"}`, e.key("scout"), idem)
	if code != 422 {
		t.Fatalf("mismatch (query): %d %s", code, out)
	}

	// errors below 500 are replayed too
	bad := map[string]string{"Idempotency-Key": "bad-1"}
	c1, o1, _ := e.reqH("POST", path, `{"body_md":""}`, e.key("scout"), bad)
	c2, o2, h := e.reqH("POST", path, `{"body_md":""}`, e.key("scout"), bad)
	if c1 != 422 || c2 != 422 || string(o1) != string(o2) || h.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("4xx replay: %d %d %q", c1, c2, h.Get("Idempotent-Replayed"))
	}

	// invalid keys
	for _, k := range []string{strings.Repeat("x", 201), "tab\there"} {
		code, out, _ := e.reqH("POST", path, `{"body_md":"x"}`, e.key("scout"), map[string]string{"Idempotency-Key": k})
		if code != 400 {
			t.Fatalf("key %q: %d %s", k, code, out)
		}
	}
	if n := e.commentCount(task); n != before+1 {
		t.Fatalf("comments %d, want %d", n, before+1)
	}
}

func TestIdempotencyNoKeyAndPrincipals(t *testing.T) {
	e := newEnv(t)
	task := e.ids["scout.task"]
	path := fmt.Sprintf("/tasks/%d/comments", task)
	before := e.commentCount(task)
	for i := 0; i < 2; i++ {
		code, out, h := e.req("POST", path, `{"body_md":"plain"}`, e.key("scout"))
		e.ok(code, out, 201)
		if h.Get("Idempotent-Replayed") != "" {
			t.Fatal("replayed without a key")
		}
	}
	// the same key from another principal (ada's session) is a different key
	idem := map[string]string{"Idempotency-Key": "shared"}
	code, out, _ := e.reqH("POST", path, `{"body_md":"plain"}`, e.key("scout"), idem)
	e.ok(code, out, 201)
	code, out, h := e.reqH("POST", path, `{"body_md":"plain"}`, e.ada(), idem)
	e.ok(code, out, 201)
	if h.Get("Idempotent-Replayed") != "" {
		t.Fatal("ada got scout's stored response")
	}
	// GET ignores the header
	code, _, h = e.reqH("GET", path, "", e.key("scout"), idem)
	if code != 200 || h.Get("Idempotent-Replayed") != "" {
		t.Fatalf("GET: %d %q", code, h.Get("Idempotent-Replayed"))
	}
	if n := e.commentCount(task); n != before+4 {
		t.Fatalf("comments %d, want %d", n, before+4)
	}
}

func TestIdempotencyInProgressAndExpiry(t *testing.T) {
	e := newEnv(t)
	task := e.ids["scout.task"]
	path := fmt.Sprintf("/tasks/%d/comments", task)
	lock := fmt.Sprintf("%p", e.a) + "\x00" + fmt.Sprintf("k%d", e.ids["scout.key"]) + "\x00busy"
	idemInflight.Store(lock, struct{}{})
	code, out, _ := e.reqH("POST", path, `{"body_md":"x"}`, e.key("scout"), map[string]string{"Idempotency-Key": "busy"})
	idemInflight.Delete(lock)
	if code != 409 || !strings.Contains(string(out), "idempotency_in_progress") {
		t.Fatalf("in progress: %d %s", code, out)
	}
	code, out, _ = e.reqH("POST", path, `{"body_md":"x"}`, e.key("scout"), map[string]string{"Idempotency-Key": "busy"})
	e.ok(code, out, 201)

	// a row older than 24 h no longer counts, and housekeeping deletes it
	if _, err := e.st.W.Exec(`UPDATE idempotency_keys SET created_at = ? WHERE key = 'busy'`, store.Now()-25*3600*1000); err != nil {
		t.Fatal(err)
	}
	before := e.commentCount(task)
	code, out, h := e.reqH("POST", path, `{"body_md":"other"}`, e.key("scout"), map[string]string{"Idempotency-Key": "busy"})
	e.ok(code, out, 201)
	if h.Get("Idempotent-Replayed") != "" || e.commentCount(task) != before+1 {
		t.Fatal("expired key replayed")
	}
	if _, err := e.st.W.Exec(`UPDATE idempotency_keys SET created_at = ?`, store.Now()-25*3600*1000); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.Housekeeping(context.Background(), store.Now()); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = e.st.R.QueryRow(`SELECT COUNT(*) FROM idempotency_keys`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d idempotency rows left after housekeeping", n)
	}
}
