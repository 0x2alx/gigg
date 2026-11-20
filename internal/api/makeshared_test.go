package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// shareEnv: helpers for the Make shared tests.
type shareEnv struct {
	*env
	t *testing.T
}

func (s shareEnv) post(path, body string, c cred, want int) []byte {
	s.t.Helper()
	code, out, _ := s.req("POST", path, body, c)
	s.ok(code, out, want)
	return out
}

func idOf(t *testing.T, out []byte) int64 {
	t.Helper()
	var x struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(out, &x); err != nil || x.ID == 0 {
		t.Fatalf("no id in %s", out)
	}
	return x.ID
}

type planOut struct {
	DryRun bool `json:"dry_run"`
	Source struct {
		ID int64 `json:"id"`
	} `json:"source"`
	Shared *struct {
		ID int64 `json:"id"`
	} `json:"shared"`
	Copies []struct {
		ID        int64  `json:"id"`
		Workspace string `json:"workspace"`
		Name      string `json:"name"`
	} `json:"copies"`
	Skipped []struct {
		ID     int64  `json:"id"`
		Reason string `json:"reason"`
	} `json:"skipped"`
	Users []struct {
		Kind string `json:"kind"`
		ID   int64  `json:"id"`
		From int64  `json:"from"`
	} `json:"users"`
	Moved   int `json:"moved"`
	Deleted int `json:"deleted"`
}

func readPlan(t *testing.T, out []byte) planOut {
	t.Helper()
	var p planOut
	if err := json.Unmarshal(out, &p); err != nil {
		t.Fatalf("plan: %v: %s", err, out)
	}
	return p
}

// actions counts the audit rows of an action.
func (e *env) actions(action string) int {
	e.t.Helper()
	var n int
	if err := e.st.R.QueryRowContext(context.Background(), `SELECT count(*) FROM activity WHERE action = ?`, action).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// snap counts the audit rows by action; delta is how many of an action came since.
func (e *env) snap() map[string]int {
	e.t.Helper()
	rows, err := e.st.R.QueryContext(context.Background(), `SELECT action, count(*) FROM activity GROUP BY action`)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var a string
		var n int
		_ = rows.Scan(&a, &n)
		out[a] = n
	}
	return out
}

func (e *env) delta(base map[string]int, action string) int { return e.actions(action) - base[action] }

func (e *env) field(path string, c cred, key string) int64 {
	e.t.Helper()
	code, out, _ := e.req("GET", path, "", c)
	e.ok(code, out, 200)
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	f, _ := m[key].(float64)
	return int64(f)
}

const daily7 = `"freq":"daily","tz":"UTC","window_start":"07:00","window_minutes":60`

// TestMakeSharedSchedule: the plan finds exact copies (in any workspace, the source's too) and skips
// near-misses; a dry run changes nothing; users move, the source and the copies are deleted, and the
// audit log shows the create, each repoint and each delete.
func TestMakeSharedSchedule(t *testing.T) {
	e := newEnv(t)
	s := shareEnv{e, t}
	ada := e.ada()
	scout, atlas := fmt.Sprint(e.ids["scout.ws"]), fmt.Sprint(e.ids["atlas.ws"])
	src := idOf(t, s.post("/workspaces/"+scout+"/schedules", `{"name":"Mornings",`+daily7+`}`, ada, 201))
	same := idOf(t, s.post("/workspaces/"+scout+"/schedules", `{"name":"Mornings again",`+daily7+`}`, ada, 201))
	copyB := idOf(t, s.post("/workspaces/"+atlas+"/schedules", `{"name":"Atlas mornings",`+daily7+`}`, ada, 201))
	near := idOf(t, s.post("/workspaces/"+atlas+"/schedules", `{"name":"Longer",`+strings.Replace(daily7, `"window_minutes":60`, `"window_minutes":90`, 1)+`}`, ada, 201))
	rec1 := idOf(t, s.post(fmt.Sprintf("/projects/%d/recurrences", e.ids["scout.p"]), fmt.Sprintf(`{"title":"One","schedule_id":%d}`, src), ada, 201))
	rec2 := idOf(t, s.post(fmt.Sprintf("/projects/%d/recurrences", e.ids["atlas.p"]), fmt.Sprintf(`{"title":"Two","schedule_id":%d}`, copyB), ada, 201))
	rec3 := idOf(t, s.post(fmt.Sprintf("/projects/%d/recurrences", e.ids["atlas.p"]), fmt.Sprintf(`{"title":"Three","schedule_id":%d}`, near), ada, 201))
	errand := idOf(t, s.post("/workspaces/"+atlas+"/errands", fmt.Sprintf(`{"name":"ping","command":"true","worker":"atlas","schedule_id":%d}`, copyB), ada, 201))

	// dry run: the plan only
	base0 := e.snap()
	p := readPlan(t, s.post(fmt.Sprintf("/schedules/%d/make-shared", src), `{"dry_run":true}`, ada, 200))
	if !p.DryRun || len(p.Copies) != 2 || len(p.Skipped) != 0 || p.Moved != 3 || p.Deleted != 3 || p.Shared != nil {
		t.Fatalf("plan: %+v", p)
	}
	for _, c := range p.Copies {
		if c.ID == near {
			t.Fatalf("the near-miss is a copy: %+v", p)
		}
		if c.Workspace == "" || c.Name == "" {
			t.Fatalf("copy without workspace or name: %+v", c)
		}
	}
	for _, id := range []int64{src, same, copyB, near} {
		if code, out, _ := e.req("GET", fmt.Sprintf("/schedules/%d", id), "", ada); code != 200 {
			t.Fatalf("the dry run touched schedule %d: %d %s", id, code, out)
		}
	}
	if got := e.snap(); got["schedule.deleted"] != 0 || got["schedule.made_shared"] != 0 || got["recurrence.updated"] != base0["recurrence.updated"] {
		t.Fatal("the dry run wrote audit rows")
	}

	// the move
	base := e.snap()
	p = readPlan(t, s.post(fmt.Sprintf("/schedules/%d/make-shared", src), `{}`, ada, 200))
	if p.DryRun || p.Shared == nil || p.Moved != 3 || p.Deleted != 3 {
		t.Fatalf("result: %+v", p)
	}
	sh := p.Shared.ID
	for _, id := range []int64{src, same, copyB} {
		if code, _, _ := e.req("GET", fmt.Sprintf("/schedules/%d", id), "", ada); code != 404 {
			t.Fatalf("schedule %d is still there: %d", id, code)
		}
	}
	if code, _, _ := e.req("GET", fmt.Sprintf("/schedules/%d", near), "", ada); code != 200 {
		t.Fatal("the near-miss was deleted")
	}
	for _, id := range []int64{rec1, rec2} {
		if got := e.field(fmt.Sprintf("/recurrences/%d", id), ada, "schedule_id"); got != sh {
			t.Fatalf("recurring gig %d is on schedule %d, want %d", id, got, sh)
		}
	}
	if got := e.field(fmt.Sprintf("/recurrences/%d", rec3), ada, "schedule_id"); got != near {
		t.Fatalf("the near-miss's recurring gig moved: %d", got)
	}
	if got := e.field(fmt.Sprintf("/errands/%d", errand), ada, "schedule_id"); got != sh {
		t.Fatalf("errand on schedule %d, want %d", got, sh)
	}
	code, out, _ := e.req("GET", "/shared/schedules", "", ada)
	e.ok(code, out, 200)
	var shared struct {
		Items []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
			Uses int64  `json:"uses"`
		} `json:"items"`
	}
	_ = json.Unmarshal(out, &shared)
	if len(shared.Items) != 1 || shared.Items[0].ID != sh || shared.Items[0].Name != "Mornings" || shared.Items[0].Uses != 2 {
		t.Fatalf("shared schedules: %s", out)
	}
	// audit: the create, each repoint, each delete, and one summary row
	for action, want := range map[string]int{"schedule.created": 1, "schedule.deleted": 3, "recurrence.updated": 2, "errand.updated": 1, "schedule.made_shared": 1} {
		if got := e.delta(base, action); got != want {
			t.Fatalf("audit %s: %d rows, want %d", action, got, want)
		}
	}
	// a shared schedule can't be made shared again
	if code, _, _ := e.req("POST", fmt.Sprintf("/schedules/%d/make-shared", sh), `{}`, ada); code != 422 {
		t.Fatalf("shared again: %d", code)
	}
}

// TestMakeSharedPermissions: Shared library and library:write; a copy in a workspace where the caller
// can't remove it is skipped (and kept, with its users); the source needs the same permission; the
// risky settings are asked on the all-workspaces level.
func TestMakeSharedPermissions(t *testing.T) {
	e := newEnv(t)
	s := shareEnv{e, t}
	ada, bob := e.ada(), e.ids["bob"]
	scoutID, atlasID := e.ids["scout.ws"], e.ids["atlas.ws"]
	scout, atlas := fmt.Sprint(scoutID), fmt.Sprint(atlasID)
	rule := func(level, body string) {
		code, out, _ := e.req("PUT", fmt.Sprintf("/users/%d/access", bob), fmt.Sprintf(`{"workspace_id":%s,%s}`, level, body), ada)
		e.ok(code, out, 200)
	}
	mkKey := func(name, scopes string) cred {
		code, out, _ := e.req("POST", "/keys", fmt.Sprintf(`{"name":%q,"user_id":%d,"scopes":[%s],"allowed_listeners":["internal"]}`, name, bob, scopes), ada)
		e.ok(code, out, 201)
		var kc KeyCreated
		_ = json.Unmarshal(out, &kc)
		return cred{token: kc.Token, listener: "internal"}
	}
	write := `"gigs:read","gigs:write","errands:read","errands:write","workspace:read"`
	lib := mkKey("lib", write+`,"library:write"`)
	noScope := mkKey("noscope", write)

	src := idOf(t, s.post("/workspaces/"+scout+"/schedules", `{"name":"Mornings",`+daily7+`}`, ada, 201))
	copyB := idOf(t, s.post("/workspaces/"+atlas+"/schedules", `{"name":"Atlas mornings",`+daily7+`}`, ada, 201))
	rec := idOf(t, s.post(fmt.Sprintf("/projects/%d/recurrences", e.ids["atlas.p"]), fmt.Sprintf(`{"title":"Two","schedule_id":%d}`, copyB), ada, 201))
	path := fmt.Sprintf("/schedules/%d/make-shared", src)

	// no Shared library: refused, dry run too
	rule("null", `"preset":"viewer"`)
	rule(scout, `"preset":"manager"`)
	for _, body := range []string{`{}`, `{"dry_run":true}`} {
		if code, out, _ := e.req("POST", path, body, lib); code != 403 {
			t.Fatalf("without Shared library %s: %d %s", body, code, out)
		}
	}
	rule("null", `"preset":"viewer","overrides":{"shared_library":true}`)
	// no library:write on the key
	if code, out, _ := e.req("POST", path, `{}`, noScope); code != 403 || !strings.Contains(string(out), "library:write") {
		t.Fatalf("without library:write: %d %s", code, out)
	}
	// a source in a workspace where the caller can't delete it
	if code, out, _ := e.req("POST", fmt.Sprintf("/schedules/%d/make-shared", copyB), `{"dry_run":true}`, lib); code != 403 {
		t.Fatalf("source without the workspace permission: %d %s", code, out)
	}

	// the copy in atlas (bob is a viewer there) is skipped, and stays with its user
	p := readPlan(t, s.post(path, `{"dry_run":true}`, lib, 200))
	if len(p.Copies) != 0 || len(p.Skipped) != 1 || p.Skipped[0].ID != copyB || p.Skipped[0].Reason == "" || p.Moved != 0 {
		t.Fatalf("plan: %+v", p)
	}

	// risky settings: an always-run recurring gig on the source needs always_run on the all-workspaces level
	always := idOf(t, s.post(fmt.Sprintf("/projects/%d/recurrences", e.ids["scout.p"]), fmt.Sprintf(`{"title":"Cron","schedule_id":%d,"always_run":true}`, src), ada, 201))
	rule(scout, `"preset":"manager","overrides":{"always_run":true}`) // a workspace's grant doesn't count
	for _, body := range []string{`{"dry_run":true}`, `{}`} {
		if code, out, _ := e.req("POST", path, body, lib); code != 403 || !strings.Contains(string(out), "always_run") {
			t.Fatalf("always-run %s: %d %s", body, code, out)
		}
	}
	if code, _, _ := e.req("GET", fmt.Sprintf("/schedules/%d", src), "", ada); code != 200 {
		t.Fatal("a refused move deleted the source")
	}
	rule("null", `"preset":"viewer","overrides":{"shared_library":true,"always_run":true}`)
	p = readPlan(t, s.post(path, `{}`, lib, 200))
	if p.Shared == nil || p.Moved != 1 || len(p.Skipped) != 1 {
		t.Fatalf("result: %+v", p)
	}
	if got := e.field(fmt.Sprintf("/recurrences/%d", always), ada, "schedule_id"); got != p.Shared.ID {
		t.Fatalf("always-run recurring gig is on %d", got)
	}
	if got := e.field(fmt.Sprintf("/recurrences/%d", rec), ada, "schedule_id"); got != copyB {
		t.Fatalf("the skipped copy's user moved: %d", got)
	}
	if code, _, _ := e.req("GET", fmt.Sprintf("/schedules/%d", copyB), "", ada); code != 200 {
		t.Fatal("the skipped copy was deleted")
	}
}

// TestMakeSharedTemplate: gig templates: the same content in another workspace is merged (the name and the
// assignees don't count, a different description does); recurring gigs keep their assignees; a template
// with a project, custom values or followers can't be shared; always-run needs its permission.
func TestMakeSharedTemplate(t *testing.T) {
	e := newEnv(t)
	s := shareEnv{e, t}
	ada := e.ada()
	scout, atlas := fmt.Sprint(e.ids["scout.ws"]), fmt.Sprint(e.ids["atlas.ws"])
	body := func(name, desc string, extra string) string {
		return fmt.Sprintf(`{"name":%q,"title":"Weekly review","description_md":%q,"priority":2,"labels":["ops"],"checklist":["a","b"]%s}`, name, desc, extra)
	}
	src := idOf(t, s.post("/workspaces/"+scout+"/templates", body("Review", "do it", fmt.Sprintf(`,"assignees":[%d]`, e.ids["scout.user"])), ada, 201))
	copyB := idOf(t, s.post("/workspaces/"+atlas+"/templates", body("Atlas review", "do it", ""), ada, 201))
	near := idOf(t, s.post("/workspaces/"+atlas+"/templates", body("Review", "do it differently", ""), ada, 201))
	rec1 := idOf(t, s.post(fmt.Sprintf("/projects/%d/recurrences", e.ids["scout.p"]), fmt.Sprintf(`{"template_id":%d,"freq":"daily"}`, src), ada, 201))
	rec2 := idOf(t, s.post(fmt.Sprintf("/projects/%d/recurrences", e.ids["atlas.p"]), fmt.Sprintf(`{"template_id":%d,"freq":"daily"}`, copyB), ada, 201))

	p := readPlan(t, s.post(fmt.Sprintf("/templates/%d/make-shared", src), `{"dry_run":true}`, ada, 200))
	if len(p.Copies) != 1 || p.Copies[0].ID != copyB || p.Moved != 2 {
		t.Fatalf("plan: %+v", p)
	}
	base := e.snap()
	p = readPlan(t, s.post(fmt.Sprintf("/templates/%d/make-shared", src), `{}`, ada, 200))
	sh := p.Shared.ID
	for _, id := range []int64{src, copyB} {
		if code, _, _ := e.req("GET", fmt.Sprintf("/templates/%d", id), "", ada); code != 404 {
			t.Fatalf("template %d is still there: %d", id, code)
		}
	}
	if code, _, _ := e.req("GET", fmt.Sprintf("/templates/%d", near), "", ada); code != 200 {
		t.Fatal("the near-miss was deleted")
	}
	for _, id := range []int64{rec1, rec2} {
		if got := e.field(fmt.Sprintf("/recurrences/%d", id), ada, "template_id"); got != sh {
			t.Fatalf("recurring gig %d is on template %d, want %d", id, got, sh)
		}
	}
	// the assignees the first recurring gig took from its template stay with it
	code, out, _ := e.req("GET", fmt.Sprintf("/recurrences/%d", rec1), "", ada)
	e.ok(code, out, 200)
	var r struct {
		Assignees []int64 `json:"assignees"`
	}
	_ = json.Unmarshal(out, &r)
	if len(r.Assignees) != 1 || r.Assignees[0] != e.ids["scout.user"] {
		t.Fatalf("assignees after the move: %s", out)
	}
	for action, want := range map[string]int{"template.created": 1, "template.deleted": 2, "recurrence.updated": 2, "template.made_shared": 1} {
		if got := e.delta(base, action); got != want {
			t.Fatalf("audit %s: %d rows, want %d", action, got, want)
		}
	}

	// what a shared template can't hold
	proj := idOf(t, s.post("/workspaces/"+scout+"/templates", fmt.Sprintf(`{"name":"P","title":"p","project_id":%d}`, e.ids["scout.p"]), ada, 201))
	if code, out, _ := e.req("POST", fmt.Sprintf("/templates/%d/make-shared", proj), `{}`, ada); code != 422 {
		t.Fatalf("a project's template: %d %s", code, out)
	}
	fol := idOf(t, s.post("/workspaces/"+scout+"/templates", fmt.Sprintf(`{"name":"F","title":"f","followers":[%d]}`, e.ids["bob"]), ada, 201))
	if code, out, _ := e.req("POST", fmt.Sprintf("/templates/%d/make-shared", fol), `{}`, ada); code != 422 {
		t.Fatalf("a template with followers: %d %s", code, out)
	}

	// always-run: Shared library alone isn't enough
	bob := e.ids["bob"]
	lib := cred{}
	code, out, _ = e.req("POST", "/keys", fmt.Sprintf(`{"name":"lib","user_id":%d,"scopes":["gigs:read","gigs:write","library:write"],"allowed_listeners":["internal"]}`, bob), ada)
	e.ok(code, out, 201)
	var kc KeyCreated
	_ = json.Unmarshal(out, &kc)
	lib = cred{token: kc.Token, listener: "internal"}
	code, out, _ = e.req("PUT", fmt.Sprintf("/users/%d/access", bob), `{"workspace_id":null,"preset":"viewer","overrides":{"shared_library":true}}`, ada)
	e.ok(code, out, 200)
	code, out, _ = e.req("PUT", fmt.Sprintf("/users/%d/access", bob), fmt.Sprintf(`{"workspace_id":%s,"preset":"manager","overrides":{"always_run":true}}`, scout), ada)
	e.ok(code, out, 200)
	run := idOf(t, s.post("/workspaces/"+scout+"/templates", `{"name":"Cron","title":"cron","always_run":true}`, ada, 201))
	if code, out, _ := e.req("POST", fmt.Sprintf("/templates/%d/make-shared", run), `{"dry_run":true}`, lib); code != 403 || !strings.Contains(string(out), "always_run") {
		t.Fatalf("always-run template: %d %s", code, out)
	}
}

// TestMakeSharedErrandTemplate: errand templates: the command, timeout, output and sandbox are the
// content; the errands of the source and the copies move; a no-sandbox template needs No-sandbox errands
// on the all-workspaces level.
func TestMakeSharedErrandTemplate(t *testing.T) {
	e := newEnv(t)
	s := shareEnv{e, t}
	ada := e.ada()
	scout, atlas := fmt.Sprint(e.ids["scout.ws"]), fmt.Sprint(e.ids["atlas.ws"])
	src := idOf(t, s.post("/workspaces/"+scout+"/errand-templates", `{"name":"ping","command":"echo hi"}`, ada, 201))
	copyB := idOf(t, s.post("/workspaces/"+atlas+"/errand-templates", `{"name":"atlas ping","command":"echo hi"}`, ada, 201))
	near := idOf(t, s.post("/workspaces/"+atlas+"/errand-templates", `{"name":"ping","command":"echo hello"}`, ada, 201))
	sch := `"rule":{"freq":"daily"}`
	e1 := idOf(t, s.post("/workspaces/"+scout+"/errands", fmt.Sprintf(`{"template_id":%d,"worker":"scout",%s}`, src, sch), ada, 201))
	e2 := idOf(t, s.post("/workspaces/"+atlas+"/errands", fmt.Sprintf(`{"template_id":%d,"worker":"atlas",%s}`, copyB, sch), ada, 201))

	p := readPlan(t, s.post(fmt.Sprintf("/errand-templates/%d/make-shared", src), `{"dry_run":true}`, ada, 200))
	if len(p.Copies) != 1 || p.Copies[0].ID != copyB || p.Moved != 2 {
		t.Fatalf("plan: %+v", p)
	}
	base := e.snap()
	p = readPlan(t, s.post(fmt.Sprintf("/errand-templates/%d/make-shared", src), `{}`, ada, 200))
	sh := p.Shared.ID
	for _, id := range []int64{src, copyB} {
		if code, _, _ := e.req("GET", fmt.Sprintf("/errand-templates/%d", id), "", ada); code != 404 {
			t.Fatalf("errand template %d is still there: %d", id, code)
		}
	}
	if code, _, _ := e.req("GET", fmt.Sprintf("/errand-templates/%d", near), "", ada); code != 200 {
		t.Fatal("the near-miss was deleted")
	}
	for _, id := range []int64{e1, e2} {
		if got := e.field(fmt.Sprintf("/errands/%d", id), ada, "template_id"); got != sh {
			t.Fatalf("errand %d is on template %d, want %d", id, got, sh)
		}
	}
	for action, want := range map[string]int{"errand_template.created": 1, "errand_template.deleted": 2, "errand.updated": 2, "errand_template.made_shared": 1} {
		if got := e.delta(base, action); got != want {
			t.Fatalf("audit %s: %d rows, want %d", action, got, want)
		}
	}
	// the shared copy keeps who set the command
	code, out, _ := e.req("GET", fmt.Sprintf("/errand-templates/%d", sh), "", ada)
	e.ok(code, out, 200)
	if !strings.Contains(string(out), `"shared":true`) || !strings.Contains(string(out), "echo hi") {
		t.Fatalf("shared template: %s", out)
	}

	// no-sandbox and a non-default timeout: permissions on the all-workspaces level
	bob := e.ids["bob"]
	code, out, _ = e.req("POST", "/keys", fmt.Sprintf(`{"name":"lib","user_id":%d,"scopes":["errands:read","errands:write","library:write"],"allowed_listeners":["internal"]}`, bob), ada)
	e.ok(code, out, 201)
	var kc KeyCreated
	_ = json.Unmarshal(out, &kc)
	lib := cred{token: kc.Token, listener: "internal"}
	for _, r := range []string{`{"workspace_id":null,"preset":"viewer","overrides":{"shared_library":true}}`,
		fmt.Sprintf(`{"workspace_id":%s,"preset":"manager","overrides":{"errand_unsandboxed":true,"run_timeout":true}}`, scout)} {
		code, out, _ = e.req("PUT", fmt.Sprintf("/users/%d/access", bob), r, ada)
		e.ok(code, out, 200)
	}
	bare := idOf(t, s.post("/workspaces/"+scout+"/errand-templates", `{"name":"bare","command":"true","unsandboxed":true}`, ada, 201))
	slow := idOf(t, s.post("/workspaces/"+scout+"/errand-templates", `{"name":"slow","command":"true","timeout_s":600}`, ada, 201))
	for _, id := range []int64{bare, slow} {
		if code, out, _ := e.req("POST", fmt.Sprintf("/errand-templates/%d/make-shared", id), `{"dry_run":true}`, lib); code != 403 {
			t.Fatalf("risky errand template %d: %d %s", id, code, out)
		}
	}
}

// TestMakeSharedCollision: two recurring gigs of one project with the same template, told apart only by the
// identical schedule they use, would become the same recurring gig: the move is refused and nothing changes.
func TestMakeSharedCollision(t *testing.T) {
	e := newEnv(t)
	s := shareEnv{e, t}
	ada := e.ada()
	scout := fmt.Sprint(e.ids["scout.ws"])
	a := idOf(t, s.post("/workspaces/"+scout+"/schedules", `{"name":"A",`+daily7+`}`, ada, 201))
	b := idOf(t, s.post("/workspaces/"+scout+"/schedules", `{"name":"B",`+daily7+`}`, ada, 201))
	tpl := idOf(t, s.post("/workspaces/"+scout+"/templates", `{"name":"T","title":"t"}`, ada, 201))
	proj := fmt.Sprintf("/projects/%d/recurrences", e.ids["scout.p"])
	s.post(proj, fmt.Sprintf(`{"template_id":%d,"schedule_id":%d}`, tpl, a), ada, 201)
	s.post(proj, fmt.Sprintf(`{"template_id":%d,"schedule_id":%d}`, tpl, b), ada, 201)
	for _, body := range []string{`{"dry_run":true}`, `{}`} {
		code, out, _ := e.req("POST", fmt.Sprintf("/schedules/%d/make-shared", a), body, ada)
		if code != 409 {
			t.Fatalf("collision %s: %d %s", body, code, out)
		}
	}
	for _, id := range []int64{a, b} {
		if code, _, _ := e.req("GET", fmt.Sprintf("/schedules/%d", id), "", ada); code != 200 {
			t.Fatalf("schedule %d is gone after a refused move", id)
		}
	}
}
