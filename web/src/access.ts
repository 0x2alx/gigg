// Access rules: a user or a group gets a preset and/or allow/deny overrides, in one workspace or
// in all of them. The API types, the admin calls and the pure helpers the admin screens use.
import { call } from "./http";

export interface Permission {
  name: string;
  label: string;
  description: string;
  section: string;
}

export interface PermissionsInfo {
  permissions: Permission[];
  presets: Record<string, string[]>;
  roles: string[];
}

export interface AccessRule {
  id: number;
  user_id?: number;
  group_id?: number;
  group_name?: string;
  workspace_id: number | null; // null = all workspaces (future ones too)
  preset: string; // "" = none
  overrides: Record<string, boolean>;
}

export type Source = "admin" | "user" | "user_all" | "group" | "group_all" | "needs_view" | "default";

export interface Decision {
  name: string;
  allowed: boolean;
  source: Source;
  group?: string;
}

export interface AccessRow {
  workspace_id: number; // 0 = all workspaces
  role: string;
  permissions: Decision[];
}

export interface UserAccess {
  user_id: number;
  groups: string[];
  rules: AccessRule[];
  grid: AccessRow[];
}

export interface Group {
  id: number;
  name: string;
  description: string;
  members: number[];
  rules: AccessRule[];
  version: number;
}

export interface RuleBody {
  workspace_id: number | null;
  preset?: string;
  overrides?: Record<string, boolean>;
}

// Tri: true = allow, false = deny, null = no override (the preset, else inherit).
export type Tri = boolean | null;

export const accessApi = {
  info: () => call<PermissionsInfo>("GET", "/permissions"),
  user: (id: number) => call<UserAccess>("GET", `/users/${id}/access`),
  setUserRule: (id: number, body: RuleBody) => call<UserAccess>("PUT", `/users/${id}/access`, body),
  setGroupRule: (id: number, body: RuleBody) => call<Group>("PUT", `/groups/${id}/access`, body),
  groups: () => call<{ items: Group[] }>("GET", "/groups"),
  createGroup: (body: { name: string; description?: string; members?: number[] }) => call<Group>("POST", "/groups", body),
  patchGroup: (g: Group, body: { name?: string; description?: string; members?: number[] }) => call<Group>("PATCH", `/groups/${g.id}`, body, g.version),
  deleteGroup: (id: number) => call<void>("DELETE", `/groups/${id}`),
};

// The order rules are read in, for the help text.
export const RULE_ORDER =
  "Most specific first: the user's rule for the workspace, then their rule for all workspaces, then their groups' rules for the workspace, then for all workspaces; otherwise off. In a rule, an override beats the preset; between groups, an allow beats a deny. Everything needs “See the workspace”.";

// ruleAt: the rule for one level (ws null = all workspaces).
export function ruleAt(rules: AccessRule[], ws: number | null): AccessRule | undefined {
  return rules.find((r) => (r.workspace_id ?? null) === ws);
}

// body: the request that sets one override (or clears it) in a rule, keeping the rest.
export function withOverride(rule: AccessRule | undefined, ws: number | null, perm: string, v: Tri): RuleBody {
  const overrides = { ...(rule?.overrides ?? {}) };
  if (v === null) delete overrides[perm];
  else overrides[perm] = v;
  return { workspace_id: ws, preset: rule?.preset ?? "", overrides };
}

export function withPreset(rule: AccessRule | undefined, ws: number | null, preset: string): RuleBody {
  return { workspace_id: ws, preset, overrides: { ...(rule?.overrides ?? {}) } };
}

// ruleSummary: a rule in a few words ("Member, +Recurring gigs, -Delete gigs").
export function ruleSummary(rule: AccessRule | undefined, perms: Permission[]): string {
  if (!rule) return "no rule";
  const label = (n: string) => perms.find((p) => p.name === n)?.label ?? n;
  const order = (n: string) => {
    const i = perms.findIndex((p) => p.name === n);
    return i < 0 ? perms.length : i;
  };
  const parts: string[] = [];
  if (rule.preset) parts.push(rule.preset[0]!.toUpperCase() + rule.preset.slice(1));
  for (const n of Object.keys(rule.overrides).sort((a, b) => order(a) - order(b))) parts.push((rule.overrides[n] ? "+" : "−") + label(n));
  return parts.join(", ") || "no rule";
}

// sourceText: where an answer comes from, in a few words.
export function sourceText(d: Pick<Decision, "source" | "group">): string {
  switch (d.source) {
    case "admin":
      return "admin: everything";
    case "user":
      return "own rule, this workspace";
    case "user_all":
      return "own rule, all workspaces";
    case "group":
      return d.group ? `group ${d.group}, this workspace` : "a group";
    case "group_all":
      return d.group ? `group ${d.group}, all workspaces` : "a group";
    case "needs_view":
      return "can't see the workspace";
    default:
      return "no rule: off";
  }
}

export function triLabel(v: Tri, inheritWord = "Inherit"): string {
  return v === true ? "Allow" : v === false ? "Deny" : inheritWord;
}

// sections: permissions grouped by section, in catalogue order.
export function sections(perms: Permission[]): { section: string; items: Permission[] }[] {
  const out: { section: string; items: Permission[] }[] = [];
  for (const p of perms) {
    const last = out[out.length - 1];
    if (last && last.section === p.section) last.items.push(p);
    else out.push({ section: p.section, items: [p] });
  }
  return out;
}
