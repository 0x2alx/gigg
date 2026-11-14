import { beforeEach, describe, expect, it } from "vitest";
import { boardColumns, loadCollapsed, saveCollapsed, toggleId } from "./collapse";

// a Map-backed storage, so the test runs the same under any environment
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

describe("collapsed columns", () => {
  beforeEach(() => localStorage.clear());
  it("round-trips per project and drops junk", () => {
    saveCollapsed(1, [3, 5]);
    expect(loadCollapsed(1)).toEqual([3, 5]);
    expect(loadCollapsed(2)).toEqual([]);
    localStorage.setItem("gigg.board.collapsed.4", '[1,"x",2.5,7]');
    expect(loadCollapsed(4)).toEqual([1, 7]);
    localStorage.setItem("gigg.board.collapsed.4", "{oops");
    expect(loadCollapsed(4)).toEqual([]);
    saveCollapsed(1, []);
    expect(localStorage.getItem("gigg.board.collapsed.1")).toBeNull();
  });
  it("toggles and builds the grid", () => {
    expect(toggleId([1, 2], 2)).toEqual([1]);
    expect(toggleId([1], 2)).toEqual([1, 2]);
    expect(boardColumns([1, 2, 3], [2])).toBe("minmax(0, 1fr) 2.6rem minmax(0, 1fr)");
  });
});
