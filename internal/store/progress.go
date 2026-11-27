package store

import (
	"context"
)

// touchProgress marks gig as moved forward now (see migration 0022 for what counts).
func touchProgress(ctx context.Context, q Q, gig int64) error {
	_, err := q.ExecContext(ctx, `UPDATE gigs SET last_progress_at = ? WHERE id = ?`, Now(), gig)
	return err
}

// StaleGig is an open gig in Doing whose last progress is older than asked.
type StaleGig struct {
	Key            string `json:"key"`
	Title          string `json:"title"`
	Status         string `json:"status"`
	LastProgressAt int64  `json:"last_progress_at" doc:"Unix ms: the latest comment, checklist tick, status change or finished run (the creation time when none)"`
	Hours          int64  `json:"hours" doc:"Whole hours since then"`
}

// StaleGigs lists ws's live, unarchived gigs in a Doing status assigned to user with no progress
// since before cutoff (unix ms), oldest first, at most limit; projects narrows them (nil = all). It
// also returns how many there are in all.
func StaleGigs(ctx context.Context, q Q, ws int64, projects []int64, user, cutoff int64, limit int) ([]StaleGig, int, error) {
	where := `t.workspace_id = ? AND s.category = 'doing' AND t.deleted_at IS NULL AND t.archived_at IS NULL
		AND EXISTS (SELECT 1 FROM gig_assignees ga WHERE ga.gig_id = t.id AND ga.user_id = ?)
		AND coalesce(t.last_progress_at, t.created_at) < ?`
	args := []any{ws, user, cutoff}
	if projects != nil {
		if len(projects) == 0 {
			return []StaleGig{}, 0, nil
		}
		where += " AND t.project_id IN (" + placeholders(len(projects)) + ")"
		args = append(args, int64Args(projects)...)
	}
	from := ` FROM gigs t JOIN projects p ON p.id = t.project_id JOIN statuses s ON s.id = t.status_id WHERE ` + where
	var total int
	if err := q.QueryRowContext(ctx, `SELECT count(*)`+from, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := q.QueryContext(ctx, `SELECT p.key || '-' || t.number, t.title, s.name, coalesce(t.last_progress_at, t.created_at)`+from+
		` ORDER BY coalesce(t.last_progress_at, t.created_at), t.id LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	now := Now()
	out := []StaleGig{}
	for rows.Next() {
		var g StaleGig
		if err := rows.Scan(&g.Key, &g.Title, &g.Status, &g.LastProgressAt); err != nil {
			return nil, 0, err
		}
		g.Hours = (now - g.LastProgressAt) / 3600000
		out = append(out, g)
	}
	return out, total, rows.Err()
}
