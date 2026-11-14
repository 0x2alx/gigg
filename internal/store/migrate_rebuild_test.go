package store

import (
	"context"
	"database/sql"
	"testing"
)

// TestRebuildMigrationKeepsReferences re-applies 0005 (the runs table rebuild) on a database
// with data: run events and comments that point at a run must survive (a plain DROP TABLE with
// foreign keys on would cascade-delete the events and null the comments' run_id), and foreign keys
// must be back on afterwards.
func TestRebuildMigrationKeepsReferences(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, dir, quiet)
	if err != nil {
		t.Fatal(err)
	}
	var runID, commentID int64
	must(t, s.Tx(ctx, func(tx *sql.Tx) error {
		admin, err := CreateUser(ctx, tx, Actor{}, NewUser{Username: "ada", Kind: "human", IsAdmin: true})
		if err != nil {
			return err
		}
		a := Actor{UserID: admin.ID}
		ws, err := CreateWorkspace(ctx, tx, a, "ws", "WS", nil)
		if err != nil {
			return err
		}
		p, err := CreateProject(ctx, tx, a, ws.ID, "main", "Main", "PRJ", nil)
		if err != nil {
			return err
		}
		task, err := CreateTask(ctx, tx, a, NewTask{ProjectID: p.ID, Title: "t"})
		if err != nil {
			return err
		}
		r, err := StartRun(ctx, tx, a, task, nil, "new", 1, nil)
		if err != nil {
			return err
		}
		pct := int64(10)
		if _, err := RunProgress(ctx, tx, r, &pct, "step"); err != nil {
			return err
		}
		r, _, err = FinishRun(ctx, tx, a, r, Finish{Status: "done", ReportMD: "report"})
		if err != nil {
			return err
		}
		runID, commentID = r.ID, *r.ReportCommentID
		_, err = tx.ExecContext(ctx, "DELETE FROM schema_migrations WHERE version = 5")
		return err
	}))
	s.Close()

	s, err = Open(ctx, dir, quiet) // applies 0005 again, on data
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var events, linked, fk int
	_ = s.R.QueryRowContext(ctx, "SELECT count(*) FROM run_events WHERE run_id = ?", runID).Scan(&events)
	_ = s.R.QueryRowContext(ctx, "SELECT count(*) FROM comments WHERE id = ? AND run_id = ?", commentID, runID).Scan(&linked)
	if events != 1 || linked != 1 {
		t.Fatalf("references lost in the rebuild: events %d, comment linked %d", events, linked)
	}
	if err := s.W.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign keys on the writer after the rebuild: %d %v", fk, err)
	}
	if _, err := s.W.ExecContext(ctx, "UPDATE runs SET status = 'parked' WHERE id = ?", runID); err != nil {
		t.Fatalf("parked status: %v", err)
	}
	if _, err := s.W.ExecContext(ctx, "UPDATE runs SET status = 'bogus' WHERE id = ?", runID); err == nil {
		t.Fatal("the CHECK constraint is gone")
	}
}
