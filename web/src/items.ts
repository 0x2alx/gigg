// Pure helpers for the gig-level features: @mentions, checklist ordering, custom field values.
import type { CustomField, Gig, Template, TemplateSubgig } from "./api";
import type { RunSettings } from "./runsettings";
import { fromDateInput, toDateInput } from "./fields";

// ---- @mentions

const nameChar = /[A-Za-z0-9_.-]/;

// activeMention: the "@query" being typed at the caret, or null. The "@" must start the text or
// follow whitespace/punctuation (so "me@host" is not a mention), and the query has no spaces.
export function activeMention(text: string, caret: number): { start: number; query: string } | null {
  let i = caret - 1;
  while (i >= 0 && nameChar.test(text[i]!)) i--;
  if (i < 0 || text[i] !== "@") return null;
  const before = i > 0 ? text[i - 1]! : "";
  if (before && !/[\s([{"'>,;]/.test(before)) return null;
  const query = text.slice(i + 1, caret);
  if (query.length > 40) return null;
  return { start: i, query };
}

// insertMention: replaces the "@query" at start..caret with "@username " and returns the new text
// and caret.
export function insertMention(text: string, start: number, caret: number, username: string): { text: string; caret: number } {
  const after = text.slice(caret);
  const ins = "@" + username + (after.startsWith(" ") ? "" : " ");
  return { text: text.slice(0, start) + ins + after, caret: start + ins.length };
}

// matchUsers: candidates whose username or display name starts with (then contains) the query.
export function matchUsers<U extends { username: string; display_name: string }>(users: U[], query: string, max = 6): U[] {
  const q = query.toLowerCase();
  const score = (u: U) => {
    const n = u.username.toLowerCase();
    const d = (u.display_name || "").toLowerCase();
    if (n.startsWith(q)) return 0;
    if (d.startsWith(q) || d.split(/\s+/).some((w) => w.startsWith(q))) return 1;
    if (n.includes(q) || d.includes(q)) return 2;
    return -1;
  };
  return users
    .map((u) => [u, score(u)] as const)
    .filter(([, s]) => s >= 0)
    .sort((a, b) => a[1] - b[1] || a[0].username.localeCompare(b[0].username))
    .slice(0, max)
    .map(([u]) => u);
}

// splitMentions: text split into plain runs and "@name" runs of known usernames (case-insensitive).
export function splitMentions(text: string, known: (name: string) => boolean): { text: string; user?: string }[] {
  const out: { text: string; user?: string }[] = [];
  const re = /(^|[^A-Za-z0-9_.@-])@([A-Za-z0-9_][A-Za-z0-9_.-]*)/g;
  let last = 0;
  for (let m = re.exec(text); m; m = re.exec(text)) {
    let name = m[2]!;
    while (name.endsWith(".") || name.endsWith("-")) name = name.slice(0, -1); // "@bob." ends a sentence
    if (!known(name)) continue;
    const at = m.index + m[1]!.length;
    if (at > last) out.push({ text: text.slice(last, at) });
    out.push({ text: "@" + name, user: name });
    last = at + 1 + name.length;
    re.lastIndex = last;
  }
  if (last < text.length) out.push({ text: text.slice(last) });
  return out;
}

// ---- checklist

// splitLines: pasted or typed text as checklist items (one per non-empty line, bullets dropped).
export function splitLines(text: string): string[] {
  return text
    .split(/\r?\n/)
    .map((l) => l.replace(/^\s*(?:[-*+•]|\d+[.)])?\s*(?:\[[ xX]?\]\s*)?/, "").trim())
    .filter(Boolean)
    .map((l) => l.slice(0, 500));
}

// moveAfter: the "after" item id that moves items[index] one step up (-1) or down (+1); 0 = top.
// null when it can't move.
export function moveAfter(ids: number[], index: number, dir: -1 | 1): number | null {
  const to = index + dir;
  if (index < 0 || index >= ids.length || to < 0 || to >= ids.length) return null;
  const rest = ids.filter((_, i) => i !== index);
  return to === 0 ? 0 : rest[to - 1]!;
}

// reorder: the local list after moving item index by dir (for an optimistic update).
export function reorder<T>(xs: T[], index: number, dir: -1 | 1): T[] {
  const to = index + dir;
  if (to < 0 || to >= xs.length) return xs;
  const out = xs.slice();
  const [x] = out.splice(index, 1);
  out.splice(to, 0, x!);
  return out;
}

export function progress(done: number, total: number): number {
  return total > 0 ? Math.round((done / total) * 100) : 0;
}

// progressWords: a card's "☑ 1/3" said in words, for screen readers ("1 of 3 checklist items done").
export function progressWords(done: number, total: number, noun: "checklist item" | "subgig"): string {
  return `${done} of ${total} ${noun}${total === 1 ? "" : "s"} done`;
}

// blockedWords: what the lock badge means, for its tooltip and the card's accessible name.
export function blockedWords(n: number): string {
  return `Blocked by ${n} open gig${n === 1 ? "" : "s"}`;
}

// ---- custom fields

// formatCustom: a stored value as short text, or "" when unset.
export function formatCustom(f: Pick<CustomField, "kind">, v: unknown): string {
  if (v === null || v === undefined || v === "") return "";
  switch (f.kind) {
    case "checkbox":
      return v === true ? "✓" : "";
    case "date":
      return typeof v === "number" ? new Date(v).toLocaleDateString(undefined, { dateStyle: "medium" }) : String(v);
    case "number":
      return typeof v === "number" ? v.toLocaleString(undefined, { maximumFractionDigits: 6 }) : String(v);
    case "url":
      try {
        const u = new URL(String(v));
        return u.host + (u.pathname === "/" ? "" : u.pathname);
      } catch {
        return String(v);
      }
    default:
      return String(v);
  }
}

// toInput / fromInput: a stored value <-> the input's string. fromInput gives null for "clear"
// and undefined for "not a valid value yet".
export function toInput(f: Pick<CustomField, "kind">, v: unknown): string {
  if (v === null || v === undefined) return "";
  if (f.kind === "date") return typeof v === "number" ? toDateInput(v) : String(v);
  return String(v);
}

export function fromInput(f: Pick<CustomField, "kind">, s: string): unknown {
  const t = s.trim();
  if (f.kind === "checkbox") return s === "true";
  if (!t) return null;
  if (f.kind === "number") {
    const n = Number(t);
    return Number.isFinite(n) ? n : undefined;
  }
  if (f.kind === "date") return fromDateInput(t) ?? undefined;
  if (f.kind === "url") return /^https?:\/\/[^\s/]+/i.test(t) ? t : undefined;
  return t;
}

// cardChips: at most `max` select/checkbox values to show on a card, in field order.
export function cardChips(gig: Pick<Gig, "custom">, fields: CustomField[], max = 2): { field: CustomField; text: string }[] {
  const out: { field: CustomField; text: string }[] = [];
  for (const f of fields) {
    if (out.length >= max) break;
    if (f.kind !== "select" && f.kind !== "checkbox") continue;
    const v = gig.custom?.[String(f.id)];
    if (f.kind === "checkbox" && v === true) out.push({ field: f, text: `✓ ${f.name}` });
    else if (f.kind === "select" && typeof v === "string" && v) out.push({ field: f, text: v });
  }
  return out;
}

// cleanCustom: only the set values (no nulls/empties), for create/template bodies.
export function cleanCustom(v: Record<string, unknown>): Record<string, unknown> | undefined {
  const out: Record<string, unknown> = {};
  for (const [k, x] of Object.entries(v)) if (x !== null && x !== undefined && x !== "" && x !== false) out[k] = x;
  return Object.keys(out).length ? out : undefined;
}

// normSubs: subgigs as the server stores them (blocks_parent always present), for comparing.
export function normSubs(xs: TemplateSubgig[]): TemplateSubgig[] {
  return xs.map((s) => ({ title: s.title.trim(), blocks_parent: !!s.blocks_parent }));
}

// sameAsTemplate: the form still holds exactly the template's content.
export function sameAsTemplate(
  t: Template,
  f: { title: string; description: string; priority: number; gigLabels: string[]; assignees: number[]; followers?: number[]; color: string | null; run: RunSettings; checklist: string[]; subgigs: TemplateSubgig[]; custom: Record<string, unknown>; startCategory?: string },
): boolean {
  const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);
  const sorted = (xs: number[]) => [...xs].sort((a, b) => a - b);
  return (
    t.title === f.title &&
    t.description_md === f.description &&
    t.priority === f.priority &&
    same(t.labels ?? [], f.gigLabels) &&
    same(sorted(t.assignees ?? []), sorted(f.assignees)) &&
    (f.followers === undefined || same(sorted(t.followers ?? []), sorted(f.followers))) &&
    (t.color ?? null) === (f.color ?? null) &&
    (t.model ?? "") === f.run.model &&
    (t.effort ?? "") === f.run.effort &&
    !!t.always_run === f.run.always_run &&
    same(t.allowed_tools ?? [], f.run.allowed_tools) &&
    (t.run_timeout_minutes ?? null) === f.run.run_timeout_minutes &&
    (t.auto_archive_minutes ?? null) === f.run.auto_archive_minutes &&
    same(t.checklist ?? [], f.checklist) &&
    same(normSubs(t.subgigs ?? []), normSubs(f.subgigs)) &&
    same(cleanCustom(t.custom ?? {}) ?? {}, cleanCustom(f.custom) ?? {}) &&
    (f.startCategory === undefined || (t.start_category ?? "") === f.startCategory)
  );
}
