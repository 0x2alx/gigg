import { describe, expect, it } from "vitest";
import { SHORTCUTS, shortcutFor, typingIn } from "./keys";

const press = (key: string, mods: Partial<{ ctrlKey: boolean; metaKey: boolean; altKey: boolean; isComposing: boolean; defaultPrevented: boolean }> = {}) => ({
  key,
  ctrlKey: false,
  metaKey: false,
  altKey: false,
  ...mods,
});
const body = { tagName: "BODY" };

describe("keyboard shortcuts", () => {
  it("?, / and n are the app-wide keys", () => {
    expect(shortcutFor(press("?"), body)).toBe("help");
    expect(shortcutFor(press("/"), body)).toBe("search");
    expect(shortcutFor(press("n"), body)).toBe("new");
    expect(shortcutFor(press("N"), body)).toBe("new");
    expect(shortcutFor(press("x"), body)).toBeNull();
  });

  it("they stay quiet while you type in a field or the rich editor", () => {
    for (const t of [{ tagName: "INPUT" }, { tagName: "INPUT", type: "search" }, { tagName: "TEXTAREA" }, { tagName: "SELECT" }, { tagName: "DIV", isContentEditable: true }]) {
      expect(typingIn(t), JSON.stringify(t)).toBe(true);
      expect(shortcutFor(press("n"), t)).toBeNull();
      expect(shortcutFor(press("/"), t)).toBeNull();
    }
  });

  it("a focused button, checkbox or link doesn't count as typing", () => {
    for (const t of [{ tagName: "BUTTON" }, { tagName: "A" }, { tagName: "INPUT", type: "checkbox" }, { tagName: "INPUT", type: "radio" }, null]) {
      expect(typingIn(t)).toBe(false);
      expect(shortcutFor(press("?"), t)).toBe("help");
    }
  });

  it("chords, composition and handled keys are left alone", () => {
    expect(shortcutFor(press("n", { ctrlKey: true }), body)).toBeNull();
    expect(shortcutFor(press("n", { metaKey: true }), body)).toBeNull();
    expect(shortcutFor(press("/", { altKey: true }), body)).toBeNull();
    expect(shortcutFor(press("n", { isComposing: true }), body)).toBeNull();
    expect(shortcutFor(press("?", { defaultPrevented: true }), body)).toBeNull();
  });

  it("the overlay lists every app-wide key", () => {
    const listed = SHORTCUTS.flatMap((g) => g.items.flatMap((s) => s.keys.map((k) => k.join("+"))));
    for (const k of ["?", "/", "N", "Ctrl+K", "Esc"]) expect(listed).toContain(k);
  });
});
