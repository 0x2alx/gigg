// Package api is gigg's HTTP API (/api/v1), built with huma so the OpenAPI document is generated
// from the same Go types the handlers use. The UI is a client of this API and nothing more.
package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/0x2alx/gigg/internal/access"
	"github.com/0x2alx/gigg/internal/auth"
	"github.com/0x2alx/gigg/internal/config"
	"github.com/0x2alx/gigg/internal/store"
	"github.com/0x2alx/gigg/internal/version"
)

// API wires handlers to the store.
type API struct {
	cfg     *config.Config
	st      *store.Store
	log     *slog.Logger
	h       huma.API
	mux     *http.ServeMux
	limiter *limiter
	// hashSlots bounds concurrent argon2id work (64 MiB each).
	hashSlots chan struct{}
}

// Prefix is where the API lives.
const Prefix = "/api/v1"

// New builds the API handler (mounted at /api/v1 by the server).
func New(cfg *config.Config, st *store.Store, log *slog.Logger) *API {
	huma.NewError = newProblem
	huma.NewErrorWithContext = func(_ huma.Context, status int, msg string, errs ...error) huma.StatusError {
		return newProblem(status, msg, errs...)
	}
	mux := http.NewServeMux()
	hc := huma.DefaultConfig("gigg", version.Version)
	hc.Info.Description = "gigg gig board API. Bearer keys or a browser session. Errors are RFC 9457 problem+json with a stable `code`. POSTs may carry `Idempotency-Key` (1-200 printable chars): a repeat within 24 h replays the first response (`Idempotent-Replayed: true`)."
	hc.OpenAPIPath = "/openapi"
	hc.DocsPath = ""
	hc.SchemasPath = ""
	hc.Servers = []*huma.Server{{URL: Prefix}}
	hc.CreateHooks = nil // no $schema links in responses
	hc.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearer":  {Type: "http", Scheme: "bearer", Description: "gigg_<prefix>_<secret> API key"},
		"session": {Type: "apiKey", In: "cookie", Name: "__Host-gigg_session"},
	}
	hc.Security = []map[string][]string{{"bearer": {}}, {"session": {}}}
	a := &API{cfg: cfg, st: st, log: log, mux: mux, limiter: newLimiter(), hashSlots: make(chan struct{}, 2)}
	a.h = humago.New(mux, hc)
	a.registerAuth()
	a.registerUsers()
	a.registerWorkspaces()
	a.registerGigs()
	a.registerRuns()
	a.registerProfile()
	a.registerRecurrence()
	a.registerSchedules()
	a.registerGroups()
	a.registerItems()
	a.registerFeed()
	a.registerRunner()
	a.registerErrands()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := fail(404, "not_found", "No such API endpoint.")
		writeProblem(w, p)
	})
	return a
}

// Handler is the API with authentication in front.
func (a *API) Handler() http.Handler {
	return http.StripPrefix(Prefix, a.authenticate(a.idempotent(a.mux)))
}

// Huma exposes the huma API (OpenAPI document, tests).
func (a *API) Huma() huma.API { return a.h }

// ---------------------------------------------------------------- errors

// Problem is an RFC 9457 error with a stable code.
type Problem struct {
	Type      string        `json:"type"`
	Title     string        `json:"title"`
	Status    int           `json:"status"`
	Code      string        `json:"code"`
	Detail    string        `json:"detail,omitempty"`
	Errors    []ProblemItem `json:"errors,omitempty"`
	RequestID string        `json:"request_id,omitempty"`
	Current   any           `json:"current,omitempty"`
}

// ProblemItem is one validation detail.
type ProblemItem struct {
	Message  string `json:"message"`
	Location string `json:"location,omitempty"`
}

func (p *Problem) Error() string             { return p.Detail }
func (p *Problem) GetStatus() int            { return p.Status }
func (p *Problem) ContentType(string) string { return "application/problem+json" }

var codeFor = map[int]string{400: "bad_request", 401: "unauthorized", 403: "forbidden", 404: "not_found", 405: "method_not_allowed",
	409: "conflict", 412: "version_conflict", 413: "too_large", 415: "unsupported_media_type", 422: "invalid", 428: "precondition_required",
	429: "rate_limited", 500: "internal"}

// statusClientClosed: the client went away before the answer (nginx's 499). Nobody reads the
// response; the status keeps a cancelled request from looking like a server failure in the logs.
const statusClientClosed = 499

func newProblem(status int, msg string, errs ...error) huma.StatusError {
	if status == http.StatusInternalServerError {
		for _, e := range errs {
			if errors.Is(e, context.Canceled) {
				return fail(statusClientClosed, "client_closed", "The request was cancelled.")
			}
		}
	}
	p := &Problem{Type: "about:blank", Title: http.StatusText(status), Status: status, Code: codeFor[status], Detail: msg}
	if p.Code == "" {
		p.Code = "error"
	}
	for _, e := range errs {
		if e == nil {
			continue
		}
		item := ProblemItem{Message: e.Error()}
		if d, ok := e.(huma.ErrorDetailer); ok {
			if dd := d.ErrorDetail(); dd != nil {
				item = ProblemItem{Message: dd.Message, Location: dd.Location}
			}
		}
		p.Errors = append(p.Errors, item)
	}
	return p
}

func fail(status int, code, msg string) *Problem {
	return &Problem{Type: "about:blank", Title: http.StatusText(status), Status: status, Code: code, Detail: msg}
}

var (
	errNotFound  = func() error { return fail(404, "not_found", "Not found.") }
	errForbidden = func(why string) error { return fail(403, "forbidden", why) }
)

// storeErr maps store errors to problems.
func storeErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrNotFound):
		return errNotFound()
	case errors.Is(err, store.ErrVersion):
		return fail(412, "version_conflict", "The resource changed since you read it; reload and retry.")
	case errors.Is(err, store.ErrInvalid):
		return fail(422, "invalid", strings.TrimPrefix(err.Error(), "invalid: "))
	case errors.Is(err, store.ErrConflict):
		return fail(409, "conflict", strings.TrimPrefix(err.Error(), "conflict: "))
	case errors.Is(err, store.ErrDenied):
		return fail(403, "forbidden", strings.TrimPrefix(err.Error(), "forbidden: "))
	case errors.Is(err, store.ErrPaused):
		return fail(423, "paused", "Gig runs are paused for this workspace; an admin resumes them from the dashboard.")
	}
	var p *Problem
	if errors.As(err, &p) {
		return p
	}
	return err
}

// ---------------------------------------------------------------- authentication

// Cookie names: __Host- needs HTTPS.
const (
	cookieSecure = "__Host-gigg_session"
	cookiePlain  = "gigg_session"
)

func (a *API) cookieName() string {
	if a.cfg.PublicURL != nil && a.cfg.PublicURL.Scheme == "https" {
		return cookieSecure
	}
	if a.cfg.PublicURL == nil && a.listener("main").TLS() {
		return cookieSecure // no public URL, but the browser reaches us over HTTPS
	}
	return cookiePlain
}

func (a *API) listener(name string) config.Listener {
	for _, l := range a.cfg.Listeners {
		if l.Name == name {
			return l
		}
	}
	return config.Listener{}
}

// requestScheme: how the browser reached us: https when gigg terminates TLS, else what a trusted proxy
// says in X-Forwarded-Proto, else http.
func (a *API) requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if peer, err := netip.ParseAddr(host); err == nil {
		for _, p := range a.cfg.TrustedProxies {
			if p.Contains(peer.Unmap()) {
				if v := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])); v == "https" || v == "http" {
					return v
				}
				break
			}
		}
	}
	return "http"
}

// clientIP: the peer, or the rightmost X-Forwarded-For hop that isn't a trusted proxy.
func (a *API) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	trusted := func(ip netip.Addr) bool {
		for _, p := range a.cfg.TrustedProxies {
			if p.Contains(ip.Unmap()) {
				return true
			}
		}
		return false
	}
	if !trusted(peer) {
		return peer.Unmap().String()
	}
	// Every X-Forwarded-For line counts: proxies may append a separate header line.
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		ip, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		if !trusted(ip) {
			return ip.Unmap().String()
		}
	}
	return peer.Unmap().String()
}

// RequestInfo is set by the server before the API runs.
type RequestInfo struct {
	Listener  string
	RequestID string
}

type infoKey struct{}

// WithInfo lets the server pass listener and request id down.
func WithInfo(ctx context.Context, i RequestInfo) context.Context {
	return context.WithValue(ctx, infoKey{}, i)
}

// whyUnavailable: keyPrincipal's reason when the database, not the key, failed.
const whyUnavailable = "unavailable"

// unavailable answers 503 with Retry-After for a lookup that failed in the database.
func unavailable(w http.ResponseWriter, requestID string) {
	p := fail(503, "unavailable", "The server couldn't check your login just now; try again.")
	p.RequestID = requestID
	w.Header().Set("Retry-After", "1")
	writeProblem(w, p)
}

func writeProblem(w http.ResponseWriter, p *Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// authenticate resolves the caller (key or session) and enforces key restrictions, CSRF rules and
// rate limits. Anonymous requests continue without a principal; handlers that need one refuse.
func (a *API) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, _ := r.Context().Value(infoKey{}).(RequestInfo)
		req := access.Request{Listener: info.Listener, IP: a.clientIP(r), RequestID: info.RequestID}
		ctx := access.WithRequest(r.Context(), req)
		w.Header().Set("Cache-Control", "no-store")
		unauth := func(msg string) {
			p := fail(401, "unauthorized", msg)
			p.RequestID = req.RequestID
			writeProblem(w, p)
		}

		if h := r.Header.Get("Authorization"); h != "" {
			tok, ok := strings.CutPrefix(h, "Bearer ")
			if !ok {
				unauth("Use Authorization: Bearer <key>.")
				return
			}
			// an address that keeps sending bad keys gets 429 before any lookup (and stops filling the log)
			if a.limiter.full("badkey:"+req.IP, badKeyMax) {
				writeProblem(w, fail(429, "rate_limited", "Too many requests with invalid keys; slow down."))
				return
			}
			p, why := a.keyPrincipal(ctx, tok, req)
			if why == whyUnavailable && r.Context().Err() != nil {
				w.WriteHeader(statusClientClosed) // the client left before the lookup finished
				return
			}
			if why == whyUnavailable {
				a.log.Error("key lookup failed", "listener", req.Listener, "request_id", req.RequestID)
				unavailable(w, req.RequestID)
				return
			}
			if p == nil {
				a.limiter.allow("badkey:"+req.IP, badKeyMax) // count the failure
				a.log.Info("key refused", "reason", why, "listener", req.Listener, "ip", req.IP, "request_id", req.RequestID)
				unauth("Invalid, expired or revoked key, or not usable here.")
				return
			}
			if !a.limiter.allow("k"+strconv.FormatInt(p.Key.ID, 10), 300) {
				writeProblem(w, fail(429, "rate_limited", "Too many requests; slow down."))
				return
			}
			next.ServeHTTP(w, r.WithContext(access.WithPrincipal(ctx, p)))
			return
		}

		if c, err := r.Cookie(a.cookieName()); err == nil && a.listener(req.Listener).UI {
			p, err := a.sessionPrincipal(ctx, c.Value, req)
			if err != nil && errors.Is(err, context.Canceled) {
				w.WriteHeader(statusClientClosed) // the client left (a page change): nothing failed
				return
			}
			if err != nil {
				// a database error is not a logged-out user: 503, so the page retries instead of
				// sending the user to the login page
				a.log.Error("session lookup", "err", err)
				unavailable(w, req.RequestID)
				return
			}
			if p != nil {
				if unsafeMethod(r.Method) {
					if why := a.csrfCheck(r); why != "" {
						writeProblem(w, fail(403, "csrf", why))
						return
					}
				}
				if !a.limiter.allow("s"+strconv.FormatInt(p.SessionID, 10), 600) {
					writeProblem(w, fail(429, "rate_limited", "Too many requests; slow down."))
					return
				}
				ctx = access.WithPrincipal(ctx, p)
			}
		}
		if access.From(ctx) == nil && !a.limiter.allow("ip"+req.IP, 120) {
			writeProblem(w, fail(429, "rate_limited", "Too many requests; slow down."))
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func unsafeMethod(m string) bool {
	return m != http.MethodGet && m != http.MethodHead && m != http.MethodOptions
}

// originOK: a browser request's Origin is this server's: the public URL (GIGG_PUBLIC_URL), or the very
// scheme, host and port the request was sent to when that name is an allowed host (so a server reached
// by both its IP and its .local name works under either; the host check has already refused other names).
func (a *API) originOK(r *http.Request, origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return false
	}
	if !a.cfg.AllowedHosts[strings.ToLower(u.Hostname())] {
		return false
	}
	if a.cfg.PublicURL != nil && strings.EqualFold(origin, a.cfg.PublicURL.Scheme+"://"+a.cfg.PublicURL.Host) {
		return true
	}
	// otherwise the very scheme, host and port this request came in on (behind a trusted proxy, the
	// scheme it reports)
	return u.Scheme == a.requestScheme(r) && strings.EqualFold(u.Host, r.Host)
}

// csrfCheck: cookie-authenticated writes must come from our own origin, as JSON (or with the
// X-Gigg-CSRF header for uploads).
func (a *API) csrfCheck(r *http.Request) string {
	if s := r.Header.Get("Sec-Fetch-Site"); s != "" && s != "same-origin" {
		return "Cross-site request refused."
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return "Missing Origin header."
	}
	if !a.originOK(r, origin) {
		return "Origin not allowed."
	}
	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") && !strings.HasPrefix(ct, "application/merge-patch+json") && r.Header.Get("X-Gigg-CSRF") == "" {
		return "Send JSON (or the X-Gigg-CSRF header)."
	}
	return ""
}

func (a *API) keyPrincipal(ctx context.Context, tok string, req access.Request) (*access.Principal, string) {
	prefix, ok := auth.ParseKeyToken(tok)
	if !ok {
		return nil, "malformed"
	}
	k, err := store.KeyByPrefix(ctx, a.st.R, prefix)
	if err != nil {
		auth.TokenMatches(tok, make([]byte, 32)) // same work either way
		if !errors.Is(err, store.ErrNotFound) {
			return nil, whyUnavailable
		}
		return nil, "unknown prefix"
	}
	if !auth.TokenMatches(tok, k.SecretHash) {
		return nil, "bad secret"
	}
	if k.Revoked {
		return nil, "revoked"
	}
	if k.ExpiresAt <= store.Now() {
		return nil, "expired"
	}
	if k.AllowedListeners != nil && !slices.Contains(k.AllowedListeners, req.Listener) {
		return nil, "listener not allowed"
	}
	if k.AllowedCIDRs != nil {
		ip, err := netip.ParseAddr(req.IP)
		allowed := false
		for _, c := range k.AllowedCIDRs {
			if p, e := netip.ParsePrefix(c); e == nil && err == nil && p.Contains(ip) {
				allowed = true
			}
		}
		if !allowed {
			return nil, "network not allowed"
		}
	}
	u, err := store.GetUser(ctx, a.st.R, k.UserID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, whyUnavailable
	}
	if err != nil || u.Disabled {
		return nil, "user disabled"
	}
	p, err := access.Load(ctx, a.st.R, u, k, 0, req)
	if err != nil {
		return nil, whyUnavailable
	}
	_ = store.TouchKey(ctx, a.st.W, k.ID, req.Listener, req.IP)
	return p, ""
}

func (a *API) sessionPrincipal(ctx context.Context, tok string, req access.Request) (*access.Principal, error) {
	if len(tok) != 43 {
		return nil, nil
	}
	s, err := store.SessionByHash(ctx, a.st.R, auth.HashToken(tok))
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u, err := store.GetUser(ctx, a.st.R, s.UserID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if err != nil || u.Disabled || u.Kind != "human" {
		return nil, nil
	}
	_ = store.TouchSession(ctx, a.st.W, s.ID)
	return access.Load(ctx, a.st.R, u, nil, s.ID, req)
}

// ---------------------------------------------------------------- rate limiting

type window struct {
	start time.Time
	n     int
}

type limiter struct {
	mu   sync.Mutex
	m    map[string]*window
	last time.Time
}

func newLimiter() *limiter { return &limiter{m: map[string]*window{}} }

// badKeyMax: refused API keys per client address and minute before further keys from it get 429.
const badKeyMax = 30

// full reports whether key already used up max events this minute, without counting one.
func (l *limiter) full(key string, max int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.m[key]
	return w != nil && time.Since(w.start) <= time.Minute && w.n >= max
}

// allow counts one event for key in the current minute; false when over max.
func (l *limiter) allow(key string, max int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if now.Sub(l.last) > time.Minute {
		for k, w := range l.m {
			if now.Sub(w.start) > time.Minute {
				delete(l.m, k)
			}
		}
		l.last = now
	}
	w := l.m[key]
	if w == nil || now.Sub(w.start) > time.Minute {
		w = &window{start: now}
		l.m[key] = w
	}
	w.n++
	return w.n <= max
}

// ---------------------------------------------------------------- helpers for handlers

// me returns the caller or a 401.
func me(ctx context.Context) (*access.Principal, error) {
	p := access.From(ctx)
	if p == nil {
		return nil, fail(401, "unauthorized", "Log in or send an API key.")
	}
	return p, nil
}

// need refuses unless the caller has scope (keys) - used for self-service endpoints.
func need(p *access.Principal, scope string) error {
	if !p.HasScope(scope) {
		return errForbidden("This key lacks the " + scope + " scope.")
	}
	return nil
}

// permNames reads well in errors.
var permNames = func() map[string]string {
	m := map[string]string{}
	for _, c := range store.Permissions {
		m[c.Name] = c.Label
	}
	return m
}()

func errNoPermission(perm string) error {
	return errForbidden("You don't have the \"" + permNames[perm] + "\" permission (" + perm + ") here; an admin can grant it in Admin › Users or through a group.")
}

// allowed refuses (403) when the caller lacks a permission in the workspace. Call it after the
// visibility checks, so an out-of-reach object still answers 404.
func allowed(p *access.Principal, ws int64, perm string) error {
	if p.Allowed(ws, perm) {
		return nil
	}
	return errNoPermission(perm)
}

// allowedAnywhere is allowed for account-level features (saved filters).
func allowedAnywhere(p *access.Principal, perm string) error {
	if p.AllowedAnywhere(perm) {
		return nil
	}
	return errNoPermission(perm)
}

// timeoutChange: setting, changing or clearing a gig's (template's, recurring gig's) run timeout needs
// the run_timeout permission.
func timeoutChange(p *access.Principal, ws int64, old, new *int64) error {
	if (old == nil && new == nil) || (old != nil && new != nil && *old == *new) {
		return nil
	}
	return allowed(p, ws, "run_timeout")
}

// alwaysRunChange: setting or clearing always_run (skips the usage budget) needs the always_run permission.
func alwaysRunChange(p *access.Principal, ws int64, old, new bool) error {
	if old == new {
		return nil
	}
	return allowed(p, ws, "always_run")
}

// alwaysRunGuard: changing what an always-run gig (or schedule, template) does, or reopening it,
// needs the always_run permission too; otherwise anyone who may edit could rewrite a cron job.
func alwaysRunGuard(p *access.Principal, ws int64, alwaysRun bool) error {
	if !alwaysRun {
		return nil
	}
	return allowed(p, ws, "always_run")
}

// userTZ: the caller's own time zone preference ("timezone" in their preferences) when it names a real
// zone, else "" (the instance default then applies).
func userTZ(p *access.Principal) string {
	if p == nil || p.User == nil || len(p.User.Preferences) == 0 {
		return ""
	}
	var prefs struct {
		Timezone string `json:"timezone"`
	}
	if json.Unmarshal(p.User.Preferences, &prefs) != nil || prefs.Timezone == "" {
		return ""
	}
	if _, err := time.LoadLocation(prefs.Timezone); err != nil {
		return ""
	}
	return prefs.Timezone
}

// toolsChange: giving a gig (template, recurring gig) a tool allowlist, changing it or clearing it
// needs the allowed_tools permission: the list decides what the runner lets the agent do.
func toolsChange(p *access.Principal, ws int64, old, new []string) error {
	if n, err := store.NormTools(new); err == nil && store.SameTools(n, old) {
		return nil
	}
	if len(old) == 0 && len(new) == 0 {
		return nil
	}
	return allowed(p, ws, "allowed_tools")
}

// toolsGuard: changing what a gig with a tool allowlist (or its template, recurring gig) does, or
// reopening it, needs the allowed_tools permission too; otherwise anyone who may edit could point the
// granted tools at other work.
func toolsGuard(p *access.Principal, ws int64, hasTools bool) error {
	if !hasTools {
		return nil
	}
	return allowed(p, ws, "allowed_tools")
}

// adminWith refuses unless the caller is an instance admin with the scope.
func adminWith(p *access.Principal, scope string) error {
	if !p.AdminWith(scope) {
		if p.IsInstanceAdmin() {
			return errForbidden("This key lacks the " + scope + " scope.")
		}
		return errForbidden("Admins only.")
	}
	return nil
}

// tx runs fn in a write transaction, mapping store errors.
func (a *API) tx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	err := a.st.Tx(ctx, fn)
	if err == nil {
		if c, ok := ctx.Value(committedKey{}).(*atomic.Bool); ok {
			c.Store(true) // the idempotency layer: this request changed something
		}
	}
	return storeErr(err)
}

// version parses an If-Match header ("N" or W/"N"); 0 = none.
func ifMatch(h string) (int64, error) {
	h = strings.TrimSpace(strings.TrimPrefix(h, "W/"))
	if h == "" || h == "*" {
		return 0, nil
	}
	v, err := strconv.ParseInt(strings.Trim(h, `"`), 10, 64)
	if err != nil || v <= 0 {
		return 0, fail(400, "bad_request", `If-Match must be a version like "3".`)
	}
	return v, nil
}

func etag(v int64) string { return `"` + strconv.FormatInt(v, 10) + `"` }

// ---------------------------------------------------------------- Opt: JSON Merge Patch fields

// Opt is a field of a PATCH body: absent (unchanged), null (cleared) or a value.
type Opt[T any] struct {
	Set   bool
	Null  bool
	Value T
}

// UnmarshalJSON records that the field was present.
func (o *Opt[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Null = true
		return nil
	}
	return json.Unmarshal(b, &o.Value)
}

// Schema makes the field nullable T in the OpenAPI document.
func (o Opt[T]) Schema(r huma.Registry) *huma.Schema {
	s := huma.SchemaFromType(r, reflect.TypeOf(o.Value))
	cp := *s
	cp.Nullable = true
	return &cp
}

// Ptr returns nil (unchanged), &nil (cleared) or &&v.
func (o Opt[T]) Ptr() **T {
	if !o.Set {
		return nil
	}
	if o.Null {
		var z *T
		return &z
	}
	v := o.Value
	pv := &v
	return &pv
}

// Val returns nil when absent or null, else &v (for fields that can't be cleared).
func (o Opt[T]) Val() *T {
	if !o.Set || o.Null {
		return nil
	}
	v := o.Value
	return &v
}

func ms(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	v := t.UnixMilli()
	return &v
}
