package store

import (
	"database/sql"
	"encoding/json"
	"slices"
	"strconv"
	"testing"
)

// followFix: the base fixture plus a human (bob) who sees the workspace as a viewer.
type followFix struct {
	*fixture
	bob *User
}

func newFollowFix(t *testing.T) *followFix {
	f := &followFix{fixture: newFixture(t)}
	f.tx(t, func(tx *sql.Tx) error {
		var err error
		if f.bob, err = CreateUser(f.ctx, tx, Actor{}, NewUser{Username: "bob", Kind: "human"}); err != nil {
			return err
		}
		return SetMembership(f.ctx, tx, f.a, f.ws.ID, f.bob.ID, "viewer")
	})
	return f
}

func (f *followFix) prefs(t *testing.T, user int64, prefs map[string]any) {
	t.Helper()
	raw := json.RawMessage(mustJSON(prefs))
	f.tx(t, func(tx *sql.Tx) error {
		_, err := PatchUser(f.ctx, tx, Actor{UserID: user}, user, 0, UserPatch{Preferences: &raw})
		return err
	})
}

// bells: a user's notification kinds about a gig, oldest first.
func (f *followFix) bells(t *testing.T, user, gig int64) []string {
	t.Helper()
	rows, err := f.s.R.QueryContext(f.ctx, `SELECT kind FROM notifications WHERE user_id = ? AND gig_id = ? ORDER BY id`, user, gig)
	must(t, err)
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var k string
		must(t, rows.Scan(&k))
		out = append(out, k)
	}
	return out
}

// run: the agent works g and finishes it with fin.
func (f *followFix) run(t *testing.T, g *Gig, fin Finish) {
	t.Helper()
	agent := Actor{UserID: f.agent.ID}
	f.tx(t, func(tx *sql.Tx) error {
		cur, err := GetGig(f.ctx, tx, g.ID)
		if err != nil {
			return err
		}
		r, err := StartRun(f.ctx, tx, agent, cur, nil, "new", 1, nil)
		if err != nil {
			return err
		}
		_, _, err = FinishRun(f.ctx, tx, agent, r, fin)
		return err
	})
}

func TestFollowersGetTheBell(t *testing.T) {
	f := newFollowFix(t)
	// the creator (ada) follows by default; bob is named as a follower; the agent is assigned
	g := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "watch me", Assignees: []int64{f.agent.ID}, Followers: []int64{f.bob.ID}})
	if !slices.Equal(g.Followers, []int64{f.admin.ID, f.bob.ID}) {
		t.Fatalf("followers %v", g.Followers)
	}
	// a clean finish rings nobody by default; a question rings every follower
	f.run(t, g, Finish{Status: "done", ReportMD: "All done.", CommentKind: "report"})
	if b := f.bells(t, f.bob.ID, g.ID); len(b) != 0 {
		t.Fatalf("bob heard a clean finish: %v", b)
	}
	f.run(t, g, Finish{Status: "needs_human", ReportMD: "Siren or chime?", CommentKind: "question"})
	if b := f.bells(t, f.bob.ID, g.ID); !slices.Equal(b, []string{"question"}) {
		t.Fatalf("bob: %v", b)
	}
	if b := f.bells(t, f.admin.ID, g.ID); !slices.Equal(b, []string{"question"}) {
		t.Fatalf("ada (creator, following): %v", b)
	}
	// the agent asked: it hears nothing about its own question
	if b := f.bells(t, f.agent.ID, g.ID); !slices.Equal(b, []string{"assigned"}) {
		t.Fatalf("agent: %v", b)
	}

	// bob turns "finished" on: a report now rings him
	f.prefs(t, f.bob.ID, map[string]any{"bell_finished": true})
	f.run(t, g, Finish{Status: "done", ReportMD: "Chime it is.", CommentKind: "report"})
	if b := f.bells(t, f.bob.ID, g.ID); !slices.Equal(b, []string{"question", "comment"}) {
		t.Fatalf("bob with finished on: %v", b)
	}
	// ...and a timeout with no report rings the watchers as a failed run
	f.run(t, g, Finish{Status: "timeout"})
	if b := f.bells(t, f.bob.ID, g.ID); !slices.Equal(b, []string{"question", "comment", "failed"}) {
		t.Fatalf("bob after a timeout: %v", b)
	}
	// with problems off, bob hears no question
	f.prefs(t, f.bob.ID, map[string]any{"bell_problems": false})
	f.run(t, g, Finish{Status: "needs_human", ReportMD: "Again?", CommentKind: "question"})
	if b := f.bells(t, f.bob.ID, g.ID); len(b) != 3 {
		t.Fatalf("bob with problems off: %v", b)
	}
}

func TestFollowerWhoLosesViewHearsNothing(t *testing.T) {
	f := newFollowFix(t)
	g := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "private soon", Followers: []int64{f.bob.ID}})
	f.tx(t, func(tx *sql.Tx) error { return SetMembership(f.ctx, tx, f.a, f.ws.ID, f.bob.ID, "") })
	// losing access takes bob off the followers, as it does for assignees
	if cur, err := GetGig(f.ctx, f.s.R, g.ID); err != nil || slices.Contains(cur.Followers, f.bob.ID) {
		t.Fatalf("bob still follows after losing access: %v %v", cur.Followers, err)
	}
	f.run(t, g, Finish{Status: "needs_human", ReportMD: "Anyone?", CommentKind: "question"})
	if b := f.bells(t, f.bob.ID, g.ID); len(b) != 0 {
		t.Fatalf("a follower without view got %v", b)
	}
	// and nobody can be made to follow a gig they can't see
	other := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "not for bob"})
	f.tx(t, func(tx *sql.Tx) error {
		cur, _ := GetGig(f.ctx, tx, other.ID)
		if _, err := PatchGig(f.ctx, tx, f.a, cur, 0, GigPatch{Followers: []int64{f.bob.ID}}); err == nil {
			t.Fatal("a follower who can't see the gig was accepted")
		}
		if _, err := FollowGig(f.ctx, tx, Actor{UserID: f.bob.ID}, cur, f.bob.ID, true); err == nil {
			t.Fatal("following a gig you can't see was accepted")
		}
		return nil
	})
}

func TestFollowerAndAssigneeRingOnce(t *testing.T) {
	f := newFollowFix(t)
	f.tx(t, func(tx *sql.Tx) error { return SetMembership(f.ctx, tx, f.a, f.ws.ID, f.bob.ID, "member") })
	g := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "both", Assignees: []int64{f.bob.ID}, Followers: []int64{f.bob.ID}})
	f.run(t, g, Finish{Status: "needs_human", ReportMD: "Which one?", CommentKind: "question"})
	if b := f.bells(t, f.bob.ID, g.ID); !slices.Equal(b, []string{"assigned", "question"}) {
		t.Fatalf("bob: %v", b)
	}
}

func TestFollowOwnGigsOff(t *testing.T) {
	f := newFollowFix(t)
	f.prefs(t, f.admin.ID, map[string]any{FollowOwnPref: false})
	g := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "for bob", Followers: []int64{f.bob.ID}})
	if !slices.Equal(g.Followers, []int64{f.bob.ID}) {
		t.Fatalf("followers %v", g.Followers)
	}
	f.run(t, g, Finish{Status: "error", ReportMD: "runner: the run ended in an error", CommentKind: "question"})
	if b := f.bells(t, f.admin.ID, g.ID); len(b) != 0 {
		t.Fatalf("the creator, not following, got %v", b)
	}
	if b := f.bells(t, f.bob.ID, g.ID); !slices.Equal(b, []string{"question"}) {
		t.Fatalf("bob: %v", b)
	}
}

func TestFollowGigAndAudit(t *testing.T) {
	f := newFollowFix(t)
	f.prefs(t, f.admin.ID, map[string]any{FollowOwnPref: false})
	g := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "follow me"})
	bob := Actor{UserID: f.bob.ID}
	f.tx(t, func(tx *sql.Tx) error {
		after, err := FollowGig(f.ctx, tx, bob, g, f.bob.ID, true)
		if err != nil {
			return err
		}
		if !slices.Equal(after.Followers, []int64{f.bob.ID}) || after.Version != g.Version {
			t.Fatalf("after follow: %v v%d (was v%d)", after.Followers, after.Version, g.Version)
		}
		if again, err := FollowGig(f.ctx, tx, bob, after, f.bob.ID, true); err != nil || again.Version != after.Version {
			t.Fatalf("following twice: %v", err)
		}
		_, err = FollowGig(f.ctx, tx, bob, after, f.bob.ID, false)
		return err
	})
	rows, err := f.s.R.QueryContext(f.ctx, `SELECT data FROM activity WHERE action = 'gig.updated' AND entity_type = 'gig' AND entity_id = ? ORDER BY id`, g.ID)
	must(t, err)
	defer rows.Close()
	var data []string
	for rows.Next() {
		var d string
		must(t, rows.Scan(&d))
		data = append(data, d)
	}
	if len(data) != 2 || data[0] != `{"followers":{"new":[`+strconv.FormatInt(f.bob.ID, 10)+`],"old":[]}}` || data[1] != `{"followers":{"new":[],"old":[`+strconv.FormatInt(f.bob.ID, 10)+`]}}` {
		t.Fatalf("audit entries %v", data)
	}
}

func TestTemplateFollowersReachItsGigs(t *testing.T) {
	f := newFollowFix(t)
	f.prefs(t, f.admin.ID, map[string]any{FollowOwnPref: false})
	var tpl *Template
	f.tx(t, func(tx *sql.Tx) error {
		var err error
		tpl, err = SaveTemplate(f.ctx, tx, f.a, &Template{WorkspaceID: f.ws.ID, Name: "nightly", Title: "Nightly", Followers: []int64{f.bob.ID, f.bob.ID}}, 0)
		return err
	})
	if !slices.Equal(tpl.Followers, []int64{f.bob.ID}) {
		t.Fatalf("template followers %v", tpl.Followers)
	}
	var g *Gig
	f.tx(t, func(tx *sql.Tx) error {
		var err error
		g, err = GigFromTemplate(f.ctx, tx, f.a, tpl, f.p1, nil, nil)
		return err
	})
	if !slices.Equal(g.Followers, []int64{f.bob.ID}) {
		t.Fatalf("gig from template: followers %v", g.Followers)
	}
	// a follower who lost access is left out of new gigs instead of failing them
	f.tx(t, func(tx *sql.Tx) error { return SetMembership(f.ctx, tx, f.a, f.ws.ID, f.bob.ID, "") })
	f.tx(t, func(tx *sql.Tx) error {
		var err error
		g, err = GigFromTemplate(f.ctx, tx, f.a, tpl, f.p1, nil, nil)
		return err
	})
	if len(g.Followers) != 0 {
		t.Fatalf("a follower without view was copied: %v", g.Followers)
	}
	// a shared template has none
	f.tx(t, func(tx *sql.Tx) error {
		if _, err := SaveTemplate(f.ctx, tx, f.a, &Template{Name: "shared", Title: "Shared", Followers: []int64{f.admin.ID}}, 0); err == nil {
			t.Fatal("a shared template took followers")
		}
		return nil
	})
}

// A disabled follower in a template never fails a gig made from it, and the creator's own follow
// never pushes a full list past the cap.
func TestFollowerCopiesNeverFail(t *testing.T) {
	f := newFollowFix(t)
	var tpl *Template
	f.tx(t, func(tx *sql.Tx) error {
		var err error
		tpl, err = SaveTemplate(f.ctx, tx, f.a, &Template{WorkspaceID: f.ws.ID, Name: "t", Title: "T", Followers: []int64{f.bob.ID}}, 0)
		return err
	})
	disabled := true
	f.tx(t, func(tx *sql.Tx) error {
		_, err := PatchUser(f.ctx, tx, f.a, f.bob.ID, 0, UserPatch{Disabled: &disabled})
		return err
	})
	var g *Gig
	f.tx(t, func(tx *sql.Tx) error {
		var err error
		g, err = GigFromTemplate(f.ctx, tx, f.a, tpl, f.p1, nil, nil)
		return err
	})
	if slices.Contains(g.Followers, f.bob.ID) {
		t.Fatalf("a disabled follower was copied: %v", g.Followers)
	}
	// 50 named followers: the creator doesn't add a 51st
	var many []int64
	f.tx(t, func(tx *sql.Tx) error {
		for i := 0; i < MaxFollowers; i++ {
			u, err := CreateUser(f.ctx, tx, Actor{}, NewUser{Username: "v" + strconv.Itoa(i), Kind: "human"})
			if err != nil {
				return err
			}
			if err := SetMembership(f.ctx, tx, f.a, f.ws.ID, u.ID, "viewer"); err != nil {
				return err
			}
			many = append(many, u.ID)
		}
		return nil
	})
	full := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "crowded", Followers: many})
	if len(full.Followers) != MaxFollowers || slices.Contains(full.Followers, f.admin.ID) {
		t.Fatalf("followers %d, creator in: %v", len(full.Followers), slices.Contains(full.Followers, f.admin.ID))
	}
}
