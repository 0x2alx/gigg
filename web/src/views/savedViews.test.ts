import { describe, expect, it } from "vitest";
import type { WorkspaceTree } from "../App";
import { DEFAULT_FILTERS, parse, summary, toQuery, toSearch, viewHref, type TableFilters } from "./savedViews";

const f = (o: Partial<TableFilters>): TableFilters => ({ ...DEFAULT_FILTERS, ...o });

const tree = [
  { id: 1, slug: "home", name: "Home", role: "manager", projects: [{ id: 10, workspace_id: 1, slug: "ops", name: "Ops", key: "OPS", color: null, archived: false }] },
  { id: 2, slug: "work", name: "Work", role: "member", projects: [] },
] as WorkspaceTree[];

describe("toQuery", () => {
  it("omits defaults", () => {
    expect(toQuery(DEFAULT_FILTERS)).toEqual({});
  });
  it("maps every control to an allowed key", () => {
    expect(toQuery(f({ q: " deploy ", category: "todo", label: "ops", assignee: 3, prioMin: 2, sort: { key: "due", desc: false }, extra: { blocked: "true" } }), 10)).toEqual({
      q: "deploy",
      category: "todo",
      label: "ops",
      assignee: "3",
      priority_min: "2",
      sort: "due",
      project: "10",
      blocked: "true",
    });
  });
  it("never stores include-done (no server key)", () => {
    expect(toQuery(f({ includeDone: true }))).toEqual({});
    expect(toSearch(f({ includeDone: true }))).toBe("done=1");
  });
});

describe("parse", () => {
  it("round-trips through the URL", () => {
    const x = f({ q: "a b", category: "doing", label: "x", assignee: 5, prioMin: 3, sort: { key: "title", desc: true }, includeDone: true });
    expect(parse(new URLSearchParams(toSearch(x))).filters).toEqual(x);
  });
  it("drops malformed values", () => {
    const { filters, project } = parse({ category: "nope", priority_min: "9", sort: "-bogus", assignee: "x", project: "-1" });
    expect(filters).toEqual(DEFAULT_FILTERS);
    expect(project).toBeUndefined();
  });
  it("turns mine into the signed-in user", () => {
    expect(parse({ mine: "true" }, 7).filters.assignee).toBe(7);
    expect(parse({ mine: "true", assignee: "3" }, 7).filters.assignee).toBe(3);
  });
  it("keeps pass-through keys and the project", () => {
    const r = parse({ status: "4", archived: "include", project: "10", unknown: "x" });
    expect(r.filters.extra).toEqual({ status: "4", archived: "include" });
    expect(r.project).toBe(10);
  });
});

describe("viewHref", () => {
  it("opens the project table when the project is in reach", () => {
    expect(viewHref(tree, { id: 1, workspace_id: 1, name: "v", query: { project: "10", label: "ops" } })).toBe("/w/home/p/10/table?label=ops");
  });
  it("falls back to the workspace table", () => {
    expect(viewHref(tree, { id: 1, workspace_id: 2, name: "v", query: { project: "10" } })).toBe("/w/work/table");
    expect(viewHref(tree, { id: 1, workspace_id: null, name: "v", query: { mine: "1" } }, 9)).toBe("/w/home/table?assignee=9");
  });
});

describe("summary", () => {
  it("lists the filters as short chips", () => {
    expect(summary({ q: "x", category: "todo", label: "ops", priority_min: "3", mine: "1", due_before: "1" })).toEqual(["“x”", "todo", "#ops", "priority ≥ 3", "assignee", "due before 1"]);
  });
});
