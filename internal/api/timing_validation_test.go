package api

import (
	"fmt"
	"strings"
	"testing"
)

// TestTimingRefusesZeros: an explicit interval 0, window_minutes 0 (or under 5) or a negative
// lead_minutes is refused with a 422 naming the field, for recurring gigs and schedules alike;
// leaving the field out still means the default.
func TestTimingRefusesZeros(t *testing.T) {
	e := newEnv(t)
	ada := e.ada()
	ws, pid := e.ids["scout.ws"], e.ids["scout.p"]
	recs := fmt.Sprintf("/projects/%d/recurrences", pid)
	scheds := fmt.Sprintf("/workspaces/%d/schedules", ws)
	cases := []struct{ extra, field string }{
		{`"interval":0`, "interval"},
		{`"window_minutes":0`, "window_minutes"},
		{`"window_minutes":4`, "window_minutes"},
		{`"lead_minutes":-5`, "lead_minutes"},
	}
	for _, c := range cases {
		code, out, _ := e.req("POST", recs, `{"title":"x","freq":"daily","tz":"UTC",`+c.extra+`}`, ada)
		if code != 422 || !strings.Contains(string(out), c.field) {
			t.Fatalf("recurring with %s: %d %s", c.extra, code, out)
		}
		code, out, _ = e.req("POST", scheds, `{"name":"s","freq":"daily","tz":"UTC",`+c.extra+`}`, ada)
		if code != 422 || !strings.Contains(string(out), c.field) {
			t.Fatalf("schedule with %s: %d %s", c.extra, code, out)
		}
	}
	// omitted: every 1, all day
	code, out, _ := e.req("POST", recs, `{"title":"x","freq":"daily","tz":"UTC"}`, ada)
	e.ok(code, out, 201)
	if !strings.Contains(string(out), `"interval":1`) || !strings.Contains(string(out), `"window_minutes":1440`) {
		t.Fatalf("defaults: %s", out)
	}
}
