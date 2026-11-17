package api

import (
	"encoding/json"
	"fmt"
	"testing"
)

// TestSubgigGates: a new gig's own draft subgigs come with it in one call, and a subgig can be
// switched to hold its parent back (a dependency change, part of Edit gigs).
func TestSubgigGates(t *testing.T) {
	e := newEnv(t)
	scout := e.key("scout")
	newGig := fmt.Sprintf("/projects/%d/gigs", e.ids["scout.p"])
	var parent, sub struct {
		ID        int64 `json:"id"`
		BlockedBy int64 `json:"blocked_by"`
		SubgigCnt int64 `json:"subgig_count"`
	}
	code, out, _ := e.req("POST", newGig, `{"title":"ship","subgigs":[{"title":"tests","blocks_parent":true},{"title":"wiki"}]}`, scout)
	e.ok(code, out, 201)
	_ = json.Unmarshal(out, &parent)
	if parent.BlockedBy != 1 || parent.SubgigCnt != 2 {
		t.Fatalf("created with subgigs: %s", out)
	}
	code, out, _ = e.req("POST", newGig, fmt.Sprintf(`{"title":"side","parent_id":%d}`, parent.ID), scout)
	e.ok(code, out, 201)
	_ = json.Unmarshal(out, &sub)

	// dependencies (blocks_parent, relations) are part of Edit gigs: no permission of their own
	code, out, _ = e.req("PATCH", fmt.Sprintf("/gigs/%d", sub.ID), `{"blocks_parent":true}`, scout)
	e.ok(code, out, 200)
	code, out, _ = e.req("GET", fmt.Sprintf("/gigs/%d", parent.ID), "", scout)
	e.ok(code, out, 200)
	_ = json.Unmarshal(out, &parent)
	if parent.BlockedBy != 2 {
		t.Fatalf("two gates: %s", out)
	}
}
