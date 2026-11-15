import { describe, expect, it } from "vitest";
import { badgeCount, kindInfo, notificationText, placeBelow } from "./notifications";

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

describe("placeBelow", () => {
  const vp = { width: 1280, height: 800 };
  const size = { width: 380, height: 420 };
  it("hangs under the bell, right edges in line, notch on the bell", () => {
    const p = placeBelow({ left: 1200, right: 1236, bottom: 48 }, size, vp);
    expect(p.top).toBe(58);
    expect(p.left).toBe(1236 - 380);
    expect(p.arrowX).toBe(1218 - p.left);
  });
  it("stays inside the left margin on a narrow viewport and drops the notch when it can't point", () => {
    const p = placeBelow({ left: 10, right: 46, bottom: 48 }, size, { width: 400, height: 800 });
    expect(p.left).toBe(8);
    expect(p.arrowX).toBe(20);
    const q = placeBelow({ left: 0, right: 12, bottom: 48 }, size, { width: 400, height: 800 });
    expect(q.arrowX).toBeNull();
  });
  it("moves up on a short viewport and then drops the notch", () => {
    const p = placeBelow({ left: 1200, right: 1236, bottom: 48 }, size, { width: 1280, height: 450 });
    expect(p.top).toBe(450 - 420 - 8);
    expect(p.arrowX).toBeNull();
  });
});
