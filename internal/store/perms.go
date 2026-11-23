package store

import (
	"context"
	"strings"
)

// Group is a set of users sharing access rules.
type Group struct {
	ID          int64         `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Members     []int64       `json:"members"`
	Rules       []*AccessRule `json:"rules"` // its access rules (all workspaces and per workspace)
	CreatedAt   int64         `json:"created_at"`
	Version     int64         `json:"version"`
}

func validGroup(name, desc string) error {
	if n := len(strings.TrimSpace(name)); n == 0 || n > 60 {
		return Invalid("group name: 1-60 characters")
	}
	if len(desc) > 500 {
		return Invalid("description: at most 500 characters")
	}
	return nil
}

// ListGroups returns every group with members and settings.
func ListGroups(ctx context.Context, q Q) ([]*Group, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, name, description, created_at, version FROM groups ORDER BY name`)
	if err != nil {
		return nil, err
	}
	var out []*Group
	byID := map[int64]*Group{}
	for rows.Next() {
		g := &Group{Members: []int64{}, Rules: []*AccessRule{}}
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.CreatedAt, &g.Version); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, g)
		byID[g.ID] = g
	}
	rows.Close()
	rows, err = q.QueryContext(ctx, `SELECT group_id, user_id FROM group_members ORDER BY user_id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var g, u int64
		if err := rows.Scan(&g, &u); err != nil {
			rows.Close()
			return nil, err
		}
		if byID[g] != nil {
			byID[g].Members = append(byID[g].Members, u)
		}
	}
	rows.Close()
	rows, err = q.QueryContext(ctx, `SELECT `+ruleCols+` FROM access_rules r JOIN groups g ON g.id = r.group_id ORDER BY r.workspace_id IS NOT NULL, r.workspace_id`)
	if err != nil {
		return nil, err
	}
	rules, err := scanRules(rows)
	if err != nil {
		return nil, err
	}
	for _, r := range rules {
		if byID[*r.GroupID] != nil {
			byID[*r.GroupID].Rules = append(byID[*r.GroupID].Rules, r)
		}
	}
	if out == nil {
		out = []*Group{}
	}
	return out, nil
}

// GetGroup loads one group.
func GetGroup(ctx context.Context, q Q, id int64) (*Group, error) {
	gs, err := ListGroups(ctx, q)
	if err != nil {
		return nil, err
	}
	for _, g := range gs {
		if g.ID == id {
			return g, nil
		}
	}
	return nil, ErrNotFound
}

// CreateGroup adds a group.
func CreateGroup(ctx context.Context, q Q, a Actor, name, desc string) (*Group, error) {
	name = strings.TrimSpace(name)
	if err := validGroup(name, desc); err != nil {
		return nil, err
	}
	res, err := q.ExecContext(ctx, `INSERT INTO groups (name, description, created_at) VALUES (?,?,?)`, name, desc, Now())
	if err != nil {
		if isUnique(err) {
			return nil, Conflict("a group called %q exists", name)
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	if _, err := Activity(ctx, q, a, nil, nil, "group.created", "group", id, map[string]any{"name": name}); err != nil {
		return nil, err
	}
	return GetGroup(ctx, q, id)
}

// UpdateGroup renames/describes a group and, when members is non-nil, replaces its members.
func UpdateGroup(ctx context.Context, q Q, a Actor, g *Group, expect int64, name, desc *string, members []int64) (*Group, error) {
	if expect != 0 && expect != g.Version {
		return nil, ErrVersion
	}
	n, d := g.Name, g.Description
	if name != nil {
		n = strings.TrimSpace(*name)
	}
	if desc != nil {
		d = *desc
	}
	if err := validGroup(n, d); err != nil {
		return nil, err
	}
	if _, err := q.ExecContext(ctx, `UPDATE groups SET name = ?, description = ?, version = version + 1 WHERE id = ?`, n, d, g.ID); err != nil {
		if isUnique(err) {
			return nil, Conflict("a group called %q exists", n)
		}
		return nil, err
	}
	if members != nil {
		if _, err := q.ExecContext(ctx, `DELETE FROM group_members WHERE group_id = ?`, g.ID); err != nil {
			return nil, err
		}
		for _, u := range members {
			if _, err := GetUser(ctx, q, u); err != nil {
				return nil, Invalid("no user %d", u)
			}
			if _, err := q.ExecContext(ctx, `INSERT INTO group_members (group_id, user_id) VALUES (?,?) ON CONFLICT DO NOTHING`, g.ID, u); err != nil {
				return nil, err
			}
		}
		if err := dropUnseenAssignees(ctx, q); err != nil {
			return nil, err
		}
	}
	if _, err := Activity(ctx, q, a, nil, nil, "group.updated", "group", g.ID, map[string]any{"name": n, "members": members}); err != nil {
		return nil, err
	}
	return GetGroup(ctx, q, g.ID)
}

// DeleteGroup removes a group (its members keep their own settings).
func DeleteGroup(ctx context.Context, q Q, a Actor, g *Group) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM groups WHERE id = ?`, g.ID); err != nil {
		return err
	}
	if err := dropUnseenAssignees(ctx, q); err != nil {
		return err
	}
	_, err := Activity(ctx, q, a, nil, nil, "group.deleted", "group", g.ID, map[string]any{"name": g.Name})
	return err
}

// GroupsOf lists the groups a user belongs to.
func GroupsOf(ctx context.Context, q Q, user int64) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT g.name FROM groups g JOIN group_members m ON m.group_id = g.id WHERE m.user_id = ? ORDER BY g.name`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
