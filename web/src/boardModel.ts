// The board as pure functions: the filter (kept in the URL), which projects it covers, and its columns.
// One project in view: that project's own status columns in its manual order. Several: one column per
// status category, tasks from every project merged and ordered by priority, then due date.
import type { Board, Category, Project, Task } from "./api";
import type { WorkspaceTree } from "./App";

export const BOARD_CATEGORIES: Category[] = ["backlog", "todo", "doing", "waiting", "done"];
export const CATEGORY_LABEL: Record<Category, string> = {
  backlog: "Backlog",
  todo: "To-Do",
  doing: "Doing",
  waiting: "Waiting",
  done: "Done",
  cancelled: "Cancelled",
};

export interface BoardFilter {
  ws: string[]; // workspace slugs; empty = every workspace
  p: number[]; // project ids; empty = every project of the chosen workspaces
  a: string; // assignee: "" any, "me", "none", or a user id
  l: string; // label name ("" any)
  pr: number; // minimum priority (0 = any)
  q: string; // text in the key, title or description
  arch: boolean; // archived tasks shown too
}

export const EMPTY_FILTER: BoardFilter = { ws: [], p: [], a: "", l: "", pr: 0, q: "", arch: false };

export function parseFilter(search: string): BoardFilter {
  const s = new URLSearchParams(search);
  const list = (k: string) => (s.get(k) ?? "").split(",").map((x) => x.trim()).filter(Boolean);
  const pr = Number(s.get("pr") ?? 0);
  return {
    ws: list("ws"),
    p: list("p").map(Number).filter((n) => Number.isInteger(n) && n > 0),
    a: s.get("a") ?? "",
    l: s.get("l") ?? "",
    pr: Number.isInteger(pr) && pr >= 0 && pr <= 4 ? pr : 0,
    q: s.get("q") ?? "",
    arch: s.get("arch") === "1",
  };
}

// filterQuery: the query string for a filter ("" when it is empty), stable field order.
export function filterQuery(f: BoardFilter): string {
  const s = new URLSearchParams();
  if (f.ws.length) s.set("ws", f.ws.join(","));
  if (f.p.length) s.set("p", f.p.join(","));
  if (f.a) s.set("a", f.a);
  if (f.l) s.set("l", f.l);
  if (f.pr) s.set("pr", String(f.pr));
  if (f.q) s.set("q", f.q);
  if (f.arch) s.set("arch", "1");
  const q = s.toString();
  return q ? "?" + q : "";
}

export const isFiltered = (f: BoardFilter) => filterQuery({ ...f, arch: false }) !== "";

// ActiveFilter: one filter that is on, as a removable chip of the folded filter row; `without` is the
// filter with just this one taken off.
export interface ActiveFilter {
  key: string;
  label: string;
  color?: string;
  without: BoardFilter;
}

export function activeFilters(
  f: BoardFilter,
  names: { ws: (slug: string) => { name: string; color?: string } | undefined; project: (id: number) => string | undefined; user: (id: number) => string | undefined; priority: (n: number) => string },
): ActiveFilter[] {
  const out: ActiveFilter[] = [];
  for (const slug of f.ws) {
    const w = names.ws(slug);
    out.push({ key: "ws:" + slug, label: w?.name ?? slug, color: w?.color, without: { ...f, ws: f.ws.filter((s) => s !== slug) } });
  }
  for (const id of f.p) out.push({ key: "p:" + id, label: names.project(id) ?? `Project ${id}`, without: { ...f, p: f.p.filter((x) => x !== id) } });
  if (f.q) out.push({ key: "q", label: `“${f.q}”`, without: { ...f, q: "" } });
  if (f.a) {
    const who = f.a === "me" ? "Assigned to me" : f.a === "none" ? "Unassigned" : `Assigned to ${names.user(Number(f.a)) ?? "someone"}`;
    out.push({ key: "a", label: who, without: { ...f, a: "" } });
  }
  if (f.l) out.push({ key: "l", label: `Label: ${f.l}`, without: { ...f, l: "" } });
  if (f.pr) out.push({ key: "pr", label: f.pr === 4 ? names.priority(4) : `${names.priority(f.pr)} or higher`, without: { ...f, pr: 0 } });
  if (f.arch) out.push({ key: "arch", label: "Archived shown", without: { ...f, arch: false } });
  return out;
}

export interface ScopedProject {
  ws: WorkspaceTree;
  project: Project;
}

// scope: the projects the board covers, in sidebar order (workspaces, then project position). Unknown
// slugs and ids are ignored (only unknown ones: as if none were chosen); a project filter that matches
// nothing in the chosen workspaces is ignored too.
export function scope(tree: WorkspaceTree[], f: BoardFilter): ScopedProject[] {
  const chosen = tree.filter((w) => f.ws.includes(w.slug));
  const wss = chosen.length ? chosen : tree; // none chosen, or only unknown ones (a lost workspace): all
  const all = wss.flatMap((ws) => ws.projects.map((project) => ({ ws, project })));
  const picked = all.filter((x) => f.p.includes(x.project.id));
  return picked.length ? picked : all;
}

// matches: the task passes the filter's task conditions (scope is handled by scope()).
export function matches(t: Task, f: BoardFilter, me: number): boolean {
  if (f.a === "me" && !t.assignees.includes(me)) return false;
  if (f.a === "none" && t.assignees.length > 0) return false;
  if (/^\d+$/.test(f.a) && !t.assignees.includes(Number(f.a))) return false;
  if (f.l && !t.labels.some((l) => l.name.toLowerCase() === f.l.toLowerCase())) return false;
  if (f.pr && t.priority < f.pr) return false;
  if (f.q) {
    const q = f.q.toLowerCase();
    if (!t.key.toLowerCase().includes(q) && !t.title.toLowerCase().includes(q) && !(t.description_md ?? "").toLowerCase().includes(q)) return false;
  }
  return true;
}

export interface BoardColumn {
  key: string;
  id: number; // a status id (one project) or -(category index + 1) (several): for collapsing
  name: string;
  color: string | null; // null = the category colour
  category: Category;
  statusId?: number; // one project: the status this column is
  tasks: Task[];
}

// statusColumns: one project's board, its own statuses (cancelled hidden), its manual order.
export function statusColumns(board: Board, f: BoardFilter, me: number): BoardColumn[] {
  return board.statuses
    .filter((s) => s.category !== "cancelled")
    .map((s) => ({ key: "s" + s.id, id: s.id, name: s.name, color: s.color, category: s.category, statusId: s.id, tasks: s.tasks.filter((t) => matches(t, f, me)) }));
}

// categoryColumns: several boards merged by category. A column takes the status name (and colour)
// when every project in view calls it the same, otherwise the category's name. Tasks: priority first,
// then the earliest due date, then the boards' own order.
export function categoryColumns(boards: Board[], f: BoardFilter, me: number): BoardColumn[] {
  const rank = new Map<number, number>();
  let n = 0;
  for (const b of boards) for (const s of b.statuses) for (const t of s.tasks) rank.set(t.id, n++);
  const order = (a: Task, b: Task) =>
    b.priority - a.priority || (a.due_at ?? Infinity) - (b.due_at ?? Infinity) || (rank.get(a.id) ?? 0) - (rank.get(b.id) ?? 0);
  return BOARD_CATEGORIES.map((category, i) => {
    const statuses = boards.flatMap((b) => b.statuses.filter((s) => s.category === category));
    const names = [...new Set(statuses.map((s) => s.name))];
    const colors = [...new Set(statuses.map((s) => s.color))];
    return {
      key: "c" + category,
      id: -(i + 1),
      name: names.length === 1 ? names[0]! : CATEGORY_LABEL[category],
      color: colors.length === 1 ? (colors[0] ?? null) : null,
      category,
      tasks: statuses.flatMap((s) => s.tasks).filter((t) => matches(t, f, me)).sort(order),
    };
  });
}

// moveToCategory: the boards after a task moves to the first status of `category` in its project
// (what the server does for {category}), or null when it can't (no such status) or nothing changes.
export function moveToCategory(boards: Map<number, Board>, taskId: number, category: Category): Map<number, Board> | null {
  for (const [pid, b] of boards) {
    const task = b.statuses.flatMap((s) => s.tasks).find((t) => t.id === taskId);
    if (!task) continue;
    const target = b.statuses.find((s) => s.category === category);
    if (!target || task.status_category === category) return null;
    const moved: Task = { ...task, status_id: target.id, status_name: target.name, status_category: target.category };
    const next = new Map(boards);
    next.set(pid, {
      ...b,
      statuses: b.statuses.map((s) => ({
        ...s,
        tasks: s.id === target.id ? [...s.tasks.filter((t) => t.id !== taskId), moved] : s.tasks.filter((t) => t.id !== taskId),
      })),
    });
    return next;
  }
  return null;
}
