import { describe, expect, it } from "vitest";
import type { CustomField, Template } from "./api";
import { activeMention, cardChips, cleanCustom, formatCustom, fromInput, insertMention, matchUsers, moveAfter, reorder, sameAsTemplate, splitLines, splitMentions } from "./items";

describe("mentions", () => {
  it("finds the @query at the caret", () => {
    expect(activeMention("hi @bo", 6)).toEqual({ start: 3, query: "bo" });
    expect(activeMention("@", 1)).toEqual({ start: 0, query: "" });
    expect(activeMention("(@al", 4)).toEqual({ start: 1, query: "al" });
    expect(activeMention("mail me@host", 12)).toBeNull();
    expect(activeMention("hi @bob there", 13)).toBeNull();
    expect(activeMention("no at here", 5)).toBeNull();
  });
  it("inserts the username and a space", () => {
    expect(insertMention("hi @bo", 3, 6, "bob")).toEqual({ text: "hi @bob ", caret: 8 });
    expect(insertMention("hi @b there", 3, 5, "bob")).toEqual({ text: "hi @bob there", caret: 7 });
  });
  it("ranks prefix matches first", () => {
    const us = [
      { username: "zed", display_name: "Bob Zed" },
      { username: "bob", display_name: "Robert" },
      { username: "abob", display_name: "" },
      { username: "carl", display_name: "" },
    ];
    expect(matchUsers(us, "bo").map((u) => u.username)).toEqual(["bob", "zed", "abob"]);
    expect(matchUsers(us, "").length).toBe(4);
  });
  it("splits known mentions only", () => {
    const known = (n: string) => ["bob", "atlas"].includes(n.toLowerCase());
    expect(splitMentions("hey @bob, ask @nobody and @ATLAS.", known)).toEqual([
      { text: "hey " },
      { text: "@bob", user: "bob" },
      { text: ", ask @nobody and " },
      { text: "@ATLAS", user: "ATLAS" },
      { text: "." },
    ]);
    expect(splitMentions("mail bob@bob.com", known)).toEqual([{ text: "mail bob@bob.com" }]);
    expect(splitMentions("@bob", known)).toEqual([{ text: "@bob", user: "bob" }]);
  });
});

describe("checklist", () => {
  it("splits pasted lines and drops bullets", () => {
    expect(splitLines("- one\n\n* [x] two\n3. three\n  four  ")).toEqual(["one", "two", "three", "four"]);
  });
  it("computes the after id for up/down", () => {
    const ids = [10, 20, 30, 40];
    expect(moveAfter(ids, 1, -1)).toBe(0); // to the top
    expect(moveAfter(ids, 2, -1)).toBe(10);
    expect(moveAfter(ids, 0, 1)).toBe(20);
    expect(moveAfter(ids, 2, 1)).toBe(40);
    expect(moveAfter(ids, 0, -1)).toBeNull();
    expect(moveAfter(ids, 3, 1)).toBeNull();
  });
  it("reorders locally", () => {
    expect(reorder([1, 2, 3], 0, 1)).toEqual([2, 1, 3]);
    expect(reorder([1, 2, 3], 2, -1)).toEqual([1, 3, 2]);
    expect(reorder([1, 2, 3], 0, -1)).toEqual([1, 2, 3]);
  });
});

describe("custom fields", () => {
  const f = (id: number, kind: CustomField["kind"], name = "F" + id): CustomField => ({ id, workspace_id: 1, name, kind, options: kind === "select" ? ["a", "b"] : null, position: id });
  it("formats values", () => {
    expect(formatCustom(f(1, "checkbox"), true)).toBe("✓");
    expect(formatCustom(f(1, "checkbox"), false)).toBe("");
    expect(formatCustom(f(1, "text"), null)).toBe("");
    expect(formatCustom(f(1, "url"), "https://example.com/a/b")).toBe("example.com/a/b");
    expect(formatCustom(f(1, "select"), "a")).toBe("a");
  });
  it("parses inputs", () => {
    expect(fromInput(f(1, "number"), "4.5")).toBe(4.5);
    expect(fromInput(f(1, "number"), "x")).toBeUndefined();
    expect(fromInput(f(1, "number"), "")).toBeNull();
    expect(fromInput(f(1, "url"), "ftp://x")).toBeUndefined();
    expect(fromInput(f(1, "checkbox"), "true")).toBe(true);
    expect(typeof fromInput(f(1, "date"), "2026-11-12")).toBe("number");
  });
  it("picks at most two card chips", () => {
    const fields = [f(1, "text"), f(2, "select", "Size"), f(3, "checkbox", "Billable"), f(4, "select")];
    const chips = cardChips({ custom: { "1": "x", "2": "b", "3": true, "4": "a" } }, fields);
    expect(chips.map((c) => c.text)).toEqual(["b", "✓ Billable"]);
    expect(cardChips({ custom: null }, fields)).toEqual([]);
  });
  it("cleans empty values", () => {
    expect(cleanCustom({ "1": "", "2": null, "3": 0, "4": false })).toEqual({ "3": 0 });
    expect(cleanCustom({})).toBeUndefined();
  });
});

describe("sameAsTemplate", () => {
  const t: Template = {
    id: 1, workspace_id: 1, project_id: null, name: "Brief", title: "Daily brief", description_md: "News", priority: 2, color: null,
    labels: ["ops"], assignees: [3, 2], checklist: ["news", "weather"], subtasks: [{ title: "gather", blocks_parent: true }], custom: { "4": "x", "5": false }, version: 1, model: "sonnet", effort: "", always_run: true, allowed_tools: ["Read"],
  };
  const form = {
    title: "Daily brief", description: "News", priority: 2, taskLabels: ["ops"], assignees: [2, 3], color: null,
    run: { model: "sonnet", effort: "", always_run: true, allowed_tools: ["Read"] }, checklist: ["news", "weather"], subtasks: [{ title: "gather ", blocks_parent: true }], custom: { "4": "x" } as Record<string, unknown>,
  };
  it("matches the template as filled in (assignee order, spaces around subtask titles and empty custom values don't count)", () => {
    expect(sameAsTemplate(t, form)).toBe(true);
  });
  it("any change gives the schedule its own copy", () => {
    expect(sameAsTemplate(t, { ...form, title: "Brief" })).toBe(false);
    expect(sameAsTemplate(t, { ...form, checklist: ["news"] })).toBe(false);
    expect(sameAsTemplate(t, { ...form, subtasks: [{ title: "gather" }] })).toBe(false);
    expect(sameAsTemplate(t, { ...form, subtasks: [] })).toBe(false);
    expect(sameAsTemplate(t, { ...form, run: { ...form.run, always_run: false } })).toBe(false);
    expect(sameAsTemplate(t, { ...form, run: { ...form.run, allowed_tools: [] } })).toBe(false);
    expect(sameAsTemplate(t, { ...form, custom: { "4": "y" } })).toBe(false);
  });
});
