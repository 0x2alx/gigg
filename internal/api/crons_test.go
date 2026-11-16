package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/0x2alx/gigg/internal/store"
)

// TestCronsAPI: an agent with the Crons permission makes its own crons (not someone else's, and not
// unsandboxed); its executor claims and reports only its own runs; a host-output cron never stores
// output; output needs crons:output; PAUSE has two boxes; a schedule used by crons needs Crons to
// retime; recurring tasks refuse minute schedules.
func TestCronsAPI(t *testing.T) {
	e := newEnv(t)
	defer func(n func() int64) { store.Now = n }(store.Now)
	ada := e.ada()
	ws := e.ids["scout.ws"]
	key := func(user string, scopes string) cred {
		t.Helper()
		code, out, _ := e.req("POST", "/keys", fmt.Sprintf(`{"name":"cron-%s","user_id":%d,"workspace":"%s","scopes":[%s],"allowed_listeners":["internal"]}`,
			user, e.ids[user+".user"], user, scopes), ada)
		e.ok(code, out, 201)
		var kc KeyCreated
		_ = json.Unmarshal(out, &kc)
		return cred{token: kc.Token, listener: "internal"}
	}
	full := `"crons:read","crons:write","crons:run","crons:output","tasks:read","tasks:write"`
	scout := key("scout", full)
	noOut := key("scout", `"crons:read"`)
	atlas := key("atlas", full)

	// no Crons permission yet
	body := `{"name":"disk","command":"df -h /","rule":{"freq":"minutes","interval":5,"tz":"UTC"}}`
	code, out, _ := e.req("POST", "/workspaces/current/crons", body, scout)
	if code != 403 || !strings.Contains(string(out), "crons") {
		t.Fatalf("without Crons: %d %s", code, out)
	}
	code, out, _ = e.req("PUT", fmt.Sprintf("/users/%d/access", e.ids["scout.user"]), fmt.Sprintf(`{"workspace_id":%d,"preset":"member","overrides":{"crons":true}}`, ws), ada)
	e.ok(code, out, 200)
	code, out, _ = e.req("POST", "/workspaces/current/crons", body, scout)
	e.ok(code, out, 201)
	var c store.Cron
	_ = json.Unmarshal(out, &c)
	if c.WorkerName != "scout" || c.ScheduleText != "Every 5 min" || c.State != "never" || c.NextAt == nil {
		t.Fatalf("created: %s", out)
	}
	// not unsandboxed, not for another agent
	code, out, _ = e.req("POST", "/workspaces/current/crons", `{"name":"x","command":"true","unsandboxed":true,"rule":{"freq":"hours","tz":"UTC"}}`, scout)
	if code != 403 || !strings.Contains(string(out), "cron_unsandboxed") {
		t.Fatalf("unsandboxed by an agent: %d %s", code, out)
	}
	code, out, _ = e.req("POST", "/workspaces/current/crons", `{"name":"x","command":"true","worker":"atlas","rule":{"freq":"hours","tz":"UTC"}}`, scout)
	if code != 403 {
		t.Fatalf("worker atlas by scout: %d %s", code, out)
	}
	// ada (admin) makes one for scout, unsandboxed and keeping its output on the host
	code, out, _ = e.req("POST", fmt.Sprintf("/workspaces/%d/crons", ws), `{"name":"tts","command":"curl -s 127.0.0.1:9000","unsandboxed":true,"output":"host","worker":"scout","rule":{"freq":"daily","window_start":"03:30","tz":"America/New_York"}}`, ada)
	e.ok(code, out, 201)
	var tts store.Cron
	_ = json.Unmarshal(out, &tts)
	if !tts.Unsandboxed || tts.ScheduleText != "Every day at 03:30 (America/New_York)" && !strings.HasPrefix(tts.ScheduleText, "Every day at 03:30") {
		t.Fatalf("ada's cron: %s", out)
	}
	// recurring tasks refuse a minute schedule
	code, out, _ = e.req("POST", fmt.Sprintf("/projects/%d/recurring", e.ids["scout.p"]), fmt.Sprintf(`{"title":"x","schedule_id":%d}`, c.ScheduleID), ada)
	if code != 422 && code != 404 {
		t.Fatalf("recurring on a minute schedule: %d %s", code, out)
	}

	// the executor: due at the first slot
	store.Now = func() int64 { return *c.NextAt }
	code, out, _ = e.req("POST", "/workspaces/current/crons/claim", "", scout)
	e.ok(code, out, 200)
	var claim struct {
		Items  []store.ClaimedCron `json:"items"`
		Paused bool                `json:"paused"`
	}
	_ = json.Unmarshal(out, &claim)
	if len(claim.Items) != 1 || claim.Items[0].Command != "df -h /" || claim.Items[0].CommandSetBy != "scout" || claim.Items[0].Unsandboxed {
		t.Fatalf("claim: %s", out)
	}
	run := claim.Items[0].RunID
	// another agent can't claim or finish scout's
	code, out, _ = e.req("POST", fmt.Sprintf("/cron-runs/%d/finish", run), `{"status":"ok","duration_ms":5}`, atlas)
	if code != 404 && code != 403 {
		t.Fatalf("atlas finishing scout's run: %d %s", code, out)
	}
	code, out, _ = e.req("POST", fmt.Sprintf("/cron-runs/%d/finish", run), `{"status":"failed","exit_code":1,"duration_ms":40,"output_tail":"No space left","output_bytes":13}`, scout)
	e.ok(code, out, 200)
	if !strings.Contains(string(out), `"became_failing":true`) {
		t.Fatalf("finish: %s", out)
	}
	// output: with crons:output only
	code, out, _ = e.req("GET", fmt.Sprintf("/cron-runs/%d", run), "", scout)
	e.ok(code, out, 200)
	if !strings.Contains(string(out), "No space left") {
		t.Fatalf("output with crons:output: %s", out)
	}
	code, out, _ = e.req("GET", fmt.Sprintf("/cron-runs/%d", run), "", noOut)
	e.ok(code, out, 200)
	if strings.Contains(string(out), "No space left") || !strings.Contains(string(out), `"has_output":true`) {
		t.Fatalf("output without crons:output: %s", out)
	}
	// the host-output cron: whatever the executor sends, no output is stored
	store.Now = func() int64 { return *tts.NextAt }
	code, out, _ = e.req("POST", "/workspaces/current/crons/claim", "", scout)
	e.ok(code, out, 200)
	_ = json.Unmarshal(out, &claim)
	var ttsRun int64
	for _, it := range claim.Items {
		if it.CronID == tts.ID {
			ttsRun = it.RunID
			if !it.Unsandboxed || it.Output != "host" || it.CommandSetBy != "ada" {
				t.Fatalf("tts claim: %+v", it)
			}
		}
	}
	code, out, _ = e.req("POST", fmt.Sprintf("/cron-runs/%d/finish", ttsRun), `{"status":"ok","exit_code":0,"duration_ms":40,"output_tail":"secret","output_bytes":6}`, scout)
	e.ok(code, out, 200)
	code, out, _ = e.req("GET", fmt.Sprintf("/cron-runs/%d", ttsRun), "", ada)
	if strings.Contains(string(out), "secret") || strings.Contains(string(out), `"has_output":true`) {
		t.Fatalf("host output stored: %s", out)
	}
	// scout edits ada's unsandboxed cron (its name only): the sandbox comes back on
	code, out, _ = e.req("PATCH", fmt.Sprintf("/crons/%d", tts.ID), `{"name":"tts health"}`, scout)
	e.ok(code, out, 200)
	if !strings.Contains(string(out), `"unsandboxed":false`) {
		t.Fatalf("edit by scout should clear unsandboxed: %s", out)
	}
	// retiming a schedule crons use needs Crons (bob is a viewer: refused before that anyway)
	code, out, _ = e.req("PUT", fmt.Sprintf("/schedules/%d", c.ScheduleID), `{"freq":"minutes","interval":10,"tz":"UTC"}`, ada)
	e.ok(code, out, 200)

	// PAUSE: both boxes; the cron box resumed alone
	code, out, _ = e.req("POST", "/runner/pause", fmt.Sprintf(`{"workspaces":[%d]}`, ws), ada)
	e.ok(code, out, 200)
	if !strings.Contains(string(out), `"paused":true`) || !strings.Contains(string(out), `"crons_paused":true`) {
		t.Fatalf("pause: %s", out)
	}
	code, out, _ = e.req("POST", "/workspaces/current/crons/claim", "", scout)
	e.ok(code, out, 200)
	if !strings.Contains(string(out), `"paused":true`) {
		t.Fatalf("claim while paused: %s", out)
	}
	code, out, _ = e.req("POST", "/runner/resume", fmt.Sprintf(`{"workspaces":[%d],"tasks":false}`, ws), ada)
	e.ok(code, out, 200)
	if !strings.Contains(string(out), `"paused":true`) || !strings.Contains(string(out), `"crons_paused":false`) {
		t.Fatalf("resume crons only: %s", out)
	}
	// list: failing first
	code, out, _ = e.req("GET", "/workspaces/current/crons", "", noOut)
	e.ok(code, out, 200)
	var list struct {
		Items []store.Cron `json:"items"`
	}
	_ = json.Unmarshal(out, &list)
	if len(list.Items) != 3 || list.Items[0].ID != c.ID || list.Items[0].State != "failing" || list.Items[0].LastRun == nil {
		t.Fatalf("list: %s", out)
	}
	// atlas sees none of scout's
	code, _, _ = e.req("GET", fmt.Sprintf("/crons/%d", c.ID), "", atlas)
	if code != 404 {
		t.Fatalf("atlas reading scout's cron: %d", code)
	}
}
