// Package server runs gigg's HTTP listeners: the main one (UI, API, MCP; HTTPS from certificate
// files or plain HTTP behind a proxy) and optional named extra listeners (API and MCP only).
//
// The server never starts processes and never opens outbound connections; `dev/check.sh` fails the
// build if a package under internal/ imports os/exec or creates an HTTP client.
package server

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/0x2alx/gigg/internal/api"
	"github.com/0x2alx/gigg/internal/config"
	"github.com/0x2alx/gigg/internal/store"
	"github.com/0x2alx/gigg/internal/version"
)

// Server is the running HTTP side of gigg.
type Server struct {
	cfg   *config.Config
	store *store.Store
	log   *slog.Logger
	ui    fs.FS // built web UI (index.html, assets/...)
	api   *api.API
	etags sync.Map  // static file path -> ETag
	swept time.Time // the last blob sweep (housekeeping goroutine only)
}

// New builds a server. ui is the embedded web build (web.Dist with "dist" stripped).
func New(cfg *config.Config, st *store.Store, log *slog.Logger, ui fs.FS) *Server {
	return &Server{cfg: cfg, store: st, log: log, ui: ui, api: api.New(cfg, st, log)}
}

// Run serves every configured listener until ctx is cancelled, then shuts them down gracefully.
func (s *Server) Run(ctx context.Context) error {
	var srvs []*http.Server
	errc := make(chan error, len(s.cfg.Listeners))
	for _, l := range s.cfg.Listeners {
		hs := &http.Server{
			Addr:              l.Addr,
			Handler:           s.Handler(l),
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       120 * time.Second,
			MaxHeaderBytes:    64 << 10,
			ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelWarn),
		}
		ln, err := net.Listen("tcp", l.Addr)
		if err != nil {
			return fmt.Errorf("listener %s: %w", l.Name, err)
		}
		if l.TLS() {
			cl := &certLoader{cert: l.TLSCert, key: l.TLSKey}
			if _, err := cl.get(nil); err != nil {
				ln.Close()
				return fmt.Errorf("listener %s: %w", l.Name, err)
			}
			hs.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: cl.get,
				NextProtos: []string{"h2", "http/1.1"}}
			ln = tls.NewListener(ln, hs.TLSConfig)
		}
		hs.RegisterOnShutdown(s.api.StopStreams)
		srvs = append(srvs, hs)
		s.log.Info("listening", "listener", l.Name, "addr", l.Addr, "tls", l.TLS(), "ui", l.UI)
		go func(l config.Listener) {
			if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errc <- fmt.Errorf("listener %s: %w", l.Name, err)
			}
		}(l)
	}
	go s.housekeeping(ctx)
	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-errc:
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, hs := range srvs {
		_ = hs.Shutdown(shutdown)
	}
	return runErr
}

type ctxKey int

const (
	keyListener ctxKey = iota
	keyRequestID
)

// Handler returns the full handler chain for one listener.
func (s *Server) Handler(l config.Listener) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	apiH := s.api.Handler()
	mux.Handle(api.Prefix+"/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := api.WithInfo(r.Context(), api.RequestInfo{Listener: l.Name, RequestID: fmt.Sprint(r.Context().Value(keyRequestID))})
		ctx = api.WithUserAgent(ctx, r.UserAgent())
		apiH.ServeHTTP(w, r.WithContext(ctx))
	}))
	mcpH := s.api.MCPHandler()
	mux.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := api.WithInfo(r.Context(), api.RequestInfo{Listener: l.Name, RequestID: fmt.Sprint(r.Context().Value(keyRequestID))})
		mcpH.ServeHTTP(w, r.WithContext(ctx))
	}))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		problem(w, r, http.StatusNotFound, "not_found", "No such API endpoint.")
	})
	if l.UI {
		mux.Handle("/", s.static())
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			problem(w, r, http.StatusNotFound, "not_found", "The web UI is not served on this listener.")
		})
	}
	var h http.Handler = mux
	h = s.securityHeaders(l, h)
	h = s.hostCheck(h)
	h = s.accessLog(l, h)
	return h
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	out := map[string]any{"status": "ok", "version": version.Version,
		"listener": r.Context().Value(keyListener)}
	code := http.StatusOK
	if err := s.store.Ping(ctx); err != nil {
		out["status"], code = "error", http.StatusServiceUnavailable
		s.log.Error("healthz: database", "err", err)
	} else if v, err := s.store.SchemaVersion(ctx); err == nil {
		out["schema"] = v
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, code, out)
}

// hostCheck refuses requests whose Host isn't this server's (DNS rebinding).
func (s *Server) hostCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.Trim(strings.ToLower(host), "[]")
		if !s.cfg.AllowedHosts[host] {
			problem(w, r, http.StatusMisdirectedRequest, "bad_host", "This server doesn't answer for that host name.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(l config.Listener, next http.Handler) http.Handler {
	styleSrc, fontSrc := "'self'", "'self'"
	if s.cfg.RemoteFonts {
		styleSrc += " https://fonts.googleapis.com"
		fontSrc += " https://fonts.gstatic.com"
	}
	csp := "default-src 'none'; script-src 'self'; style-src " + styleSrc + "; font-src " + fontSrc +
		"; img-src 'self' data: blob:; connect-src 'self'; manifest-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		if l.TLS() {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (sw *statusWriter) WriteHeader(c int) { sw.status = c; sw.ResponseWriter.WriteHeader(c) }
func (sw *statusWriter) Write(b []byte) (int, error) {
	if sw.status == 0 {
		sw.status = http.StatusOK
	}
	n, err := sw.ResponseWriter.Write(b)
	sw.bytes += n
	return n, err
}
func (sw *statusWriter) Unwrap() http.ResponseWriter { return sw.ResponseWriter }
func (sw *statusWriter) Flush() {
	if f, ok := sw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// accessLog tags the request with its listener and an id, recovers panics, and logs one line.
// It never logs headers (Authorization, cookies) or query strings.
func (s *Server) accessLog(l config.Listener, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := newID()
		ctx := context.WithValue(context.WithValue(r.Context(), keyListener, l.Name), keyRequestID, id)
		sw := &statusWriter{ResponseWriter: w}
		sw.Header().Set("X-Request-Id", id)
		defer func() {
			if p := recover(); p != nil {
				s.log.Error("panic", "request_id", id, "panic", fmt.Sprint(p))
				if sw.status == 0 {
					problem(sw, r, http.StatusInternalServerError, "internal", "Internal error.")
				}
			}
			s.log.Info("request", "listener", l.Name, "method", r.Method, "path", r.URL.Path,
				"status", sw.status, "bytes", sw.bytes, "ms", time.Since(start).Milliseconds(), "request_id", id)
		}()
		next.ServeHTTP(sw, r.WithContext(ctx))
	})
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// problem writes an RFC 9457 problem+json error with a stable string code.
func problem(w http.ResponseWriter, r *http.Request, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "about:blank", "title": http.StatusText(status), "status": status,
		"code": code, "detail": detail, "request_id": r.Context().Value(keyRequestID),
	})
}

// certLoader serves the certificate from files and reloads it when either file changes
// (renewals land without a restart). Checks the files at most every 10 s.
type certLoader struct {
	cert, key string
	mu        sync.Mutex
	cur       *tls.Certificate
	stamp     string
	checked   time.Time
}

func (c *certLoader) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cur != nil && time.Since(c.checked) < 10*time.Second {
		return c.cur, nil
	}
	c.checked = time.Now()
	stamp := ""
	for _, p := range []string{c.cert, c.key} {
		fi, err := os.Stat(p)
		if err != nil {
			if c.cur != nil {
				return c.cur, nil // keep serving the last good certificate
			}
			return nil, fmt.Errorf("TLS: %w", err)
		}
		stamp += fmt.Sprintf("%d/%d;", fi.ModTime().UnixNano(), fi.Size())
	}
	if c.cur != nil && stamp == c.stamp {
		return c.cur, nil
	}
	kp, err := tls.LoadX509KeyPair(c.cert, c.key)
	if err != nil {
		if c.cur != nil {
			return c.cur, nil
		}
		return nil, fmt.Errorf("TLS: %w", err)
	}
	c.cur, c.stamp = &kp, stamp
	return c.cur, nil
}

// housekeeping runs the scheduled data work once a minute: recurring gig occurrences, missed
// windows, stale runs and the nightly snapshot. Inside gigg only; no processes, no outbound calls.
func (s *Server) housekeeping(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		s.housekeepOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// housekeepOnce: one pass; a panic is logged and the next minute tries again (it never takes the
// server down).
func (s *Server) housekeepOnce(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("housekeeping panic", "panic", fmt.Sprint(r))
		}
	}()
	if res, err := s.store.Housekeeping(ctx, time.Now().UnixMilli()); err != nil {
		s.log.Error("housekeeping", "err", err)
	} else {
		for _, e := range res.Errors {
			s.log.Error("housekeeping", "err", e)
		}
		if res.Created+res.Skipped+res.Missed+res.Stale+len(res.Paused)+res.Archived > 0 {
			s.log.Info("housekeeping", "created", res.Created, "skipped", res.Skipped, "missed", res.Missed, "paused", res.Paused, "stale_runs", res.Stale,
				"auto_archived", res.Archived)
		}
	}
	s.nightlyBackup(ctx, time.Now())
	s.sweepBlobs(ctx, time.Now())
}

// sweepBlobs removes stored files nothing has used for longer than the snapshot window (plus a
// day), once a day; the first pass runs an hour after start, out of the way of a busy start-up.
func (s *Server) sweepBlobs(ctx context.Context, now time.Time) {
	if s.swept.IsZero() {
		s.swept = now.Add(-23 * time.Hour)
		return
	}
	if now.Sub(s.swept) < 24*time.Hour {
		return
	}
	s.swept = now
	grace := time.Duration(max(s.cfg.BackupKeep, 1)+1) * 24 * time.Hour
	n, err := s.store.SweepBlobs(ctx, grace)
	if err != nil {
		s.log.Error("file sweep", "err", err)
	} else if n > 0 {
		s.log.Info("file sweep", "removed", n)
	}
}

// nightlyBackup writes today's snapshot once the configured time has passed (so a server that was
// down at that time catches up when it starts).
func (s *Server) nightlyBackup(ctx context.Context, now time.Time) {
	if s.cfg.BackupAt == "" || s.cfg.BackupTZ == nil {
		return
	}
	local := now.In(s.cfg.BackupTZ)
	if local.Format("15:04") < s.cfg.BackupAt {
		return
	}
	file, err := s.store.NightlyBackup(ctx, local, s.cfg.BackupKeep)
	if err != nil {
		s.log.Error("nightly backup", "err", err)
	} else if file != "" {
		s.log.Info("nightly backup", "file", file)
	}
}
