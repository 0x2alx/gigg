// Groups: users that get access rules together.
import { useCallback, useEffect, useMemo, useState, type FormEvent } from "react";
import { useApp } from "../App";
import { accessApi, RULE_ORDER, ruleSummary, type Group, type Permission } from "../access";
import { Avatar, Modal } from "../ui";
import { adminApi, errMsg, type AdminUser } from "./adminApi";
import { AdminPage, ConfirmButton, Empty, ErrorLine } from "./common";
import { GroupAccessModal } from "./Permissions";

export function GroupsPage() {
  const { refresh, tree } = useApp();
  const wsName = (id: number) => tree.find((w) => w.id === id)?.name ?? `workspace ${id}`;
  const [groups, setGroups] = useState<Group[] | null>(null);
  const [perms, setPerms] = useState<Permission[]>([]);
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<Group | "new" | null>(null);
  const [rulesOf, setRulesOf] = useState<Group | null>(null);

  const load = useCallback(() => {
    accessApi
      .groups()
      .then((r) => {
        setGroups(r.items);
        setError("");
      })
      .catch((e: unknown) => setError(errMsg(e)));
  }, []);
  useEffect(() => {
    load();
    accessApi
      .info()
      .then((r) => setPerms(r.permissions))
      .catch(() => undefined);
    adminApi
      .users()
      .then((r) => setUsers(r.items))
      .catch(() => undefined);
  }, [load]);
  const byId = useMemo(() => new Map(users.map((u) => [u.id, u])), [users]);
  const changed = () => {
    load();
    refresh();
  };

  return (
    <AdminPage
      title="Groups"
      sub={RULE_ORDER}
      actions={
        <button type="button" className="button" onClick={() => setEditing("new")}>
          + New group
        </button>
      }
    >
      <ErrorLine error={error} />
      {groups === null && !error && <p className="muted">Loading…</p>}
      {groups && groups.length === 0 && <Empty>No groups yet. Create one to give several users the same access at once.</Empty>}
      <div className="admin-cards">
        {(groups ?? []).map((g) => {
          return (
            <section key={g.id} className="card admin-key grp" aria-label={`Group ${g.name}`}>
              <div className="admin-key-head">
                <span className="admin-key-name">⛭ {g.name}</span>
                <span className="pill pill-muted">
                  {g.members.length} member{g.members.length === 1 ? "" : "s"}
                </span>
                <span className="grow" />
                <button type="button" className="button secondary small" onClick={() => setRulesOf(g)}>
                  Access
                </button>
                <button type="button" className="button secondary small" onClick={() => setEditing(g)}>
                  Edit
                </button>
                <ConfirmButton
                  label="Delete"
                  title={`Delete group ${g.name}?`}
                  message="Its members keep their own rules; the group's rules stop applying."
                  confirmLabel="Delete"
                  onConfirm={() => accessApi.deleteGroup(g.id).then(changed)}
                />
              </div>
              {g.description && <p className="muted small grp-desc">{g.description}</p>}
              <div className="grp-members">
                {g.members.length === 0 && <span className="muted small">no members</span>}
                {g.members.map((id) => {
                  const u = byId.get(id);
                  return u ? <Avatar key={id} user={u} /> : <span key={id} className="pill pill-muted">#{id}</span>;
                })}
              </div>
              <ul className="grp-rules">
                {g.rules.length === 0 && <li className="muted small">no access rules</li>}
                {g.rules.map((r) => (
                  <li key={r.id} className="small">
                    <strong>{r.workspace_id === null ? "All workspaces" : wsName(r.workspace_id)}</strong>: {ruleSummary(r, perms)}
                  </li>
                ))}
              </ul>
            </section>
          );
        })}
      </div>
      {rulesOf && <GroupAccessModal group={rulesOf} onClose={() => setRulesOf(null)} onChanged={() => load()} />}
      {editing && (
        <EditGroup
          group={editing === "new" ? null : editing}
          users={users}
          onClose={() => setEditing(null)}
          onDone={() => {
            setEditing(null);
            changed();
          }}
        />
      )}
    </AdminPage>
  );
}

function EditGroup({ group, users, onClose, onDone }: {
  group: Group | null; users: AdminUser[]; onClose: () => void; onDone: () => void;
}) {
  const [name, setName] = useState(group?.name ?? "");
  const [desc, setDesc] = useState(group?.description ?? "");
  const [members, setMembers] = useState<Set<number>>(new Set(group?.members ?? []));
  const [query, setQuery] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const q = query.trim().toLowerCase();
  const shown = users
    .filter((u) => !q || u.username.includes(q) || u.display_name.toLowerCase().includes(q))
    .sort((a, b) => Number(b.disabled === false) - Number(a.disabled === false) || a.username.localeCompare(b.username));
  const toggle = (id: number) =>
    setMembers((s) => {
      const n = new Set(s);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    const memberList = [...members].sort((a, b) => a - b);
    let p: Promise<unknown>;
    if (!group) {
      p = accessApi.createGroup({
        name: name.trim(),
        ...(desc.trim() ? { description: desc.trim() } : {}),
        ...(memberList.length ? { members: memberList } : {}),
      });
    } else {
      const sameMembers = memberList.join(",") === [...group.members].sort((a, b) => a - b).join(",");
      p = accessApi.patchGroup(group, {
        ...(name.trim() !== group.name ? { name: name.trim() } : {}),
        ...(desc.trim() !== group.description ? { description: desc.trim() } : {}),
        ...(sameMembers ? {} : { members: memberList }),
      });
    }
    p.then(onDone)
      .catch((err: unknown) => setError(errMsg(err)))
      .finally(() => setBusy(false));
  };

  return (
    <Modal title={group ? `Edit group ${group.name}` : "New group"} onClose={onClose} wide>
      <form className="form-grid" onSubmit={submit}>
        <label>
          Name
          <input value={name} required maxLength={60} onChange={(e) => setName(e.target.value)} />
        </label>
        <label>
          Description
          <input value={desc} maxLength={500} placeholder="Optional" onChange={(e) => setDesc(e.target.value)} />
        </label>

        <fieldset className="span2 grp-fieldset">
          <legend>
            Members <span className="pill pill-muted">{members.size}</span>
          </legend>
          <input type="search" placeholder="Filter users…" value={query} onChange={(e) => setQuery(e.target.value)} aria-label="Filter users" />
          <ul className="grp-picker">
            {shown.map((u) => (
              <li key={u.id}>
                <label className={"admin-check grp-user" + (u.disabled ? " admin-dim" : "")}>
                  <input type="checkbox" checked={members.has(u.id)} onChange={() => toggle(u.id)} />
                  <Avatar user={u} />
                  <span className="grp-user-name">{u.display_name || u.username}</span>
                  {u.kind === "agent" ? <span className="pill pill-agent">agent</span> : <span className="pill pill-muted">human</span>}
                  {u.disabled && <span className="pill pill-danger">disabled</span>}
                </label>
              </li>
            ))}
          </ul>
        </fieldset>

        <p className="muted small span2">{group ? "Access rules: the Access button on the group's card." : "After creating the group, give it access with the Access button on its card."}</p>

        <div className="span2">
          <ErrorLine error={error} />
        </div>
        <div className="row end span2">
          <button type="button" className="button secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="button" disabled={busy || !name.trim()}>
            {group ? "Save" : "Create"}
          </button>
        </div>
      </form>
    </Modal>
  );
}
