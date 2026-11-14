package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
)

// postSteps run in Go inside a migration's transaction, right after its SQL.
var postSteps = map[int]func(context.Context, *sql.Tx) error{6: migrateAccess}

// The access model before 0006, frozen here so the conversion keeps working after the old code is gone.
var oldCaps = []struct {
	name         string
	human, agent bool
	minRole      int // 1 viewer, 2 commenter, 3 member, 4 manager: what the old routes asked for besides the capability
}{
	{"create_tasks", true, true, 3}, {"delete_tasks", true, true, 3}, {"archive_tasks", true, true, 3},
	{"relations", true, true, 3}, {"checklists", true, true, 3}, {"schedules", true, false, 3},
	{"templates", true, false, 3}, {"custom_fields", true, false, 4}, {"saved_filters", true, true, 1},
}

var oldRoleLevel = map[string]int{"viewer": 1, "commenter": 2, "member": 3, "manager": 4}

// oldAbilities: what a non-admin could do in a workspace where they had role, before 0006.
func oldAbilities(role string, caps map[string]bool) map[string]bool {
	lvl := oldRoleLevel[role]
	out := map[string]bool{
		"view": lvl >= 1, "comment": lvl >= 2, "edit_tasks": lvl >= 3, "manage_workspace": lvl >= 4, "always_run": false,
	}
	for _, c := range oldCaps {
		out[c.name] = caps[c.name] && lvl >= c.minRole
	}
	return out
}

func migrateAccess(ctx context.Context, tx *sql.Tx) error {
	type user struct {
		id    int64
		agent bool
		admin bool
	}
	var users []user
	rows, err := tx.QueryContext(ctx, `SELECT id, kind = 'agent', is_admin FROM users`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var u user
		if err := rows.Scan(&u.id, &u.agent, &u.admin); err != nil {
			rows.Close()
			return err
		}
		users = append(users, u)
	}
	rows.Close()
	read := func(query string, args ...any) (map[int64]map[string]bool, error) {
		out := map[int64]map[string]bool{}
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var c string
			var a bool
			if err := rows.Scan(&id, &c, &a); err != nil {
				return nil, err
			}
			if out[id] == nil {
				out[id] = map[string]bool{}
			}
			out[id][c] = a
		}
		return out, rows.Err()
	}
	userPerms, err := read(`SELECT user_id, capability, allowed FROM user_permissions`)
	if err != nil {
		return err
	}
	groupPerms, err := read(`SELECT group_id, capability, allowed FROM group_permissions`)
	if err != nil {
		return err
	}
	// old resolution: own setting > any group allowing > any group denying > kind default
	oldCapsOf := func(u user) (map[string]bool, error) {
		groupSays := map[string][]bool{}
		rows, err := tx.QueryContext(ctx, `SELECT p.capability, p.allowed FROM group_permissions p JOIN group_members m ON m.group_id = p.group_id WHERE m.user_id = ?`, u.id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var c string
			var a bool
			if err := rows.Scan(&c, &a); err != nil {
				rows.Close()
				return nil, err
			}
			groupSays[c] = append(groupSays[c], a)
		}
		rows.Close()
		out := map[string]bool{}
		for _, c := range oldCaps {
			v, own := userPerms[u.id][c.name]
			switch {
			case own:
			case len(groupSays[c.name]) > 0:
				v = slices.Contains(groupSays[c.name], true)
			case u.agent:
				v = c.agent
			default:
				v = c.human
			}
			out[c.name] = v
		}
		return out, nil
	}
	now := Now()
	insert := func(col string, id int64, ws any, preset string, ov map[string]bool) error {
		if ov == nil {
			ov = map[string]bool{}
		}
		b, _ := json.Marshal(ov)
		_, err := tx.ExecContext(ctx, `INSERT INTO access_rules (`+col+`, workspace_id, preset, overrides, created_at) VALUES (?,?,?,?,?)`, id, ws, preset, string(b), now)
		return err
	}
	// group switches were global: they become all-workspace overrides; a user's own switches too
	for g, ov := range groupPerms {
		if err := insert("group_id", g, nil, "", ov); err != nil {
			return err
		}
	}
	for u, ov := range userPerms {
		if err := insert("user_id", u, nil, "", ov); err != nil {
			return err
		}
	}
	type member struct {
		ws   int64
		role string
	}
	members := map[int64][]member{}
	rows, err = tx.QueryContext(ctx, `SELECT user_id, workspace_id, role FROM memberships`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var u int64
		var m member
		if err := rows.Scan(&u, &m.ws, &m.role); err != nil {
			rows.Close()
			return err
		}
		members[u] = append(members[u], m)
	}
	rows.Close()
	for u, ms := range members {
		for _, m := range ms {
			if err := insert("user_id", u, m.ws, m.role, nil); err != nil {
				return err
			}
		}
	}
	// then pin, per user and workspace, every permission the new rules would answer differently
	for _, u := range users {
		if u.admin {
			continue
		}
		caps, err := oldCapsOf(u)
		if err != nil {
			return err
		}
		g, err := LoadGrants(ctx, tx, &User{ID: u.id})
		if err != nil {
			return err
		}
		member := map[int64]string{}
		for _, m := range members[u.id] {
			member[m.ws] = m.role
		}
		for _, ws := range g.workspaces {
			role, ok := member[ws]
			if !ok {
				if g.Has(ws, "view") {
					return fmt.Errorf("access conversion: user %d would see workspace %d", u.id, ws)
				}
				continue
			}
			old := oldAbilities(role, caps)
			ov := map[string]bool{}
			for _, p := range Permissions {
				if g.Has(ws, p.Name) != old[p.Name] {
					ov[p.Name] = old[p.Name]
				}
			}
			if len(ov) == 0 {
				continue
			}
			b, _ := json.Marshal(ov)
			if _, err := tx.ExecContext(ctx, `UPDATE access_rules SET overrides = ? WHERE user_id = ? AND workspace_id = ?`, string(b), u.id, ws); err != nil {
				return err
			}
			g, err = LoadGrants(ctx, tx, &User{ID: u.id})
			if err != nil {
				return err
			}
			for _, p := range Permissions {
				if g.Has(ws, p.Name) != old[p.Name] {
					return fmt.Errorf("access conversion: user %d, workspace %d, %s still differs", u.id, ws, p.Name)
				}
			}
		}
	}
	for _, t := range []string{"memberships", "user_permissions", "group_permissions"} {
		if _, err := tx.ExecContext(ctx, `DROP TABLE `+t); err != nil {
			return err
		}
	}
	return nil
}
