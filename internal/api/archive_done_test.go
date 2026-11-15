package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/0x2alx/gigg/internal/store"
)

// TestArchiveDoneCategory: archive-done takes done and cancelled tasks by default, and only one of
// them when `category` names it (the board's Done and Cancelled columns each archive their own).
func TestArchiveDoneCategory(t *testing.T) {
	e := newEnv(t)
	tasks := fmt.Sprintf("/projects/%d/tasks", e.ids["scout.p"])
	mk := func(title, cat string) store.Task {
		t.Helper()
		code, out, _ := e.req("POST", tasks, `{"title":"`+title+`"}`, e.ada())
		e.ok(code, out, 201)
		var tk store.Task
		_ = json.Unmarshal(out, &tk)
		code, out, _ = e.req("POST", "/tasks/"+tk.Key+"/move", `{"category":"`+cat+`"}`, e.ada())
		e.ok(code, out, 200)
		return tk
	}
	mk("Finished one", "done")
	mk("Finished two", "done")
	mk("Dropped", "cancelled")
	arch := fmt.Sprintf("/projects/%d/archive-done", e.ids["scout.p"])
	code, out, _ := e.req("POST", arch, `{"older_than_days":0,"category":"cancelled"}`, e.ada())
	e.ok(code, out, 200)
	if !strings.Contains(string(out), `"archived":1`) {
		t.Fatalf("cancelled only: %s", out)
	}
	code, out, _ = e.req("POST", arch, `{"older_than_days":0,"category":"todo"}`, e.ada())
	if code != 422 && code != 400 {
		t.Fatalf("bad category: %d %s", code, out)
	}
	code, out, _ = e.req("POST", arch, `{"older_than_days":0}`, e.ada())
	e.ok(code, out, 200)
	if !strings.Contains(string(out), `"archived":2`) {
		t.Fatalf("both (the two done left): %s", out)
	}
}
