package store

import (
	"context"
	"database/sql"
	"errors"
)

// postSteps run in Go inside a migration's transaction, right after its SQL.
var postSteps = map[int]func(context.Context, *sql.Tx) error{6: migrateAccess}

// migrateAccess: 0006 replaced roles per workspace and per-user switches with access rules. No
// database from before it was ever released, so a database that still holds old roles or switches
// is refused rather than converted (a conversion would have to freeze the permission names and
// presets of that time); one with only admins, or none at all, goes through.
func migrateAccess(ctx context.Context, tx *sql.Tx) error {
	for _, t := range []string{"memberships", "user_permissions", "group_permissions"} {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+t).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return errors.New("this database is from before gigg's access rules (migration 0006) and holds roles or permissions; upgrading it isn't supported")
		}
	}
	for _, t := range []string{"memberships", "user_permissions", "group_permissions"} {
		if _, err := tx.ExecContext(ctx, `DROP TABLE `+t); err != nil {
			return err
		}
	}
	return nil
}
