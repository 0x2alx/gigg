import { beforeEach, describe, expect, it } from "vitest";
import { loadSorts, saveSorts, sortGigs, sortsFor } from "./columnSort";

const mem = new Map<string, string>();
Object.defineProperty(globalThis, "localStorage", {
  configurable: true,
  value: {
    getItem: (k: string) => mem.get(k) ?? null,
    setItem: (k: string, v: string) => void mem.set(k, String(v)),
    removeItem: (k: string) => void mem.delete(k),
    clear: () => mem.clear(),
  },
});

const g = (title: string, created_at: number, done_at: number | null = null, due_at: number | null = null, priority = 0) => ({
  title,
  created_at,
  updated_at: created_at,
  done_at,
  due_at,
  priority,
});

describe("column sort", () => {
  beforeEach(() => localStorage.clear());
  const gigs = [g("beta", 2, 20), g("Alpha", 3, null, 5), g("gamma 10", 1, 30, 1, 3), g("gamma 9", 4)];
  const titles = (xs: { title: string }[]) => xs.map((x) => x.title);

  it("keeps the board order for manual", () => {
    expect(sortGigs(gigs, "manual")).toBe(gigs);
  });
  it("sorts names case-blind and numerically", () => {
    expect(titles(sortGigs(gigs, "name-asc"))).toEqual(["Alpha", "beta", "gamma 9", "gamma 10"]);
    expect(titles(sortGigs(gigs, "name-desc"))).toEqual(["gamma 10", "gamma 9", "beta", "Alpha"]);
  });
  it("sorts by dates, missing ones last both ways", () => {
    expect(titles(sortGigs(gigs, "created-desc"))).toEqual(["gamma 9", "Alpha", "beta", "gamma 10"]);
    expect(titles(sortGigs(gigs, "done-desc"))).toEqual(["gamma 10", "beta", "Alpha", "gamma 9"]);
    expect(titles(sortGigs(gigs, "done-asc"))).toEqual(["beta", "gamma 10", "Alpha", "gamma 9"]);
    expect(titles(sortGigs(gigs, "due-asc"))).toEqual(["gamma 10", "Alpha", "beta", "gamma 9"]);
  });
  it("sorts by priority, ties in board order", () => {
    expect(titles(sortGigs(gigs, "priority-desc"))).toEqual(["gamma 10", "beta", "Alpha", "gamma 9"]);
  });
  it("offers the done sorts only in done columns", () => {
    expect(sortsFor("done")).toContain("done-desc");
    expect(sortsFor("todo")).not.toContain("done-desc");
  });
  it("remembers per board, drops manual and junk", () => {
    saveSorts(7, { cdone: "done-desc", ctodo: "manual" });
    expect(loadSorts(7)).toEqual({ cdone: "done-desc" });
    expect(loadSorts(8)).toEqual({});
    localStorage.setItem("gigg.board.sort.9", '{"a":"nope","b":"name-asc"}');
    expect(loadSorts(9)).toEqual({ b: "name-asc" });
    saveSorts(7, {});
    expect(localStorage.getItem("gigg.board.sort.7")).toBeNull();
  });
});
