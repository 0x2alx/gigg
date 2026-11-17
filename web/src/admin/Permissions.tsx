// Access rules for a user or a group: one level at a time (all workspaces, or one workspace), a
// preset and per-permission overrides (Allow / Deny / no override). For users, the effective result
// per permission and where it comes from. Browser sessions only (keys get 403), which this page is.
import { useEffect, useState } from "react";
import { useApp } from "../App";
import { accessApi, RULE_ORDER, ruleAt, ruleSummary, sections, sourceText, withOverride, withPreset, type AccessRule, type Decision, type Group, type PermissionsInfo, type RuleBody, type Tri, type UserAccess } from "../access";
import { CATEGORY_COLOR, readableOn } from "../theme";
import { Avatar, Modal, Pill } from "../ui";
import { errMsg, type AdminUser } from "./adminApi";
import { ErrorLine } from "./common";

export function TriState({ value, onChange, inheritWord = "Inherit", label, disabled }: {
  value: Tri; onChange: (v: Tri) => void; inheritWord?: string; label: string; disabled?: boolean;
}) {
  const opts: { v: Tri; text: string }[] = [
    { v: true, text: "Allow" },
    { v: false, text: "Deny" },
    { v: null, text: inheritWord },
  ];
  return (
    <div className="admin-seg tri" role="radiogroup" aria-label={label}>
      {opts.map((o) => (
        <button
          key={o.text}
          type="button"
          role="radio"
          aria-checked={value === o.v}
          disabled={disabled}
          className={"admin-seg-btn" + (value === o.v ? " on" + (o.v === true ? " tri-allow" : o.v === false ? " tri-deny" : "") : "")}
          style={value === o.v && o.v === true ? { background: CATEGORY_COLOR.done, color: readableOn(CATEGORY_COLOR.done!) } : undefined}
          onClick={() => value !== o.v && onChange(o.v)}
        >
          {o.text}
        </button>
      ))}
    </div>
  );
}

// these only take effect together with Edit gigs (the server checks that first)
export const NEEDS_EDIT = new Set(["create_gigs", "delete_gigs", "archive_gigs", "relations", "checklists", "templates", "schedules"]);

export function Effective({ allowed, blocked }: { allowed: boolean; blocked?: string }) {
  if (allowed && blocked)
    return (
      <span className="pill pill-muted" title={`Allowed, but it only works with ${blocked}, which is off`}>
        needs {blocked}
      </span>
    );
  return allowed ? (
    <Pill color={CATEGORY_COLOR.done!} title="Allowed">
      ✓ allowed
    </Pill>
  ) : (
    <span className="pill pill-danger" title="Denied">
      ✕ off
    </span>
  );
}

// LevelPicker: "All workspaces" and each workspace, with the rule summary of each.
function LevelPicker({ rules, info, level, onPick }: { rules: AccessRule[]; info: PermissionsInfo; level: number | null; onPick: (ws: number | null) => void }) {
  const { tree } = useApp();
  const levels: { ws: number | null; name: string }[] = [{ ws: null, name: "All workspaces" }, ...tree.map((w) => ({ ws: w.id, name: w.name }))];
  return (
    <ul className="acc-levels" role="tablist" aria-label="Where the rule applies">
      {levels.map((l) => {
        const r = ruleAt(rules, l.ws);
        return (
          <li key={l.ws ?? 0}>
            <button type="button" role="tab" aria-selected={level === l.ws} className={"acc-level" + (level === l.ws ? " on" : "") + (r ? " has" : "")} onClick={() => onPick(l.ws)}>
              <span className="acc-level-name">{l.ws === null ? "★ " : ""}{l.name}</span>
              <span className="muted small acc-level-sum">{ruleSummary(r, info.permissions)}</span>
            </button>
          </li>
        );
      })}
    </ul>
  );
}

// RuleEditor: one level's preset and overrides; with decisions (users), the effective answer too.
function RuleEditor({ info, rule, ws, decisions, busy, onSet, who }: {
  info: PermissionsInfo; rule: AccessRule | undefined; ws: number | null; decisions?: Decision[]; busy: boolean; onSet: (b: RuleBody) => void; who: string;
}) {
  const preset = rule?.preset ?? "";
  const inPreset = new Set(info.presets[preset] ?? []);
  const dec = new Map((decisions ?? []).map((d) => [d.name, d]));
  return (
    <div className="acc-editor">
      <label className="acc-preset">
        Preset
        <select value={preset} disabled={busy} onChange={(e) => onSet(withPreset(rule, ws, e.target.value))}>
          <option value="">None (only the overrides below)</option>
          {info.roles.map((r) => (
            <option key={r} value={r}>
              {r[0]!.toUpperCase() + r.slice(1)}
            </option>
          ))}
        </select>
      </label>
      {rule && (
        <button type="button" className="button secondary small" disabled={busy} onClick={() => onSet({ workspace_id: ws, preset: "", overrides: {} })}>
          Remove this rule
        </button>
      )}
      {sections(info.permissions).map((s) => (
        <section key={s.section} className="acc-section">
          <h3 className="acc-section-head">{s.section}</h3>
          <ul className="admin-list perm-list">
            {s.items.map((p) => {
              const ov = rule?.overrides[p.name];
              const d = dec.get(p.name);
              return (
                <li key={p.name} className="admin-list-row perm-row">
                  <span className="perm-name" title={p.description}>
                    {p.label}
                    <span className="muted small perm-desc">{p.description}</span>
                  </span>
                  {d && <Effective allowed={d.allowed} blocked={NEEDS_EDIT.has(p.name) && dec.get("edit_gigs")?.allowed === false ? "Edit gigs" : undefined} />}
                  {d && <span className={"pill perm-src perm-src-" + d.source}>{sourceText(d)}</span>}
                  <span className="grow" />
                  <TriState
                    label={`${p.label} for ${who}`}
                    inheritWord={inPreset.has(p.name) ? "Preset ✓" : "Inherit"}
                    value={ov === undefined ? null : ov}
                    disabled={busy}
                    onChange={(v) => onSet(withOverride(rule, ws, p.name, v))}
                  />
                </li>
              );
            })}
          </ul>
        </section>
      ))}
    </div>
  );
}

function usePermissionsInfo(setError: (e: string) => void) {
  const [info, setInfo] = useState<PermissionsInfo | null>(null);
  useEffect(() => {
    accessApi
      .info()
      .then(setInfo)
      .catch((e: unknown) => setError(errMsg(e)));
  }, [setError]);
  return info;
}

export function PermissionsModal({ user, onClose }: { user: AdminUser; onClose: () => void }) {
  const [error, setError] = useState("");
  const info = usePermissionsInfo(setError);
  const [acc, setAcc] = useState<UserAccess | null>(null);
  const [busy, setBusy] = useState(false);
  const [level, setLevel] = useState<number | null>(null);

  useEffect(() => {
    accessApi
      .user(user.id)
      .then(setAcc)
      .catch((e: unknown) => setError(errMsg(e)));
  }, [user.id]);

  const set = (b: RuleBody) => {
    setBusy(true);
    setError("");
    accessApi
      .setUserRule(user.id, b)
      .then(setAcc)
      .catch((e: unknown) => setError(errMsg(e)))
      .finally(() => setBusy(false));
  };
  const row = acc?.grid.find((r) => r.workspace_id === (level ?? 0));

  return (
    <Modal title={`Permissions · @${user.username}`} onClose={onClose} xl>
      <div className="perm">
        <div className="row perm-who">
          <Avatar user={user} size="m" />
          <span className="admin-user-text">
            <span>{user.display_name || user.username}</span>
            <span className="muted small">{user.kind === "agent" ? "agent" : "person"}</span>
          </span>
          <span className="chips perm-groups" aria-label="Groups">
            {user.is_admin && <span className="pill pill-accent">admin</span>}
            {(acc?.groups ?? []).map((g) => (
              <span key={g} className="pill pill-ws">
                ⛭ {g}
              </span>
            ))}
            {acc && acc.groups.length === 0 && <span className="muted small">no groups</span>}
          </span>
        </div>
        <p className="muted small perm-note">
          {user.is_admin ? "Admins can do everything; these rules apply if admin is revoked. " : ""}
          {RULE_ORDER}
        </p>
        <ErrorLine error={error} />
        {(!acc || !info) && !error && <p className="muted">Loading…</p>}
        {acc && info && (
          <div className="acc">
            <LevelPicker rules={acc.rules} info={info} level={level} onPick={setLevel} />
            <div className="acc-main">
              <p className="small acc-level-note">
                {level === null
                  ? "Applies in every workspace, including new ones, unless the user's rule for a workspace says otherwise."
                  : "Applies in this workspace only; beats the all-workspaces rule and the groups."}
                {row && row.role && <> Effective here: <strong>{row.role}</strong>.</>}
              </p>
              <RuleEditor info={info} rule={ruleAt(acc.rules, level)} ws={level} decisions={row?.permissions} busy={busy} onSet={set} who={user.username} />
            </div>
          </div>
        )}
      </div>
    </Modal>
  );
}

export function GroupAccessModal({ group, onClose, onChanged }: { group: Group; onClose: () => void; onChanged: (g: Group) => void }) {
  const [error, setError] = useState("");
  const info = usePermissionsInfo(setError);
  const [g, setG] = useState(group);
  const [busy, setBusy] = useState(false);
  const [level, setLevel] = useState<number | null>(null);
  const set = (b: RuleBody) => {
    setBusy(true);
    setError("");
    accessApi
      .setGroupRule(g.id, b)
      .then((ng) => {
        setG(ng);
        onChanged(ng);
      })
      .catch((e: unknown) => setError(errMsg(e)))
      .finally(() => setBusy(false));
  };
  return (
    <Modal title={`Access · group ${g.name}`} onClose={onClose} xl>
      <div className="perm">
        <p className="muted small perm-note">Every member gets these rules, unless their own rules say otherwise. {RULE_ORDER}</p>
        <ErrorLine error={error} />
        {!info && !error && <p className="muted">Loading…</p>}
        {info && (
          <div className="acc">
            <LevelPicker rules={g.rules} info={info} level={level} onPick={setLevel} />
            <div className="acc-main">
              <RuleEditor info={info} rule={ruleAt(g.rules, level)} ws={level} busy={busy} onSet={set} who={`group ${g.name}`} />
            </div>
          </div>
        )}
      </div>
    </Modal>
  );
}
