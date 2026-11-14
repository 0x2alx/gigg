import { describe, expect, it } from "vitest";
import { badgeCount, kindInfo, notificationText, placePopover } from "./notifications";

describe("notificationText", () => {
  it("names the actor and the task", () => {
    expect(notificationText({ kind: "mention", task_key: "GIG-12" }, "Ada")).toBe("Ada mentioned you on GIG-12");
    expect(notificationText({ kind: "assigned", task_key: "GIG-3" }, "Bot")).toBe("Bot assigned you to GIG-3");
    expect(notificationText({ kind: "question", task_key: "GIG-4" }, null)).toBe("Someone asked a question on GIG-4");
    expect(notificationText({ kind: "comment", task_key: null }, "Ada")).toBe("Ada commented on a task");
  });
  it("has no actor for a missed window", () => {
    expect(notificationText({ kind: "missed", task_key: "GIG-7" }, "Ada")).toBe("GIG-7: the window closed unclaimed");
  });
  it("copes with an unknown kind", () => {
    expect(notificationText({ kind: "weird", task_key: "X-1" }, "Ada")).toBe("Ada updated X-1");
    expect(kindInfo("weird").label).toBe("weird");
  });
  it("gives each kind its own colour", () => {
    const colours = ["mention", "assigned", "question", "missed", "comment"].map((k) => kindInfo(k).color);
    expect(new Set(colours).size).toBe(5);
  });
});

describe("badgeCount", () => {
  it("caps the badge", () => {
    expect(badgeCount(0)).toBe("");
    expect(badgeCount(7)).toBe("7");
    expect(badgeCount(150)).toBe("99+");
  });
});

describe("placePopover", () => {
  const vp = { width: 1280, height: 800 };
  const size = { width: 380, height: 420 };
  it("opens beside the sidebar edge with the notch on the bell", () => {
    const p = placePopover({ top: 100, height: 32 }, 220, size, vp);
    expect(p.left).toBe(232);
    expect(p.top).toBe(92);
    expect(p.arrowY).toBe(24);
  });
  it("stays inside the viewport near the bottom, notch still on the bell", () => {
    const p = placePopover({ top: 700, height: 32 }, 220, size, vp);
    expect(p.top).toBe(800 - 420 - 8);
    expect(p.arrowY).toBe(716 - p.top);
  });
  it("never goes above the top margin", () => {
    const p = placePopover({ top: 0, height: 20 }, 220, size, vp);
    expect(p.top).toBe(8);
    expect(p.arrowY).toBe(12);
  });
  it("clamps into narrow viewports and drops the notch", () => {
    const p = placePopover({ top: 100, height: 32 }, 220, size, { width: 500, height: 800 });
    expect(p.left).toBe(500 - 380 - 8);
    expect(p.arrowY).toBeNull();
  });
});
