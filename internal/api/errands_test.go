package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/0x2alx/gigg/internal/store"
)

// TestErrandsAPI: an agent with the Errands permission makes its own errands (not someone else's, and not
// unsandboxed); its executor claims and reports only its own runs; a host-output errand never stores
// output; output needs errands:output; PAUSE has two boxes; a schedule used by errands needs Errands to
// retime; recurring gigs refuse minute schedules.
func TestErrandsAPI(t *testing.T) {
	e := newEnv(t)
	defer func(n func() int64) { store.Now = n }(store.Now)
	ada := e.ada()
	ws := e.ids["scout.ws"]
	key := func(user string, scopes string) cred {
		t.Helper()
		code, out, _ := e.req("POST", "/keys", fmt.Sprintf(`{"name":"errand-%s","user_id":%d,"workspace":"%s","scopes":[%s],"allowed_listeners":["internal"]}`,
			user, e.ids[user+".user"], user, scopes), ada)
		e.ok(code, out, 201)
		var kc KeyCreated
		_ = json.Unmarshal(out, &kc)
		return cred{token: kc.Token, listener: "internal"}
	}
	full := `"errands:read","errands:write","errands:run","errands:output","gigs:read","gigs:work","gigs:write"`
	scout := key("scout", full)
	noOut := key("scout", `"errands:read"`)
	atlas := key("atlas", full)

	// no Errands permission yet
	body := `{"name":"disk","command":"df -h /","rule":{"freq":"minutes","interval":5,"tz":"UTC"}}`
	code, out, _ := e.req("POST", "/workspaces/current/errands", body, scout)
	if code != 403 || !strings.Contains(string(out), "errands") {
		t.Fatalf("without Errands: %d %s", code, out)
	}
	code, out, _ = e.req("PUT", fmt.Sprintf("/users/%d/access", e.ids["scout.user"]), fmt.Sprintf(`{"workspace_id":%d,"preset":"member","overrides":{"errands":true}}`, ws), ada)
	e.ok(code, out, 200)
	code, out, _ = e.req("POST", "/workspaces/current/errands", body, scout)
	e.ok(code, out, 201)
	var c store.Errand
	_ = json.Unmarshal(out, &c)
	if c.WorkerName != "scout" || c.ScheduleText != "Every 5 min" || c.State != "never" || c.NextAt == nil {
		t.Fatalf("created: %s", out)
	}
	// not unsandboxed, not for another agent
	code, out, _ = e.req("POST", "/workspaces/current/errands", `{"name":"x","command":"true","unsandboxed":true,"rule":{"freq":"hours","tz":"UTC"}}`, scout)
	if code != 403 || !strings.Contains(string(out), "errand_unsandboxed") {
		t.Fatalf("unsandboxed by an agent: %d %s", code, out)
	}
	code, out, _ = e.req("POST", "/workspaces/current/errands", `{"name":"x","command":"true","worker":"atlas","rule":{"freq":"hours","tz":"UTC"}}`, scout)
	if code != 403 {
		t.Fatalf("worker atlas by scout: %d %s", code, out)
	}
	// ada (admin) makes one for scout, unsandboxed and keeping its output on the host
	code, out, _ = e.req("POST", fmt.Sprintf("/workspaces/%d/errands", ws), `{"name":"tts","command":"curl -s 127.0.0.1:9000","unsandboxed":true,"output":"host","worker":"scout","rule":{"freq":"daily","window_start":"03:30","tz":"America/New_York"}}`, ada)
	e.ok(code, out, 201)
	var tts store.Errand
	_ = json.Unmarshal(out, &tts)
	if !tts.Unsandboxed || tts.ScheduleText != "Every day at 03:30 (America/New_York)" && !strings.HasPrefix(tts.ScheduleText, "Every day at 03:30") {
		t.Fatalf("ada's errand: %s", out)
	}
	// recurring gigs refuse a minute schedule
	code, out, _ = e.req("POST", fmt.Sprintf("/projects/%d/recurring", e.ids["scout.p"]), fmt.Sprintf(`{"title":"x","schedule_id":%d}`, c.ScheduleID), ada)
	if code != 422 && code != 404 {
		t.Fatalf("recurring on a minute schedule: %d %s", code, out)
	}

	// scout loses the Errands permission (a setup where agents only read their errands): it can't
	// change them any more, but its executor still runs them, and ada can still give it new ones
	code, out, _ = e.req("PUT", fmt.Sprintf("/users/%d/access", e.ids["scout.user"]), fmt.Sprintf(`{"workspace_id":%d,"preset":"member","overrides":{"errands":false}}`, ws), ada)
	e.ok(code, out, 200)
	if code, out, _ = e.req("PATCH", fmt.Sprintf("/errands/%d", c.ID), `{"timeout_s":60}`, scout); code != 403 {
		t.Fatalf("read-only agent changed its errand: %d %s", code, out)
	}
	code, out, _ = e.req("POST", fmt.Sprintf("/workspaces/%d/errands", ws), `{"name":"by ada","command":"true","worker":"scout","paused":true,"rule":{"freq":"hours","tz":"UTC"}}`, ada)
	e.ok(code, out, 201)

	// the executor: due at the first slot
	store.Now = func() int64 { return *c.NextAt }
	code, out, _ = e.req("POST", "/workspaces/current/errands/claim", "", scout)
	e.ok(code, out, 200)
	var claim struct {
		Items  []store.ClaimedErrand `json:"items"`
		Paused bool                  `json:"paused"`
	}
	_ = json.Unmarshal(out, &claim)
	if len(claim.Items) != 1 || claim.Items[0].Command != "df -h /" || claim.Items[0].CommandSetBy != "scout" || claim.Items[0].Unsandboxed {
		t.Fatalf("claim: %s", out)
	}
	run := claim.Items[0].RunID
	code, out, _ = e.req("PUT", fmt.Sprintf("/users/%d/access", e.ids["scout.user"]), fmt.Sprintf(`{"workspace_id":%d,"preset":"member","overrides":{"errands":true}}`, ws), ada)
	e.ok(code, out, 200)
	// another agent can't claim or finish scout's
	code, out, _ = e.req("POST", fmt.Sprintf("/errand-runs/%d/finish", run), `{"status":"ok","duration_ms":5}`, atlas)
	if code != 404 && code != 403 {
		t.Fatalf("atlas finishing scout's run: %d %s", code, out)
	}
	code, out, _ = e.req("POST", fmt.Sprintf("/errand-runs/%d/finish", run), `{"status":"failed","exit_code":1,"duration_ms":40,"output_tail":"No space left","output_bytes":13}`, scout)
	e.ok(code, out, 200)
	if !strings.Contains(string(out), `"became_failing":true`) {
		t.Fatalf("finish: %s", out)
	}
	// output: with errands:output only
	code, out, _ = e.req("GET", fmt.Sprintf("/errand-runs/%d", run), "", scout)
	e.ok(code, out, 200)
	if !strings.Contains(string(out), "No space left") {
		t.Fatalf("output with errands:output: %s", out)
	}
	code, out, _ = e.req("GET", fmt.Sprintf("/errand-runs/%d", run), "", noOut)
	e.ok(code, out, 200)
	if strings.Contains(string(out), "No space left") || !strings.Contains(string(out), `"has_output":true`) {
		t.Fatalf("output without errands:output: %s", out)
	}
	// the host-output errand: whatever the executor sends, no output is stored
	store.Now = func() int64 { return *tts.NextAt }
	code, out, _ = e.req("POST", "/workspaces/current/errands/claim", "", scout)
	e.ok(code, out, 200)
	_ = json.Unmarshal(out, &claim)
	var ttsRun int64
	for _, it := range claim.Items {
		if it.ErrandID == tts.ID {
			ttsRun = it.RunID
			if !it.Unsandboxed || it.Output != "host" || it.CommandSetBy != "ada" {
				t.Fatalf("tts claim: %+v", it)
			}
		}
	}
	code, out, _ = e.req("POST", fmt.Sprintf("/errand-runs/%d/finish", ttsRun), `{"status":"ok","exit_code":0,"duration_ms":40,"output_tail":"secret","output_bytes":6}`, scout)
	e.ok(code, out, 200)
	code, out, _ = e.req("GET", fmt.Sprintf("/errand-runs/%d", ttsRun), "", ada)
	if strings.Contains(string(out), "secret") || strings.Contains(string(out), `"has_output":true`) {
		t.Fatalf("host output stored: %s", out)
	}
	// no-sandbox errands: scout (Errands, not No-sandbox errands) may pause one, not resume it or run it now,
	// nor retime its schedule
	code, out, _ = e.req("POST", fmt.Sprintf("/errands/%d/run-now", tts.ID), "", scout)
	if code != 403 || !strings.Contains(string(out), "No-sandbox") {
		t.Fatalf("run-now of a no-sandbox errand by scout: %d %s", code, out)
	}
	code, out, _ = e.req("POST", fmt.Sprintf("/errands/%d/pause", tts.ID), "", scout)
	e.ok(code, out, 200)
	code, out, _ = e.req("POST", fmt.Sprintf("/errands/%d/resume", tts.ID), "", scout)
	if code != 403 {
		t.Fatalf("resume of a no-sandbox errand by scout: %d %s", code, out)
	}
	code, out, _ = e.req("POST", fmt.Sprintf("/errands/%d/resume", tts.ID), "{}", ada)
	e.ok(code, out, 200)
	code, out, _ = e.req("PUT", fmt.Sprintf("/users/%d/access", e.ids["scout.user"]), fmt.Sprintf(`{"workspace_id":%d,"preset":"member","overrides":{"errands":true,"schedules":true}}`, ws), ada)
	e.ok(code, out, 200)
	code, out, _ = e.req("PUT", fmt.Sprintf("/schedules/%d", tts.ScheduleID), `{"freq":"daily","window_start":"04:00","tz":"America/New_York"}`, scout)
	if code != 403 || !strings.Contains(string(out), "No-sandbox") {
		t.Fatalf("retiming a no-sandbox errand's schedule by scout: %d %s", code, out)
	}
	// the claim takes a limit (the executor's free slots)
	code, out, _ = e.req("POST", "/workspaces/current/errands/claim", `{"limit":1}`, scout)
	e.ok(code, out, 200)
	// scout edits ada's unsandboxed errand (its name only): the sandbox comes back on
	code, out, _ = e.req("PATCH", fmt.Sprintf("/errands/%d", tts.ID), `{"name":"tts health"}`, scout)
	e.ok(code, out, 200)
	if !strings.Contains(string(out), `"unsandboxed":false`) {
		t.Fatalf("edit by scout should clear unsandboxed: %s", out)
	}
	// retiming a schedule errands use needs Errands (bob is a viewer: refused before that anyway)
	code, out, _ = e.req("PUT", fmt.Sprintf("/schedules/%d", c.ScheduleID), `{"freq":"minutes","interval":10,"tz":"UTC"}`, ada)
	e.ok(code, out, 200)

	// PAUSE: both boxes; the errand box resumed alone
	code, out, _ = e.req("POST", "/runner/pause", fmt.Sprintf(`{"workspaces":[%d]}`, ws), ada)
	e.ok(code, out, 200)
	if !strings.Contains(string(out), `"paused":true`) || !strings.Contains(string(out), `"errands_paused":true`) {
		t.Fatalf("pause: %s", out)
	}
	code, out, _ = e.req("POST", "/workspaces/current/errands/claim", "", scout)
	e.ok(code, out, 200)
	if !strings.Contains(string(out), `"paused":true`) {
		t.Fatalf("claim while paused: %s", out)
	}
	code, out, _ = e.req("POST", "/runner/resume", fmt.Sprintf(`{"workspaces":[%d],"gigs":false}`, ws), ada)
	e.ok(code, out, 200)
	if !strings.Contains(string(out), `"paused":true`) || !strings.Contains(string(out), `"errands_paused":false`) {
		t.Fatalf("resume errands only: %s", out)
	}
	// list: failing first
	code, out, _ = e.req("GET", "/workspaces/current/errands", "", noOut)
	e.ok(code, out, 200)
	var list struct {
		Items []store.Errand `json:"items"`
	}
	_ = json.Unmarshal(out, &list)
	if len(list.Items) != 4 || list.Items[0].ID != c.ID || list.Items[0].State != "failing" || list.Items[0].LastRun == nil {
		t.Fatalf("list: %s", out)
	}
	// atlas sees none of scout's
	code, _, _ = e.req("GET", fmt.Sprintf("/errands/%d", c.ID), "", atlas)
	if code != 404 {
		t.Fatalf("atlas reading scout's errand: %d", code)
	}
}
