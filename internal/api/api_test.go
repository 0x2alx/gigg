package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/0x2alx/gigg/internal/auth"
	"github.com/0x2alx/gigg/internal/config"
	"github.com/0x2alx/gigg/internal/store"
)

const adminPW = "correct horse battery staple"

type env struct {
	t      *testing.T
	a      *API
	st     *store.Store
	h      http.Handler
	cookie string // ada's session
	ids    map[string]int64
	keys   map[string]string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(context.Background(), t.TempDir(), log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	u, _ := url.Parse("https://board.example")
	cfg := &config.Config{PublicURL: u, AllowedHosts: map[string]bool{"board.example": true, "localhost": true},
		Listeners:      []config.Listener{{Name: "main", Addr: ":8443", UI: true}, {Name: "internal", Addr: ":8080"}},
		TrustedProxies: []netip.Prefix{}}
	e := &env{t: t, st: st, ids: map[string]int64{}, keys: map[string]string{}}
	e.a = New(cfg, st, log)
	e.h = e.a.Handler()
	ctx := context.Background()
	h, _ := auth.HashPassword(adminPW)
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(st.Tx(ctx, func(tx *sql.Tx) error {
		var a store.Actor
		ada, err := store.CreateUser(ctx, tx, a, store.NewUser{Username: "ada", Kind: "human", PasswordHash: h, IsAdmin: true})
		if err != nil {
			return err
		}
		bobH, _ := auth.HashPassword("bob's long password")
		bob, err := store.CreateUser(ctx, tx, a, store.NewUser{Username: "bob", Kind: "human", PasswordHash: bobH})
		if err != nil {
			return err
		}
		e.ids["ada"], e.ids["bob"] = ada.ID, bob.ID
		a = store.Actor{UserID: ada.ID}
		for _, name := range []string{"scout", "atlas"} {
			ag, err := store.CreateUser(ctx, tx, a, store.NewUser{Username: name, Kind: "agent"})
			if err != nil {
				return err
			}
			w, err := store.CreateWorkspace(ctx, tx, a, name, strings.ToUpper(name), nil)
			if err != nil {
				return err
			}
			if err := store.SetMembership(ctx, tx, a, w.ID, ag.ID, "member"); err != nil {
				return err
			}
			p, err := store.CreateProject(ctx, tx, a, w.ID, "main", "Main", "GIG", nil)
			if err != nil {
				return err
			}
			p2, err := store.CreateProject(ctx, tx, a, w.ID, "side", "Side", "SID", nil)
			if err != nil {
				return err
			}
			tk, err := store.CreateGig(ctx, tx, a, store.NewGig{ProjectID: p.ID, Title: name + " secret gig", Labels: []string{name + "-label"}})
			if err != nil {
				return err
			}
			c, err := store.AddComment(ctx, tx, a, tk, "comment", name+" secret comment", nil)
			if err != nil {
				return err
			}
			r, err := store.StartRun(ctx, tx, store.Actor{UserID: ag.ID}, tk, nil, "new", 1, nil)
			if err != nil {
				return err
			}
			rec, err := store.CreateRecurrence(ctx, tx, a, &store.Recurrence{ProjectID: p.ID, Title: name + " secret schedule", Freq: "daily",
				TZ: "UTC", WindowStart: "09:00", WindowMinutes: 60, StartsOn: store.Now() + 86400000})
			if err != nil {
				return err
			}
			e.ids[name+".rec"], e.ids[name+".sched"] = rec.ID, rec.ScheduleID
			fld, err := store.SaveCustomField(ctx, tx, a, &store.CustomField{WorkspaceID: w.ID, Name: name + " secret field", Kind: "text"}, 0)
			if err != nil {
				return err
			}
			tpl, err := store.SaveTemplate(ctx, tx, a, &store.Template{WorkspaceID: w.ID, Name: name + " secret template", Title: name + " secret"}, 0)
			if err != nil {
				return err
			}
			flt, err := store.SaveFilter(ctx, tx, ag.ID, &store.SavedFilter{WorkspaceID: &w.ID, Name: name + " secret filter", Query: map[string]string{"q": "x"}})
			if err != nil {
				return err
			}
			if err := store.AddChecklistItems(ctx, tx, a, tk, []string{name + " secret item"}); err != nil {
				return err
			}
			e.ids[name+".field"], e.ids[name+".template"], e.ids[name+".filter"] = fld.ID, tpl.ID, flt.ID
			cr, err := store.CreateErrand(ctx, tx, a, store.NewErrand{WorkspaceID: w.ID, WorkerID: ag.ID, ScheduleID: rec.ScheduleID,
				Template: &store.ErrandTemplate{Name: name + " secret errand", Command: "echo " + name + " secret"}}, store.ErrandEdit{})
			if err != nil {
				return err
			}
			res, err := tx.ExecContext(ctx, `INSERT INTO errand_runs (errand_id, workspace_id, worker_id, status, output_tail, created_at) VALUES (?,?,?,'ok',?,?)`,
				cr.ID, w.ID, ag.ID, name+" secret output", store.Now())
			if err != nil {
				return err
			}
			e.ids[name+".errand"], e.ids[name+".errandtpl"] = cr.ID, cr.TemplateID
			e.ids[name+".errandrun"], _ = res.LastInsertId()
			sts, _ := store.Statuses(ctx, tx, p.ID)
			e.ids[name+".user"], e.ids[name+".ws"], e.ids[name+".p"], e.ids[name+".p2"] = ag.ID, w.ID, p.ID, p2.ID
			e.ids[name+".gig"], e.ids[name+".comment"], e.ids[name+".run"], e.ids[name+".status"] = tk.ID, c.ID, r.ID, sts[0].ID
			e.ids[name+".label"] = tk.Labels[0].ID
		}
		return store.SetMembership(ctx, tx, a, e.ids["scout.ws"], bob.ID, "viewer")
	}))
	// ada logs in through the API
	code, body, hdr := e.req("POST", "/auth/login", `{"username":"ada","password":"`+adminPW+`"}`, cred{listener: "main"})
	if code != 200 {
		t.Fatalf("login: %d %s", code, body)
	}
	m := regexp.MustCompile(`__Host-gigg_session=([^;]+)`).FindStringSubmatch(hdr.Get("Set-Cookie"))
	if m == nil || !strings.Contains(hdr.Get("Set-Cookie"), "HttpOnly") || !strings.Contains(hdr.Get("Set-Cookie"), "SameSite=Strict") || !strings.Contains(hdr.Get("Set-Cookie"), "Secure") {
		t.Fatalf("cookie: %q", hdr.Get("Set-Cookie"))
	}
	e.cookie = m[1]
	// runner keys for both agents, internal listener only (as on the host)
	for _, name := range []string{"scout", "atlas"} {
		body := fmt.Sprintf(`{"name":"runner","user_id":%d,"reach":["%s"],"scopes":["workspace:read","gigs:read","gigs:work","gigs:write","comments:read","comments:write","runs:write","profile:write","keys:self"],"allowed_listeners":["internal"]}`,
			e.ids[name+".user"], name)
		code, out, _ := e.req("POST", "/keys", body, e.ada())
		if code != 201 {
			t.Fatalf("key for %s: %d %s", name, code, out)
		}
		var kc KeyCreated
		_ = json.Unmarshal(out, &kc)
		e.keys[name] = kc.Token
		e.ids[name+".key"] = kc.Key.ID
	}
	return e
}

type cred struct {
	token    string
	cookie   string
	listener string
	origin   string
	ct       string
}

func (e *env) ada() cred {
	return cred{cookie: e.cookie, listener: "main", origin: "https://board.example"}
}
func (e *env) key(name string) cred {
	return cred{token: e.keys[name], listener: "internal"}
}

func (e *env) req(method, path, body string, c cred) (int, []byte, http.Header) {
	e.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, "https://board.example"+Prefix+path, rd)
	if body != "" {
		ct := c.ct
		if ct == "" {
			ct = "application/json"
		}
		r.Header.Set("Content-Type", ct)
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
	r = r.WithContext(WithInfo(r.Context(), RequestInfo{Listener: c.listener, RequestID: "test"}))
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w.Code, w.Body.Bytes(), w.Header()
}

func (e *env) ok(code int, body []byte, want int) {
	e.t.Helper()
	if code != want {
		e.t.Fatalf("status %d, want %d: %s", code, want, body)
	}
}

func TestAuthBasics(t *testing.T) {
	e := newEnv(t)
	// no credentials
	if code, _, _ := e.req("GET", "/auth/me", "", cred{listener: "main"}); code != 401 {
		t.Fatalf("anonymous me: %d", code)
	}
	// wrong password, unknown user: same answer
	c1, b1, _ := e.req("POST", "/auth/login", `{"username":"ada","password":"wrong wrong wrong"}`, cred{listener: "main"})
	c2, b2, _ := e.req("POST", "/auth/login", `{"username":"nobody","password":"wrong wrong wrong"}`, cred{listener: "main"})
	if c1 != 401 || c2 != 401 || !bytes.Contains(b1, []byte("bad_credentials")) || !bytes.Contains(b2, []byte("bad_credentials")) {
		t.Fatalf("bad logins: %d %s / %d %s", c1, b1, c2, b2)
	}
	// agents can't log in; no login on the internal listener
	if code, _, _ := e.req("POST", "/auth/login", `{"username":"ada","password":"`+adminPW+`"}`, cred{listener: "internal"}); code != 403 {
		t.Fatalf("login on internal: %d", code)
	}
	// session cookie is ignored on a non-UI listener
	if code, _, _ := e.req("GET", "/auth/me", "", cred{cookie: e.cookie, listener: "internal"}); code != 401 {
		t.Fatalf("cookie on internal: %d", code)
	}
	// me
	code, body, _ := e.req("GET", "/auth/me", "", e.ada())
	e.ok(code, body, 200)
	var me MeBody
	_ = json.Unmarshal(body, &me)
	if !me.Admin || len(me.Workspaces) != 2 {
		t.Fatalf("ada me: %s", body)
	}
	// key on a listener it isn't allowed on: 401 like an unknown key
	if code, _, _ := e.req("GET", "/auth/me", "", cred{token: e.keys["scout"], listener: "main"}); code != 401 {
		t.Fatalf("restricted key on main: %d", code)
	}
	code, body, _ = e.req("GET", "/auth/me", "", e.key("scout"))
	e.ok(code, body, 200)
	_ = json.Unmarshal(body, &me)
	if me.Admin || len(me.Workspaces) != 1 || me.Workspaces[0].Slug != "scout" || me.Key == nil || me.Key.Prefix == "" {
		t.Fatalf("scout me: %s", body)
	}
	if bytes.Contains(body, []byte(e.keys["scout"])) {
		t.Fatal("token echoed")
	}
	// malformed bearer, revoked key
	if code, _, _ := e.req("GET", "/auth/me", "", cred{token: "gigg_nope", listener: "internal"}); code != 401 {
		t.Fatalf("malformed: %d", code)
	}
}

func TestCSRF(t *testing.T) {
	e := newEnv(t)
	body := `{"title":"x"}`
	p := fmt.Sprintf("/projects/%d/gigs", e.ids["scout.p"])
	for name, c := range map[string]cred{
		"no origin":      {cookie: e.cookie, listener: "main"},
		"foreign origin": {cookie: e.cookie, listener: "main", origin: "https://evil.example"},
		"form post":      {cookie: e.cookie, listener: "main", origin: "https://board.example", ct: "application/x-www-form-urlencoded"},
	} {
		if code, out, _ := e.req("POST", p, body, c); code != 403 || !bytes.Contains(out, []byte("csrf")) {
			t.Errorf("%s: %d %s", name, code, out)
		}
	}
	if code, out, _ := e.req("POST", p, body, e.ada()); code != 201 {
		t.Fatalf("same-origin JSON: %d %s", code, out)
	}
	// a bearer request needs no origin
	if code, out, _ := e.req("POST", p, body, e.key("scout")); code != 201 {
		t.Fatalf("bearer create: %d %s", code, out)
	}
}

func TestRolesAndScopes(t *testing.T) {
	e := newEnv(t)
	// bob: viewer in scout's workspace
	code, body, hdr := e.req("POST", "/auth/login", `{"username":"bob","password":"bob's long password"}`, cred{listener: "main"})
	e.ok(code, body, 200)
	bob := cred{cookie: regexp.MustCompile(`__Host-gigg_session=([^;]+)`).FindStringSubmatch(hdr.Get("Set-Cookie"))[1], listener: "main", origin: "https://board.example"}
	if code, _, _ := e.req("GET", fmt.Sprintf("/gigs/%d", e.ids["scout.gig"]), "", bob); code != 200 {
		t.Fatalf("viewer read: %d", code)
	}
	if code, out, _ := e.req("POST", fmt.Sprintf("/projects/%d/gigs", e.ids["scout.p"]), `{"title":"x"}`, bob); code != 403 {
		t.Fatalf("viewer create: %d %s", code, out)
	}
	if code, _, _ := e.req("GET", fmt.Sprintf("/gigs/%d", e.ids["atlas.gig"]), "", bob); code != 404 {
		t.Fatalf("bob sees atlas: %d", code)
	}
	// the runner key can't delete, manage projects or act as admin
	for _, c := range []struct{ m, p, b string }{
		{"DELETE", fmt.Sprintf("/gigs/%d", e.ids["scout.gig"]), ""},
		{"POST", "/workspaces/scout/projects", `{"slug":"new","name":"New","key":"NEW"}`},
		{"POST", "/workspaces", `{"slug":"evil","name":"Evil"}`},
		{"GET", "/keys", ""},
		{"GET", "/audit", ""},
	} {
		if code, out, _ := e.req(c.m, c.p, c.b, e.key("scout")); code != 403 {
			t.Errorf("%s %s: %d %s", c.m, c.p, code, out)
		}
	}
}

func TestNoKeyEscalation(t *testing.T) {
	e := newEnv(t)
	cases := map[string]string{
		"wider scope":           `{"name":"x","reach":["scout"],"scopes":["gigs:delete"]}`,
		"other workspace":       `{"name":"x","reach":["atlas"],"scopes":["gigs:read"]}`,
		"admin key":             `{"name":"x","scopes":["gigs:read"]}`,
		"loosen listener":       `{"name":"x","reach":["scout"],"scopes":["gigs:read"],"allowed_listeners":["main"]}`,
		"admin scope on ws key": `{"name":"x","reach":["scout"],"scopes":["admin:users"]}`,
	}
	for name, body := range cases {
		code, out, _ := e.req("POST", "/me/keys", body, e.key("scout"))
		if code != 403 && code != 404 && code != 422 {
			t.Errorf("%s: %d %s", name, code, out)
		}
	}
	// a narrower key is fine and inherits the listener restriction
	code, out, _ := e.req("POST", "/me/keys", `{"name":"ro","reach":["current"],"scopes":["gigs:read"]}`, e.key("scout"))
	if code != 422 { // "current" isn't a slug for creation... use the slug
		_ = out
	}
	code, out, _ = e.req("POST", "/me/keys", `{"name":"ro","reach":["scout"],"scopes":["gigs:read"]}`, e.key("scout"))
	e.ok(code, out, 201)
	var kc KeyCreated
	_ = json.Unmarshal(out, &kc)
	if len(kc.Key.AllowedListeners) != 1 || kc.Key.AllowedListeners[0] != "internal" || kc.Key.CreatedByKey == nil {
		t.Fatalf("inherited restriction: %s", out)
	}
	if code, _, _ := e.req("GET", "/auth/me", "", cred{token: kc.Token, listener: "main"}); code != 401 {
		t.Fatalf("child key usable on main: %d", code)
	}
	// revoke works and the key stops working
	code, out, _ = e.req("DELETE", fmt.Sprintf("/me/keys/%d", kc.Key.ID), "", e.key("scout"))
	e.ok(code, out, 204)
	if code, _, _ := e.req("GET", "/auth/me", "", cred{token: kc.Token, listener: "internal"}); code != 401 {
		t.Fatalf("revoked key works: %d", code)
	}
}

func TestRunnerFlow(t *testing.T) {
	e := newEnv(t)
	k := e.key("scout")
	// ada creates a gig in the browser
	code, out, _ := e.req("POST", fmt.Sprintf("/projects/%d/gigs", e.ids["scout.p"]), `{"title":"Write the README","priority":3}`, e.ada())
	e.ok(code, out, 201)
	// dry run, then claim (only gigs by ada or scout)
	code, out, _ = e.req("POST", "/workspaces/current/gigs/claim", `{"created_by":["ada","scout"],"start_due":true,"dry_run":true}`, k)
	e.ok(code, out, 200)
	var t1 store.Gig
	_ = json.Unmarshal(out, &t1)
	if t1.Title != "Write the README" || t1.StatusCategory != "todo" {
		t.Fatalf("dry run: %s", out)
	}
	code, out, _ = e.req("POST", "/workspaces/current/gigs/claim", `{"created_by":["ada","scout"],"start_due":true}`, k)
	e.ok(code, out, 200)
	_ = json.Unmarshal(out, &t1)
	if t1.StatusCategory != "doing" {
		t.Fatalf("claim: %s", out)
	}
	sid := "0b9c6a52-3f1e-4b7a-9c2d-1e2f3a4b5c6d"
	code, out, _ = e.req("POST", "/gigs/"+t1.Key+"/runs", `{"session_id":"`+sid+`","mode":"new","attempt":1}`, k)
	e.ok(code, out, 201)
	var rd RunDetail
	_ = json.Unmarshal(out, &rd)
	code, out, _ = e.req("POST", fmt.Sprintf("/runs/%d/progress", rd.ID), `{"pct":50,"note":"drafting"}`, k)
	e.ok(code, out, 200)
	// lists carry the latest run in brief (a board card shows it), and the progress note is live news
	findIn := func(path string, c cred) *store.Gig {
		t.Helper()
		code, out, _ := e.req("GET", path, "", c)
		e.ok(code, out, 200)
		var l GigList
		_ = json.Unmarshal(out, &l)
		for _, x := range l.Items {
			if x.ID == t1.ID {
				return x
			}
		}
		t.Fatalf("%s: gig %s missing", path, t1.Key)
		return nil
	}
	if lt := findIn("/workspaces/current/gigs", k); lt.LastRun == nil || lt.LastRun.Status != "running" || lt.LastRun.ProgressNote == nil || *lt.LastRun.ProgressNote != "drafting" {
		t.Fatalf("last_run in list: %+v", lt.LastRun)
	}
	if acts := e.allChanges("current", k, 100); acts[len(acts)-1].Action != "run.progress" {
		t.Fatalf("progress note not in the feed: %s", acts[len(acts)-1].Action)
	}
	// a burst of progress is one event per run every few seconds; a bare heartbeat is none
	code, out, _ = e.req("POST", fmt.Sprintf("/runs/%d/progress", rd.ID), `{"pct":70}`, k)
	e.ok(code, out, 200)
	code, out, _ = e.req("POST", fmt.Sprintf("/runs/%d/progress", rd.ID), `{}`, k)
	e.ok(code, out, 200)
	n := 0
	for _, it := range e.allChanges("current", k, 100) {
		if it.Action == "run.progress" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d run.progress events, want 1", n)
	}
	// once the window has passed, the next call (even a bare heartbeat) sends what was held back, once
	old := progressEventEvery
	progressEventEvery = 0
	for i := 0; i < 2; i++ {
		code, out, _ = e.req("POST", fmt.Sprintf("/runs/%d/progress", rd.ID), `{}`, k)
		e.ok(code, out, 200)
	}
	progressEventEvery = old
	n = 0
	for _, it := range e.allChanges("current", k, 100) {
		if it.Action == "run.progress" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("%d run.progress events after the held-back one, want 2", n)
	}
	code, out, _ = e.req("POST", fmt.Sprintf("/runs/%d/finish", rd.ID),
		`{"status":"needs_human","report_md":"Siren or **chime**?","comment_kind":"question","move_to_category":"waiting","cost_usd":0.44,"num_turns":6,"transcript_path":"/home/x/.claude/projects/p/`+sid+`.jsonl"}`, k)
	e.ok(code, out, 200)
	var fr FinishResult
	_ = json.Unmarshal(out, &fr)
	if fr.Gig.StatusCategory != "waiting" || fr.Run.Status != "needs_human" || fr.Gig.CostUSDTotal != 0.44 {
		t.Fatalf("finish: %s", out)
	}
	if lt := findIn(fmt.Sprintf("/workspaces/%d/gigs", t1.WorkspaceID), e.ada()); lt.Question == nil || lt.Question.Text != "Siren or **chime**?" || lt.LastRun.Status != "needs_human" {
		t.Fatalf("question in list: %+v", lt.Question)
	}
	code, out, _ = e.req("GET", fmt.Sprintf("/projects/%d/board", t1.ProjectID), "", e.ada())
	if code != 200 || !bytes.Contains(out, []byte(`"text":"Siren or **chime**?"`)) {
		t.Fatalf("board without the question: %d %s", code, out)
	}
	// inbox shows the question to ada
	code, out, _ = e.req("GET", "/inbox", "", e.ada())
	e.ok(code, out, 200)
	if !bytes.Contains(out, []byte("Siren or **chime**?")) {
		t.Fatalf("inbox: %s", out)
	}
	// ada answers with Resume; the runner sees the answer since its report
	code, out, _ = e.req("POST", "/gigs/"+t1.Key+"/resume", `{"comment_md":"chime please"}`, e.ada())
	e.ok(code, out, 200)
	code, out, _ = e.req("GET", fmt.Sprintf("/gigs/%s/comments?since_id=%d", t1.Key, *fr.Run.ReportCommentID), "", k)
	e.ok(code, out, 200)
	var cs struct{ Items []*store.Comment }
	_ = json.Unmarshal(out, &cs)
	if len(cs.Items) != 1 || cs.Items[0].BodyMD != "chime please" || !cs.Items[0].AuthorIsAdmin || cs.Items[0].Kind != "resume_request" {
		t.Fatalf("answer: %s", out)
	}
	// nothing else to claim from atlas's or scout's other gigs created by atlas? 204 when empty
	code, _, _ = e.req("POST", "/workspaces/current/gigs/claim", `{"created_by":["nobody"]}`, k)
	if code != 204 {
		t.Fatalf("empty claim: %d", code)
	}
	// summary is plain text
	code, out, hdr := e.req("GET", "/workspaces/current/summary", "", k)
	e.ok(code, out, 200)
	if !strings.HasPrefix(hdr.Get("Content-Type"), "text/plain") || !strings.Contains(string(out), "Main (GIG):") {
		t.Fatalf("summary: %s %s", hdr.Get("Content-Type"), out)
	}
	// stats count the run and its cost
	code, out, _ = e.req("GET", "/workspaces/current/stats", "", k)
	e.ok(code, out, 200)
	if !bytes.Contains(out, []byte(`"cost_usd":0.44`)) {
		t.Fatalf("stats: %s", out)
	}
}

func TestMoveBetweenProjectsAndVersions(t *testing.T) {
	e := newEnv(t)
	k := e.key("scout")
	path := fmt.Sprintf("/gigs/%d", e.ids["scout.gig"])
	code, out, hdr := e.req("GET", path, "", k)
	e.ok(code, out, 200)
	stale := hdr.Get("ETag")
	code, out, _ = e.req("POST", path+"/move", fmt.Sprintf(`{"project":%d}`, e.ids["scout.p2"]), k)
	e.ok(code, out, 200)
	var t1 store.Gig
	_ = json.Unmarshal(out, &t1)
	if t1.Key != "SID-1" || len(t1.Aliases) != 1 || t1.Aliases[0] != "GIG-1" {
		t.Fatalf("moved: %s", out)
	}
	// the old key still resolves, for this caller only within reach
	if code, _, _ := e.req("GET", "/gigs/GIG-1", "", k); code != 200 {
		t.Fatalf("alias: %d", code)
	}
	// ada sees GIG-1 in both workspaces (scout's alias, atlas's gig): ambiguous
	if code, out, _ := e.req("GET", "/gigs/GIG-1", "", e.ada()); code != 409 || !bytes.Contains(out, []byte("ambiguous_key")) {
		t.Fatalf("ambiguous: %d %s", code, out)
	}
	// If-Match with a stale version
	r := httptest.NewRequest("PATCH", "https://board.example"+Prefix+path, strings.NewReader(`{"title":"new"}`))
	r.Header.Set("Content-Type", "application/merge-patch+json")
	r.Header.Set("Authorization", "Bearer "+e.keys["scout"])
	r.Header.Set("If-Match", stale)
	r = r.WithContext(WithInfo(r.Context(), RequestInfo{Listener: "internal"}))
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	if w.Code != 412 {
		t.Fatalf("stale If-Match: %d %s", w.Code, w.Body)
	}
	// null clears, absent keeps
	code, out, _ = e.req("PATCH", path, `{"due_at":1893456000000,"priority":2}`, k)
	e.ok(code, out, 200)
	code, out, _ = e.req("PATCH", path, `{"due_at":null}`, k)
	e.ok(code, out, 200)
	_ = json.Unmarshal(out, &t1)
	if t1.DueAt != nil || t1.Priority != 2 {
		t.Fatalf("merge patch: %s", out)
	}
}

// fingerprint of everything in workspace ws (to prove a request changed nothing).
func (e *env) fingerprint(ws int64) string {
	var parts []string
	for _, q := range []string{
		`SELECT group_concat(id || title || status_id || position || version || COALESCE(deleted_at,'') || project_id, '|') FROM gigs WHERE workspace_id = ?`,
		`SELECT group_concat(id || body_md || version || COALESCE(deleted_at,''), '|') FROM comments WHERE workspace_id = ?`,
		`SELECT group_concat(id || name || version, '|') FROM labels WHERE workspace_id = ?`,
		`SELECT group_concat(id || name || version, '|') FROM statuses WHERE workspace_id = ?`,
		`SELECT group_concat(id || name || version || COALESCE(deleted_at,''), '|') FROM projects WHERE workspace_id = ?`,
		`SELECT group_concat(IFNULL(user_id, '') || IFNULL(group_id, '') || IFNULL(project_id, '') || preset || overrides, '|') FROM access_rules WHERE workspace_id = ?`,
		`SELECT group_concat(id || status || version, '|') FROM runs WHERE workspace_id = ?`,
		`SELECT group_concat(id || version || COALESCE(revoked_at,''), '|') FROM api_keys WHERE id IN (SELECT key_id FROM api_key_reach WHERE workspace_id = ?)`,
		`SELECT group_concat(id || name || version, '|') FROM workspaces WHERE id = ?`,
		`SELECT group_concat(id || template_id || schedule_id || version || COALESCE(deleted_at,''), '|') FROM recurrences WHERE workspace_id = ?`,
		`SELECT group_concat(id || name || version || COALESCE(deleted_at,''), '|') FROM schedules WHERE workspace_id = ?`,
		`SELECT group_concat(id || name || version || COALESCE(deleted_at,''), '|') FROM custom_fields WHERE workspace_id = ?`,
		`SELECT group_concat(id || name || version || COALESCE(deleted_at,''), '|') FROM gig_templates WHERE workspace_id = ?`,
		`SELECT group_concat(id || name || query, '|') FROM saved_filters WHERE workspace_id = ?`,
		`SELECT group_concat(id || text || COALESCE(done_at,'') || position, '|') FROM checklist_items WHERE workspace_id = ?`,
		`SELECT group_concat(from_gig_id || to_gig_id || kind, '|') FROM gig_relations WHERE workspace_id = ?`,
		`SELECT group_concat(gig_id || field_id || value, '|') FROM gig_custom_values v JOIN gigs t ON t.id = v.gig_id WHERE t.workspace_id = ?`,
		`SELECT group_concat(id || template_id || schedule_id || worker_id || IFNULL(project_id, '') || version || COALESCE(paused_at,'') || COALESCE(run_now_at,'') || COALESCE(deleted_at,''), '|') FROM errands WHERE workspace_id = ?`,
		`SELECT group_concat(id || name || command || unsandboxed || version || COALESCE(deleted_at,''), '|') FROM errand_templates WHERE workspace_id = ?`,
		`SELECT group_concat(id || status || COALESCE(output_tail,''), '|') FROM errand_runs WHERE workspace_id = ?`,
	} {
		var s sql.NullString
		if err := e.st.R.QueryRow(q, ws).Scan(&s); err != nil {
			e.t.Fatal(err)
		}
		parts = append(parts, s.String)
	}
	return strings.Join(parts, "\n")
}

// TestIsolationSweep calls every API route that takes an id with workspace A's key against
// workspace B's objects: none may succeed, none may leak B's text, and B must be unchanged.
func TestIsolationSweep(t *testing.T) {
	e := newEnv(t)
	doc := e.a.Huma().OpenAPI()
	b := "atlas"
	sub := map[string]string{
		"{ws}": b, "{p}": fmt.Sprint(e.ids[b+".p"]), "{user}": fmt.Sprint(e.ids[b+".user"]), "{item}": "1",
	}
	idFor := map[string]int64{
		"/gigs/": e.ids[b+".gig"], "/comments/": e.ids[b+".comment"], "/runs/": e.ids[b+".run"], "/labels/": e.ids[b+".label"],
		"/statuses/": e.ids[b+".status"], "/keys/": e.ids[b+".key"], "/me/keys/": e.ids[b+".key"], "/users/": e.ids[b+".user"],
		"/sessions/": 1, "/auth/sessions/": 1, "/groups/": 1, "/fields/": e.ids[b+".field"], "/templates/": e.ids[b+".template"], "/me/filters/": e.ids[b+".filter"], "/recurrences/": e.ids[b+".rec"], "/schedules/": e.ids[b+".sched"],
		"/errands/": e.ids[b+".errand"], "/errand-templates/": e.ids[b+".errandtpl"], "/errand-runs/": e.ids[b+".errandrun"],
	}
	// scout's key with every non-admin scope: a route must refuse atlas's objects by workspace, not by scope
	var scopes []string
	for _, s := range store.AllScopes {
		if !store.IsAdminScope(s) {
			scopes = append(scopes, `"`+s+`"`)
		}
	}
	code, out, _ := e.req("POST", "/keys", fmt.Sprintf(`{"name":"sweep","user_id":%d,"reach":["scout"],"scopes":[%s],"allowed_listeners":["internal"]}`,
		e.ids["scout.user"], strings.Join(scopes, ",")), e.ada())
	e.ok(code, out, 201)
	var kc KeyCreated
	_ = json.Unmarshal(out, &kc)
	sweepKey := cred{token: kc.Token, listener: "internal"}
	bodies := `{"title":"pwned","body_md":"pwned","name":"pwned","slug":"pwned","key":"PWN","status":"done","category":"done","role":"member",` +
		`"scopes":["gigs:read"],"password":"pwned pwned pwned","ids":[1],"comment_md":"pwned","project":` + fmt.Sprint(e.ids[b+".p"]) + `}`
	before := e.fingerprint(e.ids[b+".ws"])
	var paths []string
	for p := range doc.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	checked := 0
	for _, p := range paths {
		if !strings.Contains(p, "{") {
			continue
		}
		item := doc.Paths[p]
		ops := map[string]bool{"GET": item.Get != nil, "POST": item.Post != nil, "PUT": item.Put != nil, "PATCH": item.Patch != nil, "DELETE": item.Delete != nil}
		concrete := p
		for k, v := range sub {
			concrete = strings.ReplaceAll(concrete, k, v)
		}
		if strings.Contains(concrete, "{id}") {
			var id int64
			best := ""
			for prefix, v := range idFor {
				if strings.HasPrefix(p, prefix) && len(prefix) > len(best) {
					best, id = prefix, v
				}
			}
			if best == "" {
				t.Fatalf("no id mapping for %s", p)
			}
			concrete = strings.ReplaceAll(concrete, "{id}", fmt.Sprint(id))
		}
		for m, has := range ops {
			if !has {
				continue
			}
			body := ""
			if m != "GET" && m != "DELETE" {
				body = bodies
			}
			code, out, _ := e.req(m, concrete, body, sweepKey)
			checked++
			if code < 400 {
				t.Errorf("%s %s (as scout's key): %d %s", m, concrete, code, out)
			}
			if bytes.Contains(out, []byte("atlas secret")) {
				t.Errorf("%s %s leaked atlas's text", m, concrete)
			}
		}
	}
	// the "current"/slug variants and key-based lookups
	for _, c := range []struct{ m, p string }{{"GET", "/gigs/GIG-1"}, {"GET", "/workspaces/atlas/summary"}, {"GET", "/workspaces/" + fmt.Sprint(e.ids["atlas.ws"])}} {
		code, out, _ := e.req(c.m, c.p, "", e.key("scout"))
		if c.p == "/gigs/GIG-1" {
			var tk store.Gig
			_ = json.Unmarshal(out, &tk)
			if code != 200 || tk.WorkspaceID != e.ids["scout.ws"] {
				t.Errorf("GIG-1 must resolve to scout's own gig: %d %s", code, out)
			}
			continue
		}
		if code != 404 {
			t.Errorf("%s %s: %d", c.m, c.p, code)
		}
	}
	// lists never include atlas's objects
	for _, p := range []string{"/workspaces", "/inbox", "/users", "/me/keys", "/workspaces/current/gigs?q=secret", "/workspaces/current/runs", "/workspaces/current/activity",
		"/workspaces/current/errands", "/workspaces/current/errand-templates"} {
		code, out, _ := e.req("GET", p, "", sweepKey)
		if code >= 400 && code != 403 {
			t.Errorf("GET %s: %d %s", p, code, out)
		}
		if bytes.Contains(out, []byte("atlas secret")) || bytes.Contains(out, []byte(`"slug":"atlas"`)) || bytes.Contains(out, []byte("atlas-label")) {
			t.Errorf("GET %s leaked atlas: %s", p, out)
		}
	}
	if after := e.fingerprint(e.ids[b+".ws"]); after != before {
		t.Fatalf("atlas's workspace changed:\nbefore %s\nafter  %s", before, after)
	}
	if checked < 40 {
		t.Fatalf("sweep only checked %d operations", checked)
	}
	t.Logf("isolation sweep: %d operations refused", checked)
}

func TestOpenAPIServed(t *testing.T) {
	e := newEnv(t)
	code, out, _ := e.req("GET", "/openapi.json", "", cred{listener: "main"})
	e.ok(code, out, 200)
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil || doc["openapi"] == nil {
		t.Fatalf("openapi: %v", err)
	}
	if !bytes.Contains(out, []byte(`"/gigs/{id}/move"`)) {
		t.Fatal("move missing from OpenAPI")
	}
}

// TestKeyAndSessionBoundaries: what a narrow key or a non-admin can and can't do with keys, admin
// routes, comment text and key events.
func TestKeyAndSessionBoundaries(t *testing.T) {
	e := newEnv(t)
	// an admin key with only admin:keys (internal listener) can't rotate a wider key and take its secret
	code, out, _ := e.req("POST", "/keys", `{"name":"keymgr","scopes":["admin:keys"],"allowed_listeners":["internal"]}`, e.ada())
	e.ok(code, out, 201)
	var mgr KeyCreated
	_ = json.Unmarshal(out, &mgr)
	km := cred{token: mgr.Token, listener: "internal"}
	if code, out, _ := e.req("POST", fmt.Sprintf("/keys/%d/rotate", e.ids["scout.key"]), "", km); code != 403 {
		t.Fatalf("narrow admin key rotated a wider key: %d %s", code, out)
	}
	// non-admins get 403 on /keys/{id} whether or not the key exists (no existence oracle)
	c1, _, _ := e.req("GET", fmt.Sprintf("/keys/%d", e.ids["atlas.key"]), "", e.key("scout"))
	c2, _, _ := e.req("GET", "/keys/99999", "", e.key("scout"))
	if c1 != 403 || c2 != 403 {
		t.Fatalf("key lookup oracle: %d vs %d", c1, c2)
	}
	// passwords and admin rights only from a browser session
	code, out, _ = e.req("POST", "/keys", `{"name":"users","scopes":["admin:users"],"allowed_listeners":["internal"]}`, e.ada())
	e.ok(code, out, 201)
	var um KeyCreated
	_ = json.Unmarshal(out, &um)
	uk := cred{token: um.Token, listener: "internal"}
	for _, c := range []struct{ m, p, b string }{
		{"POST", fmt.Sprintf("/users/%d/password", e.ids["ada"]), `{"password":"attacker password 1"}`},
		{"POST", "/users", `{"username":"evil","kind":"human","is_admin":true}`},
		{"POST", "/users", `{"username":"evil2","kind":"human","password":"attacker password 1"}`},
		{"PATCH", fmt.Sprintf("/users/%d", e.ids["bob"]), `{"is_admin":true}`},
	} {
		if code, out, _ := e.req(c.m, c.p, c.b, uk); code != 403 {
			t.Errorf("%s %s with an admin:users key: %d %s", c.m, c.p, code, out)
		}
	}
	// revoking (or rotating) a key revokes the keys it created
	code, out, _ = e.req("POST", "/me/keys", `{"name":"child","reach":["scout"],"scopes":["gigs:read"]}`, e.key("scout"))
	e.ok(code, out, 201)
	var child KeyCreated
	_ = json.Unmarshal(out, &child)
	code, out, _ = e.req("POST", fmt.Sprintf("/keys/%d/rotate", e.ids["scout.key"]), "{}", e.ada())
	e.ok(code, out, 200)
	var rot KeyCreated
	_ = json.Unmarshal(out, &rot)
	if code, _, _ := e.req("GET", "/auth/me", "", cred{token: child.Token, listener: "internal"}); code != 401 {
		t.Fatalf("child key survived its parent's rotation: %d", code)
	}
	e.keys["scout"] = rot.Token
	// a key without comments:read gets no comment text from activity, inbox or search
	code, out, _ = e.req("POST", "/keys", fmt.Sprintf(`{"name":"ro","user_id":%d,"reach":["scout"],"scopes":["workspace:read","gigs:read"],"allowed_listeners":["internal"]}`, e.ids["scout.user"]), e.ada())
	e.ok(code, out, 201)
	var ro KeyCreated
	_ = json.Unmarshal(out, &ro)
	rk := cred{token: ro.Token, listener: "internal"}
	if code, _, _ := e.req("GET", "/workspaces/current/activity", "", rk); code != 403 {
		t.Errorf("workspace activity without comments:read: %d", code)
	}
	for _, p := range []string{"/inbox", "/workspaces/current/gigs?q=secret+comment", "/workspaces/current/gigs"} {
		code, out, _ := e.req("GET", p, "", rk)
		if code != 200 || bytes.Contains(out, []byte("secret comment")) || bytes.Contains(out, []byte("scout secret gig")) && strings.Contains(p, "q=") {
			t.Errorf("GET %s leaked comment text: %d %s", p, code, out)
		}
	}
	// key events are hidden from non-admin activity readers
	code, out, _ = e.req("GET", "/workspaces/current/activity", "", e.key("scout"))
	e.ok(code, out, 200)
	if bytes.Contains(out, []byte(`"key.created"`)) {
		t.Error("workspace activity shows key events to a non-admin")
	}
}

func TestForwardedForAllLines(t *testing.T) {
	e := newEnv(t)
	e.a.cfg.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}
	r := httptest.NewRequest("GET", "https://board.example/", nil)
	r.RemoteAddr = "192.0.2.10:1234"
	r.Header.Add("X-Forwarded-For", "198.51.100.7") // spoofed by the client
	r.Header.Add("X-Forwarded-For", "203.0.113.5")  // appended by the proxy (separate line)
	if ip := e.a.clientIP(r); ip != "203.0.113.5" {
		t.Fatalf("client IP %s, want the proxy-appended 203.0.113.5", ip)
	}
}

// Query filters must reach the store (huma doesn't bind embedded structs).
func TestListFiltersApply(t *testing.T) {
	e := newEnv(t)
	k := e.key("scout")
	_, _, _ = e.req("POST", fmt.Sprintf("/projects/%d/gigs", e.ids["scout.p"]), `{"title":"findme please","priority":4,"labels":["x"]}`, k)
	count := func(p string) int {
		_, out, _ := e.req("GET", p, "", k)
		var l GigList
		_ = json.Unmarshal(out, &l)
		return len(l.Items)
	}
	for p, want := range map[string]int{
		"/workspaces/current/gigs":                                  2,
		"/workspaces/current/gigs?q=findme":                         1,
		"/workspaces/current/gigs?q=zzznomatch":                     0,
		"/workspaces/current/gigs?priority_min=4":                   1,
		"/workspaces/current/gigs?label=x":                          1,
		"/workspaces/current/gigs?category=done":                    0,
		"/workspaces/current/gigs?limit=1":                          1,
		fmt.Sprintf("/projects/%d/gigs?q=findme", e.ids["scout.p"]): 1,
	} {
		if got := count(p); got != want {
			t.Errorf("%s: %d gigs, want %d", p, got, want)
		}
	}
}

func TestThemesAndAvatar(t *testing.T) {
	e := newEnv(t)
	k := e.key("scout") // has profile:write
	theme := `{"name":"Sunset","dark":true,"background":"#1A1210","surface":"#241A16","surfaceVariant":"#30221C","border":"#4A332A",
	"accent":"#FF8A3D","onAccent":"#1A0E06","text":"#FBEFE8","mutedText":"#C9AFA3","danger":"#FF5C5C","secondary":null,
	"font":"Audiowide","fontWeight":"Regular","textScalePct":100,"cornerDp":14,"borderDp":1,"evil":"x"}`
	code, out, _ := e.req("PUT", "/me/themes/custom-1", `{"theme":`+theme+`}`, k)
	e.ok(code, out, 200)
	if bytes.Contains(out, []byte("evil")) || !bytes.Contains(out, []byte(`"name":"Sunset"`)) {
		t.Fatalf("sanitised: %s", out)
	}
	if code, _, _ := e.req("PUT", "/me/themes/nightwalker", `{"theme":`+theme+`}`, k); code != 422 {
		t.Fatalf("built-in id accepted: %d", code)
	}
	if code, _, _ := e.req("PUT", "/me/themes/x", `{"theme":{"name":"x","dark":true,"background":"blue-ish"}}`, k); code != 422 {
		t.Fatalf("bad colour accepted: %d", code)
	}
	code, out, _ = e.req("PUT", "/me/themes-selection", `{"selected_id":"custom-1","match_system":true}`, k)
	e.ok(code, out, 204)
	code, out, _ = e.req("PUT", "/me/themes-selection", `{"selected_id":"Graphite","match_system":false}`, k)
	e.ok(code, out, 204)
	code, out, _ = e.req("GET", "/me/themes", "", k)
	e.ok(code, out, 200)
	if !bytes.Contains(out, []byte(`"selected_id":"nightwalker"`)) || !bytes.Contains(out, []byte(`"id":"custom-1"`)) {
		t.Fatalf("state: %s", out)
	}
	// another user's themes are invisible (they're per user)
	code, out, _ = e.req("GET", "/me/themes", "", e.ada())
	e.ok(code, out, 200)
	if bytes.Contains(out, []byte("Sunset")) {
		t.Fatal("ada sees scout's theme")
	}
	// avatar: a small PNG is re-encoded to 256 px; garbage is refused
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	r := httptest.NewRequest("PUT", "https://board.example"+Prefix+"/me/avatar", bytes.NewReader(buf.Bytes()))
	r.Header.Set("Content-Type", "image/png")
	r.Header.Set("Authorization", "Bearer "+e.keys["scout"])
	r = r.WithContext(WithInfo(r.Context(), RequestInfo{Listener: "internal"}))
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatalf("avatar upload: %d %s", w.Code, w.Body)
	}
	// admins set pictures for agents, never for another person
	adm := e.ada()
	adm.ct = "image/png"
	csrf := map[string]string{"X-Gigg-CSRF": "1"}
	if code, out, _ := e.reqH("PUT", fmt.Sprintf("/users/%d/avatar", e.ids["bob"]), buf.String(), adm, csrf); code != 403 || !strings.Contains(string(out), "their own picture") {
		t.Fatalf("admin set a person's picture: %d %s", code, out)
	}
	if code, out, _ := e.reqH("PUT", fmt.Sprintf("/users/%d/avatar", e.ids["atlas.user"]), buf.String(), adm, csrf); code != 204 {
		t.Fatalf("admin set an agent's picture: %d %s", code, out)
	}
	if code, out, _ := e.reqH("DELETE", fmt.Sprintf("/users/%d/avatar", e.ids["atlas.user"]), "", e.ada(), csrf); code != 204 {
		t.Fatalf("admin removed an agent's picture: %d %s", code, out)
	}
	code, out, hdr := e.req("GET", fmt.Sprintf("/users/%d/avatar", e.ids["scout.user"]), "", k)
	e.ok(code, out, 200)
	cfg, err := png.DecodeConfig(bytes.NewReader(out))
	if err != nil || cfg.Width != 256 || cfg.Height != 256 || hdr.Get("Content-Type") != "image/png" {
		t.Fatalf("avatar: %v %+v %s", err, cfg, hdr.Get("Content-Type"))
	}
	r = httptest.NewRequest("PUT", "https://board.example"+Prefix+"/me/avatar", strings.NewReader("<svg onload=alert(1)>"))
	r.Header.Set("Content-Type", "image/png")
	r.Header.Set("Authorization", "Bearer "+e.keys["scout"])
	r = r.WithContext(WithInfo(r.Context(), RequestInfo{Listener: "internal"}))
	w = httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	if w.Code != 422 {
		t.Fatalf("garbage avatar: %d", w.Code)
	}
	// atlas can't see scout's picture (not a co-member)
	if code, _, _ := e.req("GET", fmt.Sprintf("/users/%d/avatar", e.ids["scout.user"]), "", e.key("atlas")); code != 404 {
		t.Fatalf("cross-workspace avatar: %d", code)
	}
}

// TestCSRFOtherAllowedName: a server reached under another allowed name (its IP, a .local name) accepts
// a page served from that same name, but not a page of one name writing to the other.
func TestCSRFOtherAllowedName(t *testing.T) {
	e := newEnv(t)
	p := fmt.Sprintf("/projects/%d/gigs", e.ids["scout.p"])
	post := func(host, origin string) int {
		r := httptest.NewRequest("POST", "https://"+host+Prefix+p, strings.NewReader(`{"title":"x"}`))
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: "__Host-gigg_session", Value: e.cookie})
		r.Header.Set("Origin", origin)
		r = r.WithContext(WithInfo(r.Context(), RequestInfo{Listener: "main", RequestID: "test"}))
		w := httptest.NewRecorder()
		e.h.ServeHTTP(w, r)
		return w.Code
	}
	for _, c := range []struct {
		host, origin string
		want         int
	}{
		{"localhost:8443", "https://localhost:8443", 201}, // the other allowed name, same origin
		{"board.example", "https://board.example", 201},   // the public URL
		{"localhost:8443", "https://board.example", 201},  // the public URL's page, sent to the other name
		{"board.example", "https://localhost:8443", 403},  // a page on the other name writing to the public one
		{"evil.example", "https://evil.example", 403},     // not an allowed host
		{"localhost:8443", "https://localhost:9999", 403}, // same name, another port
		{"localhost:8443", "ftp://localhost:8443", 403},   // not a web origin
	} {
		if got := post(c.host, c.origin); got != c.want {
			t.Errorf("host %s origin %s: %d, want %d", c.host, c.origin, got, c.want)
		}
	}
}

// TestQuestionOnWaitingCards: a waiting gig carries the agent's latest question or report until
// someone answers it; archived gigs can't be resumed.
func TestQuestionOnWaitingCards(t *testing.T) {
	e := newEnv(t)
	scout := e.key("scout")
	code, out, _ := e.req("POST", fmt.Sprintf("/projects/%d/gigs", e.ids["scout.p"]), `{"title":"Pick a colour"}`, scout)
	e.ok(code, out, 201)
	var tk store.Gig
	_ = json.Unmarshal(out, &tk)
	code, out, _ = e.req("POST", "/gigs/"+tk.Key+"/move", `{"category":"waiting"}`, scout)
	e.ok(code, out, 200)
	question := func() *store.QuestionBrief {
		t.Helper()
		code, out, _ := e.req("GET", fmt.Sprintf("/projects/%d/gigs", tk.ProjectID), "", e.ada())
		e.ok(code, out, 200)
		var l GigList
		_ = json.Unmarshal(out, &l)
		for _, x := range l.Items {
			if x.ID == tk.ID {
				return x.Question
			}
		}
		t.Fatal("gig missing")
		return nil
	}
	if q := question(); q != nil {
		t.Fatalf("question before any comment: %+v", q)
	}
	code, out, _ = e.req("POST", "/gigs/"+tk.Key+"/comments", `{"body_md":"Blue or green?","kind":"report"}`, scout)
	e.ok(code, out, 201)
	if q := question(); q == nil || q.Text != "Blue or green?" || q.Kind != "report" {
		t.Fatalf("report as the question: %+v", q)
	}
	// the agent's own follow-up doesn't answer it
	code, out, _ = e.req("POST", "/gigs/"+tk.Key+"/comments", `{"body_md":"(either is fine)"}`, scout)
	e.ok(code, out, 201)
	if q := question(); q == nil || q.Text != "Blue or green?" {
		t.Fatalf("question gone after its author's follow-up: %+v", q)
	}
	code, out, _ = e.req("POST", "/gigs/"+tk.Key+"/comments", `{"body_md":"green"}`, e.ada())
	e.ok(code, out, 201)
	if q := question(); q != nil {
		t.Fatalf("answered question still shown: %+v", q)
	}
	csrf := map[string]string{"X-Gigg-CSRF": "1"}
	code, out, _ = e.reqH("POST", "/gigs/"+tk.Key+"/archive", "", e.ada(), csrf)
	e.ok(code, out, 200)
	if code, out, _ := e.req("POST", "/gigs/"+tk.Key+"/resume", `{}`, e.ada()); code != 409 {
		t.Fatalf("resume of an archived gig: %d %s", code, out)
	}
}

// A database failure while checking a session or a key is a 503 (try again), never a 401: the web
// page sends a user to the login page on a 401.
func TestAuthLookupFailureIs503(t *testing.T) {
	e := newEnv(t)
	e.st.Close()
	for name, c := range map[string]cred{"session": e.ada(), "key": e.key("scout")} {
		code, out, h := e.req("GET", "/auth/me", "", c)
		if code != 503 {
			t.Fatalf("%s: status %d, want 503: %s", name, code, out)
		}
		if h.Get("Retry-After") == "" {
			t.Fatalf("%s: no Retry-After", name)
		}
	}
}

// TestBadKeysLimited: an address that keeps sending invalid keys gets 429 before any lookup.
func TestBadKeysLimited(t *testing.T) {
	e := newEnv(t)
	bad := cred{token: "not-a-key", listener: "internal"}
	for i := 0; i < badKeyMax; i++ {
		code, out, _ := e.req("GET", "/auth/me", "", bad)
		e.ok(code, out, 401)
	}
	code, out, _ := e.req("GET", "/auth/me", "", bad)
	e.ok(code, out, 429)
}
