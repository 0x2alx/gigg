// API keys: every key on the instance, creation with scopes and restrictions, rotation, revocation.
// A new or rotated token lives only in the one-time modal's state and is dropped when it closes.
import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { fmtTime } from "../api";
import { Avatar, Modal } from "../ui";
import {
  adminApi,
  errMsg,
  SCOPE_GROUPS,
  type AdminMembership,
  type AdminProject,
  type AdminUser,
  type AdminWorkspace,
  type ApiKey,
  type KeyCreate,
  type KeyCreated,
} from "./adminApi";
import { AdminPage, ConfirmButton, Empty, ErrorLine, TimeAgo } from "./common";

const DAY = 864e5;
const MAX_DAYS = 3 * 366;

type KeyState = "live" | "revoked" | "expired";
function keyState(k: ApiKey): KeyState {
  if (k.revoked) return "revoked";
  if (k.expires_at <= Date.now()) return "expired";
  return "live";
}

export function KeysPage() {
  const [keys, setKeys] = useState<ApiKey[] | null>(null);
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [workspaces, setWorkspaces] = useState<AdminWorkspace[]>([]);
  const [projects, setProjects] = useState<Map<number, AdminProject>>(new Map());
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);
  const [token, setToken] = useState<KeyCreated | null>(null);
  const [showDead, setShowDead] = useState(false);

  const load = useCallback(() => {
    adminApi
      .keys()
      .then((r) => {
        setKeys(r.items);
        setError("");
      })
      .catch((e: unknown) => setError(errMsg(e)));
  }, []);
  useEffect(() => {
    load();
    adminApi
      .users()
      .then((r) => setUsers(r.items))
      .catch(() => undefined);
    adminApi
      .workspaces()
      .then(async (r) => {
        setWorkspaces(r.items);
        const lists = await Promise.allSettled(r.items.map((w) => adminApi.projects(w.id)));
        const m = new Map<number, AdminProject>();
        for (const l of lists) if (l.status === "fulfilled") for (const p of l.value.items) m.set(p.id, p);
        setProjects(m);
      })
      .catch(() => undefined);
  }, [load]);

  const userById = useMemo(() => new Map(users.map((u) => [u.id, u])), [users]);
  const wsById = useMemo(() => new Map(workspaces.map((w) => [w.id, w])), [workspaces]);

  const sorted = (keys ?? []).slice().sort((a, b) => {
    const order = { live: 0, expired: 1, revoked: 2 };
    return order[keyState(a)] - order[keyState(b)] || b.created_at - a.created_at;
  });
  const dead = sorted.filter((k) => keyState(k) !== "live").length;
  const shown = showDead ? sorted : sorted.filter((k) => keyState(k) === "live");

  return (
    <AdminPage
      title="API keys"
      sub="A key acts as its user and never has more than the user. It can be limited to one workspace (or some projects), to read-only, or to picked scopes."
      actions={
        <button type="button" className="button" onClick={() => setCreating(true)}>
          + New key
        </button>
      }
    >
      <ErrorLine error={error} />
      {keys === null && !error && <p className="muted">Loading…</p>}
      {keys && (
        <>
          <div className="admin-cards">
            {shown.map((k) => (
              <KeyCard
                key={k.id}
                k={k}
                user={userById.get(k.user_id)}
                ws={k.workspace_id !== null ? wsById.get(k.workspace_id) : undefined}
                projects={projects}
                onRotate={() =>
                  adminApi.rotateKey(k.id).then((r) => {
                    setToken(r);
                    load();
                  })
                }
                onRevoke={() => adminApi.revokeKey(k.id).then(load)}
              />
            ))}
          </div>
          {shown.length === 0 && <Empty>No live keys.</Empty>}
          {dead > 0 && (
            <button type="button" className="link small" onClick={() => setShowDead((v) => !v)}>
              {showDead ? "Hide" : "Show"} {dead} revoked or expired
            </button>
          )}
        </>
      )}
      {creating && (
        <CreateKey
          users={users}
          workspaces={workspaces}
          onClose={() => setCreating(false)}
          onDone={(r) => {
            setCreating(false);
            setToken(r);
            load();
          }}
        />
      )}
      {token && <TokenModal created={token} onClose={() => setToken(null)} />}
    </AdminPage>
  );
}

function KeyCard({
  k,
  user,
  ws,
  projects,
  onRotate,
  onRevoke,
}: {
  k: ApiKey;
  user: AdminUser | undefined;
  ws: AdminWorkspace | undefined;
  projects: Map<number, AdminProject>;
  onRotate: () => Promise<unknown>;
  onRevoke: () => Promise<unknown>;
}) {
  const st = keyState(k);
  const soon = st === "live" && k.expires_at - Date.now() < 14 * DAY;
  return (
    <article className={"card admin-key" + (st !== "live" ? " admin-dim" : "")} aria-label={`Key ${k.name}`}>
      <header className="admin-key-head">
        <span className="admin-key-name">{k.name}</span>
        <span className="pill pill-key admin-mono">{k.prefix}…</span>
        {st === "revoked" && <span className="pill pill-danger">revoked</span>}
        {st === "expired" && <span className="pill pill-muted">expired</span>}
        {k.workspace_id === null ? (
          user?.is_admin ? <span className="pill pill-accent">admin key</span> : <span className="pill pill-ws">member workspaces</span>
        ) : <span className="pill pill-ws">{ws?.name ?? `workspace #${k.workspace_id}`}</span>}
        <span className="grow" />
        {st === "live" && (
          // the two actions stay together when the header wraps (phone)
          <span className="admin-key-actions">
            <ConfirmButton
              label="Rotate"
              danger={false}
              title={`Rotate ${k.name}?`}
              message="The current secret stops working at once; you get a new token to paste wherever the key is used."
              confirmLabel="Rotate"
              onConfirm={onRotate}
            />
            <ConfirmButton
              label="Revoke"
              title={`Revoke ${k.name}?`}
              message="Anything using this key loses access immediately. This can't be undone."
              confirmLabel="Revoke"
              onConfirm={onRevoke}
            />
          </span>
        )}
      </header>
      <dl className="admin-props">
        <dt>Acts as</dt>
        <dd>
          {user ? (
            <span className="user-badge">
              <Avatar user={user} />
              <span>{user.username}</span>
              {user.kind === "agent" && <span className="pill pill-agent">agent</span>}
              {user.is_admin && <span className="pill pill-accent">admin</span>}
            </span>
          ) : (
            <span className="muted">user #{k.user_id}</span>
          )}
        </dd>
        {k.project_ids && k.project_ids.length > 0 && (
          <>
            <dt>Projects</dt>
            <dd className="chips">
              {k.project_ids.map((id) => (
                <span key={id} className="pill pill-key">
                  {projects.get(id)?.key ?? `#${id}`}
                </span>
              ))}
            </dd>
          </>
        )}
        <dt>Scopes</dt>
        <dd className="chips">
          {k.scopes.map((s) => (
            <span key={s} className={"pill " + (s.startsWith("admin:") ? "pill-accent" : s.endsWith(":read") ? "pill-muted" : "pill-ws")}>
              {s}
            </span>
          ))}
        </dd>
        <dt>Usable from</dt>
        <dd className="chips">
          {k.allowed_listeners?.length ? k.allowed_listeners.map((l) => <span key={l} className="pill pill-ws">⇢ {l}</span>) : <span className="pill pill-muted">any listener</span>}
          {k.allowed_cidrs?.length ? k.allowed_cidrs.map((c) => <span key={c} className="pill pill-key admin-mono">{c}</span>) : <span className="pill pill-muted">any network</span>}
        </dd>
        <dt>Expires</dt>
        <dd>
          <span className={soon ? "danger" : undefined} title={fmtTime(k.expires_at)}>
            <TimeAgo ms={k.expires_at} />
          </span>
          <span className="muted small"> · created {fmtTime(k.created_at)}</span>
        </dd>
        <dt>Last used</dt>
        <dd className="row admin-inline">
          <TimeAgo ms={k.last_used_at} />
          {k.last_used_listener && <span className="pill pill-ws">⇢ {k.last_used_listener}</span>}
          {k.last_used_ip && <span className="pill pill-muted admin-mono">{k.last_used_ip}</span>}
        </dd>
      </dl>
    </article>
  );
}

function splitList(s: string): string[] {
  return s
    .split(/[,\s]+/)
    .map((x) => x.trim())
    .filter(Boolean);
}

function CreateKey({ users, workspaces, onClose, onDone }: { users: AdminUser[]; workspaces: AdminWorkspace[]; onClose: () => void; onDone: (r: KeyCreated) => void }) {
  const active = users.filter((u) => !u.disabled).sort((a, b) => Number(a.kind === "human") - Number(b.kind === "human") || a.username.localeCompare(b.username));
  const [name, setName] = useState("");
  const [userId, setUserId] = useState<number>(active[0]?.id ?? 0);
  const [wsId, setWsId] = useState<number>(0); // 0 = admin key
  const [projectIds, setProjectIds] = useState<number[]>([]);
  const [scopes, setScopes] = useState<string[]>(["workspace:read", "tasks:read", "comments:read"]);
  const [mode, setMode] = useState<"full" | "read" | "custom">("full"); // what the key may do, within its user's permissions
  const [listeners, setListeners] = useState("");
  const [cidrs, setCidrs] = useState("");
  const [days, setDays] = useState(365);
  const [wsProjects, setWsProjects] = useState<AdminProject[]>([]);
  const [wsMembers, setWsMembers] = useState<AdminMembership[] | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const user = users.find((u) => u.id === userId);
  const unbound = wsId === 0;
  const adminKey = unbound && !!user?.is_admin;
  const liveWs = workspaces.filter((w) => !w.archived);

  useEffect(() => {
    setProjectIds([]);
    setWsProjects([]);
    setWsMembers(null);
    if (!wsId) return;
    adminApi
      .projects(wsId)
      .then((r) => setWsProjects(r.items.filter((p) => !p.archived)))
      .catch(() => undefined);
    adminApi
      .members(wsId)
      .then((r) => setWsMembers(r.items))
      .catch(() => undefined);
  }, [wsId]);

  // admin:* scopes only on admin keys
  useEffect(() => {
    if (!adminKey) setScopes((s) => s.filter((x) => !x.startsWith("admin:")));
  }, [adminKey]);

  const notMember = !!user && !user.is_admin && !!wsMembers && !wsMembers.some((m) => m.user_id === user.id);
  const toggle = (s: string) => setScopes((cur) => (cur.includes(s) ? cur.filter((x) => x !== s) : [...cur, s]));
  const toggleProject = (id: number) => setProjectIds((cur) => (cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id]));
  const daysOk = Number.isInteger(days) && days >= 1 && days <= MAX_DAYS;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!daysOk) {
      setError(`Expiry must be 1 to ${MAX_DAYS} days.`);
      return;
    }
    const group = (n: string) => SCOPE_GROUPS.find((g) => g.name === n)?.scopes ?? [];
    const chosen = mode === "full" ? [...group("Read"), ...group("Write"), ...(adminKey ? group("Admin") : [])] : mode === "read" ? group("Read") : scopes;
    if (chosen.length === 0) {
      setError("Pick at least one scope.");
      return;
    }
    const body: KeyCreate = { name, user_id: userId, scopes: chosen, expires_at: Date.now() + days * DAY };
    if (!unbound) body.workspace = String(wsId);
    if (!unbound && projectIds.length) body.project_ids = projectIds;
    const ls = splitList(listeners);
    if (ls.length) body.allowed_listeners = ls;
    const cs = splitList(cidrs);
    if (cs.length) body.allowed_cidrs = cs;
    setBusy(true);
    setError("");
    adminApi
      .createKey(body)
      .then(onDone)
      .catch((e: unknown) => setError(errMsg(e)))
      .finally(() => setBusy(false));
  };

  return (
    <Modal title="New API key" onClose={onClose} wide>
      <form className="form-grid" onSubmit={submit}>
        <label>
          Name
          <input value={name} maxLength={64} required placeholder="e.g. scout-runner" onChange={(e) => setName(e.target.value)} />
        </label>
        <label>
          Acting user
          <select value={userId} onChange={(e) => setUserId(Number(e.target.value))} required>
            {active.map((u) => (
              <option key={u.id} value={u.id}>
                {u.username}
                {u.kind === "agent" ? " · agent" : ""}
                {u.is_admin ? " · admin" : ""}
              </option>
            ))}
          </select>
        </label>
        <label className="span2">
          Workspace
          <select value={wsId} onChange={(e) => setWsId(Number(e.target.value))}>
            <option value={0}>{user && !user.is_admin ? "Any: every workspace the user can see" : "Any: instance-wide (admin key)"}</option>
            {liveWs.map((w) => (
              <option key={w.id} value={w.id}>
                {w.name} ({w.slug})
              </option>
            ))}
          </select>
        </label>
        {notMember && <p className="danger small span2">{user?.username} can't see this workspace; give them access first (Workspaces or Users).</p>}
        {!unbound && (
          <div className="field span2">
            <span className="field-label">Projects <span className="muted small">optional; none selected = every project of the workspace</span></span>
            <div className="chips">
              {wsProjects.length === 0 && <span className="muted small">No projects.</span>}
              {wsProjects.map((p) => (
                <button key={p.id} type="button" className={"admin-toggle" + (projectIds.includes(p.id) ? " on" : "")} aria-pressed={projectIds.includes(p.id)} onClick={() => toggleProject(p.id)}>
                  <span className="pill pill-key">{p.key}</span> {p.name}
                </button>
              ))}
            </div>
          </div>
        )}
        <div className="field span2">
          <span className="field-label">The key may</span>
          <div className="admin-seg" role="radiogroup" aria-label="What the key may do">
            {(
              [
                ["full", "Everything the user can"],
                ["read", "Read only"],
                ["custom", "Pick scopes"],
              ] as const
            ).map(([v, text]) => (
              <button key={v} type="button" role="radio" aria-checked={mode === v} className={"admin-seg-btn" + (mode === v ? " on" : "")} onClick={() => setMode(v)}>
                {text}
              </button>
            ))}
          </div>
          <span className="muted small">
            {mode === "full"
              ? adminKey
                ? "Acts as the user, admin powers included. A key never has more than its user."
                : "Acts as the user. A key never has more than its user; the workspace and project limits above still apply."
              : mode === "read"
                ? "Sees what the user sees; changes nothing."
                : "Only the scopes ticked below, within the user's permissions."}
          </span>
        </div>
        {mode === "custom" && (
        <fieldset className="span2 admin-scopes">
          <legend className="field-label">Scopes</legend>
          {SCOPE_GROUPS.map((g) => {
            const off = g.name === "Admin" && !adminKey;
            return (
              <div key={g.name} className={"admin-scope-group" + (off ? " admin-dim" : "")}>
                <div className="admin-scope-head">
                  <span>{g.name}</span>
                  {!off && (
                    <button
                      type="button"
                      className="link small"
                      onClick={() => setScopes((cur) => (g.scopes.every((s) => cur.includes(s)) ? cur.filter((s) => !g.scopes.includes(s)) : [...new Set([...cur, ...g.scopes])]))}
                    >
                      {g.scopes.every((s) => scopes.includes(s)) ? "none" : "all"}
                    </button>
                  )}
                  {off && <span className="muted small">admin keys only</span>}
                </div>
                {g.scopes.map((s) => (
                  <label key={s} className="admin-check">
                    <input type="checkbox" checked={scopes.includes(s)} disabled={off} onChange={() => toggle(s)} />
                    <span className="admin-mono">{s}</span>
                  </label>
                ))}
              </div>
            );
          })}
        </fieldset>
        )}
        <label>
          Allowed listeners <span className="muted small">comma separated; empty = any</span>
          <input value={listeners} placeholder="internal" onChange={(e) => setListeners(e.target.value)} />
        </label>
        <label>
          Allowed networks (CIDR) <span className="muted small">empty = any</span>
          <input value={cidrs} placeholder="10.0.0.0/8, 192.168.1.0/24" onChange={(e) => setCidrs(e.target.value)} />
        </label>
        <label>
          Expires in (days) <span className="muted small">max {MAX_DAYS}</span>
          <input type="number" min={1} max={MAX_DAYS} value={days} onChange={(e) => setDays(Number(e.target.value))} />
        </label>
        <div className="field">
          <span className="field-label">Expiry date</span>
          <span className={daysOk ? undefined : "danger"}>{daysOk ? fmtTime(Date.now() + days * DAY) : "out of range"}</span>
        </div>
        <div className="span2">
          <ErrorLine error={error} />
        </div>
        <div className="row end span2">
          <button type="button" className="button secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="button" disabled={busy || !name || !userId}>
            Create key
          </button>
        </div>
      </form>
    </Modal>
  );
}

// TokenModal shows a fresh token once. Nothing persists it: it lives in the parent's state only
// while this modal is open, and the parent drops it on close.
function TokenModal({ created, onClose }: { created: KeyCreated; onClose: () => void }) {
  const [copied, setCopied] = useState(false);
  const ref = useRef<HTMLInputElement>(null);
  const copy = () => {
    const done = () => setCopied(true);
    if (navigator.clipboard?.writeText) {
      navigator.clipboard.writeText(created.token).then(done, () => {
        ref.current?.select();
      });
    } else {
      ref.current?.select();
    }
  };
  return (
    <Modal title={`Token for ${created.key.name}`} onClose={onClose}>
      <div className="admin-confirm">
        <p className="admin-warn" role="alert">
          Copy it now: this token is shown only once and can't be retrieved later. If it's lost, rotate the key.
        </p>
        <div className="row">
          <input ref={ref} className="admin-mono grow" readOnly value={created.token} aria-label="Token" autoComplete="off" spellCheck={false} onFocus={(e) => e.currentTarget.select()} />
          <button type="button" className="button" onClick={copy}>
            {copied ? "Copied ✓" : "Copy"}
          </button>
        </div>
        <div className="row end">
          <button type="button" className="button secondary" onClick={onClose}>
            I've stored it
          </button>
        </div>
      </div>
    </Modal>
  );
}
