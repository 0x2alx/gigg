// Fetch helpers for the table, my-gigs and command-palette views: the workspace gig list with its
// filters, cursor paging, cross-workspace fan-out and a small concurrency pool for bulk actions.
import { ApiError, noteStatus, type Category, type Gig } from "../api";
import type { WorkspaceTree } from "../App";
import { request } from "../transport";

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const r = await request(method, path, { body });
  if (r.status === 204) return undefined as T;
  if (!r.ok) {
    noteStatus(r.status, path);
    const d = (r.data ?? {}) as { code?: string; detail?: string };
    throw new ApiError(r.status, d.code ?? "error", d.detail ?? r.statusText);
  }
  return r.data as T;
}

export interface GigQuery {
  project?: number;
  status?: number;
  category?: Category | "";
  label?: string;
  priority_min?: number;
  recurring?: "" | "hide" | "only";
  assignee?: number;
  created_by?: number;
  q?: string;
  due_before?: number;
  updated_since?: number;
  include_deleted?: boolean;
  deleted?: boolean;
  sort?: string;
  limit?: number;
  cursor?: string;
}

export interface GigPage {
  items: Gig[];
  next_cursor?: string;
}

function qs(q: GigQuery): string {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(q)) {
    if (v === undefined || v === null || v === "" || v === false || v === 0) continue;
    p.set(k, String(v));
  }
  const s = p.toString();
  return s ? "?" + s : "";
}

export const viewsApi = {
  gigs: (ws: string | number, q: GigQuery) => call<GigPage>("GET", `/workspaces/${ws}/gigs${qs(q)}`),
};

// gigsAcross queries every workspace of the tree with the same filters and merges the pages.
// A workspace that fails is reported in `failed` instead of failing the whole call.
export async function gigsAcross(tree: WorkspaceTree[], q: GigQuery): Promise<{ items: Gig[]; failed: string[] }> {
  const res = await Promise.allSettled(tree.map((w) => viewsApi.gigs(w.slug, q)));
  const items: Gig[] = [];
  const failed: string[] = [];
  res.forEach((r, i) => {
    if (r.status === "fulfilled") items.push(...r.value.items);
    else failed.push(tree[i]!.name);
  });
  return { items, failed };
}

// pool runs fn over items with at most `limit` calls in flight; onDone fires after each item.
export async function pool<T>(items: T[], limit: number, fn: (item: T) => Promise<unknown>, onDone: (item: T, err: string | null) => void): Promise<void> {
  let next = 0;
  const worker = async () => {
    while (next < items.length) {
      const item = items[next++]!;
      try {
        await fn(item);
        onDone(item, null);
      } catch (e) {
        onDone(item, errMsg(e));
      }
    }
  };
  await Promise.all(Array.from({ length: Math.min(limit, items.length) }, worker));
}

export function errMsg(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  if (e instanceof Error) return e.message;
  return "Request failed.";
}

// fromHere: navigation state that brings a gig opened from a list (the table, My gigs, the bell, the
// palette) back to that page when its panel closes; the board's own links don't need it.
export function fromHere(): { state: { back: string } | null } {
  const here = location.pathname + location.search;
  return { state: location.pathname.startsWith("/board") ? null : { back: here } };
}
// backTo: where a closing gig panel goes, when it was opened by fromHere.
export function backTo(): string | null {
  const b = (history.state as { back?: unknown } | null)?.back;
  return typeof b === "string" && /^\/(?![/\\])/.test(b) && !b.includes("\\") ? b : null;
}

// gigHref: the board URL with the gig panel open.
export function gigHref(tree: WorkspaceTree[], t: Pick<Gig, "workspace_id" | "project_id" | "key">): string {
  return tree.some((w) => w.id === t.workspace_id) ? `/board/p/${t.project_id}/g/${t.key}` : "/";
}

export const OPEN_CATEGORIES: Category[] = ["backlog", "todo", "doing", "waiting"];
export const ALL_CATEGORIES: Category[] = ["backlog", "todo", "doing", "waiting", "done", "cancelled"];
export const isClosed = (c: Category) => c === "done" || c === "cancelled";
export const isOverdue = (t: Pick<Gig, "due_at" | "status_category">) => t.due_at !== null && t.due_at < Date.now() && !isClosed(t.status_category);

export type DueBucket = "overdue" | "today" | "week" | "later" | "none";
export const BUCKETS: { id: DueBucket; label: string }[] = [
  { id: "overdue", label: "Overdue" },
  { id: "today", label: "Today" },
  { id: "week", label: "This week" },
  { id: "later", label: "Later" },
  { id: "none", label: "No date" },
];

// dueBucket uses local calendar days; "this week" is the next 7 days after today.
export function dueBucket(due: number | null, now = new Date()): DueBucket {
  if (due === null) return "none";
  const start = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const day = 86_400_000;
  if (due < start) return "overdue";
  if (due < start + day) return "today";
  if (due < start + 8 * day) return "week";
  return "later";
}

// relTime: "5m ago", "3h ago", "2d ago", then the date.
export function relTime(ms: number): string {
  const d = Date.now() - ms;
  if (d < 60_000) return "just now";
  if (d < 3_600_000) return `${Math.floor(d / 60_000)}m ago`;
  if (d < 86_400_000) return `${Math.floor(d / 3_600_000)}h ago`;
  if (d < 7 * 86_400_000) return `${Math.floor(d / 86_400_000)}d ago`;
  return new Date(ms).toLocaleDateString(undefined, { dateStyle: "medium" });
}

// fuzzy: a subsequence match score (higher is better), or null when `q` isn't a subsequence of `s`.
// Contiguous runs, word starts and an exact prefix score extra.
export function fuzzy(q: string, s: string): number | null {
  const query = q.toLowerCase().replace(/\s+/g, "");
  const text = s.toLowerCase();
  if (!query) return 0;
  let score = 0;
  let ti = 0;
  let prev = -2;
  for (const ch of query) {
    const at = text.indexOf(ch, ti);
    if (at < 0) return null;
    score += 1;
    if (at === prev + 1) score += 3;
    if (at === 0 || /[\s/·_-]/.test(text[at - 1] ?? "")) score += 2;
    prev = at;
    ti = at + 1;
  }
  if (text.startsWith(q.toLowerCase())) score += 10;
  else if (text.includes(q.toLowerCase())) score += 5;
  return score - text.length * 0.01;
}
