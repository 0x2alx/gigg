// Access rules for a user or a group: one level at a time (all workspaces, one workspace, or one
// project of a workspace: the gig permissions only), a preset and per-permission overrides (Allow / Deny / no override). For users, the effective result
// per permission and where it comes from. Browser sessions only (keys get 403), which this page is.
import { useEffect, useState, type ReactNode } from "react";
import { useApp } from "../App";
import { accessApi, PROJECT_PRESETS, RULE_ORDER, ruleAt, ruleSummary, sections, sourceText, withOverride, withPreset, type AccessRule, type Decision, type Group, type PermissionsInfo, type RuleBody, type Tri, type UserAccess } from "../access";
import { CATEGORY_COLOR, readableOn } from "../theme";
import { Avatar, Modal, Pill } from "../ui";
import { errMsg, type AdminUser } from "./adminApi";
import { ErrorLine } from "./common";

// TriState: Allow / Deny / no override. With fromPreset, "no override" reads "Preset ✓" (the preset
// already allows it); screen readers hear "Preset, allows it" rather than a bare check mark.
export function TriState({ value, onChange, fromPreset, label, disabled }: {
  value: Tri; onChange: (v: Tri) => void; fromPreset?: boolean; label: string; disabled?: boolean;
}) {
  const opts: { v: Tri; key: string; text: ReactNode; name?: string }[] = [
    { v: true, key: "allow", text: "Allow" },
    { v: false, key: "deny", text: "Deny" },
    fromPreset
      ? { v: null, key: "inherit", text: <>Preset <span aria-hidden="true">✓</span></>, name: "Preset, allows it" }
      : { v: null, key: "inherit", text: "Inherit" },
  ];
  return (
    <div className="admin-seg tri" role="radiogroup" aria-label={label}>
      {opts.map((o) => (
        <button
          key={o.key}
          type="button"
          role="radio"
          aria-checked={value === o.v}
          aria-label={o.name}
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
export const NEEDS_EDIT = new Set(["create_gigs", "delete_gigs", "archive_gigs", "templates", "recurring_gigs"]);

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

// Level: where a rule applies: all workspaces (ws null), one workspace, or one project of it.
export interface Level {
  ws: number | null;
  project: number | null;
}
const ALL: Level = { ws: null, project: null };

// LevelPicker: "All workspaces", each workspace, and under a workspace its projects (shown for the
// workspace being looked at, and wherever a project has a rule), with the rule summary of each.
function LevelPicker({ rules, info, level, onPick }: { rules: AccessRule[]; info: PermissionsInfo; level: Level; onPick: (l: Level) => void }) {
  const { tree } = useApp();
  const levels: (Level & { name: string })[] = [{ ...ALL, name: "All workspaces" }];
  for (const w of tree) {
    levels.push({ ws: w.id, project: null, name: w.name });
    const open = level.ws === w.id;
    for (const p of w.projects) if (open || ruleAt(rules, w.id, p.id)) levels.push({ ws: w.id, project: p.id, name: p.name });
  }
  return (
    <ul className="acc-levels" role="tablist" aria-label="Where the rule applies">
      {levels.map((l) => {
        const r = ruleAt(rules, l.ws, l.project);
        const on = level.ws === l.ws && level.project === l.project;
        return (
          <li key={`${l.ws ?? 0}/${l.project ?? 0}`}>
            <button type="button" role="tab" aria-selected={on} className={"acc-level" + (l.project ? " acc-level-project" : "") + (on ? " on" : "") + (r ? " has" : "")} onClick={() => onPick({ ws: l.ws, project: l.project })}>
              <span className="acc-level-name">{l.ws === null ? "★ " : l.project ? "› " : ""}{l.name}</span>
              <span className="muted small acc-level-sum">{ruleSummary(r, info.permissions)}</span>
            </button>
          </li>
        );
      })}
    </ul>
  );
}

// RuleEditor: one level's preset and overrides; with decisions (users), the effective answer too.
function RuleEditor({ info, rule, level, decisions, busy, onSet, who }: {
  info: PermissionsInfo; rule: AccessRule | undefined; level: Level; decisions?: Decision[]; busy: boolean; onSet: (b: RuleBody) => void; who: string;
}) {
  const { ws, project } = level;
  const preset = rule?.preset ?? "";
  const inPreset = new Set(info.presets[preset] ?? []);
  const dec = new Map((decisions ?? []).map((d) => [d.name, d]));
  // a project's rule holds the gig permissions only (an older server marks none: show them all)
  const marked = info.permissions.some((p) => p.project);
  // a workspace's or a project's rule never holds the all-workspaces ones (the shared library)
  const levelPerms = ws === null ? info.permissions : info.permissions.filter((p) => !p.all_workspaces);
  const perms = project && marked ? levelPerms.filter((p) => p.project) : levelPerms;
  const roles = project ? info.roles.filter((r) => PROJECT_PRESETS.includes(r)) : info.roles;
  return (
    <div className="acc-editor">
      <label className="acc-preset">
        Preset
        <select value={preset} disabled={busy} onChange={(e) => onSet(withPreset(rule, ws, e.target.value, project))}>
          <option value="">None (only the overrides below)</option>
          {roles.map((r) => (
            <option key={r} value={r}>
              {r[0]!.toUpperCase() + r.slice(1)}
            </option>
          ))}
        </select>
      </label>
      {rule && (
        <button type="button" className="button secondary small" disabled={busy} onClick={() => onSet({ workspace_id: ws, ...(project ? { project_id: project } : {}), preset: "", overrides: {} })}>
          Remove this rule
        </button>
      )}
      {sections(perms).map((s) => (
        <section key={s.section} className="acc-section">
          <h3 className="acc-section-head">{s.section}</h3>
          <ul className="admin-list perm-list">
            {s.items.map((p) => {
              const ov = rule?.overrides[p.name];
              const d = dec.get(p.name);
              return (
                <li key={p.name} className="admin-list-row perm-row">
                  <span className="perm-name" title={p.description}>
                    {project && p.name === "view" ? "See the project" : p.label}
                    <span className="muted small perm-desc">{project && p.name === "view" ? "See this project: its gigs, comments, runs and errands (and its workspace's name). Every other permission here needs it." : p.description}</span>
                  </span>
                  {d && <Effective allowed={d.allowed} blocked={NEEDS_EDIT.has(p.name) && dec.get("edit_gigs")?.allowed === false ? "Edit gigs" : undefined} />}
                  {d && <span className={"pill perm-src perm-src-" + d.source}>{sourceText(d, !!p.all_workspaces)}</span>}
                  <span className="grow" />
                  <TriState
                    label={`${p.label} for ${who}`}
                    fromPreset={inPreset.has(p.name)}
                    value={ov === undefined ? null : ov}
                    disabled={busy}
                    onChange={(v) => onSet(withOverride(rule, ws, p.name, v, project))}
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
  const [level, setLevel] = useState<Level>(ALL);

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
  const row = acc?.grid.find((r) => r.workspace_id === (level.ws ?? 0) && (r.project_id ?? null) === level.project);

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
                {level.ws === null
                  ? "Applies in every workspace, including new ones, unless the user's rule for a workspace says otherwise."
                  : level.project
                    ? "Applies in this project only (its gigs and errands): beats every other rule. On its own it shows the user this project, and none of the rest of the workspace."
                    : "Applies in this workspace only; beats the all-workspaces rule and the groups."}
                {row && row.role && <> Effective here: <strong>{row.role}</strong>.</>}
              </p>
              <RuleEditor info={info} rule={ruleAt(acc.rules, level.ws, level.project)} level={level} decisions={row?.permissions} busy={busy} onSet={set} who={user.username} />
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
  const [level, setLevel] = useState<Level>(ALL);
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
              <RuleEditor info={info} rule={ruleAt(g.rules, level.ws, level.project)} level={level} busy={busy} onSet={set} who={`group ${g.name}`} />
            </div>
          </div>
        )}
      </div>
    </Modal>
  );
}
