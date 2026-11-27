import { describe, expect, it } from "vitest";
import { rovingNext, swatchList } from "./swatches";

describe("swatch groups", () => {
  const presets = ["#111111", "#222222", "#333333"];

  it("a preset colour keeps the presets' order (the picked one doesn't jump to the front)", () => {
    expect(swatchList("#222222", presets)).toEqual(presets);
    expect(swatchList("#333333", presets)).toEqual(presets);
  });

  it("a colour that isn't a preset comes first, upper-cased, without duplicates", () => {
    expect(swatchList("#abcdef", presets)).toEqual(["#ABCDEF", ...presets]);
    expect(swatchList("#222222", ["#222222", "#222222"])).toEqual(["#222222"]);
    expect(swatchList("#22222a", ["#22222A"])).toEqual(["#22222A"]);
  });

  it("arrow keys walk the row and wrap; Home and End jump; other keys are left alone", () => {
    expect(rovingNext("ArrowRight", 0, 3)).toBe(1);
    expect(rovingNext("ArrowDown", 2, 3)).toBe(0);
    expect(rovingNext("ArrowLeft", 0, 3)).toBe(2);
    expect(rovingNext("ArrowUp", 2, 3)).toBe(1);
    expect(rovingNext("Home", 2, 3)).toBe(0);
    expect(rovingNext("End", 0, 3)).toBe(2);
    expect(rovingNext("Enter", 0, 3)).toBe(-1);
    expect(rovingNext("ArrowRight", 0, 0)).toBe(-1);
  });
});
