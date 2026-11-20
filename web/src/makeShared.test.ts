import { describe, expect, it } from "vitest";
import { byWorkspace, doneMessage, planSummary, usersText } from "./makeShared";
import type { ShareItem, ShareUser } from "./api";

const u = (kind: ShareUser["kind"], id: number, workspace: string): ShareUser => ({ kind, id, workspace_id: 1, workspace, label: "x", from: 1 });
const item = (id: number): ShareItem => ({ id, workspace_id: 1, workspace: "A", name: "n", recurring_gigs: 0, errands: 0 });

describe("make shared plan text", () => {
  it("counts users by kind", () => {
    expect(usersText([])).toBe("");
    expect(usersText([u("recurring_gig", 1, "A")])).toBe("1 recurring gig");
    expect(usersText([u("recurring_gig", 1, "A"), u("recurring_gig", 2, "A"), u("errand", 3, "B")])).toBe("2 recurring gigs and 1 errand");
  });
  it("sums the plan up", () => {
    expect(planSummary({ kind: "schedule", copies: [], users: [] })).toBe("No identical schedule to merge. No recurring gig or errand uses it yet.");
    expect(planSummary({ kind: "template", copies: [item(1), item(2)], users: [u("errand", 1, "A")] })).toBe("Merges 2 identical copies into it. 1 errand moves to the shared template.");
  });
  it("groups the users by workspace", () => {
    const g = byWorkspace([u("errand", 1, "A"), u("errand", 2, "B"), u("recurring_gig", 3, "A")]);
    expect(g.map((x) => [x.workspace, x.users.length])).toEqual([["A", 2], ["B", 1]]);
  });
  it("words the toast", () => {
    expect(doneMessage({ kind: "schedule", new_name: "Mornings", copies: [item(1)], users: [u("recurring_gig", 1, "A")] })).toBe("Shared schedule “Mornings” made, 1 copy merged, 1 recurring gig moved");
    expect(doneMessage({ kind: "errand_template", new_name: "ping", copies: [], users: [] })).toBe("Shared errand template “ping” made");
  });
});
