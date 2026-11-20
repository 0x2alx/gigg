// Import JSON / Copy as JSON: one format for every object type, shared by both. An object is the body its
// create call accepts, plus "kind". This module holds the kind -> fields mapping, the export (Copy) and
// the parse (Import: kind check, unknown fields dropped with a note, references resolved). It only
// shapes data: the create calls (and so every validation and permission check) stay the normal ones.
import type { Gig, Recurrence, Schedule, Template } from "./api";
import type { Errand, ErrandTemplate } from "./errands";

export type Kind = "schedule" | "gig_template" | "errand_template" | "gig" | "errand" | "recurring_gig";

export const KIND_LABEL: Record<Kind, string> = {
  schedule: "Schedule",
  gig_template: "Gig template",
  errand_template: "Errand template",
  gig: "Gig",
  errand: "Errand",
  recurring_gig: "Recurring gig",
};

export const MAX_OBJECTS = 100;
export const MAX_BYTES = 1024 * 1024;

const RUN = ["model", "effort", "always_run", "run_timeout_minutes", "auto_archive_minutes", "allowed_tools"];
const RULE = ["freq", "interval", "weekdays", "month_day", "tz", "window_start", "window_minutes", "lead_minutes"];
const ERRAND_CONTENT = ["name", "description_md", "command", "timeout_s", "output", "unsandboxed"];

// FIELDS: what each kind's create call accepts (the kind itself aside). Anything else is dropped. Places
// (workspace, project), ids and the like are the importer's to choose, so they are not in here;
// template_id/schedule_id (recurring gigs) are references: see REFS.
export const FIELDS: Record<Kind, string[]> = {
  schedule: ["name", ...RULE],
  gig_template: ["name", "title", "description_md", "priority", "color", "labels", "assignees", "followers", "checklist", "subgigs", "custom", ...RUN],
  errand_template: ERRAND_CONTENT,
  gig: ["title", "description_md", "priority", "category", "labels", "assignees", "followers", "color", "start_at", "due_at", "not_after", "checklist", "subgigs", "custom", ...RUN],
  errand: [...ERRAND_CONTENT, "rule", "worker", "on_failure_gig", "paused"],
  recurring_gig: [
    "template_id", "schedule_id", "title", "description_md", "priority", "color", "labels", "assignees", "followers", "checklist", "subgigs", "custom", ...RUN,
    ...RULE, "starts_on", "ends_on", "max_count", "overlap", "paused",
  ],
};

// Fields that name users (usernames in the JSON, ids in the create body). Numbers (an id) are accepted too.
const USER_LISTS = ["assignees", "followers"];
// Name fields next to a reference id (recurring gigs): resolved within reach, else the id is used.
const REFS: Record<string, "template" | "schedule"> = { template: "template", schedule: "schedule" };

export type Obj = Record<string, unknown>;
const isObj = (v: unknown): v is Obj => typeof v === "object" && v !== null && !Array.isArray(v);

// ---- Copy

export interface ExportCtx {
  userName?: (id: number) => string | undefined; // username of a user id
}

const clean = (o: Obj): Obj => {
  const out: Obj = {};
  for (const [k, v] of Object.entries(o)) {
    if (v === undefined || v === null || v === "" || v === false || v === 0) continue;
    if (Array.isArray(v) && v.length === 0) continue;
    if (isObj(v) && Object.keys(v).length === 0) continue;
    out[k] = v;
  }
  return out;
};

const pick = (src: Obj, keys: string[]): Obj => {
  const out: Obj = {};
  for (const k of keys) if (k in src) out[k] = src[k];
  return out;
};

const names = (ids: unknown, ctx: ExportCtx): unknown => (Array.isArray(ids) ? ids.map((id) => (typeof id === "number" ? (ctx.userName?.(id) ?? id) : id)) : ids);

// exportObject: the JSON object for Copy: the kind plus what create accepts, no server-only fields.
// Zero values are left out (create's defaults are the same), so the copy stays short.
export function exportObject(kind: Kind, src: Schedule | Template | ErrandTemplate | Gig | Errand | Recurrence | Obj, ctx: ExportCtx = {}): Obj {
  const s = { ...(src as unknown as Obj) };
  if (kind === "gig") {
    s.category = s.status_category;
    s.labels = Array.isArray(s.labels) ? (s.labels as ({ name: string } | string)[]).map((l) => (typeof l === "string" ? l : l.name)) : s.labels;
  }
  if (kind === "errand") {
    const r = s.rule as Obj | undefined;
    s.worker = s.worker_username;
    if (r) s.rule = clean(pick(r, RULE));
  }
  if (kind === "recurring_gig") {
    s.template = s.template_name;
    s.schedule = s.schedule_name;
    for (const k of RULE) delete s[k]; // the schedule holds the timing
    for (const k of ["title", "description_md", "priority", "color", "labels", "checklist", "subgigs", "custom", ...RUN]) delete s[k]; // the template holds the content
    if (!s.assignees_own) delete s.assignees;
    delete s.followers;
  }
  for (const k of USER_LISTS) if (k in s) s[k] = names(s[k], ctx);
  const out = clean(pick(s, [...FIELDS[kind], ...(kind === "recurring_gig" ? Object.keys(REFS) : [])]));
  return { kind, ...out };
}

// ---- Import

export interface ImportCtx {
  // a username or a user id -> the user id, or undefined when nobody in reach has it
  userId: (v: string | number) => number | undefined;
  // a reference by name (or by id when no name is given), within the target's reach; undefined: unresolved
  ref: (what: "template" | "schedule", name: string | undefined, id: number | undefined) => number | undefined;
}

export interface Parsed {
  index: number; // position in the pasted list
  kind: Kind | null;
  label: string; // name or title for the preview
  body: Obj; // what create takes, references not yet resolved
  dropped: string[]; // keys that create doesn't take
  problems: string[]; // why this row will be skipped
}

export interface ParseResult {
  rows: Parsed[];
  error?: string; // the whole input is unusable
}

// parseImport: text -> one row per object. `allowed`: the kinds this page takes; `fallback`: the kind a
// row without "kind" is taken for (pages that import one kind only). A kind not in `allowed` is refused.
export function parseImport(text: string, allowed: Kind[], fallback?: Kind): ParseResult {
  if (!text.trim()) return { rows: [], error: "Paste JSON or choose a file." };
  if (new Blob([text]).size > MAX_BYTES) return { rows: [], error: "That is over 1 MB: split it in smaller files." };
  let data: unknown;
  try {
    data = JSON.parse(text);
  } catch (e) {
    return { rows: [], error: `That isn't valid JSON: ${e instanceof Error ? e.message : "parse error"}.` };
  }
  const list = Array.isArray(data) ? data : [data];
  if (list.length === 0) return { rows: [], error: "The list is empty." };
  if (list.length > MAX_OBJECTS) return { rows: [], error: `At most ${MAX_OBJECTS} objects at a time; there are ${list.length}.` };
  return { rows: list.map((v, i) => parseOne(v, i, allowed, fallback)) };
}

const isKind = (v: unknown): v is Kind => typeof v === "string" && v in FIELDS;

function parseOne(v: unknown, index: number, allowed: Kind[], fallback?: Kind): Parsed {
  const row: Parsed = { index, kind: null, label: "", body: {}, dropped: [], problems: [] };
  if (!isObj(v)) {
    row.problems.push("Not an object.");
    return row;
  }
  const k = v.kind === undefined ? fallback : v.kind;
  if (k === undefined) row.problems.push(`No "kind". Expected one of: ${allowed.join(", ")}.`);
  else if (!isKind(k)) row.problems.push(`Unknown kind ${JSON.stringify(k)}. Expected one of: ${allowed.join(", ")}.`);
  else if (!allowed.includes(k)) row.problems.push(`A ${KIND_LABEL[k].toLowerCase()} doesn't belong on this page (it takes ${allowed.map((a) => KIND_LABEL[a].toLowerCase()).join(", ")}).`);
  else row.kind = k;
  const label = v.name ?? v.title;
  row.label = typeof label === "string" ? label : "";
  if (!row.kind) return row;
  const fields = new Set([...FIELDS[row.kind], ...(row.kind === "recurring_gig" ? Object.keys(REFS) : [])]);
  for (const [key, val] of Object.entries(v)) {
    if (key === "kind") continue;
    if (fields.has(key)) row.body[key] = val;
    else row.dropped.push(key);
  }
  if (row.kind === "recurring_gig" && !row.label) row.label = String(row.body.template ?? row.body.title ?? "");
  row.problems.push(...missing(row.kind, row.body));
  return row;
}

// missing: the plain gaps the create call would refuse, said before it is tried (the call stays the judge).
function missing(kind: Kind, b: Obj): string[] {
  const has = (k: string) => typeof b[k] === "string" && (b[k] as string).trim() !== "";
  const p: string[] = [];
  switch (kind) {
    case "schedule":
      if (!has("freq")) p.push('"freq" is missing.');
      else if (!["daily", "weekly", "monthly"].includes(b.freq as string)) p.push(`Schedules for gigs take daily, weekly or monthly, not ${JSON.stringify(b.freq)} (minutes and hours are for errands).`);
      break;
    case "gig_template":
      if (!has("name")) p.push('"name" is missing.');
      if (!has("title")) p.push('"title" is missing.');
      break;
    case "errand_template":
      if (!has("name")) p.push('"name" is missing.');
      if (!has("command")) p.push('"command" is missing.');
      break;
    case "gig":
      if (!has("title")) p.push('"title" is missing.');
      break;
    case "errand":
      if (!has("name")) p.push('"name" is missing.');
      if (!has("command")) p.push('"command" is missing.');
      if (!isObj(b.rule)) p.push('"rule" (the timing) is missing.');
      break;
    case "recurring_gig":
      if (!has("template") && b.template_id === undefined && !has("title")) p.push('Give a "template" (name or template_id) or a "title".');
      if (!has("schedule") && b.schedule_id === undefined && !has("freq")) p.push('Give a "schedule" (name or schedule_id) or a "freq".');
      break;
  }
  return p;
}

// toBody: the create body for a parsed row: user names and references resolved. problems lists what
// doesn't resolve (the row is then skipped).
export function toBody(row: Parsed, ctx: ImportCtx): { body: Obj; problems: string[] } {
  const body: Obj = { ...row.body };
  const problems: string[] = [];
  for (const k of USER_LISTS) {
    const v = body[k];
    if (v === undefined) continue;
    if (!Array.isArray(v)) {
      problems.push(`"${k}" must be a list of usernames.`);
      continue;
    }
    const ids = v.map((u) => (typeof u === "string" || typeof u === "number" ? ctx.userId(u) : undefined));
    const bad = v.filter((_, i) => ids[i] === undefined);
    if (bad.length) problems.push(`Unknown ${k === "assignees" ? "assignee" : "follower"}${bad.length > 1 ? "s" : ""}: ${bad.map(String).join(", ")}.`);
    body[k] = ids.filter((x): x is number => x !== undefined);
  }
  if (row.kind === "recurring_gig") {
    for (const what of ["template", "schedule"] as const) {
      const name = typeof body[what] === "string" && (body[what] as string).trim() ? (body[what] as string).trim() : undefined;
      const idKey = `${what}_id`;
      const id = typeof body[idKey] === "number" ? (body[idKey] as number) : undefined;
      delete body[what];
      if (name === undefined && id === undefined) continue;
      const found = ctx.ref(what, name, id);
      if (found === undefined) problems.push(name !== undefined ? `No ${what} named “${name}” in reach.` : `No ${what} with id ${id} in reach.`);
      else body[idKey] = found;
    }
  }
  return { body, problems };
}

// describe: one line for the preview: what the row is.
export function describeRow(r: Parsed): string {
  return r.label || "(no name)";
}
