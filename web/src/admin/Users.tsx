// Users: people and agents, admin rights, disabling, passwords.
import { useCallback, useEffect, useRef, useState, type FormEvent, type KeyboardEvent } from "react";
import { Link } from "wouter";
import { useApp } from "../App";
import { ColorPicker } from "../fields";
import { Avatar, Modal } from "../ui";
import { adminApi, errMsg, type AdminUser } from "./adminApi";
import { AdminPage, ConfirmButton, Empty, ErrorLine, Field, FormActions, InfoLine, TimeAgo } from "./common";
import { followName, PICTURE_TYPES, pictureProblem, usernameFrom } from "./forms";
import { PermissionsModal } from "./Permissions";

type Filter = "all" | "human" | "agent" | "admin" | "disabled";

export function UsersPage() {
  const { me, refresh } = useApp();
  const [items, setItems] = useState<AdminUser[] | null>(null);
  const [error, setError] = useState("");
  const [filter, setFilter] = useState<Filter>("all");
  const [query, setQuery] = useState("");
  const [creating, setCreating] = useState(false);
  // ids, not snapshots: a picture change bumps the user's version, and edits send If-Match
  const [editingId, setEditingId] = useState<number | null>(null);
  const [pictureId, setPictureId] = useState<number | null>(null);
  const [pwFor, setPwFor] = useState<AdminUser | null>(null);
  const [permsFor, setPermsFor] = useState<AdminUser | null>(null);

  const load = useCallback(() => {
    adminApi
      .users()
      .then((r) => {
        setItems(r.items);
        setError("");
      })
      .catch((e: unknown) => setError(errMsg(e)));
  }, []);
  useEffect(load, [load]);
  const changed = () => {
    load();
    refresh();
  };

  const q = query.trim().toLowerCase();
  const shown = (items ?? [])
    .filter((u) => {
      switch (filter) {
        case "human":
        case "agent":
          return u.kind === filter;
        case "admin":
          return u.is_admin;
        case "disabled":
          return u.disabled;
        default:
          return true;
      }
    })
    .filter((u) => !q || u.username.includes(q) || u.display_name.toLowerCase().includes(q))
    .sort((a, b) => Number(a.disabled) - Number(b.disabled) || Number(a.kind === "agent") - Number(b.kind === "agent") || a.username.localeCompare(b.username));

  const editingUser = items?.find((u) => u.id === editingId) ?? null;
  const pictureUser = items?.find((u) => u.id === pictureId && u.kind === "agent") ?? null;
  const count = (f: Filter) => (items ?? []).filter((u) => (f === "admin" ? u.is_admin : f === "disabled" ? u.disabled : f === "all" ? true : u.kind === f)).length;

  return (
    <AdminPage
      title="Users"
      sub="People log in with a password; agents use API keys."
      actions={
        <button type="button" className="button" onClick={() => setCreating(true)}>
          + New user
        </button>
      }
    >
      <div className="row admin-toolbar">
        <div className="admin-seg" role="group" aria-label="Filter users">
          {(["all", "human", "agent", "admin", "disabled"] as Filter[]).map((f) => (
            <button key={f} type="button" className={"admin-seg-btn" + (filter === f ? " on" : "")} aria-pressed={filter === f} onClick={() => setFilter(f)}>
              {f} <span className="admin-seg-count">{count(f)}</span>
            </button>
          ))}
        </div>
        <input className="admin-search" type="search" placeholder="Search…" value={query} onChange={(e) => setQuery(e.target.value)} aria-label="Search users" />
      </div>
      <ErrorLine error={error} />
      {items === null && !error && <p className="muted">Loading…</p>}
      {items && (
        <div className="card admin-table-card">
          <table className="admin-table">
            <thead>
              <tr>
                <th>User</th>
                <th>Kind</th>
                <th>Flags</th>
                <th>Last login</th>
                <th aria-label="Actions" />
              </tr>
            </thead>
            <tbody>
              {shown.map((u) => (
                <tr key={u.id} className={u.disabled ? "admin-dim" : undefined}>
                  <td>
                    <span className="admin-user">
                      <Avatar user={u} />
                      <span className="admin-user-text">
                        <span>{u.display_name || u.username}</span>
                        <span className="muted small">
                          @{u.username}
                          {u.description ? ` · ${u.description}` : ""}
                        </span>
                      </span>
                    </span>
                  </td>
                  <td>{u.kind === "agent" ? <span className="pill pill-agent">agent</span> : <span className="pill pill-muted">human</span>}</td>
                  <td>
                    <span className="chips admin-flags">
                      {u.is_admin && <span className="pill pill-accent">admin</span>}
                      {u.disabled && <span className="pill pill-danger">disabled</span>}
                      {u.id === me.user.id && <span className="pill pill-ws">you</span>}
                    </span>
                  </td>
                  <td className="small admin-lastlogin" data-label="Last login">{u.kind === "agent" ? <span className="muted">n/a</span> : <TimeAgo ms={u.last_login_at} />}</td>
                  <td className="admin-actions">
                    <button type="button" className="button secondary small" onClick={() => setEditingId(u.id)}>
                      Edit
                    </button>
                    {u.kind === "agent" && (
                      <button type="button" className="button secondary small" onClick={() => setPictureId(u.id)}>
                        Picture
                      </button>
                    )}
                    <button type="button" className="button secondary small" onClick={() => setPermsFor(u)}>
                      Permissions
                    </button>
                    {u.kind === "human" && (
                      <button type="button" className="button secondary small" onClick={() => setPwFor(u)}>
                        Password
                      </button>
                    )}
                    <ConfirmButton
                      label={u.is_admin ? "Revoke admin" : "Make admin"}
                      danger={u.is_admin}
                      title={u.is_admin ? `Revoke admin from ${u.username}?` : `Make ${u.username} an admin?`}
                      message={
                        u.is_admin
                          ? "They keep their workspace memberships but lose instance-wide access. The last admin can't be demoted."
                          : "Admins reach every workspace, manage users and keys, and read the audit log."
                      }
                      confirmLabel={u.is_admin ? "Revoke" : "Make admin"}
                      onConfirm={() => adminApi.patchUser(u, { is_admin: !u.is_admin }).then(changed)}
                    />
                    {u.disabled ? (
                      <ConfirmButton
                        label="Enable"
                        danger={false}
                        title={`Enable ${u.username}?`}
                        message="They can log in and use their keys again."
                        confirmLabel="Enable"
                        onConfirm={() => adminApi.patchUser(u, { disabled: false }).then(changed)}
                      />
                    ) : (
                      <ConfirmButton
                        label="Disable"
                        title={`Disable ${u.username}?`}
                        message="Their sessions and keys stop working until you enable them again. Their tasks and comments stay."
                        confirmLabel="Disable"
                        disabled={u.id === me.user.id}
                        onConfirm={() => adminApi.patchUser(u, { disabled: true }).then(changed)}
                      />
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {shown.length === 0 && <Empty>No users match.</Empty>}
        </div>
      )}
      {creating && (
        <CreateUser
          onClose={() => setCreating(false)}
          onDone={() => {
            setCreating(false);
            changed();
          }}
        />
      )}
      {editingUser && (
        <EditUser
          user={editingUser}
          onClose={() => setEditingId(null)}
          onPicture={changed}
          onDone={() => {
            setEditingId(null);
            changed();
          }}
        />
      )}
      {pictureUser && (
        <Modal title={`Picture for @${pictureUser.username}`} onClose={() => setPictureId(null)}>
          <AgentPicture user={pictureUser} onChanged={changed} />
          <FormActions>
            <button type="button" className="button" onClick={() => setPictureId(null)}>
              Done
            </button>
          </FormActions>
        </Modal>
      )}
      {pwFor && <SetPassword user={pwFor} onClose={() => setPwFor(null)} />}
      {permsFor && <PermissionsModal user={permsFor} onClose={() => setPermsFor(null)} />}
    </AdminPage>
  );
}

const KINDS = [
  { id: "human", label: "Person", icon: "☺" },
  { id: "agent", label: "Agent", icon: "⚙" },
] as const;

// KindSwitch: a two-way segmented control (radio group: arrow keys move between the halves).
function KindSwitch({ value, onChange }: { value: "human" | "agent"; onChange: (k: "human" | "agent") => void }) {
  const refs = useRef<(HTMLButtonElement | null)[]>([]);
  const key = (e: KeyboardEvent<HTMLButtonElement>, i: number) => {
    if (!["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(e.key)) return;
    e.preventDefault();
    const next = KINDS[(i + 1) % 2]!;
    onChange(next.id);
    refs.current[(i + 1) % 2]?.focus();
  };
  return (
    <div className="admin-segfull" role="radiogroup" aria-label="Kind">
      {KINDS.map((k, i) => (
        <button
          key={k.id}
          ref={(el) => {
            refs.current[i] = el;
          }}
          type="button"
          role="radio"
          aria-checked={value === k.id}
          tabIndex={value === k.id ? 0 : -1}
          className={value === k.id ? "on" : undefined}
          onClick={() => onChange(k.id)}
          onKeyDown={(e) => key(e, i)}
        >
          <span aria-hidden="true">{k.icon}</span> {k.label}
        </button>
      ))}
    </div>
  );
}

function CreateUser({ onClose, onDone }: { onClose: () => void; onDone: () => void }) {
  const [username, setUsername] = useState("");
  const [touched, setTouched] = useState(false);
  const [kind, setKind] = useState<"human" | "agent">("human");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [isAdmin, setIsAdmin] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    adminApi
      .createUser({
        username,
        kind,
        display_name: displayName || undefined,
        password: kind === "human" && password ? password : undefined,
        is_admin: isAdmin || undefined,
      })
      .then(onDone)
      .catch((e: unknown) => setError(errMsg(e)))
      .finally(() => setBusy(false));
  };
  return (
    <Modal title="New user" onClose={onClose}>
      <form className="form-grid admin-form" onSubmit={submit}>
        <div className="admin-field span2">
          <span className="admin-field-label">Kind</span>
          <KindSwitch value={kind} onChange={setKind} />
        </div>
        <Field label="Display name" hint="Optional; shown instead of the username.">
          <input
            value={displayName}
            maxLength={64}
            data-autofocus
            autoComplete="off"
            onChange={(e) => {
              setDisplayName(e.target.value);
              setUsername((cur) => followName(e.target.value, cur, touched, usernameFrom));
            }}
          />
        </Field>
        <Field label="Username" hint={touched ? "Lowercase a-z, 0-9, _ and -." : "Filled in from the name; edit it if you like."}>
          <input
            value={username}
            required
            pattern="[a-z][a-z0-9_\-]{1,31}"
            maxLength={32}
            autoComplete="off"
            spellCheck={false}
            className="admin-mono"
            onChange={(e) => {
              const v = e.target.value.toLowerCase();
              setTouched(v !== "");
              setUsername(v);
            }}
          />
        </Field>
        {kind === "human" ? (
          <Field span2 label="Password" hint="Optional, 12+ characters. Without one they can't log in until you set it.">
            <input type="password" autoComplete="new-password" minLength={12} maxLength={256} value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
        ) : (
          <InfoLine span2>
            Agents have no password: they sign in with an API key. After creating the agent, <Link href="/keys">create a key on the Keys page</Link>.
          </InfoLine>
        )}
        <label className="admin-check span2">
          <input type="checkbox" checked={isAdmin} onChange={(e) => setIsAdmin(e.target.checked)} />
          <span>Instance admin</span>
        </label>
        {error && (
          <div className="span2">
            <ErrorLine error={error} />
          </div>
        )}
        <FormActions>
          <button type="button" className="button secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="button" disabled={busy || !username}>
            {busy ? "Creating…" : kind === "agent" ? "Create agent" : "Create user"}
          </button>
        </FormActions>
      </form>
    </Modal>
  );
}

// AgentPicture: upload, replace or remove an agent's picture (admins only; the server refuses
// other people's pictures, so human rows never show this).
function AgentPicture({ user, onChanged }: { user: AdminUser; onChanged: () => void }) {
  const input = useRef<HTMLInputElement>(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState<"" | "up" | "rm">("");
  const run = (kind: "up" | "rm", p: Promise<unknown>) => {
    setBusy(kind);
    setErr("");
    p.then(onChanged)
      .catch((e: unknown) => setErr(errMsg(e)))
      .finally(() => setBusy(""));
  };
  const upload = (f: File | undefined) => {
    if (!f) return;
    const problem = pictureProblem(f);
    if (problem) {
      setErr(problem);
      return;
    }
    run("up", adminApi.putUserAvatar(user.id, f));
  };
  return (
    <div className="admin-picture">
      <span className="admin-picture-preview">
        <Avatar user={user} size="m" />
      </span>
      <div className="admin-picture-body">
        <div className="admin-picture-actions">
          <button type="button" className="button secondary small" onClick={() => input.current?.click()} disabled={busy !== ""}>
            {busy === "up" ? "Uploading…" : user.has_avatar ? "Replace picture" : "Upload a picture"}
          </button>
          {user.has_avatar && (
            <button type="button" className="button danger-button small" onClick={() => run("rm", adminApi.deleteUserAvatar(user.id))} disabled={busy !== ""}>
              {busy === "rm" ? "Removing…" : "Remove"}
            </button>
          )}
          <input
            ref={input}
            type="file"
            accept={PICTURE_TYPES.join(",")}
            hidden
            onChange={(e) => {
              upload(e.target.files?.[0]);
              e.target.value = "";
            }}
          />
        </div>
        <span className="admin-field-hint">PNG, JPEG, WebP or GIF, up to 2 MB; cropped to a square. Without one, the initials and colour show.</span>
        <ErrorLine error={err} />
      </div>
    </div>
  );
}

function EditUser({ user, onClose, onDone, onPicture }: { user: AdminUser; onClose: () => void; onDone: () => void; onPicture: () => void }) {
  const [name, setName] = useState(user.display_name);
  const [desc, setDesc] = useState(user.description ?? "");
  const [color, setColor] = useState<string | null>(user.color);
  const [error, setError] = useState("");
  const save = (e: FormEvent) => {
    e.preventDefault();
    adminApi
      .patchUser(user, { display_name: name, description: desc, color })
      .then(onDone)
      .catch((e: unknown) => setError(errMsg(e)));
  };
  return (
    <Modal title={`Edit @${user.username}`} onClose={onClose}>
      <form className="form-grid admin-form" onSubmit={save}>
        {user.kind === "agent" ? (
          <div className="span2">
            <AgentPicture user={{ ...user, display_name: name, color }} onChanged={onPicture} />
          </div>
        ) : (
          <div className="row span2 admin-preview">
            <Avatar user={{ ...user, display_name: name, color }} size="m" />
            <span className="muted small">Person</span>
          </div>
        )}
        <Field label="Display name">
          <input value={name} maxLength={64} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label="About">
          <input value={desc} maxLength={200} placeholder="One line" onChange={(e) => setDesc(e.target.value)} />
        </Field>
        <div className="field span2">
          <span className="field-label">Colour</span>
          <ColorPicker value={color} onChange={setColor} />
        </div>
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

function SetPassword({ user, onClose }: { user: AdminUser; onClose: () => void }) {
  const [pw, setPw] = useState("");
  const [again, setAgain] = useState("");
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (pw !== again) {
      setError("The passwords differ.");
      return;
    }
    adminApi
      .setPassword(user.id, pw)
      .then(() => {
        setPw("");
        setAgain("");
        setDone(true);
      })
      .catch((e: unknown) => setError(errMsg(e)));
  };
  return (
    <Modal title={`Password for @${user.username}`} onClose={onClose}>
      {done ? (
        <div className="admin-confirm">
          <p>Password set. Their existing sessions were logged out.</p>
          <div className="row end">
            <button type="button" className="button" onClick={onClose}>
              Done
            </button>
          </div>
        </div>
      ) : (
        <form className="form-grid admin-form" onSubmit={submit}>
          <Field label="New password" hint="12+ characters.">
            <input type="password" autoComplete="new-password" minLength={12} maxLength={256} required value={pw} onChange={(e) => setPw(e.target.value)} />
          </Field>
          <Field label="Again">
            <input type="password" autoComplete="new-password" minLength={12} maxLength={256} required value={again} onChange={(e) => setAgain(e.target.value)} />
          </Field>
          <InfoLine span2>Setting it logs out every session of this user.</InfoLine>
          <div className="span2">
            <ErrorLine error={error} />
          </div>
          <div className="row end span2">
            <button type="button" className="button secondary" onClick={onClose}>
              Cancel
            </button>
            <button type="submit" className="button">
              Set password
            </button>
          </div>
        </form>
      )}
    </Modal>
  );
}
