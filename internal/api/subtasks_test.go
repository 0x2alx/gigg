package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestSubtaskGatePermission: making a subtask hold its parent back is a dependency change, so it
// needs the relations permission (at create, on a switch, and when a holding subtask moves to
// another parent); a new task's own draft subtasks and links come with it in one call.
func TestSubtaskGatePermission(t *testing.T) {
	e := newEnv(t)
	scout := e.key("scout")
	ws := e.ids["scout.ws"]
	newTask := fmt.Sprintf("/projects/%d/tasks", e.ids["scout.p"])
	var parent, sub struct {
		ID         int64 `json:"id"`
		BlockedBy  int64 `json:"blocked_by"`
		SubtaskCnt int64 `json:"subtask_count"`
	}
	code, out, _ := e.req("POST", newTask, `{"title":"ship","subtasks":[{"title":"tests","blocks_parent":true},{"title":"wiki"}]}`, scout)
	e.ok(code, out, 201)
	_ = json.Unmarshal(out, &parent)
	if parent.BlockedBy != 1 || parent.SubtaskCnt != 2 {
		t.Fatalf("created with subtasks: %s", out)
	}
	code, out, _ = e.req("POST", newTask, fmt.Sprintf(`{"title":"side","parent_id":%d}`, parent.ID), scout)
	e.ok(code, out, 201)
	_ = json.Unmarshal(out, &sub)

	uAccess := fmt.Sprintf("/users/%d/access", e.ids["scout.user"])
	e.req("PUT", uAccess, fmt.Sprintf(`{"workspace_id":%d,"preset":"member","overrides":{"relations":false}}`, ws), e.ada())
	if code, out, _ := e.req("POST", newTask, fmt.Sprintf(`{"title":"gate","parent_id":%d,"blocks_parent":true}`, parent.ID), scout); code != 403 || !strings.Contains(string(out), "relations") {
		t.Fatalf("gate at create without relations: %d %s", code, out)
	}
	if code, out, _ := e.req("PATCH", fmt.Sprintf("/tasks/%d", sub.ID), `{"blocks_parent":true}`, scout); code != 403 {
		t.Fatalf("gate switch without relations: %d %s", code, out)
	}
	if code, out, _ := e.req("POST", newTask, `{"title":"x","relations":[{"task":"1","direction":"blocks"}]}`, scout); code != 403 {
		t.Fatalf("relations at create without the permission: %d %s", code, out)
	}
	// a plain subtask is fine without it
	if code, out, _ := e.req("POST", newTask, fmt.Sprintf(`{"title":"plain","parent_id":%d}`, parent.ID), scout); code != 201 {
		t.Fatalf("plain subtask: %d %s", code, out)
	}
	e.req("PUT", uAccess, fmt.Sprintf(`{"workspace_id":%d,"preset":"member"}`, ws), e.ada())
	code, out, _ = e.req("PATCH", fmt.Sprintf("/tasks/%d", sub.ID), `{"blocks_parent":true}`, scout)
	e.ok(code, out, 200)
	code, out, _ = e.req("GET", fmt.Sprintf("/tasks/%d", parent.ID), "", scout)
	e.ok(code, out, 200)
	_ = json.Unmarshal(out, &parent)
	if parent.BlockedBy != 2 {
		t.Fatalf("two gates: %s", out)
	}
}
