// Workspaces: the list, and per workspace its settings, members, projects and project statuses.
import { useCallback, useEffect, useMemo, useState, type FormEvent } from "react";
import { Link } from "wouter";
import { fmtDate, type Category, type User } from "../api";
import { useApp } from "../App";
import { ColorPicker } from "../fields";
import { colorFor } from "../theme";
import { CATEGORY_LABEL } from "../boardModel";
import { fmtSize } from "../attachments";
import { Avatar, Modal } from "../ui";
import {
  adminApi,
  CATEGORIES,
  errMsg,
  PROJECT_ROLES,
  ROLES,
  type AdminMembership,
  type AdminProject,
  type AdminStatus,
  type AdminUser,
  type AdminWorkspace,
  type AttachLimits,
  type ProjectRole,
  type Role,
} from "./adminApi";
import { CustomFields } from "./CustomFields";
import { AdminPage, CategoryDot, CategoryPill, ConfirmButton, Empty, ErrorLine, Field, FormActions, InfoLine, RolePill } from "./common";
import { followName, slugify } from "./forms";

interface WsRow {
  ws: AdminWorkspace;
  members: number | null;
  projects: number | null;
}

export function WorkspacesPage() {
  const { refresh, users } = useApp();
  const [rows, setRows] = useState<WsRow[] | null>(null);
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);
  const [showArchived, setShowArchived] = useState(false);

  const load = useCallback(() => {
    adminApi
      .workspaces()
      .then(async (r) => {
        const out = await Promise.all(
          r.items.map(async (ws) => {
            const [m, p] = await Promise.allSettled([adminApi.members(ws.id), adminApi.projects(ws.id)]);
            return {
              ws,
              members: m.status === "fulfilled" ? m.value.items.length : null,
              projects: p.status === "fulfilled" ? p.value.items.length : null,
            };
          }),
        );
        setRows(out);
        setError("");
      })
      .catch((e: unknown) => setError(errMsg(e)));
  }, []);
  useEffect(load, [load]);

  const archive = (w: AdminWorkspace, archived: boolean) =>
    adminApi.patchWorkspace(w, { archived }).then(() => {
      load();
      refresh();
    });

  const visible = (rows ?? []).filter((r) => showArchived || !r.ws.archived);
  const archivedCount = (rows ?? []).filter((r) => r.ws.archived).length;

  return (
    <AdminPage
      title="Workspaces"
      sub="Each workspace has its own members, projects and labels."
      actions={
        <button type="button" className="button" onClick={() => setCreating(true)}>
          + New workspace
        </button>
      }
    >
      <ErrorLine error={error} />
      {rows === null && !error && <p className="muted">Loading…</p>}
      {rows && (
        <div className="card admin-table-card">
          <table className="admin-table admin-fit ws-table">
            <thead>
              <tr>
                <th>Workspace</th>
                <th>Slug</th>
                <th>Owner</th>
                <th className="num">Members</th>
                <th className="num">Projects</th>
                <th>Created</th>
                <th aria-label="Actions" />
              </tr>
            </thead>
            <tbody>
              {visible.map(({ ws, members, projects }) => (
                <tr key={ws.id} className={ws.archived ? "admin-dim" : undefined}>
                  <td>
                    <Link href={`/workspaces/${ws.id}`} className="admin-name">
                      <span className="dot" style={{ background: colorFor(ws.slug, ws.color) }} aria-hidden="true" />
                      {ws.name}
                    </Link>
                    {ws.archived && <span className="pill pill-muted">archived</span>}
                  </td>
                  <td className="ws-slug">
                    <span className="pill pill-key">{ws.slug}</span>
                  </td>
                  <td className="ws-owner" data-label="Owner">
                    <OwnerBadge id={ws.owner_id} users={users} />
                  </td>
                  <td className="num ws-members" data-label="Members">{members ?? "?"}</td>
                  <td className="num ws-projects" data-label="Projects">{projects ?? "?"}</td>
                  <td className="muted small ws-created" data-label="Created">{fmtDate(ws.created_at)}</td>
                  <td className="admin-actions">
                    <Link href={`/workspaces/${ws.id}`} className="button secondary small" aria-label={`Manage workspace ${ws.name}`}>
                      Manage
                    </Link>
                    {ws.archived ? (
                      <ConfirmButton
                        label="Unarchive"
                        name={`Unarchive workspace ${ws.name}`}
                        danger={false}
                        title={`Unarchive ${ws.name}?`}
                        message="Its members see it again."
                        confirmLabel="Unarchive"
                        onConfirm={() => archive(ws, false)}
                      />
                    ) : (
                      <ConfirmButton
                        label="Archive"
                        name={`Archive workspace ${ws.name}`}
                        title={`Archive ${ws.name}?`}
                        message="The workspace becomes read-only and leaves the sidebar. Nothing is deleted; you can unarchive it."
                        confirmLabel="Archive"
                        onConfirm={() => archive(ws, true)}
                      />
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {visible.length === 0 && <Empty>No workspaces yet.</Empty>}
          {archivedCount > 0 && (
            <button type="button" className="link small admin-table-more" onClick={() => setShowArchived((v) => !v)}>
              {showArchived ? "Hide" : "Show"} {archivedCount} archived
            </button>
          )}
        </div>
      )}
      {creating && (
        <CreateWorkspace
          onClose={() => setCreating(false)}
          onDone={() => {
            setCreating(false);
            load();
            refresh();
          }}
        />
      )}
    </AdminPage>
  );
}

function CreateWorkspace({ onClose, onDone }: { onClose: () => void; onDone: () => void }) {
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [touched, setTouched] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    adminApi
      .createWorkspace({ slug, name })
      .then(onDone)
      .catch((e: unknown) => setError(errMsg(e)))
      .finally(() => setBusy(false));
  };
  return (
    <Modal title="New workspace" onClose={onClose}>
      <form className="form-grid admin-form" onSubmit={submit}>
        <Field label="Name" hint="Shown in the sidebar and on every page.">
          <input
            value={name}
            maxLength={64}
            required
            data-autofocus
            autoComplete="off"
            onChange={(e) => {
              setName(e.target.value);
              setSlug((cur) => followName(e.target.value, cur, touched));
            }}
          />
        </Field>
        <Field label="Slug" hint={touched ? "Lowercase a-z, 0-9 and dashes." : "Filled in from the name; edit it if you like."}>
          <input
            value={slug}
            required
            pattern="[a-z][a-z0-9\-]{1,31}"
            maxLength={32}
            autoComplete="off"
            spellCheck={false}
            className="admin-mono"
            onChange={(e) => {
              const v = e.target.value.toLowerCase();
              setTouched(v !== "");
              setSlug(v);
            }}
          />
        </Field>
        <InfoLine span2>New projects start from the default statuses: Backlog, To-Do, Doing, Waiting, Done, Cancelled.</InfoLine>
        {error && (
          <div className="span2">
            <ErrorLine error={error} />
          </div>
        )}
        <FormActions>
          <button type="button" className="button secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="button" disabled={busy || !name.trim() || !slug}>
            {busy ? "Creating…" : "Create workspace"}
          </button>
        </FormActions>
      </form>
    </Modal>
  );
}

// ---------------------------------------------------------------- one workspace

// wsRef is the URL segment: a numeric id or a slug (the API takes either).
export function WorkspaceDetailPage({ wsRef }: { wsRef: string }) {
  const { refresh } = useApp();
  const [ws, setWs] = useState<AdminWorkspace | null>(null);
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [error, setError] = useState("");

  const load = useCallback(() => {
    adminApi
      .workspace(wsRef)
      .then((w) => {
        setWs(w);
        setError("");
      })
      .catch((e: unknown) => setError(errMsg(e)));
    adminApi
      .users()
      .then((r) => setUsers(r.items))
      .catch(() => undefined);
  }, [wsRef]);
  useEffect(load, [load]);

  if (!ws)
    return (
      <AdminPage title="Workspace">
        <ErrorLine error={error} />
        {!error && <p className="muted">Loading…</p>}
      </AdminPage>
    );

  return (
    <AdminPage
      title={ws.name}
      sub={
        <span className="crumb">
          <Link href="/workspaces">Workspaces</Link> / <span className="pill pill-key">{ws.slug}</span>
          {ws.archived && <span className="pill pill-muted">archived</span>}
        </span>
      }
    >
      <div className="admin-stack">
        <WorkspaceSettings
          ws={ws}
          users={users}
          onSaved={(w) => {
            setWs(w);
            refresh();
          }}
        />
        <AttachmentLimits ws={ws} />
        <Members ws={ws} users={users} />
        <Projects ws={ws} />
        <CustomFields ws={ws} />
      </div>
    </AdminPage>
  );
}

function OwnerBadge({ id, users }: { id: number | null; users: Map<number, User> }) {
  const u = id != null ? users.get(id) : undefined;
  if (!u) return <span className="muted small">{id != null ? `user ${id}` : "—"}</span>;
  return (
    <span className="user-badge">
      <Avatar user={u} size="xs" />
      {u.display_name || u.username}
    </span>
  );
}

function WorkspaceSettings({ ws, users, onSaved }: { ws: AdminWorkspace; users: AdminUser[]; onSaved: (w: AdminWorkspace) => void }) {
  const [name, setName] = useState(ws.name);
  const [color, setColor] = useState<string | null>(ws.color);
  const [owner, setOwner] = useState<number | null>(ws.owner_id);
  const [desc, setDesc] = useState(ws.description_md);
  const [error, setError] = useState("");
  const [msg, setMsg] = useState("");
  useEffect(() => {
    setName(ws.name);
    setColor(ws.color);
    setOwner(ws.owner_id);
    setDesc(ws.description_md);
  }, [ws]);
  const dirty = name !== ws.name || color !== ws.color || owner !== ws.owner_id || desc !== ws.description_md;
  const save = (e: FormEvent) => {
    e.preventDefault();
    setMsg("");
    setError("");
    adminApi
      .patchWorkspace(ws, { name, color, description_md: desc, ...(owner !== ws.owner_id ? { owner_id: owner } : {}) })
      .then((w) => {
        onSaved(w);
        setMsg("Saved.");
      })
      .catch((e: unknown) => setError(errMsg(e)));
  };
  const toggleArchive = () =>
    adminApi.patchWorkspace(ws, { archived: !ws.archived }).then((w) => {
      onSaved(w);
    });
  return (
    <form className="card form-grid" onSubmit={save} aria-label="Workspace settings">
      <h2 className="span2">Settings</h2>
      <label>
        Name
        <input value={name} maxLength={64} required onChange={(e) => setName(e.target.value)} />
      </label>
      <div className="field">
        <span className="field-label">Colour</span>
        <ColorPicker value={color} onChange={setColor} />
      </div>
      <label>
        Owner <span className="muted small">their picture shows on the board’s workspace filter</span>
        <select value={owner ?? ""} onChange={(e) => setOwner(e.target.value ? Number(e.target.value) : null)}>
          <option value="">None</option>
          {users
            .filter((u) => u.id === owner || !u.disabled)
            .map((u) => (
              <option key={u.id} value={u.id}>
                {(u.display_name || u.username) + (u.kind === "agent" ? " (agent)" : "")}
              </option>
            ))}
        </select>
      </label>
      <label className="span2">
        Description <span className="muted small">Markdown</span>
        <textarea rows={3} value={desc} onChange={(e) => setDesc(e.target.value)} />
      </label>
      <div className="span2">
        <ErrorLine error={error} />
        {msg && !dirty && <p className="accent small">{msg}</p>}
      </div>
      <div className="row span2 admin-split">
        {ws.archived ? (
          <ConfirmButton label="Unarchive workspace" danger={false} small={false} title={`Unarchive ${ws.name}?`} message="Its members see it again." confirmLabel="Unarchive" onConfirm={toggleArchive} />
        ) : (
          <ConfirmButton
            label="Archive workspace"
            small={false}
            title={`Archive ${ws.name}?`}
            message="The workspace becomes read-only and leaves the sidebar. Nothing is deleted."
            confirmLabel="Archive"
            onConfirm={toggleArchive}
          />
        )}
        <button className="button" type="submit" disabled={!dirty}>
          Save
        </button>
      </div>
    </form>
  );
}

// ---------------------------------------------------------------- attachment limits

const MB = 1 << 20;
const GB = 1 << 30;

// AttachmentLimits: the largest file and the total for the workspace's attachments, with what is used.
// Everyone here sees them; only instance admins change them (an empty box = the default).
function AttachmentLimits({ ws }: { ws: AdminWorkspace }) {
  const { me } = useApp();
  const [lim, setLim] = useState<AttachLimits | null>(null);
  const [fileMB, setFileMB] = useState("");
  const [quotaGB, setQuotaGB] = useState("");
  const [error, setError] = useState("");
  const [msg, setMsg] = useState("");
  const show = (l: AttachLimits) => {
    setLim(l);
    setFileMB(l.file_max !== 10 * MB ? String(+(l.file_max / MB).toFixed(2)) : "");
    setQuotaGB(l.quota !== GB ? String(+(l.quota / GB).toFixed(3)) : "");
  };
  useEffect(() => {
    adminApi
      .attachLimits(ws.id)
      .then(show)
      .catch((e: unknown) => setError(errMsg(e)));
  }, [ws.id]);
  if (!lim) return error ? <ErrorLine error={error} /> : null;
  const num = (v: string, unit: number) => (v.trim() === "" ? null : Math.round(Number(v) * unit));
  const save = (e: FormEvent) => {
    e.preventDefault();
    setMsg("");
    setError("");
    const file_max = num(fileMB, MB);
    const quota = num(quotaGB, GB);
    if ((file_max !== null && !(file_max > 0)) || (quota !== null && !(quota > 0))) {
      setError("Use a number above 0, or leave the box empty for the default.");
      return;
    }
    adminApi
      .setAttachLimits(ws.id, { file_max, quota })
      .then((l) => {
        show(l);
        setMsg("Saved.");
      })
      .catch((e: unknown) => setError(errMsg(e)));
  };
  const pct = lim.quota > 0 ? Math.min(100, Math.round((lim.used / lim.quota) * 100)) : 0;
  return (
    <form className="card form-grid" onSubmit={save} aria-label="Attachment limits">
      <h2 className="span2">Attachments</h2>
      <p className="span2 muted small">
        {fmtSize(lim.used)} of {fmtSize(lim.quota)} used ({pct}%). Files up to {fmtSize(lim.file_max)} each.
      </p>
      {me.admin && (
        <>
          <label>
            Largest file, MB <span className="muted small">default 10, at most 50</span>
            <input type="number" min="0.01" max="50" step="any" inputMode="decimal" placeholder="10" value={fileMB} onChange={(e) => setFileMB(e.target.value)} />
          </label>
          <label>
            Total for the workspace, GB <span className="muted small">default 1</span>
            <input type="number" min="0.001" step="any" inputMode="decimal" placeholder="1" value={quotaGB} onChange={(e) => setQuotaGB(e.target.value)} />
          </label>
          <div className="span2">
            <ErrorLine error={error} />
            {msg && <p className="accent small">{msg}</p>}
          </div>
          <div className="row span2 admin-split">
            <span />
            <button className="button" type="submit">
              Save
            </button>
          </div>
        </>
      )}
    </form>
  );
}

// ---------------------------------------------------------------- members

function Members({ ws, users }: { ws: AdminWorkspace; users: AdminUser[] }) {
  const { refresh } = useApp();
  const [items, setItems] = useState<AdminMembership[] | null>(null);
  const [error, setError] = useState("");
  const [addUser, setAddUser] = useState("");
  const [addWhere, setAddWhere] = useState(0); // 0 = the whole workspace, else one project
  const [addRole, setAddRole] = useState<Role>("member");
  const [projects, setProjects] = useState<AdminProject[]>([]);
  const byId = useMemo(() => new Map(users.map((u) => [u.id, u])), [users]);
  const projById = useMemo(() => new Map(projects.map((p) => [p.id, p])), [projects]);

  const load = useCallback(() => {
    adminApi
      .members(ws.id)
      .then((r) => setItems(r.items))
      .catch((e: unknown) => setError(errMsg(e)));
    adminApi
      .projects(ws.id)
      .then((r) => setProjects(r.items.filter((p) => !p.deleted).sort((a, b) => a.position - b.position)))
      .catch(() => undefined);
  }, [ws.id]);
  useEffect(load, [load]);

  const act = (p: Promise<unknown>) =>
    p
      .then(() => {
        setError("");
        load();
        refresh();
        return true;
      })
      .catch((e: unknown) => {
        setError(errMsg(e));
        return false;
      });

  // whole-workspace members can't be added again; project-only ones can get the workspace or more projects
  const wholeIds = new Set((items ?? []).filter((m) => m.whole !== false).map((m) => m.user_id));
  const candidates = users.filter((u) => !wholeIds.has(u.id) && !u.disabled).sort((a, b) => a.username.localeCompare(b.username));
  const roleChoices: readonly Role[] = addWhere ? PROJECT_ROLES : ROLES;
  const add = (e: FormEvent) => {
    e.preventDefault();
    if (!addUser) return;
    const req = addWhere ? adminApi.setProjectMember(addWhere, Number(addUser), addRole as ProjectRole) : adminApi.setMember(ws.id, Number(addUser), addRole);
    act(req).then((ok) => ok && setAddUser(""));
  };
  const sourceWords = (src: string) =>
    src === "user_all" ? "own rule, all workspaces" : src === "group" || src === "group_project" ? "via a group" : src === "group_all" ? "via a group, all workspaces" : src === "user" ? "workspace rule" : src;

  return (
    <section className="card" aria-label="Members">
      <div className="admin-section-head">
        <h2>Members</h2>
        <span className="muted small">Admins reach every workspace without a membership. A member of only some projects sees just those, with their gigs and errands (none of the workspace's templates, schedules or settings).</span>
      </div>
      <ErrorLine error={error} />
      {items && items.length === 0 && <Empty>No members yet.</Empty>}
      <ul className="admin-list">
        {(items ?? []).map((m) => {
          const u = byId.get(m.user_id);
          const whole = m.whole !== false;
          const own = (m.projects ?? []).filter((pm) => !whole || pm.preset || pm.role);
          return (
            <li key={m.user_id} className="admin-member">
            <div className="admin-list-row">
              {u ? <Avatar user={u} /> : <span className="avatar" />}
              <span className="admin-name">
                {u ? u.display_name || u.username : `user #${m.user_id}`}
                {u && <span className="muted small">@{u.username}</span>}
              </span>
              {u?.kind === "agent" && <span className="pill pill-agent">agent</span>}
              {u?.is_admin && <span className="pill pill-accent">admin</span>}
              {whole ? <RolePill role={m.role as Role} /> : <span className="pill pill-muted">some projects</span>}
              {whole && m.source && m.source !== "user" && (
                <span className="pill pill-muted" title="Where their access to this workspace comes from">
                  {m.source === "user_all" ? "own rule, all workspaces" : m.source === "group" ? "via a group" : m.source === "group_all" ? "via a group, all workspaces" : m.source}
                </span>
              )}
              <span className="grow" />
              <select
                aria-label={`Preset of ${u?.username ?? m.user_id} in the whole workspace`}
                value={m.preset ?? m.role}
                onChange={(e) => e.target.value && act(adminApi.setMember(ws.id, m.user_id, e.target.value as Role))}
              >
                {(m.preset ?? m.role) === "" && <option value="">{whole ? "no own preset" : "only projects"}</option>}
                {ROLES.map((r) => (
                  <option key={r} value={r}>
                    {r}
                  </option>
                ))}
              </select>
              {(m.preset ?? m.role) !== "" && (
                <ConfirmButton
                  label="Remove"
                  name={`Remove ${u?.username ?? "member " + m.user_id} from ${ws.name}`}
                  title={`Remove ${u?.username ?? "this member"}?`}
                  message={`Their own rule for ${ws.name} goes; access from groups or all-workspace rules stays. Gigs they can no longer see are unassigned.`}
                  confirmLabel="Remove"
                  onConfirm={() =>
                    adminApi.removeMember(ws.id, m.user_id).then(() => {
                      load();
                      refresh();
                    })
                  }
                />
              )}
            </div>
            {own.length > 0 && (
              <ul className="admin-member-projects" aria-label={`${u?.username ?? "Their"} projects`}>
                {own.map((pm) => {
                  const pr = projById.get(pm.project_id);
                  const name = pr?.name ?? `project #${pm.project_id}`;
                  return (
                    <li key={pm.project_id} className="admin-list-row">
                      <span className="admin-name">
                        <span className="muted" aria-hidden="true">›</span> {name}
                      </span>
                      {pm.role ? <RolePill role={pm.role} /> : <span className="pill pill-danger">can't see it</span>}
                      {pm.source && pm.source !== "user_project" && <span className="pill pill-muted">{sourceWords(pm.source)}</span>}
                      <span className="grow" />
                      <select
                        aria-label={`Preset of ${u?.username ?? m.user_id} in ${name}`}
                        value={pm.preset ?? ""}
                        onChange={(e) => e.target.value && act(adminApi.setProjectMember(pm.project_id, m.user_id, e.target.value as ProjectRole))}
                      >
                        {!pm.preset && <option value="">no own preset</option>}
                        {PROJECT_ROLES.map((r) => (
                          <option key={r} value={r}>
                            {r}
                          </option>
                        ))}
                      </select>
                      {pm.preset ? (
                        <ConfirmButton
                          label="Remove"
                          name={`Remove ${u?.username ?? "member " + m.user_id} from ${name}`}
                          title={`Remove ${u?.username ?? "them"} from ${name}?`}
                          message={`Their own rule for ${name} goes; their workspace access and groups stay. Gigs there they can no longer see are unassigned.`}
                          confirmLabel="Remove"
                          onConfirm={() =>
                            adminApi.removeProjectMember(pm.project_id, m.user_id).then(() => {
                              load();
                              refresh();
                            })
                          }
                        />
                      ) : null}
                    </li>
                  );
                })}
              </ul>
            )}
            </li>
          );
        })}
      </ul>
      <form className="row admin-add" onSubmit={add} aria-label="Add a member">
        <label className="grow">
          Add a member
          <select value={addUser} onChange={(e) => setAddUser(e.target.value)}>
            <option value="">Choose a user…</option>
            {candidates.map((u) => (
              <option key={u.id} value={u.id}>
                {u.username}
                {u.display_name && u.display_name !== u.username ? ` (${u.display_name})` : ""}
                {u.kind === "agent" ? " · agent" : ""}
              </option>
            ))}
          </select>
        </label>
        <label>
          Where
          <select
            value={addWhere}
            onChange={(e) => {
              const v = Number(e.target.value);
              setAddWhere(v);
              if (v && !(PROJECT_ROLES as readonly string[]).includes(addRole)) setAddRole("member");
            }}
          >
            <option value={0}>The whole workspace</option>
            {projects
              .filter((p) => !p.archived)
              .map((p) => (
                <option key={p.id} value={p.id}>
                  Only {p.name}
                </option>
              ))}
          </select>
        </label>
        <label>
          Role
          <select value={addRole} onChange={(e) => setAddRole(e.target.value as Role)}>
            {roleChoices.map((r) => (
              <option key={r} value={r}>
                {r}
              </option>
            ))}
          </select>
        </label>
        <button type="submit" className="button" disabled={!addUser}>
          Add
        </button>
      </form>
    </section>
  );
}

// ---------------------------------------------------------------- projects

function Projects({ ws }: { ws: AdminWorkspace }) {
  const { refresh } = useApp();
  const [items, setItems] = useState<AdminProject[] | null>(null);
  const [error, setError] = useState("");
  const [open, setOpen] = useState<number | null>(null);
  const [editing, setEditing] = useState<AdminProject | null>(null);
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [key, setKey] = useState("");
  const [busy, setBusy] = useState(false);

  const load = useCallback(() => {
    adminApi
      .projects(ws.id)
      .then((r) => setItems(r.items))
      .catch((e: unknown) => setError(errMsg(e)));
  }, [ws.id]);
  useEffect(load, [load]);
  const changed = () => {
    load();
    refresh();
  };

  const create = (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    adminApi
      .createProject(ws.id, { slug, name, key })
      .then(() => {
        setName("");
        setSlug("");
        setKey("");
        setError("");
        changed();
      })
      .catch((e: unknown) => setError(errMsg(e)))
      .finally(() => setBusy(false));
  };

  return (
    <section className="card" aria-label="Projects">
      <div className="admin-section-head">
        <h2>Projects</h2>
        <span className="muted small">Gig keys look like KEY-12.</span>
      </div>
      <ErrorLine error={error} />
      {items && items.length === 0 && <Empty>No projects yet.</Empty>}
      <ul className="admin-list">
        {(items ?? []).map((p) => (
          <li key={p.id} className={"admin-list-block" + (p.archived ? " admin-dim" : "")}>
            <div className="admin-list-row">
              <span className="pill pill-key" style={{ borderColor: colorFor(p.key, p.color) }}>
                {p.key}
              </span>
              <span className="admin-name">
                <span className="dot" style={{ background: colorFor(p.key, p.color) }} aria-hidden="true" />
                {p.name}
                <span className="muted small">{p.slug}</span>
              </span>
              {p.archived && <span className="pill pill-muted">archived</span>}
              <span className="grow" />
              <button type="button" className="button secondary small" aria-expanded={open === p.id} aria-label={`${open === p.id ? "Hide statuses" : "Statuses"} of project ${p.key}`} onClick={() => setOpen(open === p.id ? null : p.id)}>
                {open === p.id ? "Hide statuses" : "Statuses"}
              </button>
              <button type="button" className="button secondary small" aria-label={`Edit project ${p.key}`} onClick={() => setEditing(p)}>
                Edit
              </button>
              {p.archived ? (
                <button type="button" className="button secondary small" aria-label={`Unarchive project ${p.key}`} onClick={() => adminApi.patchProject(p, { archived: false }).then(changed, (e: unknown) => setError(errMsg(e)))}>
                  Unarchive
                </button>
              ) : (
                <ConfirmButton
                  label="Archive"
                  name={`Archive project ${p.key}`}
                  title={`Archive ${p.name}?`}
                  message="The project becomes read-only and leaves the sidebar. You can unarchive it."
                  confirmLabel="Archive"
                  onConfirm={() => adminApi.patchProject(p, { archived: true }).then(changed)}
                />
              )}
              <ConfirmButton
                label="Delete"
                name={`Delete project ${p.key}`}
                title={`Delete ${p.name}?`}
                message="Only an empty project can be deleted: move or delete its gigs first."
                confirmLabel="Delete"
                onConfirm={() => adminApi.deleteProject(p.id).then(changed)}
              />
            </div>
            {open === p.id && <Statuses project={p} />}
          </li>
        ))}
      </ul>
      <form className="row admin-add" onSubmit={create} aria-label="New project">
        <label className="grow">
          New project
          <input
            value={name}
            maxLength={64}
            placeholder="Name"
            required
            onChange={(e) => {
              const v = e.target.value;
              setName(v);
              setSlug(slugify(v));
              setKey(
                v
                  .toUpperCase()
                  .replace(/[^A-Z0-9]/g, "")
                  .replace(/^[0-9]+/, "")
                  .slice(0, 3),
              );
            }}
          />
        </label>
        <label>
          Slug
          <input value={slug} required pattern="[a-z][a-z0-9\-]{1,31}" onChange={(e) => setSlug(e.target.value)} />
        </label>
        <label className="admin-key-input">
          Key
          <input value={key} required pattern="[A-Z][A-Z0-9]{1,4}" maxLength={5} onChange={(e) => setKey(e.target.value.toUpperCase())} />
        </label>
        <button type="submit" className="button" disabled={busy}>
          Create
        </button>
      </form>
      {editing && (
        <EditProject
          project={editing}
          onClose={() => setEditing(null)}
          onDone={() => {
            setEditing(null);
            changed();
          }}
        />
      )}
    </section>
  );
}

function EditProject({ project, onClose, onDone }: { project: AdminProject; onClose: () => void; onDone: () => void }) {
  const [name, setName] = useState(project.name);
  const [color, setColor] = useState<string | null>(project.color);
  const [desc, setDesc] = useState(project.description_md);
  const [error, setError] = useState("");
  const save = (e: FormEvent) => {
    e.preventDefault();
    adminApi
      .patchProject(project, { name, color, description_md: desc })
      .then(onDone)
      .catch((e: unknown) => setError(errMsg(e)));
  };
  return (
    <Modal title={`Edit ${project.key}`} onClose={onClose}>
      <form className="form-grid" onSubmit={save}>
        <label className="span2">
          Name
          <input value={name} maxLength={64} required onChange={(e) => setName(e.target.value)} />
        </label>
        <div className="field span2">
          <span className="field-label">Colour</span>
          <ColorPicker value={color} onChange={setColor} />
        </div>
        <label className="span2">
          Description <span className="muted small">Markdown</span>
          <textarea rows={3} value={desc} onChange={(e) => setDesc(e.target.value)} />
        </label>
        <p className="muted small span2">The slug ({project.slug}) and key ({project.key}) are fixed once gigs exist.</p>
        <div className="span2">
          <ErrorLine error={error} />
        </div>
        <div className="row end span2">
          <button type="button" className="button secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="button">
            Save
          </button>
        </div>
      </form>
    </Modal>
  );
}

// ---------------------------------------------------------------- statuses

function Statuses({ project }: { project: AdminProject }) {
  const [items, setItems] = useState<AdminStatus[] | null>(null);
  const [error, setError] = useState("");
  const [editId, setEditId] = useState<number | null>(null);
  const [name, setName] = useState("");
  const [category, setCategory] = useState<Category>("todo");

  const load = useCallback(() => {
    adminApi
      .statuses(project.id)
      .then((r) => setItems(r.items))
      .catch((e: unknown) => setError(errMsg(e)));
  }, [project.id]);
  useEffect(load, [load]);

  const act = (p: Promise<unknown>) =>
    p
      .then(() => {
        setError("");
        load();
        return true;
      })
      .catch((e: unknown) => {
        setError(errMsg(e));
        return false;
      });

  const move = (i: number, d: -1 | 1) => {
    if (!items) return;
    const ids = items.map((s) => s.id);
    const j = i + d;
    if (j < 0 || j >= ids.length) return;
    [ids[i], ids[j]] = [ids[j]!, ids[i]!];
    act(adminApi.reorderStatuses(project.id, ids));
  };

  const add = (e: FormEvent) => {
    e.preventDefault();
    act(adminApi.createStatus(project.id, { name, category })).then((ok) => ok && setName(""));
  };

  return (
    <div className="admin-statuses" aria-label={`Statuses of ${project.name}`}>
      <ErrorLine error={error} />
      <ol className="admin-list">
        {(items ?? []).map((s, i) =>
          editId === s.id ? (
            <StatusEdit
              key={s.id}
              status={s}
              onCancel={() => setEditId(null)}
              onSave={(body) =>
                act(adminApi.patchStatus(s, body)).then((ok) => ok && setEditId(null))
              }
            />
          ) : (
            <li key={s.id} className="admin-list-row">
              <span className="admin-order">
                <button type="button" className="icon-button" disabled={i === 0} onClick={() => move(i, -1)} aria-label={`Move ${s.name} up`}>
                  ↑
                </button>
                <button type="button" className="icon-button" disabled={i === (items?.length ?? 0) - 1} onClick={() => move(i, 1)} aria-label={`Move ${s.name} down`}>
                  ↓
                </button>
              </span>
              <CategoryDot category={s.category} />
              <span className="admin-name">{s.name}</span>
              <CategoryPill category={s.category} />
              {s.is_default && (
                <span className="pill pill-accent" title={`New ${CATEGORY_LABEL[s.category]} gigs land here`}>
                  default
                </span>
              )}
              <span className="grow" />
              {!s.is_default && (
                <button type="button" className="button secondary small" aria-label={`Set default: ${s.name}`} onClick={() => act(adminApi.patchStatus(s, { is_default: true }))} title={`Make it the default for ${CATEGORY_LABEL[s.category]}`}>
                  Set default
                </button>
              )}
              <button type="button" className="button secondary small" aria-label={`Edit status ${s.name}`} onClick={() => setEditId(s.id)}>
                Edit
              </button>
              <ConfirmButton
                label="Delete"
                name={`Delete status ${s.name}`}
                title={`Delete status ${s.name}?`}
                message="Only an empty status can be deleted, and each category keeps at least one status."
                confirmLabel="Delete"
                onConfirm={() => adminApi.deleteStatus(s.id).then(load)}
              />
            </li>
          ),
        )}
      </ol>
      <form className="row admin-add" onSubmit={add} aria-label="Add a status">
        <label className="grow">
          Add a status
          <input value={name} maxLength={40} required placeholder="Name" onChange={(e) => setName(e.target.value)} />
        </label>
        <CategorySelect value={category} onChange={setCategory} />
        <button type="submit" className="button">
          Add
        </button>
      </form>
    </div>
  );
}

function CategorySelect({ value, onChange }: { value: Category; onChange: (c: Category) => void }) {
  return (
    <label>
      Category
      <select value={value} onChange={(e) => onChange(e.target.value as Category)}>
        {CATEGORIES.map((c) => (
          <option key={c} value={c}>
            {CATEGORY_LABEL[c]}
          </option>
        ))}
      </select>
    </label>
  );
}

function StatusEdit({ status, onCancel, onSave }: { status: AdminStatus; onCancel: () => void; onSave: (b: { name?: string; category?: Category }) => void }) {
  const [name, setName] = useState(status.name);
  const [category, setCategory] = useState<Category>(status.category);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const body: { name?: string; category?: Category } = {};
    if (name !== status.name) body.name = name;
    if (category !== status.category) body.category = category;
    if (Object.keys(body).length === 0) onCancel();
    else onSave(body);
  };
  return (
    <li className="admin-list-row admin-editing">
      <form className="row grow" onSubmit={submit} aria-label={`Edit ${status.name}`}>
        <CategoryDot category={category} />
        <label className="grow">
          Name
          <input value={name} maxLength={40} required autoFocus onChange={(e) => setName(e.target.value)} />
        </label>
        <CategorySelect value={category} onChange={setCategory} />
        <button type="button" className="button secondary small" onClick={onCancel}>
          Cancel
        </button>
        <button type="submit" className="button small">
          Save
        </button>
      </form>
    </li>
  );
}
