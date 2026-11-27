package api

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"testing"
)

// TestGigListFieldFilter: the gig lists filter on custom fields (field=<id>:<value>, repeatable):
// a choice exactly, a checkbox true/false, a number with a comparison, text containing a value, and
// an empty value for "not set". A field of another workspace, a date field or a bad value is a 422.
func TestGigListFieldFilter(t *testing.T) {
	e := newEnv(t)
	ada := e.ada()
	pid := e.ids["scout.p"]
	field := func(body string) int64 {
		code, out, _ := e.req("POST", "/workspaces/scout/fields", body, ada)
		e.ok(code, out, 201)
		var f struct {
			ID int64 `json:"id"`
		}
		_ = json.Unmarshal(out, &f)
		return f.ID
	}
	size := field(`{"name":"Size","kind":"select","options":["S","M","L"]}`)
	urgent := field(`{"name":"Urgent","kind":"checkbox"}`)
	points := field(`{"name":"Points","kind":"number"}`)
	note := field(`{"name":"Note","kind":"text"}`)
	due := field(`{"name":"Ship date","kind":"date"}`)

	mk := func(title string, custom string) {
		code, out, _ := e.req("POST", fmt.Sprintf("/projects/%d/gigs", pid), fmt.Sprintf(`{"title":%q,"custom":%s}`, title, custom), ada)
		e.ok(code, out, 201)
	}
	mk("small", fmt.Sprintf(`{"%d":"S","%d":true,"%d":1,"%d":"Waits on <legal> review"}`, size, urgent, points, note))
	mk("medium", fmt.Sprintf(`{"%d":"M","%d":false,"%d":3.5}`, size, urgent, points))
	mk("large", fmt.Sprintf(`{"%d":"L","%d":8,"%d":"ready"}`, size, points, note))
	mk("bare", `{}`)

	list := func(filters ...string) (int, []string) {
		q := url.Values{"limit": {"100"}}
		for _, f := range filters {
			q.Add("field", f)
		}
		code, out, _ := e.req("GET", "/workspaces/scout/gigs?"+q.Encode(), "", ada)
		if code != 200 {
			return code, nil
		}
		var l struct {
			Items []struct {
				Title string `json:"title"`
			} `json:"items"`
		}
		_ = json.Unmarshal(out, &l)
		titles := []string{}
		for _, g := range l.Items {
			if g.Title == "small" || g.Title == "medium" || g.Title == "large" || g.Title == "bare" {
				titles = append(titles, g.Title)
			}
		}
		sort.Strings(titles)
		return code, titles
	}
	want := func(got []string, w ...string) {
		t.Helper()
		sort.Strings(w)
		if strings.Join(got, ",") != strings.Join(w, ",") {
			t.Fatalf("got %v, want %v", got, w)
		}
	}
	f := func(id int64, v string) string { return fmt.Sprintf("%d:%s", id, v) }

	_, got := list(f(size, "M"))
	want(got, "medium")
	_, got = list(f(urgent, "true"))
	want(got, "small")
	_, got = list(f(urgent, "false")) // unticked or never set
	want(got, "medium", "large", "bare")
	_, got = list(f(points, ">=3.5"))
	want(got, "medium", "large")
	_, got = list(f(points, "<3"))
	want(got, "small")
	_, got = list(f(points, "8"))
	want(got, "large")
	_, got = list(f(note, "LEGAL")) // contains, any case; a value JSON escapes (<) still matches
	want(got, "small")
	_, got = list(f(note, "<legal>"))
	want(got, "small")
	_, got = list(f(note, "100%")) // LIKE wildcards are literal
	want(got)
	_, got = list(f(size, "")) // no value
	want(got, "bare")
	_, got = list(f(size, "L"), f(points, ">5")) // all must match
	want(got, "large")
	_, got = list(f(size, "L"), f(points, "<5"))
	want(got)

	// the project list takes them too
	code, out, _ := e.req("GET", fmt.Sprintf("/projects/%d/gigs?field=%d:S", pid, size), "", ada)
	if code != 200 || !strings.Contains(string(out), `"title":"small"`) || strings.Contains(string(out), `"title":"medium"`) {
		t.Fatalf("project list: %d %s", code, out)
	}

	// a saved view keeps them (field.<id>) and the columns (cols); other keys are still refused
	code, out, _ = e.req("POST", "/me/filters", fmt.Sprintf(`{"name":"Big","query":{"field.%d":"L","cols":"%d,%d"}}`, size, size, points), ada)
	e.ok(code, out, 201)
	for _, k := range []string{"field.x", "field.", "field.01", "fields"} {
		if code, _, _ := e.req("POST", "/me/filters", fmt.Sprintf(`{"name":"Bad","query":{%q:"L"}}`, k), ada); code != 422 {
			t.Fatalf("saved filter key %q: %d", k, code)
		}
	}

	for _, bad := range []string{f(due, "2026-01-01"), f(points, "lots"), f(urgent, "yes"), "x:1", f(e.ids["atlas.field"], "M")} {
		if code, _ := list(bad); code != 422 {
			t.Fatalf("filter %q: %d", bad, code)
		}
	}
}
