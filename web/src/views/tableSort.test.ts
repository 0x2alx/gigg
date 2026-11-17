import { describe, expect, it } from "vitest";
import { compareGigs } from "./tableSort";

const t = (id: number, key: string, o: Partial<{ title: string; priority: number; due_at: number | null; updated_at: number; created_at: number }> = {}) => ({
  id, key, title: o.title ?? key, priority: o.priority ?? 0, due_at: o.due_at ?? null, updated_at: o.updated_at ?? 0, created_at: o.created_at ?? 0,
});
const order = (rows: ReturnType<typeof t>[], key: Parameters<typeof compareGigs>[2]["key"], desc = false) => [...rows].sort((a, b) => compareGigs(a, b, { key, desc })).map((r) => r.key);

describe("compareGigs", () => {
  it("sorts keys by project, then number (not as text)", () => {
    expect(order([t(1, "GIG-10"), t(2, "DOC-3"), t(3, "GIG-9")], "number")).toEqual(["DOC-3", "GIG-9", "GIG-10"]);
  });
  it("puts gigs without a due date last in both directions", () => {
    const rows = [t(1, "A-1", { due_at: 200 }), t(2, "A-2"), t(3, "A-3", { due_at: 100 })];
    expect(order(rows, "due")).toEqual(["A-3", "A-1", "A-2"]);
    expect(order(rows, "due", true)).toEqual(["A-1", "A-3", "A-2"]);
  });
  it("sorts titles case-insensitively, like SQLite NOCASE", () => {
    expect(order([t(1, "A-1", { title: "beta" }), t(2, "A-2", { title: "Alpha" }), t(3, "A-3", { title: "_x" })], "title")).toEqual(["A-3", "A-2", "A-1"]); // "_" sorts before letters, as in SQLite
  });
  it("sorts by priority and recency descending", () => {
    const rows = [t(1, "A-1", { priority: 1, updated_at: 5 }), t(2, "A-2", { priority: 3, updated_at: 9 })];
    expect(order(rows, "priority", true)).toEqual(["A-2", "A-1"]);
    expect(order(rows, "updated", true)).toEqual(["A-2", "A-1"]);
  });
});
