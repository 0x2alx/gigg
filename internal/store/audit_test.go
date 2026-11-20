package store

import (
	"database/sql"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// lastAudit returns the action and data of the newest activity row about an object.
func (f *fixture) lastAudit(t *testing.T, entity string, id int64) (string, map[string]json.RawMessage) {
	t.Helper()
	var action, data string
	must(t, f.s.R.QueryRowContext(f.ctx, `SELECT action, data FROM activity WHERE entity_type = ? AND entity_id = ? ORDER BY id DESC LIMIT 1`,
		entity, id).Scan(&action, &data))
	var m map[string]json.RawMessage
	must(t, json.Unmarshal([]byte(data), &m))
	return action, m
}

// wantChanged checks an update row: exactly the fields given, each with its old and new value (JSON).
func wantChanged(t *testing.T, label string, data map[string]json.RawMessage, want map[string][2]string) {
	t.Helper()
	var keys, wantKeys []string
	for k := range data {
		keys = append(keys, k)
	}
	for k := range want {
		wantKeys = append(wantKeys, k)
	}
	slices.Sort(keys)
	slices.Sort(wantKeys)
	if !slices.Equal(keys, wantKeys) {
		t.Fatalf("%s: changed fields %v, want %v (%s)", label, keys, wantKeys, mustJSON(data))
	}
	for k, ow := range want {
		var on struct{ Old, New json.RawMessage }
		must(t, json.Unmarshal(data[k], &on))
		if !sameJSON(on.Old, json.RawMessage(ow[0])) || !sameJSON(on.New, json.RawMessage(ow[1])) {
			t.Errorf("%s: %s = %s → %s, want %s → %s", label, k, on.Old, on.New, ow[0], ow[1])
		}
	}
}

// wantFields checks a create or delete row: the fields given, with these values (others may follow).
func wantFields(t *testing.T, label string, data map[string]json.RawMessage, want map[string]string) {
	t.Helper()
	for k, v := range want {
		if got, ok := data[k]; !ok || !sameJSON(got, json.RawMessage(v)) {
			t.Errorf("%s: %s = %s, want %s", label, k, got, v)
		}
	}
	for _, k := range []string{"version", "updated_at"} {
		if _, ok := data[k]; ok {
			t.Errorf("%s: bookkeeping field %s recorded", label, k)
		}
	}
}

// TestAuditRecordsChanges: on every object, an update records exactly the fields that changed with
// their old and new values, a create every initial value, a delete the last values.
func TestAuditRecordsChanges(t *testing.T) {
	f := newFixture(t)
	str := func(s string) *string { return &s }

	// workspaces
	_, d := f.lastAudit(t, "workspace", f.ws.ID)
	wantFields(t, "workspace created", d, map[string]string{"slug": `"scout"`, "name": `"Scout"`, "archived": "false"})
	f.tx(t, func(tx *sql.Tx) error {
		_, err := PatchWorkspace(f.ctx, tx, f.a, f.ws.ID, 0, WorkspacePatch{Name: str("Scout HQ")})
		return err
	})
	act, d := f.lastAudit(t, "workspace", f.ws.ID)
	if act != "workspace.updated" {
		t.Fatalf("action %s", act)
	}
	wantChanged(t, "workspace", d, map[string][2]string{"name": {`"Scout"`, `"Scout HQ"`}})

	// projects: long text in full, old and new; a delete keeps the last values
	f.tx(t, func(tx *sql.Tx) error {
		_, err := PatchProject(f.ctx, tx, f.a, f.p2.ID, 0, ProjectPatch{DescriptionMD: str("line one\nline two")})
		return err
	})
	_, d = f.lastAudit(t, "project", f.p2.ID)
	wantChanged(t, "project", d, map[string][2]string{"description_md": {`""`, `"line one\nline two"`}})
	p2, err := GetProject(f.ctx, f.s.R, f.p2.ID)
	must(t, err)
	f.tx(t, func(tx *sql.Tx) error { return DeleteProject(f.ctx, tx, f.a, p2) })
	act, d = f.lastAudit(t, "project", f.p2.ID)
	if act != "project.deleted" {
		t.Fatalf("action %s", act)
	}
	wantFields(t, "project deleted", d, map[string]string{"key": `"OPS"`, "name": `"Ops"`, "description_md": `"line one\nline two"`})

	// statuses
	var st *Status
	f.tx(t, func(tx *sql.Tx) error {
		st, err = CreateStatus(f.ctx, tx, f.a, f.p1, "Review", "doing", nil)
		return err
	})
	_, d = f.lastAudit(t, "status", st.ID)
	wantFields(t, "status created", d, map[string]string{"name": `"Review"`, "category": `"doing"`, "wip_limit": "null"})
	three := int64(3)
	wip := &three
	f.tx(t, func(tx *sql.Tx) error {
		_, err := PatchStatus(f.ctx, tx, f.a, st, 0, StatusPatch{WIPLimit: &wip})
		return err
	})
	_, d = f.lastAudit(t, "status", st.ID)
	wantChanged(t, "status", d, map[string][2]string{"wip_limit": {"null", "3"}})

	// labels
	var lb *Label
	f.tx(t, func(tx *sql.Tx) error { lb, err = CreateLabel(f.ctx, tx, f.a, f.ws.ID, "ui", nil, ""); return err })
	red := str("#ff0000")
	f.tx(t, func(tx *sql.Tx) error {
		_, err := PatchLabel(f.ctx, tx, f.a, lb, 0, LabelPatch{Color: &red})
		return err
	})
	_, d = f.lastAudit(t, "label", lb.ID)
	wantChanged(t, "label", d, map[string][2]string{"color": {"null", `"#FF0000"`}})

	// gigs: one field; the description old and new in full; labels (a table of their own)
	g := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "audit me", DescriptionMD: "before"})
	_, d = f.lastAudit(t, "gig", g.ID)
	wantFields(t, "gig created", d, map[string]string{"title": `"audit me"`, "description_md": `"before"`, "key": `"` + g.Key + `"`, "priority": "0"})
	prio := int64(3)
	f.tx(t, func(tx *sql.Tx) error {
		_, err := PatchGig(f.ctx, tx, f.a, g, 0, GigPatch{Priority: &prio})
		return err
	})
	_, d = f.lastAudit(t, "gig", g.ID)
	wantChanged(t, "gig priority", d, map[string][2]string{"priority": {"0", "3"}})
	f.tx(t, func(tx *sql.Tx) error {
		_, err := PatchGig(f.ctx, tx, f.a, f.get(t, g.ID), 0, GigPatch{DescriptionMD: str("after"), Labels: []string{"ui"}})
		return err
	})
	_, d = f.lastAudit(t, "gig", g.ID)
	if !strings.Contains(string(d["labels"]), `"name":"ui"`) {
		t.Errorf("gig labels: %s", d["labels"])
	}
	delete(d, "labels")
	wantChanged(t, "gig description", d, map[string][2]string{"description_md": {`"before"`, `"after"`}})
	// a change to the same value records nothing at all
	f.tx(t, func(tx *sql.Tx) error {
		_, err := PatchGig(f.ctx, tx, f.a, f.get(t, g.ID), 0, GigPatch{Priority: &prio})
		return err
	})
	_, d = f.lastAudit(t, "gig", g.ID)
	if len(d) != 0 {
		t.Errorf("same value: %s", mustJSON(d))
	}

	// custom fields
	var fld *CustomField
	f.tx(t, func(tx *sql.Tx) error {
		fld, err = SaveCustomField(f.ctx, tx, f.a, &CustomField{WorkspaceID: f.ws.ID, Name: "Size", Kind: "text"}, 0)
		return err
	})
	_, d = f.lastAudit(t, "field", fld.ID)
	wantFields(t, "field created", d, map[string]string{"name": `"Size"`, "kind": `"text"`})
	renamed := *fld
	renamed.Name = "T-shirt size"
	f.tx(t, func(tx *sql.Tx) error { _, err := SaveCustomField(f.ctx, tx, f.a, &renamed, 0); return err })
	_, d = f.lastAudit(t, "field", fld.ID)
	wantChanged(t, "field", d, map[string][2]string{"name": {`"Size"`, `"T-shirt size"`}})

	// schedules: the rule's fields too (lead_minutes went unrecorded before)
	var sch *Schedule
	f.tx(t, func(tx *sql.Tx) error {
		sch, err = SaveSchedule(f.ctx, tx, f.a, &Schedule{WorkspaceID: f.ws.ID, Name: "Mornings",
			Rule: Rule{Freq: "daily", Interval: 1, TZ: "America/New_York", WindowStart: "09:00", WindowMinutes: 60}}, 0)
		return err
	})
	_, d = f.lastAudit(t, "schedule", sch.ID)
	wantFields(t, "schedule created", d, map[string]string{"name": `"Mornings"`, "freq": `"daily"`, "window_start": `"09:00"`, "lead_minutes": "0"})
	lead := *sch
	lead.LeadMinutes = 5760
	f.tx(t, func(tx *sql.Tx) error { _, err := SaveSchedule(f.ctx, tx, f.a, &lead, 0); return err })
	_, d = f.lastAudit(t, "schedule", sch.ID)
	wantChanged(t, "schedule", d, map[string][2]string{"lead_minutes": {"0", "5760"}})

	// gig templates
	var tpl *Template
	f.tx(t, func(tx *sql.Tx) error {
		tpl, err = SaveTemplate(f.ctx, tx, f.a, &Template{WorkspaceID: f.ws.ID, Name: "Weekly review", Title: "Review the week"}, 0)
		return err
	})
	_, d = f.lastAudit(t, "template", tpl.ID)
	wantFields(t, "template created", d, map[string]string{"name": `"Weekly review"`, "title": `"Review the week"`, "checklist": "[]"})
	tp := *tpl
	tp.Checklist = []string{"inbox", "calendar"}
	f.tx(t, func(tx *sql.Tx) error { _, err := SaveTemplate(f.ctx, tx, f.a, &tp, 0); return err })
	_, d = f.lastAudit(t, "template", tpl.ID)
	wantChanged(t, "template", d, map[string][2]string{"checklist": {"[]", `["inbox","calendar"]`}})

	// recurring gigs: their own settings (max count went unrecorded before)
	var rec *Recurrence
	f.tx(t, func(tx *sql.Tx) error {
		rec, err = CreateRecurrence(f.ctx, tx, f.a, &Recurrence{ProjectID: f.p1.ID, TemplateID: tpl.ID, ScheduleID: sch.ID, StartsOn: ny(t, "2026-11-20 00:00")})
		return err
	})
	_, d = f.lastAudit(t, "recurrence", rec.ID)
	wantFields(t, "recurrence created", d, map[string]string{"title": `"Review the week"`, "lead_minutes": "5760", "max_count": "null", "paused": "false"})
	five := int64(5)
	rc := *rec
	rc.MaxCount = &five
	f.tx(t, func(tx *sql.Tx) error {
		_, err := UpdateRecurrence(f.ctx, tx, f.a, &rc, 0, RecurrenceChange{})
		return err
	})
	_, d = f.lastAudit(t, "recurrence", rec.ID)
	wantChanged(t, "recurrence", d, map[string][2]string{"max_count": {"null", "5"}})
	f.tx(t, func(tx *sql.Tx) error { return DeleteRecurrence(f.ctx, tx, f.a, rec) })
	act, d = f.lastAudit(t, "recurrence", rec.ID)
	if act != "recurrence.deleted" {
		t.Fatalf("action %s", act)
	}
	wantFields(t, "recurrence deleted", d, map[string]string{"title": `"Review the week"`, "template_id": jsonInt(tpl.ID), "schedule_id": jsonInt(sch.ID)})

	// errands and errand templates: commands old and new, secrets masked
	every := &Rule{Freq: "minutes", Interval: 5, TZ: "America/New_York"}
	var er *Errand
	f.tx(t, func(tx *sql.Tx) error {
		er, err = CreateErrand(f.ctx, tx, f.a, NewErrand{WorkspaceID: f.ws.ID, WorkerID: f.agent.ID,
			Template: &ErrandTemplate{Name: "ping", Command: "curl -H 'Authorization: Bearer abcdef1234567890' https://example.org"}, Rule: every}, ErrandEdit{})
		return err
	})
	_, d = f.lastAudit(t, "errand", er.ID)
	wantFields(t, "errand created", d, map[string]string{"name": `"ping"`, "command": `"curl -H 'Authorization: Bearer [masked]' https://example.org"`,
		"on_failure_gig": "false", "worker_id": jsonInt(f.agent.ID)})
	on := true
	f.tx(t, func(tx *sql.Tx) error {
		_, err := UpdateErrand(f.ctx, tx, f.a, er.ID, 0, ErrandChange{OnFailureGig: &on}, ErrandEdit{})
		return err
	})
	_, d = f.lastAudit(t, "errand", er.ID)
	wantChanged(t, "errand", d, map[string][2]string{"on_failure_gig": {"false", "true"}})
	_, d = f.lastAudit(t, "errand_template", er.TemplateID)
	wantFields(t, "errand template created", d, map[string]string{"name": `"ping"`, "command": `"curl -H 'Authorization: Bearer [masked]' https://example.org"`})
	et, err := GetErrandTemplate(f.ctx, f.s.R, er.TemplateID)
	must(t, err)
	et.Command = "GH_TOKEN=" + fakeToken("gh"+"p_", 30) + " gh api user"
	f.tx(t, func(tx *sql.Tx) error { _, err := SaveErrandTemplate(f.ctx, tx, f.a, et, 0, ErrandEdit{}); return err })
	_, d = f.lastAudit(t, "errand_template", er.TemplateID)
	delete(d, "command_set_at") // moves with the command
	wantChanged(t, "errand template", d, map[string][2]string{"command": {`"curl -H 'Authorization: Bearer [masked]' https://example.org"`,
		`"GH_TOKEN=[masked] gh api user"`}})
	er, err = GetErrand(f.ctx, f.s.R, er.ID)
	must(t, err)
	f.tx(t, func(tx *sql.Tx) error { return DeleteErrand(f.ctx, tx, f.a, er) })
	act, d = f.lastAudit(t, "errand", er.ID)
	if act != "errand.deleted" {
		t.Fatalf("action %s", act)
	}
	wantFields(t, "errand deleted", d, map[string]string{"name": `"ping"`, "command": `"GH_TOKEN=[masked] gh api user"`, "on_failure_gig": "true"})
}

func jsonInt(v int64) string { return mustJSON(v) }

func TestMaskSecrets(t *testing.T) {
	for in, want := range map[string]string{
		"echo hello":                 "echo hello",
		"API_KEY=abc123def ./run.sh": "API_KEY=[masked] ./run.sh",
		`curl -H "Authorization: Bearer abcdefgh12345678" x`: `curl -H "Authorization: Bearer [masked]" x`,
		"mysql --password=hunter2hunter -u root":             "mysql --password=[masked] -u root",
		"tool --token s3cr3tvalue":                           "tool --token [masked]",
		"git clone https://bob:pa55word@git.example.org/r":   "git clone https://bob:[masked]@git.example.org/r",
		`GH_TOKEN="$GH_TOKEN" gh api user`:                   `GH_TOKEN="$GH_TOKEN" gh api user`,
		"TOKEN=${TOKEN} run":                                 "TOKEN=${TOKEN} run",
		"backup --to /srv/x && echo 'author: someone'":       "backup --to /srv/x && echo 'author: someone'",
		"PASSWORD='multi word pass' cmd":                     "PASSWORD='[masked]' cmd",
		"curl -u user https://example.org/token/refresh?x=1": "curl -u user https://example.org/token/refresh?x=1",
	} {
		if got := MaskSecrets(in); got != want {
			t.Errorf("MaskSecrets(%q) = %q, want %q", in, got, want)
		}
	}
	// well-known token formats, built here so that no token-shaped literal sits in the source
	for in, want := range map[string]string{
		"export AWS=" + "AK" + "IA" + strings.ToUpper(fakeToken("", 16)): "export AWS=[masked]",
		"echo " + "xo" + "xb-" + fakeToken("", 20):                       "echo [masked]",
		"x " + fakeToken("gh"+"p_", 36) + " y":                           "x [masked] y",
		`{"api_` + `key": "` + fakeToken("", 12) + `"}`:                  `{"api_key": "[masked]"}`,
	} {
		if got := MaskSecrets(in); got != want {
			t.Errorf("MaskSecrets(%q) = %q, want %q", in, got, want)
		}
	}
}

// fakeToken: prefix plus n letters and digits.
func fakeToken(prefix string, n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	var b strings.Builder
	b.WriteString(prefix)
	for i := 0; i < n; i++ {
		b.WriteByte(chars[(i*7+3)%len(chars)])
	}
	return b.String()
}
