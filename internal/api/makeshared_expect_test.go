package api

import (
	"fmt"
	"strings"
	"testing"
)

// TestMakeSharedExpectCopies: expect_copies ties the execute call to the plan that was reviewed: when
// the copies computed now differ, the call is refused (409) and nothing changes.
func TestMakeSharedExpectCopies(t *testing.T) {
	e := newEnv(t)
	s := shareEnv{e, t}
	ada := e.ada()
	scout, atlas := fmt.Sprint(e.ids["scout.ws"]), fmt.Sprint(e.ids["atlas.ws"])
	src := idOf(t, s.post("/workspaces/"+scout+"/schedules", `{"name":"Mornings",`+daily7+`}`, ada, 201))
	copyA := idOf(t, s.post("/workspaces/"+atlas+"/schedules", `{"name":"Atlas",`+daily7+`}`, ada, 201))
	path := fmt.Sprintf("/schedules/%d/make-shared", src)

	p := readPlan(t, s.post(path, `{"dry_run":true}`, ada, 200))
	if len(p.Copies) != 1 || p.Copies[0].ID != copyA {
		t.Fatalf("plan: %+v", p)
	}
	// a new identical copy appears after the review
	late := idOf(t, s.post("/workspaces/"+atlas+"/schedules", `{"name":"Late",`+daily7+`}`, ada, 201))
	for _, body := range []string{fmt.Sprintf(`{"expect_copies":[%d]}`, copyA), `{"expect_copies":[]}`, fmt.Sprintf(`{"expect_copies":[%d,%d,999]}`, copyA, late)} {
		code, out, _ := e.req("POST", path, body, ada)
		if code != 409 || !strings.Contains(string(out), "review the plan again") {
			t.Fatalf("%s: %d %s", body, code, out)
		}
	}
	for _, id := range []int64{src, copyA, late} {
		if code, _, _ := e.req("GET", fmt.Sprintf("/schedules/%d", id), "", ada); code != 200 {
			t.Fatalf("schedule %d changed by a refused move", id)
		}
	}
	// a dry run takes no expectation; the order of the ids doesn't matter; without it nothing is checked
	s.post(path, fmt.Sprintf(`{"dry_run":true,"expect_copies":[%d]}`, copyA), ada, 200)
	p = readPlan(t, s.post(path, fmt.Sprintf(`{"expect_copies":[%d,%d]}`, late, copyA), ada, 200))
	if p.Shared == nil || len(p.Copies) != 2 {
		t.Fatalf("result: %+v", p)
	}
	src2 := idOf(t, s.post("/workspaces/"+scout+"/schedules", `{"name":"Other",`+strings.Replace(daily7, "07:00", "05:00", 1)+`}`, ada, 201))
	p = readPlan(t, s.post(fmt.Sprintf("/schedules/%d/make-shared", src2), `{}`, ada, 200))
	if p.Shared == nil {
		t.Fatalf("without expect_copies: %+v", p)
	}
}
