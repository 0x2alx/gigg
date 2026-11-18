import { describe, expect, it } from "vitest";
import { launchOn } from "./launchPref";

describe("launch animation preference", () => {
  it("is on unless turned off", () => {
    expect(launchOn({})).toBe(true);
    expect(launchOn({ launch_animation: true })).toBe(true);
    expect(launchOn({ launch_animation: false })).toBe(false);
    expect(launchOn({ launch_animation: null })).toBe(true);
  });
});
