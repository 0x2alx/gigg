package api

import (
	"encoding/json"
	"fmt"
	"testing"
)

// TestReorderFields: managers set the order of a workspace's custom fields with every id, in order;
// the list comes back in that order. A partial list, a foreign field or an agent is refused.
func TestReorderFields(t *testing.T) {
	e := newEnv(t)
	ada, scout := e.ada(), e.key("scout")
	for _, n := range []string{"Alpha", "Beta", "Gamma"} {
		code, out, _ := e.req("POST", "/workspaces/scout/fields", fmt.Sprintf(`{"name":%q,"kind":"text"}`, n), ada)
		e.ok(code, out, 201)
	}
	type field struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	list := func() []field {
		code, out, _ := e.req("GET", "/workspaces/scout/fields", "", ada)
		e.ok(code, out, 200)
		var r struct {
			Items []field `json:"items"`
		}
		if err := json.Unmarshal(out, &r); err != nil {
			t.Fatal(err)
		}
		return r.Items
	}
	fs := list()
	if len(fs) < 3 {
		t.Fatalf("fields: %v", fs)
	}
	// reversed
	ids := make([]int64, len(fs))
	for i, f := range fs {
		ids[len(fs)-1-i] = f.ID
	}
	body := func(ids []int64) string {
		b, _ := json.Marshal(map[string]any{"ids": ids})
		return string(b)
	}
	if code, _, _ := e.req("POST", "/workspaces/scout/fields/reorder", body(ids), scout); code != 403 {
		t.Fatalf("agent reorders fields: %d", code)
	}
	if code, _, _ := e.req("POST", "/workspaces/scout/fields/reorder", body(ids[1:]), ada); code != 422 {
		t.Fatalf("partial list: %d", code)
	}
	bad := append([]int64{e.ids["atlas.field"]}, ids[1:]...)
	if code, _, _ := e.req("POST", "/workspaces/scout/fields/reorder", body(bad), ada); code != 422 {
		t.Fatalf("another workspace's field: %d", code)
	}
	dup := append([]int64{ids[1]}, ids[1:]...)
	if code, _, _ := e.req("POST", "/workspaces/scout/fields/reorder", body(dup), ada); code != 422 {
		t.Fatalf("a field listed twice: %d", code)
	}
	code, out, _ := e.req("POST", "/workspaces/scout/fields/reorder", body(ids), ada)
	e.ok(code, out, 204)
	got := list()
	for i, f := range got {
		if f.ID != ids[i] {
			t.Fatalf("order after reorder: %v, want ids %v", got, ids)
		}
	}
}
