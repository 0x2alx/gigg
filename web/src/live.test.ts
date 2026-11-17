import { describe, expect, it } from "vitest";
import { backoff, combine, countsChange, treeChange } from "./live";

describe("live helpers", () => {
  it("backs off from 5 s, doubling, capped at 5 min", () => {
    expect([1, 2, 3, 4].map(backoff)).toEqual([5000, 10000, 20000, 40000]);
    expect(backoff(20)).toBe(300000);
    expect(backoff(0)).toBe(5000);
  });
  it("combines stream states", () => {
    expect(combine([])).toBe("off");
    expect(combine(["off", "connecting"])).toBe("connecting");
    expect(combine(["off", "live", "connecting"])).toBe("live");
  });
  it("classifies actions", () => {
    expect(countsChange("gig.moved")).toBe(true);
    expect(countsChange("comment.created")).toBe(true);
    expect(countsChange("label.created")).toBe(false);
    expect(treeChange("project.created")).toBe(true);
    expect(treeChange("member.set")).toBe(true);
    expect(treeChange("gig.created")).toBe(false);
  });
});
