import { describe, expect, it } from "vitest";
import type { WorkspaceTree } from "./App";
import { optionOf, placeMatch, placeStart } from "./places";

const ws = (id: number, pids: number[], role = "member") => ({ id, slug: `w${id}`, name: `W${id}`, role, projects: pids.map((p) => ({ id: p, key: `P${p}`, name: `p${p}` })) }) as unknown as WorkspaceTree;
const tree = [ws(1, [10, 11]), ws(2, [20], "viewer"), ws(3, [30])];

describe("placeMatch", () => {
  it("shows everything with no filter", () => {
    expect(placeMatch("", tree, 2, 20)).toBe(true);
  });
  it("narrows to a workspace", () => {
    expect(placeMatch("w:1", tree, 1, 11)).toBe(true);
    expect(placeMatch("w:1", tree, 3, 30)).toBe(false);
  });
  it("narrows to a project, keeping its workspace's any-project items", () => {
    expect(placeMatch("p:10", tree, 1, 10)).toBe(true);
    expect(placeMatch("p:10", tree, 1, 11)).toBe(false);
    expect(placeMatch("p:10", tree, 1, null)).toBe(true);
    expect(placeMatch("p:10", tree, 3, null)).toBe(false);
  });
});

describe("placeStart", () => {
  const ok = (w: WorkspaceTree) => w.role !== "viewer";
  it("starts in the filtered project, else the workspace's first, else the first allowed", () => {
    expect(placeStart("p:11", tree, ok)).toMatchObject({ projectId: 11 });
    expect(placeStart("w:3", tree, ok)).toMatchObject({ projectId: 30 });
    expect(placeStart("", tree, ok)).toMatchObject({ projectId: 10 });
  });
  it("skips places that aren't allowed", () => {
    expect(placeStart("w:2", tree, ok)).toMatchObject({ projectId: 10 });
    expect(placeStart("", [tree[1]!], ok)).toBeNull();
  });
});

describe("optionOf", () => {
  it("shows a project of a one-project workspace as the workspace", () => {
    expect(optionOf("p:20", tree)).toBe("w:2");
    expect(optionOf("p:10", tree)).toBe("p:10");
    expect(optionOf("p:10", tree, false)).toBe("w:1");
    expect(optionOf("p:99", tree)).toBe("");
    expect(optionOf("w:3", tree)).toBe("w:3");
  });
});
