package server

import (
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
)

// static serves the embedded web build. Hashed files under /assets/ are cached forever;
// index.html is never cached. Unknown paths that look like page loads get index.html (the UI
// routes on the client); API and MCP paths never do.
func (s *Server) static() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			problem(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET.")
			return
		}
		p := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if p != "" && p != "index.html" {
			if s.serveFile(w, r, p) {
				return
			}
			if strings.HasPrefix(p, "assets/") || strings.HasPrefix(p, "mcp") || path.Ext(p) != "" {
				problem(w, r, http.StatusNotFound, "not_found", "Not found.")
				return
			}
		}
		if !s.serveFile(w, r, "index.html") {
			problem(w, r, http.StatusServiceUnavailable, "ui_missing", "This build has no web UI.")
		}
	})
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, p string) bool {
	f, err := s.ui.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.IsDir() {
		return false
	}
	if strings.HasPrefix(p, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	ct := mime.TypeByExtension(path.Ext(p))
	if extra, ok := extraTypes[path.Ext(p)]; ok && ct == "" {
		ct = extra
	}
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, "", fi.ModTime(), rs)
		return true
	}
	if r.Method == http.MethodGet {
		_, _ = io.Copy(w, f)
	}
	return true
}

// extraTypes covers what Go's built-in table lacks (the image has no /etc/mime.types).
var extraTypes = map[string]string{
	".ico":         "image/x-icon",
	".webmanifest": "application/manifest+json",
}
