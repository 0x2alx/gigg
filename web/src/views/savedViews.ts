// Saved views ("saved filters" in the API): the calls, and the mapping between the table's filter
// state, the stored query (only the server's allowed keys) and the table URL's search string.
import type { Category } from "../api";
import type { WorkspaceTree } from "../App";
import { call } from "../http";
import { ALL_CATEGORIES } from "./viewsApi";

export interface SavedFilter {
  id: number;
  workspace_id: number | null;
  name: string;
  query: Record<string, string>;
}

export const savedViewsApi = {
  list: () => call<{ items: SavedFilter[] }>("GET", "/me/filters"),
  create: (body: { workspace_id?: number; name: string; query: Record<string, string> }) => call<SavedFilter>("POST", "/me/filters", body),
  update: (id: number, body: { workspace_id?: number; name: string; query: Record<string, string> }) => call<SavedFilter>("PUT", `/me/filters/${id}`, body),
  remove: (id: number) => call<void>("DELETE", `/me/filters/${id}`),
};

export type SortKey = "number" | "title" | "priority" | "due" | "updated" | "created";
export const SORT_KEYS: SortKey[] = ["number", "title", "priority", "due", "updated", "created"];

export interface TableFilters {
  q: string;
  category: Category | "";
  label: string;
  assignee: number;
  prioMin: number;
  sort: { key: SortKey; desc: boolean };
  includeDone: boolean; // client-side only: no server key, so it lives in the URL (done=1) but isn't saved
  extra: Record<string, string>; // allowed keys the table has no control for, passed through to the list call
}

export const DEFAULT_FILTERS: TableFilters = {
  q: "",
  category: "",
  label: "",
  assignee: 0,
  prioMin: 0,
  sort: { key: "updated", desc: true },
  includeDone: false,
  extra: {},
};

// keys passed straight to the task list (the list endpoint understands them; the table has no control)
export const PASSTHROUGH = ["status", "created_by", "due_before", "archived", "blocked"] as const;

const sortStr = (s: TableFilters["sort"]) => (s.desc ? "-" : "") + s.key;
const isDefaultSort = (s: TableFilters["sort"]) => s.key === DEFAULT_FILTERS.sort.key && s.desc === DEFAULT_FILTERS.sort.desc;

// toQuery: the stored query (allowed keys only; defaults omitted). project only when given.
export function toQuery(f: TableFilters, project?: number): Record<string, string> {
  const o: Record<string, string> = {};
  if (f.q.trim()) o.q = f.q.trim();
  if (f.category) o.category = f.category;
  if (f.label) o.label = f.label;
  if (f.assignee) o.assignee = String(f.assignee);
  if (f.prioMin) o.priority_min = String(f.prioMin);
  if (!isDefaultSort(f.sort)) o.sort = sortStr(f.sort);
  if (project) o.project = String(project);
  for (const k of PASSTHROUGH) if (f.extra[k]) o[k] = f.extra[k]!;
  return o;
}

// toSearch: the table URL's search string (no leading "?"): the query without project, plus done=1.
export function toSearch(f: TableFilters): string {
  const p = new URLSearchParams(toQuery(f));
  if (f.includeDone) p.set("done", "1");
  return p.toString();
}

// parse: filters from a stored query or URL params. "mine" becomes the signed-in user as assignee;
// unknown or malformed values are dropped.
export function parse(src: Record<string, string> | URLSearchParams, meId?: number): { filters: TableFilters; project?: number } {
  const get = (k: string): string => (src instanceof URLSearchParams ? (src.get(k) ?? "") : (src[k] ?? ""));
  const f: TableFilters = { ...DEFAULT_FILTERS, sort: { ...DEFAULT_FILTERS.sort }, extra: {} };
  f.q = get("q");
  const cat = get("category");
  if ((ALL_CATEGORIES as string[]).includes(cat)) f.category = cat as Category;
  f.label = get("label");
  const a = Number(get("assignee"));
  if (Number.isInteger(a) && a > 0) f.assignee = a;
  else if (meId && /^(1|true|yes)$/i.test(get("mine"))) f.assignee = meId;
  const pm = Number(get("priority_min"));
  if (Number.isInteger(pm) && pm >= 1 && pm <= 4) f.prioMin = pm;
  const s = /^(-?)([a-z]+)$/.exec(get("sort"));
  if (s && (SORT_KEYS as string[]).includes(s[2]!)) f.sort = { key: s[2] as SortKey, desc: s[1] === "-" };
  f.includeDone = get("done") === "1";
  for (const k of PASSTHROUGH) if (get(k)) f.extra[k] = get(k);
  const pid = Number(get("project"));
  return { filters: f, project: Number.isInteger(pid) && pid > 0 ? pid : undefined };
}

// viewHref: where a saved view opens — the table of its workspace (or project) with its filters.
export function viewHref(tree: WorkspaceTree[], v: SavedFilter, meId?: number): string {
  const ws = (v.workspace_id ? tree.find((w) => w.id === v.workspace_id) : undefined) ?? tree[0];
  if (!ws) return "/";
  const { filters, project } = parse(v.query, meId);
  const search = toSearch(filters);
  const base = project && ws.projects.some((p) => p.id === project) ? `/w/${ws.slug}/p/${project}/table` : `/w/${ws.slug}/table`;
  return search ? `${base}?${search}` : base;
}

// summary: the view's filters as short chips ("todo", "label ops", "≥ High"...).
export function summary(query: Record<string, string>): string[] {
  const out: string[] = [];
  if (query.q) out.push(`“${query.q}”`);
  if (query.category) out.push(query.category);
  if (query.label) out.push(`#${query.label}`);
  if (query.priority_min) out.push(`priority ≥ ${query.priority_min}`);
  if (query.assignee || query.mine) out.push("assignee");
  for (const k of PASSTHROUGH) if (query[k]) out.push(`${k.replace("_", " ")} ${query[k]}`);
  return out;
}
