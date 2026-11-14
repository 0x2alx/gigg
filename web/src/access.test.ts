import { describe, expect, it } from "vitest";
import { ruleAt, ruleSummary, sections, sourceText, triLabel, withOverride, withPreset, type AccessRule, type Permission } from "./access";

const perms: Permission[] = [
  { name: "view", label: "See the workspace", description: "", section: "Basics" },
  { name: "create_tasks", label: "Create tasks", description: "", section: "Tasks" },
  { name: "delete_tasks", label: "Delete tasks", description: "", section: "Tasks" },
  { name: "schedules", label: "Recurring tasks", description: "", section: "Planning" },
];
const rule = (ws: number | null, preset: string, overrides: Record<string, boolean> = {}): AccessRule => ({ id: 1, workspace_id: ws, preset, overrides });

describe("rules", () => {
  it("finds the rule of a level", () => {
    const rs = [rule(null, "viewer"), rule(3, "member")];
    expect(ruleAt(rs, null)?.preset).toBe("viewer");
    expect(ruleAt(rs, 3)?.preset).toBe("member");
    expect(ruleAt(rs, 4)).toBeUndefined();
  });
  it("sets and clears one override, keeping the preset and the rest", () => {
    const r = rule(3, "member", { schedules: true });
    expect(withOverride(r, 3, "delete_tasks", false)).toEqual({ workspace_id: 3, preset: "member", overrides: { schedules: true, delete_tasks: false } });
    expect(withOverride(r, 3, "schedules", null)).toEqual({ workspace_id: 3, preset: "member", overrides: {} });
    expect(withOverride(undefined, null, "view", true)).toEqual({ workspace_id: null, preset: "", overrides: { view: true } });
    expect(withPreset(r, 3, "manager")).toEqual({ workspace_id: 3, preset: "manager", overrides: { schedules: true } });
  });
  it("summarises a rule in catalogue order", () => {
    expect(ruleSummary(rule(3, "member", { schedules: true, delete_tasks: false }), perms)).toBe("Member, −Delete tasks, +Recurring tasks");
    expect(ruleSummary(undefined, perms)).toBe("no rule");
    expect(ruleSummary(rule(null, ""), perms)).toBe("no rule");
  });
  it("names sources", () => {
    expect(sourceText({ source: "user_all" })).toBe("own rule, all workspaces");
    expect(sourceText({ source: "group", group: "agents" })).toBe("group agents, this workspace");
    expect(sourceText({ source: "needs_view" })).toBe("can't see the workspace");
    expect(sourceText({ source: "default" })).toBe("no rule: off");
    expect(triLabel(null, "Preset ✓")).toBe("Preset ✓");
  });
  it("groups permissions by section", () => {
    expect(sections(perms).map((s) => [s.section, s.items.length])).toEqual([["Basics", 1], ["Tasks", 2], ["Planning", 1]]);
  });
});
