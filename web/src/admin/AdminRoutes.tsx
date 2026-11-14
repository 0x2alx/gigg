// Admin area. Mount it nested under /admin:  <Route path="/admin" nest><AdminRoutes /></Route>
// Paths below are relative to that base.
import { useEffect, useState } from "react";
import { Link, Route, Switch } from "wouter";
import { useApp } from "../App";
import { AuditPage } from "./Audit";
import { ADMIN_TABS, AdminPage } from "./common";
import { GroupsPage } from "./Groups";
import { KeysPage } from "./Keys";
import { UsersPage } from "./Users";
import { WorkspaceDetailPage, WorkspacesPage } from "./Workspaces";
import { accessApi } from "../access";
import { adminApi } from "./adminApi";

export function AdminRoutes() {
  const { me } = useApp();
  if (!me.admin) {
    return (
      <div className="center muted" role="status">
        The admin area is for instance admins.
      </div>
    );
  }
  return (
    <Switch>
      <Route path="/">
        <AdminHome />
      </Route>
      <Route path="/workspaces">
        <WorkspacesPage />
      </Route>
      <Route path="/workspaces/:ws">{(p) => <WorkspaceDetailPage wsId={Number(p.ws)} />}</Route>
      <Route path="/users">
        <UsersPage />
      </Route>
      <Route path="/groups">
        <GroupsPage />
      </Route>
      <Route path="/keys">
        <KeysPage />
      </Route>
      <Route path="/audit">
        <AuditPage />
      </Route>
      <Route>
        <AdminPage title="Not found">
          <p className="muted">No such admin page.</p>
        </AdminPage>
      </Route>
    </Switch>
  );
}

const BLURB: Record<string, string> = {
  "/workspaces": "Workspaces, members and roles, projects and their statuses.",
  "/users": "People and agents: admin rights, passwords, disabling.",
  "/groups": "Groups of users that switch capabilities on or off together.",
  "/keys": "API keys for agents and scripts: scopes, restrictions, rotation.",
  "/audit": "Everything that changed, newest first.",
};

export function AdminHome() {
  const [counts, setCounts] = useState<Record<string, string>>({});
  useEffect(() => {
    Promise.allSettled([adminApi.workspaces(), adminApi.users(), adminApi.keys(), accessApi.groups()]).then(([w, u, k, g]) => {
      const c: Record<string, string> = {};
      if (w.status === "fulfilled") c["/workspaces"] = `${w.value.items.filter((x) => !x.archived).length} active`;
      if (u.status === "fulfilled") c["/users"] = `${u.value.items.filter((x) => !x.disabled).length} active`;
      if (g.status === "fulfilled") c["/groups"] = `${g.value.items.length} groups`;
      if (k.status === "fulfilled") c["/keys"] = `${k.value.items.filter((x) => !x.revoked && x.expires_at > Date.now()).length} live`;
      setCounts(c);
    });
  }, []);
  return (
    <AdminPage title="Admin" sub="Instance settings. Only admins see this area.">
      <div className="admin-home">
        {ADMIN_TABS.map((t) => (
          <Link key={t.href} href={t.href} className="card admin-home-card">
            <span className="admin-home-icon" aria-hidden="true">
              {t.icon}
            </span>
            <span className="admin-home-title">{t.label}</span>
            <span className="muted small">{BLURB[t.href]}</span>
            {counts[t.href] && <span className="pill pill-ws">{counts[t.href]}</span>}
          </Link>
        ))}
      </div>
    </AdminPage>
  );
}
