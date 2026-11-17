import { describe, expect, it } from "vitest";
import { ruleAt, sourceText, withOverride, withPreset, type AccessRule } from "./access";
import { canWriteIn, hasCap, type Me } from "./api";
import { defaultErrandForm, errandBody, formOfErrand, type Errand } from "./errands";

// a user who is a member of workspace 1 (edits off in project 11) and a consumer of project 21 only
const me: Pick<Me, "capabilities" | "workspaces" | "admin"> = {
  admin: false,
  capabilities: ["view", "comment", "work_gigs", "edit_gigs", "create_gigs"],
  workspaces: [
    {
      id: 1, slug: "home", name: "Home", role: "member", whole: true,
      permissions: ["view", "comment", "work_gigs", "edit_gigs", "create_gigs", "templates"],
      projects: [
        { id: 10, role: "member", permissions: ["view", "comment", "work_gigs", "edit_gigs", "create_gigs"] },
        { id: 11, role: "consumer", permissions: ["view", "comment", "work_gigs"] },
      ],
    },
    { id: 2, slug: "other", name: "Other", role: "", whole: false, permissions: [], projects: [{ id: 21, role: "consumer", permissions: ["view", "comment", "work_gigs"] }] },
  ],
};

describe("project access", () => {
  it("hasCap reads a project's own permissions for the gig permissions", () => {
    expect(hasCap(me, "edit_gigs", 1, 10)).toBe(true);
    expect(hasCap(me, "edit_gigs", 1, 11)).toBe(false);
    expect(hasCap(me, "work_gigs", 2, 21)).toBe(true);
    expect(hasCap(me, "create_gigs", 2, 21)).toBe(false);
    // workspace-level permissions are the whole workspace's, project or not
    expect(hasCap(me, "templates", 1, 11)).toBe(true);
    expect(hasCap(me, "templates", 2, 21)).toBe(false);
    // no project given: the whole workspace
    expect(hasCap(me, "edit_gigs", 2)).toBe(false);
  });

  it("canWriteIn: per project from the access list, else the workspace role; project-only sees nothing whole", () => {
    const home = { role: "member", whole: true, access: me.workspaces[0]!.projects };
    const other = { role: "", whole: false, access: me.workspaces[1]!.projects };
    expect(canWriteIn(home, 10)).toBe(true);
    expect(canWriteIn(home, 11)).toBe(false);
    expect(canWriteIn(home)).toBe(true);
    expect(canWriteIn(other)).toBe(false);
    expect(canWriteIn(other, 21)).toBe(false);
    expect(canWriteIn({ role: "consumer" })).toBe(false);
    expect(canWriteIn({ role: "member" })).toBe(true); // an older server: no access lists
  });

  it("rules for one project", () => {
    const rules: AccessRule[] = [
      { id: 1, workspace_id: 1, preset: "member", overrides: {} },
      { id: 2, workspace_id: 1, project_id: 11, preset: "consumer", overrides: {} },
    ];
    expect(ruleAt(rules, 1)?.id).toBe(1);
    expect(ruleAt(rules, 1, 11)?.id).toBe(2);
    expect(ruleAt(rules, 1, 10)).toBeUndefined();
    expect(withPreset(rules[1], 1, "viewer", 11)).toEqual({ workspace_id: 1, project_id: 11, preset: "viewer", overrides: {} });
    expect(withOverride(rules[1], 1, "comment", false, 11)).toEqual({ workspace_id: 1, project_id: 11, preset: "consumer", overrides: { comment: false } });
    expect(withOverride(undefined, 1, "view", true)).toEqual({ workspace_id: 1, preset: "", overrides: { view: true } });
    expect(sourceText({ source: "user_project" })).toBe("own rule, this project");
    expect(sourceText({ source: "group_project", group: "crew" })).toBe("group crew, this project");
  });

  it("an errand's project: sent when set or changed", () => {
    expect(errandBody({ ...defaultErrandForm(), name: "x", command: "true", project: 7 }, null, false).project).toBe(7);
    expect("project" in errandBody({ ...defaultErrandForm(), name: "x", command: "true" }, null, false)).toBe(false);
    const cur = {
      id: 1, workspace_id: 1, project_id: 7, worker_id: 2, worker_username: "a", created_by: 1, on_failure_gig: false, next_at: null, run_now_at: null,
      paused: false, state: "ok", fail_streak: 0, failing_since: null, last_run: null, created_at: 0, version: 1, template_id: 1, name: "x",
      description_md: "", command: "true", timeout_s: 300, output: "gigg", unsandboxed: false, command_set_by: null, unsandboxed_set_by: null,
      template_uses: 1, schedule_id: 1, schedule_name: "s", schedule_text: "s",
      rule: { freq: "minutes", interval: 15, tz: "UTC", window_start: "00:00", window_minutes: 1440, weekdays: null, month_day: null },
    } as unknown as Errand;
    const f = formOfErrand(cur);
    expect(f.project).toBe(7);
    expect("project" in errandBody(f, cur, false)).toBe(false);
    expect(errandBody({ ...f, project: 8 }, cur, false).project).toBe(8);
  });
});
