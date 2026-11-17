import type { Gig } from "./api";

// Per-column sort on the board, remembered in this browser per board (one project: its id; several: 0)
// and column key. "manual" is the board's own order (a project's manual order, or the merged board's
// priority / due / board order); any other sort turns drag-to-reorder off in that column.
export type ColumnSort = "manual" | "name-asc" | "name-desc" | "created-desc" | "created-asc" | "updated-desc" | "done-desc" | "done-asc" | "due-asc" | "priority-desc";

export const SORT_LABEL: Record<ColumnSort, string> = {
  manual: "Board order",
  "name-asc": "Name A → Z",
  "name-desc": "Name Z → A",
  "created-desc": "Newest first",
  "created-asc": "Oldest first",
  "updated-desc": "Recently updated",
  "done-desc": "Done, latest first",
  "done-asc": "Done, earliest first",
  "due-asc": "Due soonest",
  "priority-desc": "Priority, highest first",
};

// the sorts a column offers: the done ones only where gigs are done
export function sortsFor(category: string): ColumnSort[] {
  const base: ColumnSort[] = ["manual", "name-asc", "name-desc", "created-desc", "created-asc", "updated-desc", "due-asc", "priority-desc"];
  return category === "done" ? [...base.slice(0, 1), "done-desc", "done-asc", ...base.slice(1)] : base;
}

const isSort = (v: unknown): v is ColumnSort => typeof v === "string" && v in SORT_LABEL;
const key = (board: number) => `gigg.board.sort.${board}`;

export function loadSorts(board: number): Record<string, ColumnSort> {
  try {
    const v: unknown = JSON.parse(localStorage.getItem(key(board)) ?? "{}");
    if (!v || typeof v !== "object" || Array.isArray(v)) return {};
    return Object.fromEntries(Object.entries(v).filter((e): e is [string, ColumnSort] => isSort(e[1]) && e[1] !== "manual"));
  } catch {
    return {};
  }
}

export function saveSorts(board: number, sorts: Record<string, ColumnSort>): void {
  try {
    const kept = Object.fromEntries(Object.entries(sorts).filter(([, s]) => s !== "manual"));
    if (Object.keys(kept).length) localStorage.setItem(key(board), JSON.stringify(kept));
    else localStorage.removeItem(key(board));
  } catch {
    /* storage unavailable */
  }
}

type Sortable = Pick<Gig, "title" | "created_at" | "updated_at" | "due_at" | "priority" | "done_at">;

// sortGigs: the column's gigs in `sort` order (a copy; "manual" keeps them as given). Missing dates go
// last either way; ties keep the board order (the sort is stable).
export function sortGigs<T extends Sortable>(gigs: T[], sort: ColumnSort): T[] {
  if (sort === "manual") return gigs;
  const last = (v: number | null | undefined, desc: boolean) => (v === null || v === undefined ? Infinity : desc ? -v : v);
  const by: Record<Exclude<ColumnSort, "manual">, (a: T, b: T) => number> = {
    "name-asc": (a, b) => a.title.localeCompare(b.title, undefined, { sensitivity: "base", numeric: true }),
    "name-desc": (a, b) => b.title.localeCompare(a.title, undefined, { sensitivity: "base", numeric: true }),
    "created-desc": (a, b) => b.created_at - a.created_at,
    "created-asc": (a, b) => a.created_at - b.created_at,
    "updated-desc": (a, b) => b.updated_at - a.updated_at,
    "done-desc": (a, b) => last(a.done_at, true) - last(b.done_at, true),
    "done-asc": (a, b) => last(a.done_at, false) - last(b.done_at, false),
    "due-asc": (a, b) => last(a.due_at, false) - last(b.due_at, false),
    "priority-desc": (a, b) => b.priority - a.priority,
  };
  return [...gigs].sort(by[sort]);
}
