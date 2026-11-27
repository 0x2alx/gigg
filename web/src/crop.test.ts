import { describe, expect, it } from "vitest";
import { centred, clampOffset, clampZoom, coverScale, sourceSquare, zoomAround } from "./crop";

describe("avatar crop maths", () => {
  const box = 200;

  it("zoom 1 covers the box with the short side", () => {
    expect(coverScale(1440, 900, box)).toBeCloseTo(200 / 900);
    expect(coverScale(300, 600, box)).toBeCloseTo(200 / 300);
  });

  it("zoom stays between 1 and 4", () => {
    expect(clampZoom(0.2)).toBe(1);
    expect(clampZoom(9)).toBe(4);
    expect(clampZoom(2.5)).toBe(2.5);
  });

  it("a centred wide image shows its middle square", () => {
    const s = coverScale(1440, 900, box);
    const off = centred(1440, 900, s, box);
    const sq = sourceSquare(off, s, box);
    expect(sq.size).toBeCloseTo(900);
    expect(sq.sx).toBeCloseTo(270);
    expect(sq.sy).toBeCloseTo(0);
  });

  it("the image can't be dragged off the box", () => {
    const s = coverScale(1440, 900, box); // shown 320 × 200
    expect(clampOffset({ x: 50, y: 10 }, 1440, 900, s, box)).toEqual({ x: 0, y: 0 });
    const far = clampOffset({ x: -999, y: -999 }, 1440, 900, s, box);
    expect(far.x).toBeCloseTo(-120);
    expect(far.y).toBeCloseTo(0);
    const sq = sourceSquare(far, s, box);
    expect(sq.sx + sq.size).toBeCloseTo(1440); // the right edge, not past it
  });

  it("zooming around the centre keeps the centre on the same image point", () => {
    const s1 = coverScale(1000, 1000, box);
    const off1 = centred(1000, 1000, s1, box);
    const s2 = s1 * 2;
    const off2 = zoomAround(off1, s1, s2, { x: box / 2, y: box / 2 });
    const a = sourceSquare(off1, s1, box), b = sourceSquare(off2, s2, box);
    expect(a.sx + a.size / 2).toBeCloseTo(b.sx + b.size / 2);
    expect(b.size).toBeCloseTo(a.size / 2);
  });
});
