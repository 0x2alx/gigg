import { describe, expect, it } from "vitest";
import clumsy from "../../testdata/themes/clumsy-clanker.json";
import daywalker from "../../testdata/themes/daywalker.json";
import nightwalker from "../../testdata/themes/nightwalker.json";
import petty from "../../testdata/themes/petty-officer.json";
import { BUILTINS, contrastProblems, findTheme, firstJSON, fixFor, fromObject, importText, nearestPassing, parseColour, ratio, sane, themeJSON, type Theme } from "./theme";

const current = BUILTINS[1]!;
const byName = (n: string) => BUILTINS.find((t) => t.name.toLowerCase() === n.toLowerCase() || t.id === n.toLowerCase()) ?? findTheme(n);

describe("built-ins", () => {
  it("all pass the contrast pairs", () => {
    for (const t of BUILTINS) expect(contrastProblems(t), t.name).toEqual([]);
  });
  it("Copy JSON then import gives back the same theme", () => {
    for (const t of BUILTINS) {
      const notes: string[] = [];
      const back = fromObject(JSON.parse(themeJSON(t)), current, notes);
      expect({ ...back, id: t.id, description: t.description }).toEqual(t);
      expect(notes).toEqual([]);
    }
  });
});

describe("real app exports (fixtures)", () => {
  for (const [name, fx] of Object.entries({ clumsy, daywalker, nightwalker, petty })) {
    it(`${name} round-trips byte-for-byte`, () => {
      const t = fromObject(fx as Record<string, unknown>, current, []);
      expect(themeJSON(t)).toEqual(JSON.stringify(fx, null, 4));
    });
  }
});

describe("retired ids", () => {
  it("map by id or name, case-insensitive", () => {
    expect(findTheme("graphite")?.id).toBe("nightwalker");
    expect(findTheme("Daylight")?.id).toBe("daywalker");
    expect(findTheme("SIGNATURE")?.id).toBe("clumsy-clanker");
  });
});

describe("parseColour", () => {
  it("accepts every format", () => {
    expect(parseColour("#abc")).toBe("#AABBCC");
    expect(parseColour("abcd")).toBe("#AABBCC");
    expect(parseColour("#1a2b3c")).toBe("#1A2B3C");
    expect(parseColour("80FF5252")).toBe("#FF5252");
    expect(parseColour(0xff1c2520)).toBe("#1C2520");
    expect(parseColour("rgb(255, 82, 82)")).toBe("#FF5252");
    expect(parseColour("blue-ish")).toBeNull();
    expect(parseColour("#12345")).toBeNull();
  });
});

describe("sane", () => {
  it("clamps, trims and forces opaque colours", () => {
    const t = sane({ ...current, name: "  " + "x".repeat(50), textScalePct: 300, cornerDp: -5, borderDp: 9, background: "#80112233", widget: { opacityPct: 150 } } as Theme);
    expect(t.name).toHaveLength(40);
    expect([t.textScalePct, t.cornerDp, t.borderDp]).toEqual([130, 0, 3]);
    expect(t.background).toBe("#112233");
    expect(t.widget?.opacityPct).toBe(100);
    expect(sane({ ...current, name: "  " }).name).toBe("My theme");
  });
});

describe("import", () => {
  it("finds JSON inside an AI's prose and code fences", () => {
    const text = 'Sure! Here is your theme:\n```json\n{"name":"Sunset","accent":"#FF8A3D"}\n```\nEnjoy {not json}.';
    expect(firstJSON(text)).toEqual({ name: "Sunset", accent: "#FF8A3D" });
    const r = importText(text, current, byName);
    expect(r.themes).toHaveLength(1);
    expect(r.themes[0]!.name).toBe("Sunset");
    expect(r.themes[0]!.surface).toBe(current.surface); // missing fields come from the current theme
  });
  it("drops an inherited secondary when the accent changes without one", () => {
    const base: Theme = { ...BUILTINS[3]! }; // Petty Officer has a set secondary
    const t = fromObject({ name: "Purple", accent: "#BE5CFF" }, base, []);
    expect(t.secondary).toBeNull();
    const kept = fromObject({ name: "Same", cornerDp: 4 }, base, []);
    expect(kept.secondary).toBe(base.secondary);
  });
  it("keeps the old value and notes an unreadable colour", () => {
    const notes: string[] = [];
    const t = fromObject({ name: "Sunset", background: "blue-ish" }, current, notes);
    expect(t.background).toBe(current.background);
    expect(notes[0]).toContain('Sunset: background "blue-ish" isn\'t a colour');
  });
  it("accepts arrays, the full state and bare names", () => {
    expect(importText('[{"name":"A"},{"name":"B"}]', current, byName).themes.map((t) => t.name)).toEqual(["A", "B"]);
    const st = importText('{"themes":[{"name":"A"}],"selectedId":"neon","matchPhone":true}', current, byName);
    expect([st.themes.length, st.selectId, st.matchPhone]).toEqual([1, "neon", true]);
    expect(importText('"Daywalker"', current, byName).selectId).toBe("daywalker");
    expect(importText("nope-theme", current, byName).summary).toContain('no theme called "nope-theme"');
  });
  it("can't overwrite a built-in id", () => {
    const t = fromObject({ id: "neon", name: "Fake neon" }, current, []);
    expect(t.id).toBe("");
  });
});

describe("contrast fixes", () => {
  it("Fix never makes another warning on the same colour worse", () => {
    // the QA case: dark grey secondary text on a dark card, blue board columns
    const t = { ...byName("nightwalker")!, mutedText: "#3E4C59", laneColor: "#0090FF" } as Theme;
    const before = contrastProblems(t).find((p) => p.label.startsWith("Secondary text on the board columns"))!;
    const fixed = { ...t, mutedText: fixFor(t, "mutedText") };
    const after = contrastProblems(fixed);
    expect(after.find((p) => p.label.startsWith("Secondary text on cards"))).toBeUndefined();
    const lane = after.find((p) => p.label.startsWith("Secondary text on the board columns"));
    if (lane) expect(lane.have).toBeGreaterThanOrEqual(before.have);
  });
  it("warns when a status chip blends into the columns, and Fix moves the column colour", () => {
    const t = { ...byName("nightwalker")!, laneColor: "#0090FF" } as Theme;
    expect(contrastProblems(t).some((p) => p.label.includes("To-Do status chip"))).toBe(true);
    const fixed = { ...t, laneColor: fixFor(t, "laneColor") };
    expect(contrastProblems(fixed).filter((p) => p.fg === "laneColor")).toEqual([]);
  });
  it("nearestPassing reaches the minimum", () => {
    const fixed = nearestPassing("#777777", "#808080", 4.5);
    expect(ratio(fixed, "#808080")).toBeGreaterThanOrEqual(4.5);
    const notes: string[] = [];
    fromObject({ name: "Bad", text: "#1C2529" }, current, notes);
    expect(notes.some((n) => n.includes("hard to read"))).toBe(true);
  });
});

describe("gigg extras (control shape, board columns)", () => {
  it("round-trip through Copy JSON and are left out when unset or invalid", () => {
    const t = { ...BUILTINS[0]!, controlShape: "pill" as const, laneColor: "#112233", laneBorder: "#445566", logoColor: "#778899" };
    const json = themeJSON(t);
    expect(json).toContain('"controlShape": "pill"');
    const back = fromObject(JSON.parse(json), current, []);
    expect([back.controlShape, back.laneColor, back.laneBorder, back.logoColor]).toEqual(["pill", "#112233", "#445566", "#778899"]);
    const notes: string[] = [];
    const bad = fromObject({ ...JSON.parse(json), controlShape: "blob", laneColor: "nope" }, current, notes);
    expect(bad.controlShape).toBeUndefined();
    expect(bad.laneColor).toBeUndefined();
    expect(notes.some((n) => n.includes("laneColor"))).toBe(true);
    expect(themeJSON({ ...findTheme("nightwalker")! })).not.toContain("lane");
  });
});
