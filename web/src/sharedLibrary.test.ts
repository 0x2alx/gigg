import { describe, expect, it } from "vitest";
import { canShare, hasAllCap } from "./api";

// The shared library (shared templates, schedules, errand templates) asks for permissions on the
// all-workspaces level: Shared library itself, and the run settings inside a shared item.
describe("shared library capabilities", () => {
  it("admins hold everything", () => {
    expect(canShare({ admin: true })).toBe(true);
    expect(hasAllCap({ admin: true }, "errand_unsandboxed")).toBe(true);
  });
  it("others only what the all-workspaces level gives", () => {
    const me = { admin: false, all_workspaces: ["view", "shared_library", "run_timeout"] };
    expect(canShare(me)).toBe(true);
    expect(hasAllCap(me, "run_timeout")).toBe(true);
    expect(hasAllCap(me, "always_run")).toBe(false);
    expect(canShare({ admin: false, all_workspaces: ["view", "templates"] })).toBe(false);
  });
  it("an older server without the list: admins only", () => {
    expect(canShare({ admin: false })).toBe(false);
    expect(canShare(null)).toBe(false);
  });
});
