package api

import (
	"fmt"
	"strings"
	"testing"
)

// TestTimeZoneDefaults: a recurring task or schedule that names no zone takes the user's time zone
// preference, else the instance default; an edit that names none keeps the current zone.
func TestTimeZoneDefaults(t *testing.T) {
	e := newEnv(t)
	recs := fmt.Sprintf("/projects/%d/recurrences", e.ids["scout.p"])
	code, out, _ := e.req("POST", recs, `{"title":"Default zone","freq":"daily","window_start":"07:00","window_minutes":60}`, e.ada())
	e.ok(code, out, 201)
	if !strings.Contains(string(out), `"tz":"UTC"`) {
		t.Fatalf("instance default: %s", out)
	}
	code, out, _ = e.req("PATCH", "/me", `{"preferences":{"timezone":"Europe/Paris"}}`, e.ada())
	e.ok(code, out, 200)
	code, out, _ = e.req("POST", recs, `{"title":"My zone","freq":"daily","window_start":"08:00","window_minutes":60}`, e.ada())
	e.ok(code, out, 201)
	if !strings.Contains(string(out), `"tz":"Europe/Paris"`) {
		t.Fatalf("user preference: %s", out)
	}
	code, out, _ = e.req("POST", "/workspaces/scout/schedules", `{"name":"Mornings","freq":"daily","window_start":"06:00","window_minutes":60}`, e.ada())
	e.ok(code, out, 201)
	if !strings.Contains(string(out), `"tz":"Europe/Paris"`) {
		t.Fatalf("schedule: user preference: %s", out)
	}
	// a junk preference falls back to the instance default
	code, out, _ = e.req("PATCH", "/me", `{"preferences":{"timezone":"Mars/Olympus"}}`, e.ada())
	e.ok(code, out, 200)
	code, out, _ = e.req("POST", recs, `{"title":"Junk zone","freq":"daily","window_start":"09:00","window_minutes":60}`, e.ada())
	e.ok(code, out, 201)
	if !strings.Contains(string(out), `"tz":"UTC"`) {
		t.Fatalf("junk preference: %s", out)
	}
}

// TestPreferencesMerge: PATCH /me merges preferences by top-level key, so two tabs each saving their own
// key don't undo each other; null removes a key.
func TestPreferencesMerge(t *testing.T) {
	e := newEnv(t)
	code, out, _ := e.req("PATCH", "/me", `{"preferences":{"sidebar":"collapsed","board_filter":"a=me"}}`, e.ada())
	e.ok(code, out, 200)
	code, out, _ = e.req("PATCH", "/me", `{"preferences":{"board_filter":"l=bug"}}`, e.ada())
	e.ok(code, out, 200)
	if !strings.Contains(string(out), `"sidebar":"collapsed"`) || !strings.Contains(string(out), `"board_filter":"l=bug"`) {
		t.Fatalf("merge: %s", out)
	}
	code, out, _ = e.req("PATCH", "/me", `{"preferences":{"sidebar":null}}`, e.ada())
	e.ok(code, out, 200)
	if strings.Contains(string(out), `"sidebar"`) || !strings.Contains(string(out), `"board_filter":"l=bug"`) {
		t.Fatalf("null removes: %s", out)
	}
	code, out, _ = e.req("PATCH", "/me", `{"preferences":["not","an","object"]}`, e.ada())
	if code != 400 && code != 422 {
		t.Fatalf("array accepted: %d %s", code, out)
	}
}
