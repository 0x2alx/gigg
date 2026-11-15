import { describe, expect, it } from "vitest";
import type { Schedule } from "./api";
import { defaultRepeat } from "./recurrence";
import { repeatOfSchedule, sameTiming, scheduleBody } from "./scheduleTiming";

const fri: Schedule = {
  id: 3, workspace_id: 1, name: "Fridays", freq: "weekly", interval: 1, weekdays: [5], month_day: null, tz: "UTC",
  window_start: "16:00", window_minutes: 60, lead_minutes: 15, version: 1, uses: 2,
};

describe("schedules", () => {
  it("a schedule round-trips through the repeat form", () => {
    const s = repeatOfSchedule(fri);
    expect(sameTiming(s, fri)).toBe(true);
    expect(scheduleBody(" Fridays ", s)).toEqual({ name: "Fridays", freq: "weekly", interval: 1, tz: "UTC", window_start: "16:00", window_minutes: 60, lead_minutes: 15, weekdays: [5] });
  });
  it("picking a schedule keeps the recurring task's own settings", () => {
    const base = { ...defaultRepeat(), freq: "daily" as const, endMode: "count" as const, count: 3, overlap: "create" as const, paused: true };
    const s = repeatOfSchedule(fri, base);
    expect(sameTiming(s, fri)).toBe(true);
    expect([s.endMode, s.count, s.overlap, s.paused]).toEqual(["count", 3, "create", true]);
  });
  it("any timing change is a different schedule", () => {
    const s = repeatOfSchedule(fri);
    expect(sameTiming({ ...s, start: "17:00" }, fri)).toBe(false);
    expect(sameTiming({ ...s, weekdays: [4, 5] }, fri)).toBe(false);
    expect(sameTiming({ ...s, lead: 0 }, fri)).toBe(false);
    expect(sameTiming({ ...s, tz: "America/New_York" }, fri)).toBe(false);
  });
});
