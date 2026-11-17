package access

import (
	"slices"
	"testing"

	"github.com/0x2alx/gigg/internal/store"
)

func ptr(v int64) *int64 { return &v }

var wsAll = []int64{10, 11, 12}

func grants(rules ...*store.AccessRule) *store.Grants { return store.NewGrants(false, rules, wsAll) }

func TestMatrix(t *testing.T) {
	admin := &store.User{ID: 1, IsAdmin: true}
	bob := &store.User{ID: 2}
	bobs := grants(&store.AccessRule{WorkspaceID: ptr(10), Preset: "member"}, &store.AccessRule{WorkspaceID: ptr(11), Preset: "viewer"})

	session := &Principal{User: bob, Grants: bobs}
	if !session.Can(10, Editor, "gigs:write") || session.Can(11, Editor, "gigs:write") || session.Can(12, Viewer, "gigs:read") {
		t.Fatal("session can")
	}
	if !session.SeesWorkspace(11) || session.SeesWorkspace(12) || !slices.Equal(session.Workspaces(), []int64{10, 11}) {
		t.Fatalf("session reach %v", session.Workspaces())
	}

	// a workspace key: bound to ws 10, scopes intersected with the permissions
	key := &Principal{User: bob, Grants: bobs, Key: &store.APIKey{WorkspaceID: ptr(10), Scopes: []string{"gigs:read"}}}
	if key.SeesWorkspace(11) || !key.SeesWorkspace(10) {
		t.Fatal("key binding")
	}
	if key.Can(10, Editor, "gigs:write") || !key.Can(10, Viewer, "gigs:read") {
		t.Fatal("key scopes")
	}
	if ws := key.Workspaces(); len(ws) != 1 || ws[0] != 10 {
		t.Fatalf("key workspaces %v", ws)
	}
	if key.AllowedAnywhere("create_gigs") != true || key.Allowed(11, "view") {
		t.Fatal("key anywhere is limited to its workspace")
	}

	// narrowed key: only project 100
	narrow := &Principal{User: bob, Grants: bobs, Key: &store.APIKey{WorkspaceID: ptr(10), ProjectIDs: []int64{100}, Scopes: []string{"gigs:read"}}}
	if !narrow.SeesProject(10, 100) || narrow.SeesProject(10, 101) || narrow.SeesProject(11, 100) {
		t.Fatal("narrowing")
	}

	// a key bound to a workspace the user can't see sees nothing
	stale := &Principal{User: bob, Grants: bobs, Key: &store.APIKey{WorkspaceID: ptr(12), Scopes: []string{"gigs:read"}}}
	if stale.SeesWorkspace(12) || len(stale.Workspaces()) != 0 {
		t.Fatal("key for a workspace out of reach")
	}

	// admin session: everything; admin user's workspace key: everything, but only there
	ag := store.NewGrants(true, nil, wsAll)
	if a := (&Principal{User: admin, Grants: ag}); !a.IsInstanceAdmin() || !a.Allowed(99, Manager) || a.Workspaces() != nil || !a.AdminWith("admin:users") {
		t.Fatal("admin session")
	}
	wsKey := &Principal{User: admin, Grants: ag, Key: &store.APIKey{WorkspaceID: ptr(10), Scopes: []string{"gigs:write"}}}
	if wsKey.IsInstanceAdmin() || wsKey.SeesWorkspace(11) || !wsKey.Allowed(10, Manager) || wsKey.AdminWith("admin:users") {
		t.Fatal("admin's workspace key must not be instance admin")
	}
	adminKey := &Principal{User: admin, Grants: ag, Key: &store.APIKey{Scopes: []string{"admin:users", "gigs:read"}}}
	if !adminKey.AdminWith("admin:users") || adminKey.AdminWith("admin:keys") || adminKey.Can(5, Editor, "gigs:write") {
		t.Fatal("admin key scopes")
	}
	// an unbound key of a non-admin (or of a demoted admin) acts like the user's session
	demoted := &Principal{User: bob, Grants: bobs, Key: &store.APIKey{Scopes: []string{"admin:users", "gigs:read"}}}
	if demoted.IsInstanceAdmin() || demoted.AdminWith("admin:users") {
		t.Fatal("demoted admin key is still an admin")
	}
	if !demoted.Allowed(10, Editor) || demoted.Allowed(11, Editor) || demoted.SeesWorkspace(99) || len(demoted.Workspaces()) != 2 {
		t.Fatalf("unbound key reach: %v", demoted.Workspaces())
	}
}

// The rule order: admin > user/workspace > user/all > groups/workspace > groups/all > off; inside a
// rule an override beats the preset; between groups at one level an allow beats a deny; every
// permission needs view.
func TestRuleOrder(t *testing.T) {
	g1, g2 := ptr(1), ptr(2)
	cases := []struct {
		name  string
		rules []*store.AccessRule
		ws    int64
		perm  string
		want  bool
		src   string
	}{
		{"nothing", nil, 10, "view", false, "default"},
		{"preset", []*store.AccessRule{{WorkspaceID: ptr(10), Preset: "member"}}, 10, "create_gigs", true, "user"},
		{"preset elsewhere", []*store.AccessRule{{WorkspaceID: ptr(10), Preset: "member"}}, 11, "view", false, "default"},
		{"override beats preset", []*store.AccessRule{{WorkspaceID: ptr(10), Preset: "member", Overrides: map[string]bool{"create_gigs": false}}}, 10, "create_gigs", false, "user"},
		{"all workspaces covers others", []*store.AccessRule{{Preset: "viewer"}}, 12, "view", true, "user_all"},
		{"workspace beats all", []*store.AccessRule{{Preset: "manager"}, {WorkspaceID: ptr(10), Overrides: map[string]bool{"recurring_gigs": false}}}, 10, "recurring_gigs", false, "user"},
		{"preset gap inherits from all", []*store.AccessRule{{Overrides: map[string]bool{"recurring_gigs": true}}, {WorkspaceID: ptr(10), Preset: "member"}}, 10, "recurring_gigs", true, "user_all"},
		{"user beats group", []*store.AccessRule{{GroupID: g1, WorkspaceID: ptr(10), Preset: "manager"}, {Overrides: map[string]bool{"templates": false}}}, 10, "templates", false, "user_all"},
		{"group gives", []*store.AccessRule{{GroupID: g1, WorkspaceID: ptr(10), Preset: "member"}}, 10, "edit_gigs", true, "group"},
		{"group allow beats group deny", []*store.AccessRule{{GroupID: g1, WorkspaceID: ptr(10), Preset: "member", Overrides: map[string]bool{"templates": false}},
			{GroupID: g2, WorkspaceID: ptr(10), Preset: "manager"}}, 10, "templates", true, "group"},
		{"group ws beats group all", []*store.AccessRule{{GroupID: g1, Preset: "manager"}, {GroupID: g2, WorkspaceID: ptr(10), Preset: "viewer", Overrides: map[string]bool{"comment": false}}}, 10, "comment", false, "group"},
		{"group all", []*store.AccessRule{{GroupID: g1, Preset: "consumer"}}, 11, "comment", true, "group_all"},
		{"needs view", []*store.AccessRule{{Overrides: map[string]bool{"create_gigs": true}}}, 10, "create_gigs", false, "needs_view"},
		{"errand_unsandboxed is never in a preset", []*store.AccessRule{{WorkspaceID: ptr(10), Preset: "manager"}}, 10, "errand_unsandboxed", false, "default"},
	}
	for _, c := range cases {
		d := store.NewGrants(false, c.rules, wsAll).Decide(c.ws, c.perm)
		if d.Allowed != c.want || d.Source != c.src {
			t.Errorf("%s: got %v from %s, want %v from %s", c.name, d.Allowed, d.Source, c.want, c.src)
		}
	}
}
