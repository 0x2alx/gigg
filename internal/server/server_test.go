package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/0x2alx/gigg/internal/config"
	"github.com/0x2alx/gigg/internal/store"
)

func newTestServer(t *testing.T) (*Server, *config.Config) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(context.Background(), t.TempDir(), log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := &config.Config{
		AllowedHosts: map[string]bool{"localhost": true, "board.example": true},
		RemoteFonts:  true,
		Listeners: []config.Listener{
			{Name: "main", Addr: ":8443", UI: true, TLSCert: "/c", TLSKey: "/k"},
			{Name: "internal", Addr: ":8080"},
		},
	}
	ui := fstest.MapFS{
		"index.html":       {Data: []byte("<!doctype html><title>gigg</title>")},
		"assets/app-1.js":  {Data: []byte("console.log(1)")},
		"assets/app-1.css": {Data: []byte("body{}")},
	}
	return New(cfg, st, log, ui), cfg
}

func do(h http.Handler, method, host, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://"+host+path, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestHealthzOnBothListeners(t *testing.T) {
	s, cfg := newTestServer(t)
	for _, l := range cfg.Listeners {
		w := do(s.Handler(l), "GET", "localhost", "/healthz")
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if w.Code != 200 || body["status"] != "ok" || body["listener"] != l.Name || body["schema"] == nil {
			t.Fatalf("%s: %d %s", l.Name, w.Code, w.Body)
		}
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Request-Id") == "" {
			t.Fatalf("%s: headers %v", l.Name, w.Header())
		}
	}
}

func TestHostCheck(t *testing.T) {
	s, cfg := newTestServer(t)
	h := s.Handler(cfg.Listeners[0])
	if w := do(h, "GET", "evil.example", "/healthz"); w.Code != http.StatusMisdirectedRequest ||
		!strings.Contains(w.Body.String(), `"bad_host"`) || w.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("foreign host: %d %s", w.Code, w.Body)
	}
	for _, host := range []string{"board.example:3443", "BOARD.example", "localhost:8443"} {
		if w := do(h, "GET", host, "/healthz"); w.Code != 200 {
			t.Fatalf("%s: %d", host, w.Code)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	s, cfg := newTestServer(t)
	w := do(s.Handler(cfg.Listeners[0]), "GET", "localhost", "/")
	h := w.Header()
	csp := h.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "script-src 'self'", "frame-ancestors 'none'", "https://fonts.gstatic.com", "connect-src 'self'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP lacks %q: %s", want, csp)
		}
	}
	if strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "unsafe-eval") {
		t.Errorf("CSP must not allow inline/eval: %s", csp)
	}
	if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Strict-Transport-Security") == "" || h.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("headers: %v", h)
	}
	// plain-HTTP listener: no HSTS
	if w := do(s.Handler(cfg.Listeners[1]), "GET", "localhost", "/healthz"); w.Header().Get("Strict-Transport-Security") != "" {
		t.Fatal("HSTS on a plain-HTTP listener")
	}
	// fonts off: no Google origins
	s.cfg.RemoteFonts = false
	if csp := do(s.Handler(cfg.Listeners[0]), "GET", "localhost", "/").Header().Get("Content-Security-Policy"); strings.Contains(csp, "google") {
		t.Fatalf("remote fonts off but CSP allows Google: %s", csp)
	}
}

func TestStatic(t *testing.T) {
	s, cfg := newTestServer(t)
	main, internal := s.Handler(cfg.Listeners[0]), s.Handler(cfg.Listeners[1])
	cases := []struct {
		h        http.Handler
		method   string
		path     string
		code     int
		body, cc string
	}{
		{main, "GET", "/", 200, "<title>gigg</title>", "no-cache"},
		{main, "GET", "/w/scout/board", 200, "<title>gigg</title>", "no-cache"}, // client route
		{main, "GET", "/assets/app-1.js", 200, "console.log", "immutable"},
		{main, "GET", "/assets/missing.js", 404, `"not_found"`, ""},
		{main, "GET", "/favicon.ico", 404, `"not_found"`, ""},
		{main, "GET", "/api/v1/nothing", 404, `"not_found"`, ""},
		{main, "GET", "/mcp", 401, `"unauthorized"`, ""}, // MCP needs a key
		{main, "POST", "/", 405, "method_not_allowed", ""},
		{main, "GET", "/a/../../etc/passwd", 307, "", ""}, // the router redirects to the cleaned path
		{internal, "GET", "/", 404, "not served on this listener", ""},
	}
	for _, c := range cases {
		w := do(c.h, c.method, "localhost", c.path)
		if w.Code != c.code || !strings.Contains(w.Body.String(), c.body) || !strings.Contains(w.Header().Get("Cache-Control"), c.cc) {
			t.Errorf("%s %s: %d cc=%q body=%.80q", c.method, c.path, w.Code, w.Header().Get("Cache-Control"), w.Body.String())
		}
	}
	if ct := do(main, "GET", "localhost", "/assets/app-1.css").Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("css content type %q", ct)
	}
}

func TestPanicIsContained(t *testing.T) {
	s, cfg := newTestServer(t)
	h := s.accessLog(cfg.Listeners[0], http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	w := do(h, "GET", "localhost", "/x")
	if w.Code != 500 || !strings.Contains(w.Body.String(), `"internal"`) || strings.Contains(w.Body.String(), "boom") {
		t.Fatalf("panic response: %d %s", w.Code, w.Body)
	}
}
