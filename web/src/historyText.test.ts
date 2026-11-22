import { describe as group, expect, it } from "vitest";
import { describe } from "./historyText";
import type { User } from "./api";

const users = new Map<number, User>([[2, { id: 2, username: "bob", display_name: "Bob" } as User]]);
const say = (action: string, data: unknown = null) => describe({ action, data }, users);

group("history lines", () => {
  it("reads gig edits field by field", () => {
    expect(say("gig.updated", { title: { old: "a", new: "Fix login" }, priority: { old: 1, new: 3 } })).toBe(
      "renamed it to “Fix login”, set priority to High (was Low)",
    );
    expect(say("gig.updated", { model: { old: "", new: "opus" }, effort: { old: "", new: "high" } })).toBe("changed the agent run settings");
    expect(say("gig.updated", { assignees: [2] })).toBe("assigned Bob");
    expect(say("gig.updated", { due_at: null })).toBe("cleared the due date");
    expect(say("gig.updated", { custom_fields: {} })).toBe("changed custom fields");
    // entries with old and new for every field
    expect(say("gig.updated", { due_at: { old: 5, new: null }, color: { old: null, new: "#FF0000" } })).toBe("cleared the due date, changed the colour");
    expect(say("gig.updated", { color: { old: "#FF0000", new: null } })).toBe("removed the colour");
    expect(say("gig.updated", { labels: { old: [], new: [{ id: 1, name: "ui", color: null }] }, assignees: { old: [], new: [2] } })).toBe(
      "set labels to ui, assigned Bob",
    );
    expect(say("gig.updated", { parent_id: { old: 4, new: null } })).toBe("removed the parent gig");
    expect(say("gig.updated", { followers: { old: [], new: [2] } })).toBe("added Bob as follower");
    expect(say("gig.updated", { followers: { old: [2], new: [] } })).toBe("removed Bob from the followers");
    expect(say("gig.updated", { not_after: { old: null, new: 9 }, missed_at: { old: 3, new: null } })).toMatch(/^set the window close to /);
    expect(say("gig.updated", { labels: ["bug"] })).toBe("set labels to bug");
  });

  it("reads moves, links and the rest", () => {
    expect(say("gig.moved", { from_status: "Doing", to_status: "Done" })).toBe("moved it from Doing to Done");
    expect(say("gig.moved", { from_status: "To-Do", to_status: "To-Do", new_key: "OPS-4" })).toBe("moved it to another project (now OPS-4)");
    expect(say("relation.added", { to: "GIG-4", kind: "blocks" })).toBe("linked it: blocks GIG-4");
    expect(say("checklist.added", { items: ["a", "b"] })).toBe("added 2 checklist items");
    expect(say("run.finished", { status: "needs_review" })).toBe("finished an agent run (needs review)");
    expect(say("gig.custom", { Team: "Backend" })).toBe("set Team");
    expect(say("gig.custom", { Team: null })).toBe("cleared Team");
    expect(say("attachment.created", { gig_id: 3, kind: "file", filename: "report.md" })).toBe("attached a file “report.md”");
    expect(say("attachment.created", { gig_id: 3, kind: "link", value: "https://example.com/pr/7" })).toBe("attached a link “https://example.com/pr/7”");
    expect(say("attachment.created", { gig_id: 3, kind: "commit", value: "9e29e9c0ffee1234" })).toBe("attached a commit “9e29e9c”");
    expect(say("attachment.deleted", { gig_id: 3, kind: "file", filename: "shot.png" })).toBe("removed a file “shot.png”");
    expect(say("attachment.deleted", null)).toBe("removed a file");
    expect(say("something.new")).toBe("something new");
  });
});
