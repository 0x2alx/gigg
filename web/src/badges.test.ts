import { describe, expect, it } from "vitest";
import { BADGE_TINT, BUILTINS, DATA_COLORS, badgeTextShare, mix, ratio, strongWeight } from "./theme";

// A tinted badge (see theme.css .pill): text = text colour pulled `share`% toward the data colour, on
// the surface tinted 14% with that colour.
const badge = (t: { surface: string; text: string }, c: string, share: number) => ratio(mix(t.text, c, share / 100), mix(t.surface, c, BADGE_TINT));

describe("tinted badges", () => {
  for (const t of BUILTINS) {
    it(`${t.name}: every data colour reads at 4.5:1`, () => {
      const share = badgeTextShare(t);
      expect(share).toBeGreaterThan(0);
      for (const c of [...DATA_COLORS, t.accent, t.danger, t.mutedText]) expect(badge(t, c, share), `${t.name} ${c} at ${share}%`).toBeGreaterThanOrEqual(4.5);
    });
  }
  it("yellow on a light surface falls back to less hue; dark themes keep the full 55%", () => {
    const day = BUILTINS.find((t) => t.id === "daywalker")!;
    expect(badge(day, "#FFC53D", 55)).toBeLessThan(4.5);
    expect(badgeTextShare(day)).toBeLessThan(55);
    expect(badgeTextShare(BUILTINS.find((t) => t.id === "nightwalker")!)).toBe(55);
  });
  it("a theme whose text is unreadable on every tint gets no hue at all", () => {
    expect(badgeTextShare({ surface: "#FFFFFF", text: "#FFFFFF", accent: "#FFFFFF", danger: "#FFFFFF", mutedText: "#FFFFFF" })).toBe(0);
  });
});

describe("strongWeight", () => {
  it("is 600 unless the theme is Light or Bold", () => {
    expect([strongWeight("Light"), strongWeight("Regular"), strongWeight("Medium"), strongWeight("Bold")]).toEqual([500, 600, 600, 700]);
  });
});
