package api

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/0x2alx/gigg/internal/access"
	"github.com/0x2alx/gigg/internal/store"
)

// TestMakeSharedUserProjects: what Make shared repoints is checked in the project of each recurring gig
// and errand, as editing it by hand would be. A copy with a user the caller can't edit is skipped; a
// source with one is refused.
func TestMakeSharedUserProjects(t *testing.T) {
	l := newLibEnv(t)
	ada := l.ada()
	lib := l.mkKey("lib", "gigs:write", "errands:write")
	scout, atlas := fmt.Sprint(l.ids["scout.ws"]), fmt.Sprint(l.ids["atlas.ws"])
	scoutP, atlasP2 := l.ids["scout.p"], l.ids["atlas.p2"]
	proj := func(ws string, p int64, body string) {
		l.rule(ws, fmt.Sprintf(`"project_id":%d,"overrides":%s`, p, body))
	}

	// recurring gigs: Edit gigs and Create gigs in the project (Recurring gigs is a workspace's permission:
	// a copy in a workspace without it is already skipped)
	for i, perm := range []string{"edit_gigs", "create_gigs"} {
		timing := strings.Replace(daily7, "07:00", fmt.Sprintf("1%d:00", i), 1) // told apart from the other rounds
		src := idOf(t, l.post("/workspaces/"+scout+"/schedules", `{"name":"S `+perm+`",`+timing+`}`, ada, 201))
		copyB := idOf(t, l.post("/workspaces/"+atlas+"/schedules", `{"name":"C `+perm+`",`+timing+`}`, ada, 201))
		rec := idOf(t, l.post(fmt.Sprintf("/projects/%d/recurrences", atlasP2), fmt.Sprintf(`{"title":"R","schedule_id":%d}`, copyB), ada, 201))
		path := fmt.Sprintf("/schedules/%d/make-shared", src)
		p := readPlan(t, l.post(path, `{"dry_run":true}`, lib, 200))
		if len(p.Copies) != 1 {
			t.Fatalf("%s: with every permission: %+v", perm, p)
		}
		proj(atlas, atlasP2, fmt.Sprintf(`{%q:false}`, perm))
		for _, body := range []string{`{"dry_run":true}`, `{}`} {
			p = readPlan(t, l.post(path, body, lib, 200))
			if len(p.Copies) != 0 || len(p.Skipped) != 1 || p.Skipped[0].ID != copyB || p.Skipped[0].Reason == "" || p.Moved != 0 {
				t.Fatalf("%s %s: plan: %+v", perm, body, p)
			}
		}
		if got := l.field(fmt.Sprintf("/recurrences/%d", rec), ada, "schedule_id"); got != copyB {
			t.Fatalf("%s: the skipped copy's recurring gig moved: %d", perm, got)
		}
		if code, _ := l.status("GET", fmt.Sprintf("/schedules/%d", copyB), "", ada); code != 200 {
			t.Fatalf("%s: the skipped copy was deleted", perm)
		}
		proj(atlas, atlasP2, `{}`)
	}

	// errands: Errands in the project
	esrc := idOf(t, l.post("/workspaces/"+scout+"/errand-templates", `{"name":"ping","command":"echo hi"}`, ada, 201))
	ecopy := idOf(t, l.post("/workspaces/"+atlas+"/errand-templates", `{"name":"atlas ping","command":"echo hi"}`, ada, 201))
	er := idOf(t, l.post("/workspaces/"+atlas+"/errands", fmt.Sprintf(`{"template_id":%d,"worker":"atlas","project":%d,"rule":{"freq":"daily"}}`, ecopy, atlasP2), ada, 201))
	epath := fmt.Sprintf("/errand-templates/%d/make-shared", esrc)
	if p := readPlan(t, l.post(epath, `{"dry_run":true}`, lib, 200)); len(p.Copies) != 1 {
		t.Fatalf("errands: with the permission: %+v", p)
	}
	proj(atlas, atlasP2, `{"errands":false}`)
	for _, body := range []string{`{"dry_run":true}`, `{}`} {
		p := readPlan(t, l.post(epath, body, lib, 200))
		if len(p.Copies) != 0 || len(p.Skipped) != 1 || p.Skipped[0].ID != ecopy || p.Moved != 0 {
			t.Fatalf("errands %s: plan: %+v", body, p)
		}
	}
	if got := l.field(fmt.Sprintf("/errands/%d", er), ada, "template_id"); got != ecopy {
		t.Fatalf("the skipped copy's errand moved: %d", got)
	}
	proj(atlas, atlasP2, `{}`)

	// the source's own users: refused (403), nothing changes
	ssrc := idOf(t, l.post("/workspaces/"+scout+"/schedules", `{"name":"Own",`+daily7+`}`, ada, 201))
	l.post(fmt.Sprintf("/projects/%d/recurrences", scoutP), fmt.Sprintf(`{"title":"Mine","schedule_id":%d}`, ssrc), ada, 201)
	proj(scout, scoutP, `{"edit_gigs":false}`)
	for _, body := range []string{`{"dry_run":true}`, `{}`} {
		if code, out := l.status("POST", fmt.Sprintf("/schedules/%d/make-shared", ssrc), body, lib); code != 403 {
			t.Fatalf("a source user the caller can't edit %s: %d %s", body, code, out)
		}
	}
	if code, _ := l.status("GET", fmt.Sprintf("/schedules/%d", ssrc), "", ada); code != 200 {
		t.Fatal("a refused move deleted the source")
	}
}

// TestMakeSharedHiddenProject: a project hidden from the caller takes Shared library away, and as a
// second guard the plan leaves a copy alone when its users sit in a project the caller can't see.
func TestMakeSharedHiddenProject(t *testing.T) {
	l := newLibEnv(t)
	ada := l.ada()
	scout, atlas := fmt.Sprint(l.ids["scout.ws"]), fmt.Sprint(l.ids["atlas.ws"])
	src := idOf(t, l.post("/workspaces/"+scout+"/schedules", `{"name":"Mornings",`+daily7+`}`, ada, 201))
	copyB := idOf(t, l.post("/workspaces/"+atlas+"/schedules", `{"name":"Atlas",`+daily7+`}`, ada, 201))
	l.post(fmt.Sprintf("/projects/%d/recurrences", l.ids["atlas.p2"]), fmt.Sprintf(`{"title":"R","schedule_id":%d}`, copyB), ada, 201)

	lib := l.mkKey("lib", "gigs:write")
	l.post(fmt.Sprintf("/schedules/%d/make-shared", src), `{"dry_run":true}`, lib, 200)
	l.rule(atlas, fmt.Sprintf(`"project_id":%d,"overrides":{"view":false}`, l.ids["atlas.p2"]))
	if code, out := l.status("POST", fmt.Sprintf("/schedules/%d/make-shared", src), `{"dry_run":true}`, lib); code != 403 {
		t.Fatalf("a hidden project: %d %s", code, out)
	}
	if code, out := l.status("POST", "/shared/schedules", `{"name":"X",`+daily7+`}`, lib); code != 403 {
		t.Fatalf("a hidden project, shared schedule: %d %s", code, out)
	}
	// the plan itself, asked for directly
	ctx := context.Background()
	u, err := store.GetUser(ctx, l.st.R, l.bob)
	if err != nil {
		t.Fatal(err)
	}
	p, err := access.Load(ctx, l.st.R, u, nil, 0, access.Request{})
	if err != nil {
		t.Fatal(err)
	}
	plan, _, err := l.a.sharePlan(ctx, l.st.R, p, store.ShareSchedule, src)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Copies) != 0 || len(plan.Skipped) != 1 || plan.Skipped[0].ID != copyB || plan.Skipped[0].Reason == "" {
		t.Fatalf("plan with a hidden project: %+v", plan)
	}
}
