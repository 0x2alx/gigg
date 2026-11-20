package store

import (
	"context"
	"encoding/json"
	"slices"
)

// Followers and the bell. Who hears about a gig: its assignees and its followers (anyone who can see
// it may follow it; the creator does when their "follow the gigs I create" setting is on, which it is
// unless they turned it off). What each of them hears: the kinds their bell settings allow. Mentions
// and assignments ring the person concerned whether they follow or not, within the same settings.
// A user who can no longer see the gig hears nothing, follower or not.

// MaxFollowers bounds a gig's follower list.
const MaxFollowers = 50

// Bell settings: one preference per kind of event, read from the user's preferences (a missing or
// non-boolean value means the default). The notification kinds each setting covers:
//
//	bell_finished   comment (a report: a run that finished cleanly)     default off
//	bell_problems   question (needs a person), failed (error, timeout)  default on
//	bell_assigned   assigned                                            default on
//	bell_mentioned  mention                                             default on
//	bell_missed     missed (a window closed before anyone started)      default on
var bellSetting = map[string]struct {
	pref string
	def  bool
}{
	"comment":  {"bell_finished", false},
	"question": {"bell_problems", true},
	"failed":   {"bell_problems", true},
	"assigned": {"bell_assigned", true},
	"mention":  {"bell_mentioned", true},
	"missed":   {"bell_missed", true},
}

// FollowOwnPref is the preference "follow the gigs I create" (default on).
const FollowOwnPref = "follow_own_gigs"

// userPrefs reads a user's preferences as a map (empty on any error).
func userPrefs(ctx context.Context, q Q, user int64) map[string]any {
	var raw string
	m := map[string]any{}
	if err := q.QueryRowContext(ctx, `SELECT preferences FROM users WHERE id = ?`, user).Scan(&raw); err == nil {
		_ = json.Unmarshal([]byte(raw), &m)
	}
	return m
}

func prefBool(m map[string]any, key string, def bool) bool {
	if b, ok := m[key].(bool); ok {
		return b
	}
	return def
}

// BellAllows reports whether the user's bell settings let a notification kind through.
func BellAllows(ctx context.Context, q Q, user int64, kind string) bool {
	s, ok := bellSetting[kind]
	if !ok {
		return true
	}
	return prefBool(userPrefs(ctx, q, user), s.pref, s.def)
}

// FollowsOwnGigs reports the user's "follow the gigs I create" setting.
func FollowsOwnGigs(ctx context.Context, q Q, user int64) bool {
	return prefBool(userPrefs(ctx, q, user), FollowOwnPref, true)
}

// Watchers are the people a gig's bells go to: its assignees and followers, each once.
func Watchers(t *Gig) []int64 {
	out := slices.Clone(t.Assignees)
	for _, u := range t.Followers {
		if !slices.Contains(out, u) {
			out = append(out, u)
		}
	}
	return out
}

// NotifyWatchers rings a gig's assignees and followers.
func NotifyWatchers(ctx context.Context, q Q, a Actor, t *Gig, commentID *int64, kind string) error {
	return Notify(ctx, q, a, t, commentID, kind, Watchers(t))
}

// uniqIDs: each id once, zeros dropped, order kept.
func uniqIDs(ids []int64) []int64 {
	out := []int64{}
	for _, u := range ids {
		if u != 0 && !slices.Contains(out, u) {
			out = append(out, u)
		}
	}
	return out
}

// whoSee keeps the users who can see project (0: the whole workspace) of ws, sorted, at most max.
// For lists copied from a template, where someone may have lost access since.
func whoSee(ctx context.Context, q Q, ws, project int64, ids []int64, max int) []int64 {
	out := []int64{}
	for _, u := range uniqIDs(ids) {
		if _, g, err := UserGrants(ctx, q, u); err == nil && g.HasIn(ws, project, "view") {
			out = append(out, u)
		}
	}
	slices.Sort(out)
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// setFollowers replaces a gig's followers (nil: unchanged). Each must be able to see the gig.
func setFollowers(ctx context.Context, q Q, ws, project, gig int64, users []int64) error {
	if users == nil {
		return nil
	}
	uniq := uniqIDs(users)
	if len(uniq) > MaxFollowers {
		return Invalid("at most %d followers", MaxFollowers)
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM gig_followers WHERE gig_id = ?`, gig); err != nil {
		return err
	}
	for _, u := range uniq {
		if !UserHasIn(ctx, q, u, ws, project, "view") {
			return Invalid("user %d can't see this project, so can't follow its gigs", u)
		}
		if _, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO gig_followers (gig_id, user_id) VALUES (?,?)`, gig, u); err != nil {
			return err
		}
	}
	return nil
}

// FollowGig starts (on) or stops following a gig for one user, with an activity entry when it
// changes something. Following needs the user to see the gig; stopping never does.
func FollowGig(ctx context.Context, q Q, a Actor, t *Gig, user int64, on bool) (*Gig, error) {
	if on == slices.Contains(t.Followers, user) {
		return t, nil
	}
	if on {
		if len(t.Followers) >= MaxFollowers {
			return nil, Invalid("at most %d followers", MaxFollowers)
		}
		if !UserHasIn(ctx, q, user, t.WorkspaceID, t.ProjectID, "view") {
			return nil, Invalid("user %d can't see this gig", user)
		}
		if _, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO gig_followers (gig_id, user_id) VALUES (?,?)`, t.ID, user); err != nil {
			return nil, err
		}
	} else if _, err := q.ExecContext(ctx, `DELETE FROM gig_followers WHERE gig_id = ? AND user_id = ?`, t.ID, user); err != nil {
		return nil, err
	}
	after, err := GetGig(ctx, q, t.ID)
	if err != nil {
		return nil, err
	}
	if _, err := Activity(ctx, q, a, &t.WorkspaceID, &t.ProjectID, "gig.updated", "gig", t.ID, AuditDiff("gig", t, after)); err != nil {
		return nil, err
	}
	return after, nil
}
