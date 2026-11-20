import { describe, expect, it } from "vitest";
import { BELL_SETTINGS, FOLLOW_OWN, prefOn, prefValue } from "./bellPrefs";

describe("bell settings", () => {
  it("read defaults when unset", () => {
    const on = Object.fromEntries(BELL_SETTINGS.map((b) => [b.pref, prefOn({}, b.pref, b.def)]));
    expect(on).toEqual({ bell_problems: true, bell_finished: false, bell_assigned: true, bell_mentioned: true, bell_missed: true });
    expect(prefOn({}, FOLLOW_OWN.pref, FOLLOW_OWN.def)).toBe(true);
  });
  it("read a saved choice, ignore junk", () => {
    expect(prefOn({ bell_finished: true }, "bell_finished", false)).toBe(true);
    expect(prefOn({ bell_finished: "yes" }, "bell_finished", false)).toBe(false);
  });
  it("save only what differs from the default", () => {
    expect(prefValue(true, true)).toBeUndefined();
    expect(prefValue(false, true)).toBe(false);
    expect(prefValue(true, false)).toBe(true);
  });
});
