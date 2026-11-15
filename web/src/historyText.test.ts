import { describe as group, expect, it } from "vitest";
import { describe } from "./historyText";
import type { User } from "./api";

const users = new Map<number, User>([[2, { id: 2, username: "bob", display_name: "Bob" } as User]]);
const say = (action: string, data: unknown = null) => describe({ action, data }, users);

group("history lines", () => {
  it("reads task edits field by field", () => {
    expect(say("task.updated", { title: { old: "a", new: "Fix login" }, priority: { old: 1, new: 3 } })).toBe(
      "renamed it to “Fix login”, set priority to High (was Low)",
    );
    expect(say("task.updated", { model: { old: "", new: "opus" }, effort: { old: "", new: "high" } })).toBe("changed the agent run settings");
    expect(say("task.updated", { assignees: [2] })).toBe("assigned Bob");
    expect(say("task.updated", { due_at: null })).toBe("cleared the due date");
    expect(say("task.updated", { custom_fields: {} })).toBe("changed custom fields");
  });

  it("reads moves, links and the rest", () => {
    expect(say("task.moved", { from_status: "Doing", to_status: "Done" })).toBe("moved it from Doing to Done");
    expect(say("task.moved", { from_status: "To-Do", to_status: "To-Do", new_key: "OPS-4" })).toBe("moved it to another project (now OPS-4)");
    expect(say("relation.added", { to: "GIG-4", kind: "blocks" })).toBe("linked it: blocks GIG-4");
    expect(say("checklist.added", { items: ["a", "b"] })).toBe("added 2 checklist items");
    expect(say("run.finished", { status: "needs_review" })).toBe("finished an agent run (needs review)");
    expect(say("task.custom", { Team: "Backend" })).toBe("set Team");
    expect(say("task.custom", { Team: null })).toBe("cleared Team");
    expect(say("something.new")).toBe("something new");
  });
});
