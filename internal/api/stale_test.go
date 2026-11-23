package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/0x2alx/gigg/internal/store"
)

// last_progress_at and /stale: the caller's gigs in Doing with no progress for N hours.
func TestStaleGigs(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	gig, user := e.ids["scout.gig"], e.ids["scout.user"]
	old := store.Now() - 48*3600*1000
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := e.st.W.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT OR IGNORE INTO gig_assignees (gig_id, user_id) VALUES (?, ?)`, gig, user)
	exec(`UPDATE gigs SET status_id = (SELECT s.id FROM statuses s JOIN gigs g ON g.project_id = s.project_id
		WHERE g.id = ? AND s.category = 'doing' ORDER BY s.position LIMIT 1), last_progress_at = ? WHERE id = ?`, gig, old, gig)

	stale := func(hours int) (int, []store.StaleGig) {
		t.Helper()
		code, out, _ := e.req("GET", fmt.Sprintf("/workspaces/scout/stale?hours=%d", hours), "", e.key("scout"))
		e.ok(code, out, 200)
		var b struct {
			Items []store.StaleGig `json:"items"`
			Total int              `json:"total"`
		}
		if err := json.Unmarshal(out, &b); err != nil {
			t.Fatal(err)
		}
		return b.Total, b.Items
	}
	n, items := stale(24)
	if n != 1 || len(items) != 1 || items[0].Hours < 47 || items[0].Status == "" || !strings.HasPrefix(items[0].Key, "GIG-") {
		t.Fatalf("stale: %d %+v", n, items)
	}
	if n, _ := stale(72); n != 0 {
		t.Fatalf("not 72h old yet: %d", n)
	}
	code, out, _ := e.req("GET", "/workspaces/scout/summary", "", e.key("scout"))
	e.ok(code, out, 200)
	if !strings.Contains(string(out), "Stale: 1 of your gigs in Doing") {
		t.Fatalf("summary: %s", out)
	}
	// someone else's view: not assigned to them
	if code, out, _ := e.req("GET", "/workspaces/scout/stale", "", e.ada()); code == 200 && strings.Contains(string(out), `"total":1`) {
		t.Fatalf("ada sees scout's stale gig as hers: %s", out)
	}

	a := store.Actor{UserID: e.ids["ada"]}
	touch := func(what string, fn func(tx *sql.Tx, g *store.Gig) error, counts bool) {
		t.Helper()
		exec(`UPDATE gigs SET last_progress_at = ? WHERE id = ?`, old, gig)
		if err := e.st.Tx(ctx, func(tx *sql.Tx) error {
			g, err := store.GetGig(ctx, tx, gig)
			if err != nil {
				return err
			}
			return fn(tx, g)
		}); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if n, _ := stale(24); (n == 0) != counts {
			t.Fatalf("%s: counts as progress %v, want %v", what, n == 0, counts)
		}
	}
	touch("a comment", func(tx *sql.Tx, g *store.Gig) error {
		_, err := store.AddComment(ctx, tx, a, g, "comment", "still on it", nil)
		return err
	}, true)
	touch("a system note", func(tx *sql.Tx, g *store.Gig) error {
		_, err := store.AddComment(ctx, tx, a, g, "system", "moved by the runner", nil)
		return err
	}, false)
	touch("a title edit", func(tx *sql.Tx, g *store.Gig) error {
		_, err := tx.Exec(`UPDATE gigs SET title = 'renamed', updated_at = ? WHERE id = ?`, store.Now(), g.ID)
		return err
	}, false)

	// a checklist tick through the API
	exec(`UPDATE gigs SET last_progress_at = ? WHERE id = ?`, old, gig)
	var item int64
	if err := e.st.R.QueryRow(`SELECT id FROM checklist_items WHERE gig_id = ? LIMIT 1`, gig).Scan(&item); err != nil {
		t.Fatal(err)
	}
	code, out, _ = e.reqH("PATCH", fmt.Sprintf("/gigs/%d/checklist/%d", gig, item), `{"done":true}`, e.ada(), map[string]string{"X-Gigg-CSRF": "1"})
	e.ok(code, out, 200)
	if n, _ := stale(24); n != 0 {
		t.Fatal("a checklist tick isn't progress")
	}
	// the gig carries it
	code, out, _ = e.req("GET", fmt.Sprintf("/gigs/%d", gig), "", e.ada())
	e.ok(code, out, 200)
	var g store.Gig
	_ = json.Unmarshal(out, &g)
	if g.LastProgressAt < store.Now()-60000 {
		t.Fatalf("last_progress_at %d", g.LastProgressAt)
	}
}
