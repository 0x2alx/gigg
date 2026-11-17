package api

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/0x2alx/gigg/internal/store"
)

// TestConsumerKey: an agent with Work on gigs but without Edit gigs (and a key with gigs:work, not
// gigs:write) comments, moves, ticks, claims, runs and reports, and is refused every producer edit.
func TestConsumerKey(t *testing.T) {
	e := newEnv(t)
	ada := e.ada()
	ws, pid, p2 := e.ids["scout.ws"], e.ids["scout.p"], e.ids["scout.p2"]
	// a consumer agent: member, minus the producer's permissions
	code, out, _ := e.req("PUT", fmt.Sprintf("/users/%d/access", e.ids["scout.user"]), fmt.Sprintf(`{"workspace_id":%d,"preset":"member",
		"overrides":{"edit_gigs":false,"delete_gigs":false,"archive_gigs":false,"create_gigs":false}}`, ws), ada)
	e.ok(code, out, 200)
	code, out, _ = e.req("POST", "/keys", fmt.Sprintf(`{"name":"consumer","user_id":%d,"reach":["scout"],
		"scopes":["workspace:read","gigs:read","gigs:work","comments:read","comments:write","runs:write"],"allowed_listeners":["internal"]}`, e.ids["scout.user"]), ada)
	e.ok(code, out, 201)
	var kc KeyCreated
	_ = json.Unmarshal(out, &kc)
	k := cred{token: kc.Token, listener: "internal"}

	code, out, _ = e.req("POST", fmt.Sprintf("/projects/%d/gigs", pid), `{"title":"Consume me","checklist":["one","two"]}`, ada)
	e.ok(code, out, 201)
	var g store.Gig
	_ = json.Unmarshal(out, &g)
	gig := "/gigs/" + g.Key
	code, out, _ = e.req("GET", gig+"/checklist", "", k)
	e.ok(code, out, 200)
	var cl struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	_ = json.Unmarshal(out, &cl)

	// what a consumer may do
	for _, c := range []struct{ method, path, body string }{
		{"POST", gig + "/comments", `{"body_md":"on it"}`},
		{"POST", gig + "/move", `{"category":"backlog"}`},
		{"POST", gig + "/move", `{"category":"todo","position":"top"}`},
		{"PATCH", fmt.Sprintf("%s/checklist/%d", gig, cl.Items[0].ID), `{"done":true}`},
	} {
		if code, out, _ := e.req(c.method, c.path, c.body, k); code >= 300 {
			t.Fatalf("consumer %s %s: %d %s", c.method, c.path, code, out)
		}
	}
	code, out, _ = e.req("POST", "/workspaces/current/gigs/claim", `{}`, k)
	e.ok(code, out, 200)
	code, out, _ = e.req("POST", gig+"/runs", `{"mode":"new","attempt":1}`, k)
	e.ok(code, out, 201)
	var rd RunDetail
	_ = json.Unmarshal(out, &rd)
	code, out, _ = e.req("POST", fmt.Sprintf("/runs/%d/progress", rd.ID), `{"pct":50,"note":"half"}`, k)
	e.ok(code, out, 200)
	code, out, _ = e.req("POST", fmt.Sprintf("/runs/%d/finish", rd.ID), `{"status":"needs_human","report_md":"which one?","comment_kind":"question","move_to_category":"waiting"}`, k)
	e.ok(code, out, 200)
	code, out, _ = e.req("POST", gig+"/resume", `{}`, k)
	e.ok(code, out, 200)

	// what it may not: field edits, items, trash, archive, another project
	for _, c := range []struct{ method, path, body string }{
		{"PATCH", gig, `{"title":"mine now"}`},
		{"PATCH", gig, `{"description_md":"new prompt"}`},
		{"PATCH", gig, `{"model":"opus"}`},
		{"PATCH", gig, `{"allowed_tools":["Bash"]}`},
		{"PATCH", gig, `{"always_run":true}`},
		{"PATCH", gig, `{"assignees":[]}`},
		{"POST", gig + "/checklist", `{"items":["three"]}`},
		{"PATCH", fmt.Sprintf("%s/checklist/%d", gig, cl.Items[1].ID), `{"text":"changed"}`},
		{"DELETE", fmt.Sprintf("%s/checklist/%d", gig, cl.Items[1].ID), ``},
		{"DELETE", gig, ``},
		{"POST", gig + "/archive", ``},
		{"POST", gig + "/move", fmt.Sprintf(`{"project":%d}`, p2)},
		{"POST", fmt.Sprintf("/projects/%d/gigs", pid), `{"title":"new"}`},
	} {
		if code, out, _ := e.req(c.method, c.path, c.body, k); code != 403 {
			t.Fatalf("consumer %s %s %s: %d %s", c.method, c.path, c.body, code, out)
		}
	}
	// the old runner key still has gigs:write, but the user no longer has Edit gigs
	if code, out, _ := e.req("PATCH", gig, `{"title":"mine now"}`, e.key("scout")); code != 403 {
		t.Fatalf("gigs:write without Edit gigs: %d %s", code, out)
	}
	// and a producer key without gigs:work can't move
	code, out, _ = e.req("POST", "/keys", fmt.Sprintf(`{"name":"producer","user_id":%d,"reach":["atlas"],"scopes":["gigs:read","gigs:write"]}`, e.ids["atlas.user"]), ada)
	e.ok(code, out, 201)
	_ = json.Unmarshal(out, &kc)
	if code, out, _ := e.req("POST", fmt.Sprintf("/gigs/%d/move", e.ids["atlas.gig"]), `{"category":"done"}`, cred{token: kc.Token, listener: "main"}); code != 403 {
		t.Fatalf("move without gigs:work: %d %s", code, out)
	}
}

// TestOwnerAndAssigneeRules: a new gig, workspace template or recurring gig with nobody assigned gets
// the workspace's owner (creation only); a runner claims and runs only gigs assigned to it.
func TestOwnerAndAssigneeRules(t *testing.T) {
	e := newEnv(t)
	ada, k := e.ada(), e.key("scout")
	pid, scout, bob := e.ids["scout.p"], e.ids["scout.user"], e.ids["bob"]
	gigOf := func(out []byte) store.Gig {
		var g store.Gig
		_ = json.Unmarshal(out, &g)
		return g
	}
	// the owner by default, explicit assignees as given
	code, out, _ := e.req("POST", fmt.Sprintf("/projects/%d/gigs", pid), `{"title":"Owner's"}`, ada)
	e.ok(code, out, 201)
	own := gigOf(out)
	if !slices.Equal(own.Assignees, []int64{scout}) {
		t.Fatalf("default assignee: %v", own.Assignees)
	}
	code, out, _ = e.req("POST", fmt.Sprintf("/projects/%d/gigs", pid), fmt.Sprintf(`{"title":"Bob's","assignees":[%d],"priority":4}`, bob), ada)
	e.ok(code, out, 201)
	bobs := gigOf(out)
	if !slices.Equal(bobs.Assignees, []int64{bob}) {
		t.Fatalf("explicit assignee: %v", bobs.Assignees)
	}
	// the default is an assignment like any other: the owner is notified
	var n int
	_ = e.st.R.QueryRow(`SELECT count(*) FROM notifications WHERE user_id = ? AND kind = 'assigned' AND gig_id = ?`, scout, own.ID).Scan(&n)
	if n != 1 {
		t.Fatalf("%d assigned notifications for the default, want 1", n)
	}
	// later edits may leave it unassigned
	code, out, _ = e.req("PATCH", "/gigs/"+own.Key, `{"assignees":[]}`, ada)
	e.ok(code, out, 200)
	if g := gigOf(out); len(g.Assignees) != 0 {
		t.Fatalf("unassigning: %v", g.Assignees)
	}
	// claim skips Bob's (higher priority); a gig shared with bob is fine; nothing left once it's only bob's
	code, out, _ = e.req("POST", fmt.Sprintf("/gigs/%d/move", e.ids["scout.gig"]), `{"category":"done"}`, ada) // the setup's gig
	e.ok(code, out, 200)
	code, out, _ = e.req("PATCH", "/gigs/"+own.Key, fmt.Sprintf(`{"assignees":[%d,%d]}`, bob, scout), ada)
	e.ok(code, out, 200)
	code, out, _ = e.req("POST", "/workspaces/current/gigs/claim", `{"dry_run":true}`, k)
	e.ok(code, out, 200)
	if g := gigOf(out); g.ID != own.ID {
		t.Fatalf("claimed %s, want %s (shared with bob)", g.Key, own.Key)
	}
	if code, out, _ := e.req("POST", "/gigs/"+bobs.Key+"/runs", `{"mode":"new","attempt":1}`, k); code != 403 {
		t.Fatalf("run on someone else's gig: %d %s", code, out)
	}
	code, out, _ = e.req("PATCH", "/gigs/"+own.Key, fmt.Sprintf(`{"assignees":[%d]}`, bob), ada)
	e.ok(code, out, 200)
	if code, out, _ := e.req("POST", "/workspaces/current/gigs/claim", `{}`, k); code != 204 {
		t.Fatalf("claim with nothing assigned: %d %s", code, out)
	}

	// templates: a workspace one gets the owner; a recurring gig on a shared one gets it as its own
	code, out, _ = e.req("POST", fmt.Sprintf("/workspaces/%d/templates", e.ids["scout.ws"]), `{"name":"t","title":"t"}`, ada)
	e.ok(code, out, 201)
	var tpl store.Template
	_ = json.Unmarshal(out, &tpl)
	if !slices.Equal(tpl.Assignees, []int64{scout}) {
		t.Fatalf("template default: %v", tpl.Assignees)
	}
	code, out, _ = e.req("POST", "/shared/templates", `{"name":"s","title":"s"}`, ada)
	e.ok(code, out, 201)
	_ = json.Unmarshal(out, &tpl)
	if len(tpl.Assignees) != 0 {
		t.Fatalf("shared template got assignees: %v", tpl.Assignees)
	}
	code, out, _ = e.req("POST", fmt.Sprintf("/projects/%d/recurrences", pid), fmt.Sprintf(`{"template_id":%d,"freq":"daily","tz":"UTC","window_start":"09:00","window_minutes":60}`, tpl.ID), ada)
	e.ok(code, out, 201)
	var rec store.Recurrence
	_ = json.Unmarshal(out, &rec)
	if !slices.Equal(rec.Assignees, []int64{scout}) || !rec.AssigneesOwn {
		t.Fatalf("recurring gig on a shared template: %v own=%v", rec.Assignees, rec.AssigneesOwn)
	}
}
