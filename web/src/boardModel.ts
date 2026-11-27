// The board as pure functions: the filter (kept in the URL), which projects it covers, and its columns.
// One project in view: that project's own status columns in its manual order. Several: one column per
// status category, gigs from every project merged and ordered by priority, then due date.
import type { Board, Category, Project, Gig } from "./api";
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

// The yes/no filters, one rule each for the board, the table and the list API (the key is the API's
// and the URL's): "" every gig, "hide" leaves out the gigs that have the flag, "only" shows just those.
// recurring: made by a recurring gig (recurrence_id set); always_run: worked whatever the usage budget.
export type FlagFilter = "" | "hide" | "only";
export type RecurringFilter = FlagFilter;
export interface FlagChoice {
  value: FlagFilter;
  label: string; // in the picker
  chip: string; // in the folded filter row and a saved view's summary ("" for every gig)
}
export const FLAG_FILTERS: Record<"recurring" | "always_run", { name: string; aria: string; choices: FlagChoice[] }> = {
  recurring: {
    name: "Recurring",
    aria: "Recurring gigs",
    choices: [
      { value: "", label: "Recurring or not", chip: "" },
      { value: "hide", label: "Hide recurring", chip: "Recurring hidden" },
      { value: "only", label: "Only recurring", chip: "Only recurring" },
    ],
  },
  always_run: {
    name: "Always run",
    aria: "Always-run gigs",
    choices: [
      { value: "", label: "Always run or not", chip: "" },
      { value: "hide", label: "Hide always run", chip: "Always run hidden" },
      { value: "only", label: "Only always run", chip: "Only always run" },
    ],
  },
};
export type FlagKey = keyof typeof FLAG_FILTERS;
export const flagChip = (k: FlagKey, v: FlagFilter): string => FLAG_FILTERS[k].choices.find((c) => c.value === v)?.chip ?? "";
export const parseFlag = (v: string | null | undefined): FlagFilter => (v === "hide" || v === "only" ? v : "");
const flagMatches = (has: boolean, r: FlagFilter): boolean => r === "" || (r === "only") === has;
export const recurringMatches = (t: Pick<Gig, "recurrence_id">, r: RecurringFilter): boolean => flagMatches(t.recurrence_id != null, r);
export const alwaysRunMatches = (t: Pick<Gig, "always_run">, r: FlagFilter): boolean => flagMatches(!!t.always_run, r);

export interface BoardFilter {
  ws: string[]; // workspace slugs; empty = every workspace
  p: number[]; // project ids; empty = every project of the chosen workspaces
  a: string; // assignee: "" any, "me", "none", or a user id
  l: string; // label name ("" any)
  pr: number; // minimum priority (0 = any)
  q: string; // text in the key, title or description
  rec: RecurringFilter; // gigs a recurring gig made: "" all, "hide", "only" (URL key "recurring")
  run: FlagFilter; // always-run gigs: "" all, "hide", "only" (URL key "always_run")
  arch: boolean; // archived gigs shown too
}

export const EMPTY_FILTER: BoardFilter = { ws: [], p: [], a: "", l: "", pr: 0, q: "", rec: "", run: "", arch: false };

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
    rec: parseFlag(s.get("recurring")),
    run: parseFlag(s.get("always_run")),
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
  if (f.rec) s.set("recurring", f.rec);
  if (f.run) s.set("always_run", f.run);
  if (f.arch) s.set("arch", "1");
  const q = s.toString();
  return q ? "?" + q : "";
}

// isFiltered: anything narrows or widens the board from its default, "Show archived gigs" included
export const isFiltered = (f: BoardFilter) => filterQuery(f) !== "";

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
  names: { ws: (slug: string) => { name: string; color?: string } | undefined; project: (id: number) => { name: string; color?: string } | undefined; user: (id: number) => string | undefined; priority: (n: number) => string },
): ActiveFilter[] {
  const out: ActiveFilter[] = [];
  for (const slug of f.ws) {
    const w = names.ws(slug);
    out.push({ key: "ws:" + slug, label: w?.name ?? slug, color: w?.color, without: { ...f, ws: f.ws.filter((s) => s !== slug) } });
  }
  for (const id of f.p) {
    const p = names.project(id);
    out.push({ key: "p:" + id, label: p?.name ?? `Project ${id}`, color: p?.color, without: { ...f, p: f.p.filter((x) => x !== id) } });
  }
  if (f.q) out.push({ key: "q", label: `“${f.q}”`, without: { ...f, q: "" } });
  if (f.a) {
    const who = f.a === "me" ? "Assigned to me" : f.a === "none" ? "Unassigned" : `Assigned to ${names.user(Number(f.a)) ?? "someone"}`;
    out.push({ key: "a", label: who, without: { ...f, a: "" } });
  }
  if (f.l) out.push({ key: "l", label: `Label: ${f.l}`, without: { ...f, l: "" } });
  if (f.pr) out.push({ key: "pr", label: f.pr === 4 ? names.priority(4) : `${names.priority(f.pr)} or higher`, without: { ...f, pr: 0 } });
  if (f.rec) out.push({ key: "recurring", label: flagChip("recurring", f.rec), without: { ...f, rec: "" } });
  if (f.run) out.push({ key: "always_run", label: flagChip("always_run", f.run), without: { ...f, run: "" } });
  if (f.arch) out.push({ key: "arch", label: "Archived shown", without: { ...f, arch: false } });
  return out;
}

export interface ScopedProject {
  ws: WorkspaceTree;
  project: Project;
}

// scope: the projects the board covers, in sidebar order (workspaces, then project position): every
// project of the workspaces in f.ws, plus the projects in f.p (so a pick can span workspaces). Unknown
// slugs and ids are ignored; nothing known picked means every project.
export function scope(tree: WorkspaceTree[], f: BoardFilter): ScopedProject[] {
  const all = tree.flatMap((ws) => ws.projects.map((project) => ({ ws, project })));
  const picked = all.filter((x) => f.ws.includes(x.ws.slug) || f.p.includes(x.project.id));
  return picked.length ? picked : all;
}


// ---- the "Where" picker: workspaces and projects as one tree, and the shortcut row under the toolbar ----

// A project named "Main" (any case) can't be told from the next workspace's: it gets no shortcut chip and
// is shown with its workspace, "Workspace / Main".
export const isMainProject = (p: Pick<Project, "name">): boolean => p.name.trim().toLowerCase() === "main";
export const placeName = (w: Pick<WorkspaceTree, "name">, p: Pick<Project, "name">): string => (isMainProject(p) ? `${w.name} / ${p.name}` : p.name);

// A workspace or project the filter names (unknown slugs and ids are skipped), workspaces first.
export interface PickedPlace {
  key: string;
  label: string;
  ws: WorkspaceTree;
  kind: "ws" | "p";
  id: string | number; // the workspace slug, or the project id
  main: boolean; // a project named "Main"
  implied: boolean; // a project whose workspace is picked too: already in view through it
}
function pickedPlaces(tree: WorkspaceTree[], f: BoardFilter): PickedPlace[] {
  const out: PickedPlace[] = [];
  for (const slug of f.ws) {
    const w = tree.find((x) => x.slug === slug);
    if (w) out.push({ key: "ws:" + slug, label: w.name, ws: w, kind: "ws", id: slug, main: false, implied: false });
  }
  for (const id of f.p) {
    const w = tree.find((x) => x.projects.some((p) => p.id === id));
    const p = w?.projects.find((x) => x.id === id);
    if (w && p) out.push({ key: "p:" + id, label: placeName(w, p), ws: w, kind: "p", id, main: isMainProject(p), implied: f.ws.includes(w.slug) });
  }
  return out;
}

// whereLabel: the Where button's face: "All", the one name picked, or "3 places".
export function whereLabel(tree: WorkspaceTree[], f: BoardFilter): string {
  const picked = pickedPlaces(tree, f).filter((x) => !x.implied);
  return picked.length === 0 ? "All" : picked.length === 1 ? picked[0]!.label : `${picked.length} places`;
}

// shortcutProjects: the projects with a chip of their own under the toolbar (those of every workspace,
// but not the ones named "Main").
export function shortcutProjects(tree: WorkspaceTree[]): ScopedProject[] {
  return tree.flatMap((ws) => ws.projects.filter((p) => !isMainProject(p)).map((project) => ({ ws, project })));
}

// removablePlaces: the picked items that have no shortcut chip (every picked workspace, and the picked
// projects named "Main"), each as a chip with an x. Removing one is the same toggle as ticking it.
export const removablePlaces = (tree: WorkspaceTree[], f: BoardFilter): PickedPlace[] => pickedPlaces(tree, f).filter((x) => !x.implied && (x.kind === "ws" || x.main));

// whereTree: the rows of the Where panel for a search text. Workspaces in order, each with its projects
// (all of them: the picks add up across workspaces). A workspace whose name matches keeps all
// its projects; otherwise only the projects that match, and the workspace stays only for one.
export function whereTree(tree: WorkspaceTree[], query: string): { ws: WorkspaceTree; projects: Project[] }[] {
  const q = query.trim().toLowerCase();
  const out: { ws: WorkspaceTree; projects: Project[] }[] = [];
  for (const ws of tree) {
    const all = ws.projects;
    if (!q || ws.name.toLowerCase().includes(q)) out.push({ ws, projects: all });
    else {
      const projects = all.filter((p) => p.name.toLowerCase().includes(q) || placeName(ws, p).toLowerCase().includes(q));
      if (projects.length) out.push({ ws, projects });
    }
  }
  return out;
}

// matches: the gig passes the filter's gig conditions (scope is handled by scope()).
export function matches(t: Gig, f: BoardFilter, me: number): boolean {
  if (f.a === "me" && !t.assignees.includes(me)) return false;
  if (f.a === "none" && t.assignees.length > 0) return false;
  if (/^\d+$/.test(f.a) && !t.assignees.includes(Number(f.a))) return false;
  if (f.l && !t.labels.some((l) => l.name.toLowerCase() === f.l.toLowerCase())) return false;
  if (f.pr && t.priority < f.pr) return false;
  if (!recurringMatches(t, f.rec)) return false;
  if (!alwaysRunMatches(t, f.run)) return false;
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
  gigs: Gig[];
}

// statusColumns: one project's board, its own statuses (cancelled hidden), its manual order.
export function statusColumns(board: Board, f: BoardFilter, me: number): BoardColumn[] {
  return board.statuses
    .filter((s) => s.category !== "cancelled")
    .map((s) => ({ key: "s" + s.id, id: s.id, name: s.name, color: s.color, category: s.category, statusId: s.id, gigs: s.gigs.filter((t) => matches(t, f, me)) }));
}

// categoryColumns: several boards merged by category. A column takes the status name (and colour)
// when every project in view calls it the same, otherwise the category's name. Gigs: priority first,
// then the earliest due date, then the boards' own order.
export function categoryColumns(boards: Board[], f: BoardFilter, me: number): BoardColumn[] {
  const rank = new Map<number, number>();
  let n = 0;
  for (const b of boards) for (const s of b.statuses) for (const t of s.gigs) rank.set(t.id, n++);
  const order = (a: Gig, b: Gig) =>
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
      gigs: statuses.flatMap((s) => s.gigs).filter((t) => matches(t, f, me)).sort(order),
    };
  });
}

// pickColumn: the column a phone shows (one at a time, as tabs). A column the user picked under the
// current filter stays, even when empty. Otherwise the default (the picked one from before the filter
// changed, else the first To-Do, else the first) holds unless a gig filter leaves it empty while
// another column has matches: then the first column with matches, so a search never looks like it
// found nothing.
export function pickColumn(columns: BoardColumn[], picked: string | null, pickedUnderFilter: boolean, gigFiltered: boolean): BoardColumn | undefined {
  const chosen = picked !== null ? columns.find((c) => c.key === picked) : undefined;
  if (chosen && pickedUnderFilter) return chosen;
  const base = chosen ?? columns.find((c) => c.category === "todo") ?? columns[0];
  if (gigFiltered && base && base.gigs.length === 0) return columns.find((c) => c.gigs.length > 0) ?? base;
  return base;
}

// moveToCategory: the boards after a gig moves to the first status of `category` in its project
// (what the server does for {category}), or null when it can't (no such status) or nothing changes.
export function moveToCategory(boards: Map<number, Board>, gigId: number, category: Category): Map<number, Board> | null {
  for (const [pid, b] of boards) {
    const gig = b.statuses.flatMap((s) => s.gigs).find((t) => t.id === gigId);
    if (!gig) continue;
    const target = b.statuses.find((s) => s.category === category);
    if (!target || gig.status_category === category) return null;
    const moved: Gig = { ...gig, status_id: target.id, status_name: target.name, status_category: target.category };
    const next = new Map(boards);
    next.set(pid, {
      ...b,
      statuses: b.statuses.map((s) => ({
        ...s,
        gigs: s.id === target.id ? [...s.gigs.filter((t) => t.id !== gigId), moved] : s.gigs.filter((t) => t.id !== gigId),
      })),
    });
    return next;
  }
  return null;
}
