package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0x2alx/gigg/internal/access"
)

// import_json: the server-side twin of the web UI's Import JSON (web/src/importJson.ts, ImportJsonModal.tsx).
// The same JSON (one object or a list, an object is the body its create call takes plus "kind"), the same
// field lists, the same gaps caught before trying, the same creation order and the same name resolution.
// It only creates. Every object goes through the REST create call of its kind, in-process with the
// caller's own credentials (as every MCP tool does), so validation, permissions and scopes are those of
// the forms and of the existing create_* / save_* tools: nothing is checked twice or differently here.

const (
	importMaxBytes   = 1024 * 1024 // as MAX_BYTES in importJson.ts
	importMaxObjects = 100         // as MAX_OBJECTS
)

const (
	kSchedule       = "schedule"
	kGigTemplate    = "gig_template"
	kErrandTemplate = "errand_template"
	kGig            = "gig"
	kErrand         = "errand"
	kRecurring      = "recurring_gig"
)

var (
	impRun         = []string{"model", "effort", "always_run", "run_timeout_minutes", "auto_archive_minutes", "allowed_tools"}
	impRule        = []string{"freq", "interval", "weekdays", "month_day", "tz", "window_start", "window_minutes", "lead_minutes"}
	impErrandItems = []string{"name", "description_md", "command", "timeout_s", "output", "unsandboxed"}
)

func impCat(parts ...[]string) []string {
	var out []string
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// impFields: what each kind's create call accepts (the kind itself aside); anything else is dropped.
var impFields = map[string][]string{
	kSchedule:       impCat([]string{"name"}, impRule),
	kGigTemplate:    impCat([]string{"name", "title", "description_md", "priority", "color", "labels", "assignees", "followers", "checklist", "subgigs", "custom"}, impRun),
	kErrandTemplate: impErrandItems,
	kGig:            impCat([]string{"title", "description_md", "priority", "category", "labels", "assignees", "followers", "color", "start_at", "due_at", "not_after", "checklist", "subgigs", "custom"}, impRun),
	kErrand:         impCat(impErrandItems, []string{"rule", "worker", "on_failure_gig", "paused"}),
	kRecurring: impCat([]string{"template_id", "schedule_id", "title", "description_md", "priority", "color", "labels", "assignees", "followers", "checklist", "subgigs", "custom"}, impRun, impRule,
		[]string{"starts_on", "ends_on", "max_count", "overlap", "paused"}),
}

// impRank: the order rows are created in: what others refer to first (schedules, then templates), then
// what uses them (recurring gigs, errands), then gigs.
var impRank = map[string]int{kSchedule: 0, kGigTemplate: 1, kErrandTemplate: 1, kRecurring: 2, kErrand: 2, kGig: 3}

// impKindNames: the kinds as written in the JSON; the short names are accepted too.
var impKindAlias = map[string]string{"template": kGigTemplate, "recurring": kRecurring}

const impKindList = "gig, template (or gig_template), schedule, errand_template, errand, recurring (or recurring_gig)"

type importArgs struct {
	JSON      any    `json:"json" jsonschema:"The JSON to import: a list (or one object), as a JSON value or as text. Up to 100 objects and 1 MB. An object is the body its create call takes plus a kind: gig, template, schedule, errand_template, errand or recurring. A recurring gig names its template and schedule with template and schedule (names of ones in the list, in the workspace or shared) or template_id and schedule_id. People are usernames in assignees and followers. Fields the create call doesn't take (ids, workspace, ...) are ignored and reported. See the import guide."`
	Workspace string `json:"workspace,omitempty" jsonschema:"Workspace id or slug where everything is made; omit to use your key's workspace"`
	Project   int64  `json:"project,omitempty" jsonschema:"Project for gigs, recurring gigs and errands, and the project a gig template is limited to (default: the workspace's first project for gigs and recurring gigs, any project for templates, the server's default for errands)"`
	Shared    bool   `json:"shared,omitempty" jsonschema:"Make the schedules, templates and errand templates shared instead of the workspace's (needs the Shared library permission and library:write; gigs, recurring gigs and errands still go to the workspace)"`
	DryRun    bool   `json:"dry_run,omitempty" jsonschema:"Only validate and return the plan (what would be made, what is skipped and why); nothing is created. Permissions and caps are decided by the create calls, so a row can pass here and still fail"`
}

type impRow struct {
	index   int
	kind    string
	label   string
	body    map[string]any
	dropped []string
	problem []string // why the row is skipped
	after   []impAfter
}

// impAfter: a reference of a row to be filled in when the row it names is created.
type impAfter struct {
	key  string // template_id | schedule_id
	row  int
	what string
	name string
}

// impResult is one row of the answer.
type impResult struct {
	Row     int      `json:"row" jsonschema:"Position in the list, from 0"`
	Kind    string   `json:"kind,omitempty"`
	Name    string   `json:"name,omitempty"`
	Status  string   `json:"status" jsonschema:"made, failed (the create call refused it), skipped (not tried: it has a problem or needs a row that wasn't made) or, in a dry run, ready"`
	ID      int64    `json:"id,omitempty"`
	Key     string   `json:"key,omitempty"`
	Message string   `json:"message,omitempty"`
	Ignored []string `json:"ignored,omitempty"`
}

type impOut struct {
	DryRun  bool        `json:"dry_run"`
	Made    int         `json:"made"`
	Ready   int         `json:"ready,omitempty"`
	Failed  int         `json:"failed"`
	Skipped int         `json:"skipped"`
	Order   string      `json:"order,omitempty"`
	Results []impResult `json:"results"`
}

func impIsObj(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func impHas(b map[string]any, k string) bool {
	s, ok := b[k].(string)
	return ok && strings.TrimSpace(s) != ""
}

// impMissing: the plain gaps the create call would refuse, said before it is tried (the call stays the judge).
func impMissing(kind string, b map[string]any) []string {
	var p []string
	switch kind {
	case kSchedule:
		if !impHas(b, "freq") {
			p = append(p, `"freq" is missing.`)
		} else if f, _ := b["freq"].(string); f != "daily" && f != "weekly" && f != "monthly" {
			p = append(p, fmt.Sprintf("Schedules for gigs take daily, weekly or monthly, not %s (minutes and hours are for errands).", impQuote(b["freq"])))
		}
	case kGigTemplate:
		if !impHas(b, "name") {
			p = append(p, `"name" is missing.`)
		}
		if !impHas(b, "title") {
			p = append(p, `"title" is missing.`)
		}
	case kErrandTemplate:
		if !impHas(b, "name") {
			p = append(p, `"name" is missing.`)
		}
		if !impHas(b, "command") {
			p = append(p, `"command" is missing.`)
		}
	case kGig:
		if !impHas(b, "title") {
			p = append(p, `"title" is missing.`)
		}
	case kErrand:
		if !impHas(b, "name") {
			p = append(p, `"name" is missing.`)
		}
		if !impHas(b, "command") {
			p = append(p, `"command" is missing.`)
		}
		if _, ok := impIsObj(b["rule"]); !ok {
			p = append(p, `"rule" (the timing) is missing.`)
		}
	case kRecurring:
		if _, has := b["template_id"]; !impHas(b, "template") && !has && !impHas(b, "title") {
			p = append(p, `Give a "template" (name or template_id) or a "title".`)
		}
		if _, has := b["schedule_id"]; !impHas(b, "schedule") && !has && !impHas(b, "freq") {
			p = append(p, `Give a "schedule" (name or schedule_id) or a "freq".`)
		}
	}
	return p
}

func impQuote(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func impStr(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	}
	return fmt.Sprint(v)
}

// impParse: the text -> one row per object, or the reason the whole input is unusable.
func impParse(raw []byte) ([]*impRow, string) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, "Give the JSON to import."
	}
	if len(raw) > importMaxBytes {
		return nil, "That is over 1 MB: split it in smaller lists."
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var data any
	if err := dec.Decode(&data); err != nil {
		return nil, "That isn't valid JSON: " + err.Error() + "."
	}
	if dec.More() {
		return nil, "That isn't valid JSON: more than one value."
	}
	list, ok := data.([]any)
	if !ok {
		list = []any{data}
	}
	if len(list) == 0 {
		return nil, "The list is empty."
	}
	if len(list) > importMaxObjects {
		return nil, fmt.Sprintf("At most %d objects at a time; there are %d.", importMaxObjects, len(list))
	}
	rows := make([]*impRow, len(list))
	for i, v := range list {
		rows[i] = impParseOne(v, i)
	}
	return rows, ""
}

func impParseOne(v any, index int) *impRow {
	row := &impRow{index: index, body: map[string]any{}}
	o, ok := impIsObj(v)
	if !ok {
		row.problem = append(row.problem, "Not an object.")
		return row
	}
	switch k, has := o["kind"]; {
	case !has:
		row.problem = append(row.problem, `No "kind". Expected one of: `+impKindList+".")
	default:
		name, _ := k.(string)
		if a, ok := impKindAlias[name]; ok {
			name = a
		}
		if _, known := impFields[name]; !known {
			row.problem = append(row.problem, fmt.Sprintf("Unknown kind %s. Expected one of: %s.", impQuote(k), impKindList))
		} else {
			row.kind = name
		}
	}
	label := o["name"]
	if label == nil {
		label = o["title"]
	}
	if s, ok := label.(string); ok {
		row.label = s
	}
	if row.kind == "" {
		return row
	}
	fields := map[string]bool{}
	for _, f := range impFields[row.kind] {
		fields[f] = true
	}
	if row.kind == kRecurring {
		fields["template"], fields["schedule"] = true, true
	}
	keys := make([]string, 0, len(o))
	for k := range o {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch {
		case k == "kind":
		case fields[k]:
			row.body[k] = o[k]
		default:
			row.dropped = append(row.dropped, k)
		}
	}
	if row.kind == kRecurring && row.label == "" {
		if t := row.body["template"]; t != nil {
			row.label = impStr(t)
		} else {
			row.label = impStr(row.body["title"])
		}
	}
	row.problem = append(row.problem, impMissing(row.kind, row.body)...)
	return row
}

// importer holds what one import resolves against: the place, and (looked up once, when a row needs them)
// the users, the project, and the templates and schedules in reach.
type importer struct {
	call      apiCall
	ws        string // path-escaped workspace reference
	project   int64  // the project the caller named
	proj      int64  // the project gigs and recurring gigs go to: that one, else the workspace's first
	shared    bool
	users     map[string]int64
	usersErr  string
	usersByID map[int64]bool
	usersDone bool
	projDone  bool
	projErr   string
	tpls      map[string]int64 // lower-case name -> id (workspace's first, then shared)
	tplIDs    map[int64]bool
	schs      map[string]int64
	schIDs    map[int64]bool
	libDone   bool
}

func (im *importer) loadUsers() {
	if im.usersDone {
		return
	}
	im.usersDone = true
	code, body := im.call("GET", "/users", nil, nil)
	if code != 200 {
		im.usersErr = failText(code, body)
		return
	}
	var out struct {
		Items []struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		im.usersErr = "the user list is unreadable"
		return
	}
	im.users, im.usersByID = map[string]int64{}, map[int64]bool{}
	for _, u := range out.Items {
		im.users[strings.ToLower(u.Username)] = u.ID
		im.usersByID[u.ID] = true
	}
}

// userID: a username or a user id -> the user id, if anybody in reach has it.
func (im *importer) userID(v any) (int64, bool) {
	im.loadUsers()
	switch x := v.(type) {
	case string:
		id, ok := im.users[strings.ToLower(strings.TrimSpace(x))]
		return id, ok
	case json.Number:
		id, err := x.Int64()
		return id, err == nil && im.usersByID[id]
	}
	return 0, false
}

// firstProject: the project gigs and recurring gigs go to when none is named: the workspace's first.
func (im *importer) firstProject() (int64, string) {
	if im.project != 0 {
		return im.project, ""
	}
	if !im.projDone {
		im.projDone = true
		code, body := im.call("GET", "/workspaces/"+im.ws+"/projects", nil, nil)
		var out struct {
			Items []struct {
				ID int64 `json:"id"`
			} `json:"items"`
		}
		switch {
		case code != 200:
			im.projErr = "Couldn't list the workspace's projects (" + failText(code, body) + "): give a project."
		case json.Unmarshal(body, &out) != nil || len(out.Items) == 0:
			im.projErr = "The workspace has no project you can use: give a project."
		default:
			im.proj = out.Items[0].ID
		}
	}
	return im.proj, im.projErr
}

func (im *importer) projectFor() int64 {
	id, _ := im.firstProject()
	return id
}

// loadLib: the workspace's templates and schedules and the shared ones, for recurring gigs' references.
func (im *importer) loadLib() {
	if im.libDone {
		return
	}
	im.libDone = true
	im.tpls, im.tplIDs, im.schs, im.schIDs = map[string]int64{}, map[int64]bool{}, map[string]int64{}, map[int64]bool{}
	fill := func(path string, names map[string]int64, ids map[int64]bool) {
		code, body := im.call("GET", "/workspaces/"+im.ws+path, nil, nil)
		if code != 200 {
			return // as in the UI: nothing in reach
		}
		var out struct {
			Items []struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
			} `json:"items"`
		}
		if json.Unmarshal(body, &out) != nil {
			return
		}
		for _, it := range out.Items {
			ids[it.ID] = true
			if k := strings.ToLower(it.Name); names[k] == 0 {
				names[k] = it.ID
			}
		}
	}
	fill("/templates", im.tpls, im.tplIDs)
	fill("/schedules", im.schs, im.schIDs)
}

// impToBody: the create body of a row: user names and references resolved. problems lists what doesn't
// resolve (the row is then skipped); after, the references to rows of this same list.
func (im *importer) toBody(row *impRow, rows []*impRow) (map[string]any, []string, []impAfter) {
	body := map[string]any{}
	for k, v := range row.body {
		body[k] = v
	}
	var problems []string
	var after []impAfter
	for _, k := range []string{"assignees", "followers"} {
		v, has := body[k]
		if !has {
			continue
		}
		list, ok := v.([]any)
		if !ok {
			problems = append(problems, fmt.Sprintf(`"%s" must be a list of usernames.`, k))
			continue
		}
		ids := []int64{}
		var bad []string
		for _, u := range list {
			if id, ok := im.userID(u); ok {
				ids = append(ids, id)
			} else {
				bad = append(bad, impStr(u))
			}
		}
		if len(bad) > 0 {
			what := "assignee"
			if k == "followers" {
				what = "follower"
			}
			if im.usersErr != "" {
				problems = append(problems, "Couldn't look up the users ("+im.usersErr+").")
			} else {
				s := ""
				if len(bad) > 1 {
					s = "s"
				}
				problems = append(problems, fmt.Sprintf("Unknown %s%s: %s.", what, s, strings.Join(bad, ", ")))
			}
		}
		body[k] = ids
	}
	if row.kind == kRecurring {
		for _, what := range []string{"template", "schedule"} {
			var name string
			if s, ok := body[what].(string); ok {
				name = strings.TrimSpace(s)
			}
			idKey := what + "_id"
			var id int64
			var hasID bool
			if n, ok := body[idKey].(json.Number); ok {
				if i, err := n.Int64(); err == nil {
					id, hasID = i, true
				}
			}
			delete(body, what)
			if name == "" && !hasID {
				continue
			}
			delete(body, idKey)
			if name != "" {
				if pend, bad := impFindInBatch(rows, what, name); bad != "" {
					problems = append(problems, bad)
				} else if pend >= 0 {
					after = append(after, impAfter{key: idKey, row: pend, what: what, name: name})
				} else if found := im.lookup(what, name, 0); found != 0 {
					body[idKey] = found
				} else {
					problems = append(problems, fmt.Sprintf("No %s named “%s” in reach.", what, name))
				}
			} else if found := im.lookup(what, "", id); found != 0 {
				body[idKey] = found
			} else {
				problems = append(problems, fmt.Sprintf("No %s with id %d in reach.", what, id))
			}
		}
	}
	return body, problems, after
}

func (im *importer) lookup(what, name string, id int64) int64 {
	im.loadLib()
	names, ids := im.tpls, im.tplIDs
	if what == "schedule" {
		names, ids = im.schs, im.schIDs
	}
	if name != "" {
		return names[strings.ToLower(name)]
	}
	if ids[id] {
		return id
	}
	return 0
}

// impFindInBatch: a template or schedule of the same list that a row's reference names: the row's index
// (a row that will be created first), or -1; or a problem (that row is skipped).
func impFindInBatch(rows []*impRow, what, name string) (int, string) {
	kind := kSchedule
	if what == "template" {
		kind = kGigTemplate
	}
	for _, r := range rows {
		if r.kind == kind && strings.EqualFold(strings.TrimSpace(r.label), name) {
			if len(r.problem) > 0 {
				return -1, fmt.Sprintf("The %s “%s” in this list is skipped.", what, name)
			}
			return r.index, ""
		}
	}
	return -1, ""
}

// failText: what the API said, as the forms show it: the detail and the fields it names.
func failText(code int, body []byte) string {
	var p Problem
	if json.Unmarshal(body, &p) == nil && p.Code != "" {
		s := p.Detail
		if s == "" {
			s = fmt.Sprintf("%d %s", p.Status, p.Code)
		}
		for _, e := range p.Errors {
			s += "; " + e.Location + " " + e.Message
		}
		return s
	}
	return fmt.Sprintf("Error %d.", code)
}

// create: the REST create call of the row's kind.
func (im *importer) create(row *impRow, body map[string]any) (int, []byte) {
	shared := im.shared && (row.kind == kSchedule || row.kind == kGigTemplate || row.kind == kErrandTemplate)
	switch row.kind {
	case kSchedule:
		if shared {
			return im.call("POST", "/shared/schedules", nil, body)
		}
		return im.call("POST", "/workspaces/"+im.ws+"/schedules", nil, body)
	case kGigTemplate:
		if shared {
			return im.call("POST", "/shared/templates", nil, body)
		}
		if im.project != 0 {
			body["project_id"] = im.project
		}
		return im.call("POST", "/workspaces/"+im.ws+"/templates", nil, body)
	case kErrandTemplate:
		if shared {
			return im.call("POST", "/shared/errand-templates", nil, body)
		}
		return im.call("POST", "/workspaces/"+im.ws+"/errand-templates", nil, body)
	case kGig:
		return im.call("POST", "/projects/"+strconv.FormatInt(im.projectFor(), 10)+"/gigs", nil, body)
	case kErrand:
		if im.project != 0 {
			body["project"] = im.project
		}
		return im.call("POST", "/workspaces/"+im.ws+"/errands", nil, body)
	default:
		return im.call("POST", "/projects/"+strconv.FormatInt(im.projectFor(), 10)+"/recurrences", nil, body)
	}
}

// runImport parses, plans and (unless dry) creates. A returned string is a problem with the whole input.
func runImport(in importArgs, call apiCall, ws func(string) string) (*impOut, string) {
	var raw []byte
	switch x := in.JSON.(type) {
	case nil:
	case string:
		raw = []byte(x)
	default:
		raw, _ = json.Marshal(x)
	}
	rows, bad := impParse(raw)
	if bad != "" {
		return nil, bad
	}
	im := &importer{call: call, ws: ws(in.Workspace), project: in.Project, shared: in.Shared}
	type planned struct {
		row   *impRow
		body  map[string]any
		after []impAfter
	}
	plan := make([]planned, len(rows))
	for i, r := range rows {
		plan[i] = planned{row: r}
		if len(r.problem) > 0 || r.kind == "" {
			continue
		}
		body, problems, after := im.toBody(r, rows)
		if r.kind == kGig || r.kind == kRecurring {
			if _, why := im.firstProject(); why != "" {
				problems = append(problems, why)
			}
		}
		r.problem = append(r.problem, problems...)
		plan[i].body, plan[i].after = body, after
	}
	out := &impOut{DryRun: in.DryRun, Results: make([]impResult, len(rows))}
	for i, r := range rows {
		out.Results[i] = impResult{Row: i, Kind: r.kind, Name: r.label, Ignored: r.dropped}
	}
	var order []planned
	for _, p := range plan {
		if len(p.row.problem) > 0 {
			res := &out.Results[p.row.index]
			res.Status, res.Message = "skipped", strings.Join(p.row.problem, " ")
			continue
		}
		order = append(order, p)
	}
	sort.SliceStable(order, func(a, b int) bool { return impRank[order[a].row.kind] < impRank[order[b].row.kind] })
	if len(order) > 0 {
		kinds := map[int]bool{}
		for _, p := range order {
			kinds[impRank[p.row.kind]] = true
		}
		if len(kinds) > 1 {
			out.Order = "Created in order: schedules, then templates, then what uses them (recurring gigs, errands), then gigs."
		}
	}
	created := map[int]int64{}
	for _, p := range order {
		res := &out.Results[p.row.index]
		if in.DryRun {
			res.Status = "ready"
			continue
		}
		body := p.body
		missing := ""
		for _, dep := range p.after {
			id, ok := created[dep.row]
			if !ok {
				missing = fmt.Sprintf("%s '%s' wasn't created", dep.what, dep.name)
				break
			}
			body[dep.key] = id
		}
		if missing != "" {
			res.Status, res.Message = "skipped", missing
			continue
		}
		code, resp := im.create(p.row, body)
		if code >= 300 {
			res.Status, res.Message = "failed", failText(code, resp)
			continue
		}
		var made struct {
			ID  int64  `json:"id"`
			Key string `json:"key"`
		}
		_ = json.Unmarshal(resp, &made)
		res.Status, res.ID, res.Key = "made", made.ID, made.Key
		if made.ID != 0 {
			created[p.row.index] = made.ID
		}
	}
	for _, r := range out.Results {
		switch r.Status {
		case "made":
			out.Made++
		case "ready":
			out.Ready++
		case "failed":
			out.Failed++
		default:
			out.Skipped++
		}
	}
	return out, ""
}

// addImportTool: import_json, for a key that may write gigs or errands; each object is then allowed or
// refused by its own create call.
func addImportTool(s *mcp.Server, p *access.Principal, call apiCall, ws func(string) string) {
	if !p.HasScope("gigs:write") && !p.HasScope("errands:write") {
		return
	}
	mcp.AddTool(s, &mcp.Tool{Name: "import_json", Description: "Create many things from JSON, the way the web UI's Import JSON does: a list (up to 100 objects, 1 MB) that may mix gigs, templates, schedules, errand templates, errands and recurring gigs, each an object with a kind and the fields of its create call. Rows are made in dependency order (schedules, templates, then recurring gigs and errands, then gigs); a recurring gig can name a template or schedule from the same list by name. It only creates, never changes or deletes. Each row goes through the normal create call with your permissions and key scopes; the answer lists every row as made (with id or key), failed (with the reason; the others still go through) or skipped (it has a problem, or needs a row that wasn't made). dry_run validates and returns the plan without creating anything.", Annotations: rw("Import JSON", false)},
		func(ctx context.Context, _ *mcp.CallToolRequest, in importArgs) (*mcp.CallToolResult, any, error) {
			out, bad := runImport(in, call, ws)
			if bad != "" {
				return toolResult(422, []byte(fmt.Sprintf(`{"status":422,"code":"invalid","detail":%q}`, bad)))
			}
			b, _ := json.Marshal(out)
			return toolResult(200, b)
		})
}
