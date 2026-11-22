package store

import (
	"context"
	"database/sql"
	"slices"
	"strconv"
	"testing"
)

// TestConsumersMigration: 0016 gives work_gigs wherever a rule says something about edit_gigs and
// gigs:work to every key with gigs:write; turns commenter rules into viewer + comment, renames
// schedules to recurring_gigs, folds a checklists or relations deny into edit_gigs and drops
// the scopes nothing checked; and assigns the workspace owner to open gigs, workspace
// templates and recurring gigs on a shared template that had nobody (not to done gigs, not where the
// owner can't be assigned).
func TestConsumersMigration(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	migrateLimit = 15
	s, err := Open(ctx, dir, quiet)
	migrateLimit = 0
	if err != nil {
		t.Fatal(err)
	}
	// today's code reads access_rules.project_id (0017) and followers (0018): add them for the setup,
	// drop them before 0016 runs
	for _, q := range []string{`ALTER TABLE access_rules ADD COLUMN project_id INTEGER`,
		`CREATE TABLE gig_followers (gig_id INTEGER NOT NULL, user_id INTEGER NOT NULL, PRIMARY KEY (gig_id, user_id)) STRICT`,
		`ALTER TABLE gig_templates ADD COLUMN followers TEXT NOT NULL DEFAULT '[]'`,
		`CREATE TABLE attachments (id INTEGER PRIMARY KEY, gig_id INTEGER NOT NULL) STRICT`} {
		if _, err := s.W.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	var ws, ws2, pid, own, deny, open, done, tplNone, tplAda, tplShared, rec int64
	var ada *User
	must(t, s.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		if ada, err = CreateUser(ctx, tx, Actor{}, NewUser{Username: "ada", Kind: "human", IsAdmin: true}); err != nil {
			return err
		}
		a := Actor{UserID: ada.ID}
		o, err := CreateUser(ctx, tx, a, NewUser{Username: "own", Kind: "agent"})
		if err != nil {
			return err
		}
		d, err := CreateUser(ctx, tx, a, NewUser{Username: "lost", Kind: "agent"})
		if err != nil {
			return err
		}
		own, deny = o.ID, d.ID
		w, err := CreateWorkspace(ctx, tx, a, "own", "Own", nil) // owned by "own" (same name)
		if err != nil {
			return err
		}
		w2, err := CreateWorkspace(ctx, tx, a, "lost", "Lost", nil) // owned by "lost", who can't see it
		if err != nil {
			return err
		}
		ws, ws2 = w.ID, w2.ID
		if err := SetMembership(ctx, tx, a, ws, own, "member"); err != nil {
			return err
		}
		p, err := CreateProject(ctx, tx, a, ws, "main", "Main", "OWN", nil)
		if err != nil {
			return err
		}
		pid = p.ID
		p2, err := CreateProject(ctx, tx, a, ws2, "main", "Main", "LST", nil)
		if err != nil {
			return err
		}
		mk := func(project int64, title string) (*Gig, error) {
			return CreateGig(ctx, tx, a, NewGig{ProjectID: project, Title: title})
		}
		g, err := mk(pid, "open")
		if err != nil {
			return err
		}
		open = g.ID
		if g, err = mk(pid, "done"); err != nil {
			return err
		}
		if _, err = MoveGig(ctx, tx, a, g, 0, Move{Category: "done"}); err != nil {
			return err
		}
		done = g.ID
		if _, err = mk(p2.ID, "lost's"); err != nil {
			return err
		}
		t1, err := SaveTemplate(ctx, tx, a, &Template{WorkspaceID: ws, Name: "none", Title: "none"}, 0)
		if err != nil {
			return err
		}
		t2, err := SaveTemplate(ctx, tx, a, &Template{WorkspaceID: ws, Name: "ada's", Title: "ada's", Assignees: []int64{ada.ID}}, 0)
		if err != nil {
			return err
		}
		t3, err := SaveTemplate(ctx, tx, a, &Template{Name: "shared", Title: "shared"}, 0)
		if err != nil {
			return err
		}
		tplNone, tplAda, tplShared = t1.ID, t2.ID, t3.ID
		r, err := CreateRecurrence(ctx, tx, a, &Recurrence{ProjectID: pid, TemplateID: tplShared, Freq: "daily", TZ: "UTC", WindowStart: "09:00", WindowMinutes: 60})
		if err != nil {
			return err
		}
		rec = r.ID
		return nil
	}))
	// the state before 0016: nobody assigned (today's code would have given the owner already)
	for _, q := range []string{
		`DELETE FROM gig_assignees`,
		`UPDATE recurrences SET assignees = NULL`,
		`INSERT INTO access_rules (user_id, workspace_id, preset, overrides, created_at) VALUES (?1, ?4, 'member', '{"edit_gigs":false,"delete_gigs":false}', 1)`,
		`INSERT INTO api_keys (name, prefix, secret_hash, user_id, workspace_id, scopes, expires_at, created_at) VALUES ('w', 'gk_w', x'00', ?3, ?2, '["gigs:read","gigs:write"]', 9000000000000, 1)`,
		`INSERT INTO api_keys (name, prefix, secret_hash, user_id, workspace_id, scopes, expires_at, created_at) VALUES ('r', 'gk_r', x'00', ?3, ?2, '["gigs:read"]', 9000000000000, 1)`,
		`INSERT INTO api_keys (name, prefix, secret_hash, user_id, workspace_id, scopes, expires_at, created_at) VALUES ('a', 'gk_a', x'00', ?3, ?2, '["artifacts:write","gigs:read"]', 9000000000000, 1)`,
		`INSERT INTO api_keys (name, prefix, secret_hash, user_id, workspace_id, scopes, expires_at, created_at) VALUES ('adm', 'gk_adm', x'00', 1, NULL, '["admin:export","admin:keys","admin:settings","admin:transfer"]', 9000000000000, 1)`,
		`INSERT INTO groups (id, name, created_at) VALUES (90, 'g', 1)`,
		`ALTER TABLE access_rules DROP COLUMN project_id`,
		`DROP TABLE gig_followers`,
		`ALTER TABLE gig_templates DROP COLUMN followers`,
		`DROP TABLE attachments`,
		`INSERT INTO access_rules (group_id, workspace_id, preset, overrides, created_at) VALUES (90, ?4, 'commenter', '{}', 1)`,
		`INSERT INTO access_rules (group_id, workspace_id, preset, overrides, created_at) VALUES (90, ?2, 'member', '{"schedules":true,"checklists":false,"relations":true}', 1)`,
		`INSERT INTO access_rules (group_id, workspace_id, preset, overrides, created_at) VALUES (90, NULL, 'manager', '{"schedules":false}', 1)`,
	} {
		if _, err := s.W.ExecContext(ctx, q, deny, ws2, own, ws); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	s.Close()
	if s, err = Open(ctx, dir, quiet); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var overrides, scopesW, scopesR string
	must(t, s.R.QueryRowContext(ctx, `SELECT overrides FROM access_rules WHERE user_id = ?`, deny).Scan(&overrides))
	must(t, s.R.QueryRowContext(ctx, `SELECT scopes FROM api_keys WHERE name = 'w'`).Scan(&scopesW))
	must(t, s.R.QueryRowContext(ctx, `SELECT scopes FROM api_keys WHERE name = 'r'`).Scan(&scopesR))
	if overrides != `{"edit_gigs":false,"delete_gigs":false,"work_gigs":false}` || scopesW != `["gigs:read","gigs:write","gigs:work","attachments:write"]` || scopesR != `["gigs:read"]` {
		t.Fatalf("rules and keys: %s %s %s", overrides, scopesW, scopesR)
	}
	for _, c := range []struct{ where, want string }{
		{"workspace_id = " + strconv.FormatInt(ws, 10), `viewer {"comment":true}`},
		{"workspace_id = " + strconv.FormatInt(ws2, 10), `member {"recurring_gigs":true,"edit_gigs":false}`},
		{"workspace_id IS NULL", `manager {"recurring_gigs":false}`},
	} {
		var preset, ov string
		must(t, s.R.QueryRowContext(ctx, `SELECT preset, overrides FROM access_rules WHERE group_id = 90 AND `+c.where).Scan(&preset, &ov))
		if preset+" "+ov != c.want {
			t.Fatalf("group rule %s: %s %s, want %s", c.where, preset, ov, c.want)
		}
	}
	var scopesA string
	must(t, s.R.QueryRowContext(ctx, `SELECT scopes FROM api_keys WHERE name = 'a'`).Scan(&scopesA))
	if scopesA != `["gigs:read"]` {
		t.Fatalf("artifacts:write left: %s", scopesA)
	}
	must(t, s.R.QueryRowContext(ctx, `SELECT scopes FROM api_keys WHERE name = 'adm'`).Scan(&scopesA))
	if scopesA != `["admin:keys"]` {
		t.Fatalf("unused admin scopes left: %s", scopesA)
	}
	g, err := GetGig(ctx, s.R, open)
	must(t, err)
	d, err := GetGig(ctx, s.R, done)
	must(t, err)
	if !slices.Equal(g.Assignees, []int64{own}) || len(d.Assignees) != 0 {
		t.Fatalf("gigs: open %v, done %v", g.Assignees, d.Assignees)
	}
	var lost int
	must(t, s.R.QueryRowContext(ctx, `SELECT count(*) FROM gig_assignees a JOIN gigs t ON t.id = a.gig_id WHERE t.workspace_id = ?`, ws2).Scan(&lost))
	if lost != 0 {
		t.Fatalf("assigned an owner who can't see the workspace")
	}
	var listener string
	must(t, s.R.QueryRowContext(ctx, `SELECT listener FROM activity WHERE entity_type = 'gig' AND entity_id = ? ORDER BY id DESC LIMIT 1`, open).Scan(&listener))
	if listener != "migration" {
		t.Fatalf("the backfill isn't in the gig's history: %q", listener)
	}
	for id, want := range map[int64][]int64{tplNone: {own}, tplAda: {ada.ID}, tplShared: {}} {
		tp, err := GetTemplate(ctx, s.R, id)
		must(t, err)
		if !slices.Equal(tp.Assignees, want) {
			t.Fatalf("template %s: %v, want %v", tp.Name, tp.Assignees, want)
		}
	}
	r, err := GetRecurrence(ctx, s.R, rec)
	must(t, err)
	if !slices.Equal(r.Assignees, []int64{own}) || !r.AssigneesOwn {
		t.Fatalf("recurring gig on the shared template: %v own=%v", r.Assignees, r.AssigneesOwn)
	}
}
