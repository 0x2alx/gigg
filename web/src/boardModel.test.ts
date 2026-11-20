import { describe, expect, it } from "vitest";
import type { Board, Category, Project, Gig } from "./api";
import type { WorkspaceTree } from "./App";
import { activeFilters, alwaysRunMatches, categoryColumns, EMPTY_FILTER, filterQuery, matches, moveToCategory, parseFilter, recurringMatches, scope, statusColumns } from "./boardModel";

const gig = (id: number, x: Partial<Gig> = {}): Gig =>
  ({ id, key: `K-${id}`, title: `gig ${id}`, description_md: "", priority: 0, due_at: null, assignees: [], labels: [], status_id: 0, status_name: "", status_category: "todo", ...x }) as Gig;
const status = (id: number, name: string, category: Category, gigs: Gig[], color: string | null = null) => ({
  id,
  name,
  category,
  color,
  position: id,
  gigs: gigs.map((t) => ({ ...t, status_id: id, status_name: name, status_category: category })),
});
const board = (pid: number, statuses: ReturnType<typeof status>[]): Board => ({ project: { id: pid } as Project, statuses, labels: [] }) as unknown as Board;
const proj = (id: number) => ({ id, key: "P" + id, name: "p" + id }) as Project;
const tree: WorkspaceTree[] = [
  { id: 1, slug: "scout", name: "Scout", role: "member", projects: [proj(1), proj(2)] },
  { id: 2, slug: "atlas", name: "Atlas", role: "member", projects: [proj(3)] },
];

describe("board filter", () => {
  it("round-trips through the URL and ignores junk", () => {
    const f = { ...EMPTY_FILTER, ws: ["scout", "atlas"], p: [3], a: "me", l: "ops", pr: 2, q: "brief", arch: true };
    expect(parseFilter(filterQuery(f))).toEqual(f);
    expect(filterQuery(EMPTY_FILTER)).toBe("");
    expect(parseFilter("?p=x,0,4&pr=9")).toEqual({ ...EMPTY_FILTER, p: [4] });
  });
  it("scopes workspaces and projects; a project outside the chosen workspaces is ignored", () => {
    expect(scope(tree, EMPTY_FILTER).map((x) => x.project.id)).toEqual([1, 2, 3]);
    expect(scope(tree, { ...EMPTY_FILTER, ws: ["atlas"] }).map((x) => x.project.id)).toEqual([3]);
    expect(scope(tree, { ...EMPTY_FILTER, p: [2, 3] }).map((x) => x.project.id)).toEqual([2, 3]);
    expect(scope(tree, { ...EMPTY_FILTER, ws: ["scout"], p: [3] }).map((x) => x.project.id)).toEqual([1, 2]);
    expect(scope(tree, { ...EMPTY_FILTER, ws: ["gone"] }).map((x) => x.project.id)).toEqual([1, 2, 3]); // a lost workspace: all
  });
  it("matches assignee, label, priority and text", () => {
    const t = gig(1, { assignees: [7], labels: [{ id: 1, name: "Ops", color: null }], priority: 3, title: "Morning brief" });
    expect(matches(t, { ...EMPTY_FILTER, a: "me" }, 7)).toBe(true);
    expect(matches(t, { ...EMPTY_FILTER, a: "none" }, 7)).toBe(false);
    expect(matches(t, { ...EMPTY_FILTER, a: "8" }, 7)).toBe(false);
    expect(matches(t, { ...EMPTY_FILTER, l: "ops", pr: 3, q: "BRIEF" }, 7)).toBe(true);
    expect(matches(t, { ...EMPTY_FILTER, pr: 4 }, 7)).toBe(false);
  });
});

describe("recurring filter", () => {
  const oneOff = gig(1);
  const made = gig(2, { recurrence_id: 9 });
  it("all, hide and only", () => {
    expect([oneOff, made].map((t) => recurringMatches(t, ""))).toEqual([true, true]);
    expect([oneOff, made].map((t) => recurringMatches(t, "hide"))).toEqual([true, false]);
    expect([oneOff, made].map((t) => recurringMatches(t, "only"))).toEqual([false, true]);
    expect(recurringMatches(gig(3, { recurrence_id: null }), "only")).toBe(false);
  });
  it("is part of the board filter, kept in the URL and in the saved board_filter", () => {
    const f = { ...EMPTY_FILTER, l: "ops", rec: "hide" as const };
    // the board saves filterQuery() as the account's board_filter and restores it with parseFilter()
    const saved = filterQuery(f);
    expect(saved).toBe("?l=ops&recurring=hide");
    expect(parseFilter(saved)).toEqual(f);
    expect(parseFilter("?recurring=only").rec).toBe("only");
    expect(parseFilter("?recurring=sometimes").rec).toBe("");
    expect(matches(made, { ...EMPTY_FILTER, rec: "hide" }, 7)).toBe(false);
    expect(matches(made, { ...EMPTY_FILTER, rec: "only" }, 7)).toBe(true);
  });
  it("shows as a chip that takes it off", () => {
    const names = { ws: () => undefined, project: () => undefined, user: () => undefined, priority: (n: number) => String(n) };
    const chips = activeFilters({ ...EMPTY_FILTER, rec: "only" }, names);
    expect(chips.map((c) => [c.key, c.label])).toEqual([["recurring", "Only recurring"]]);
    expect(chips[0]!.without).toEqual(EMPTY_FILTER);
  });
});

describe("always-run filter", () => {
  const plain = gig(1);
  const nightly = gig(2, { always_run: true });
  it("all, hide and only", () => {
    expect([plain, nightly].map((t) => alwaysRunMatches(t, ""))).toEqual([true, true]);
    expect([plain, nightly].map((t) => alwaysRunMatches(t, "hide"))).toEqual([true, false]);
    expect([plain, nightly].map((t) => alwaysRunMatches(t, "only"))).toEqual([false, true]);
    expect(alwaysRunMatches(gig(3, { always_run: false }), "only")).toBe(false);
  });
  it("is part of the board filter, next to recurring, in the URL and the saved board_filter", () => {
    const f = { ...EMPTY_FILTER, rec: "hide" as const, run: "only" as const };
    const saved = filterQuery(f);
    expect(saved).toBe("?recurring=hide&always_run=only");
    expect(parseFilter(saved)).toEqual(f);
    expect(parseFilter("?always_run=yes").run).toBe("");
    expect(matches(nightly, { ...EMPTY_FILTER, run: "hide" }, 7)).toBe(false);
    expect(matches(nightly, { ...EMPTY_FILTER, run: "only" }, 7)).toBe(true);
    expect(matches(plain, { ...EMPTY_FILTER, run: "only" }, 7)).toBe(false);
  });
  it("shows as a chip that takes it off", () => {
    const names = { ws: () => undefined, project: () => undefined, user: () => undefined, priority: (n: number) => String(n) };
    const chips = activeFilters({ ...EMPTY_FILTER, run: "hide" }, names);
    expect(chips.map((c) => [c.key, c.label])).toEqual([["always_run", "Always run hidden"]]);
    expect(chips[0]!.without).toEqual(EMPTY_FILTER);
  });
});

describe("board columns", () => {
  const b1 = board(1, [status(11, "To-Do", "todo", [gig(1), gig(2, { priority: 4 })]), status(12, "Done", "done", []), status(13, "Gone", "cancelled", [gig(9)])]);
  const b2 = board(3, [status(31, "Next", "todo", [gig(3, { priority: 4, due_at: 5 })]), status(32, "Done", "done", [gig(4)])]);
  it("one project: its own statuses, cancelled hidden, manual order", () => {
    const cols = statusColumns(b1, EMPTY_FILTER, 0);
    expect(cols.map((c) => c.name)).toEqual(["To-Do", "Done"]);
    expect(cols[0]!.gigs.map((t) => t.id)).toEqual([1, 2]);
  });
  it("several: one column per category, named after the status when all agree, priority then due", () => {
    const cols = categoryColumns([b1, b2], EMPTY_FILTER, 0);
    expect(cols.map((c) => c.name)).toEqual(["Backlog", "To-Do", "Doing", "Waiting", "Done"]);
    const todo = cols.find((c) => c.category === "todo")!;
    expect(todo.gigs.map((t) => t.id)).toEqual([3, 2, 1]);
    expect(cols.find((c) => c.category === "done")!.name).toBe("Done");
  });
  it("moves a gig to its project's first status of a category", () => {
    const boards = new Map([[1, b1]]);
    const next = moveToCategory(boards, 1, "done")!;
    expect(next.get(1)!.statuses.find((s) => s.id === 12)!.gigs.map((t) => t.id)).toEqual([1]);
    expect(moveToCategory(boards, 1, "todo")).toBeNull();
    expect(moveToCategory(boards, 1, "doing")).toBeNull(); // no doing status in that project
  });
});

describe("activeFilters", () => {
  const names = {
    ws: (s: string) => (s === "acme" ? { name: "Acme", color: "#E5484D" } : undefined),
    project: (id: number) => (id === 7 ? { name: "Platform" } : undefined),
    user: (id: number) => (id === 3 ? "Sam" : undefined),
    priority: (n: number) => ["None", "Low", "Medium", "High", "Urgent"][n]!,
  };
  it("is empty without filters", () => expect(activeFilters(EMPTY_FILTER, names)).toEqual([]));
  it("lists each filter with the filter minus that one", () => {
    const f = { ...EMPTY_FILTER, ws: ["acme"], p: [7], a: "3", l: "bug", pr: 3, q: "login", arch: true };
    const got = activeFilters(f, names);
    expect(got.map((x) => x.label)).toEqual(["Acme", "Platform", "“login”", "Assigned to Sam", "Label: bug", "High or higher", "Archived shown"]);
    expect(got[0]!.color).toBe("#E5484D");
    expect(got[0]!.without.ws).toEqual([]);
    expect(got[3]!.without).toEqual({ ...f, a: "" });
    expect(activeFilters({ ...EMPTY_FILTER, pr: 4, a: "none" }, names).map((x) => x.label)).toEqual(["Unassigned", "Urgent"]);
  });
});
