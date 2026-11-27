import { describe, expect, it } from "vitest";
import {
  clock,
  defaultRepeat,
  describeRule,
  fmtLead,
  sameAsRule,
  durText,
  fmtInstant,
  fmtWallWindow,
  fromDateTimeInput,
  numIn,
  ordinal,
  repeatIssue,
  previewOccurrences,
  repeatFromRule,
  repeatProblem,
  repeatRule,
  ruleOf,
  toDateTimeInput,
  windowBadge,
  windowText,
  type Rule,
} from "./recurrence";

// Thu 12 Nov 2026 15:00 UTC
const NOW = Date.UTC(2026, 10, 12, 15, 0);
const base: Rule = { freq: "daily", interval: 1, weekdays: null, month_day: null, tz: "UTC", window_start: "09:00", window_minutes: 120, starts_on: NOW };
const fmt = (r: Rule, n = 3) => previewOccurrences(r, NOW, n).map((ms) => fmtWallWindow(ms, r.window_minutes));

describe("fmtLead and sameAsRule", () => {
  it("shortens the lead time", () => {
    expect(fmtLead(45)).toBe("45 min");
    expect(fmtLead(960)).toBe("16 h");
    expect(fmtLead(90)).toBe("1 h 30 min");
    expect(fmtLead(2880)).toBe("2 d");
  });
  it("treats a name that only repeats the timing as the rule", () => {
    expect(sameAsRule("Every day 08:00-09:30", "Every day 08:00–09:30")).toBe(true);
    expect(sameAsRule("Every day 09:20–11:20, 960 min early", "Every day 09:20–11:20")).toBe(true);
    expect(sameAsRule("Backup window", "Every day 08:00–09:30")).toBe(false);
  });
});

describe("describeRule", () => {
  it("reads like a sentence", () => {
    expect(describeRule({ ...base, freq: "weekly", weekdays: [5, 1, 2, 3, 4] })).toBe("Every weekday 09:00–11:00");
    expect(describeRule({ ...base, freq: "weekly", weekdays: [1, 4] })).toBe("Every Mon, Thu 09:00–11:00");
    expect(describeRule({ ...base, freq: "weekly", interval: 2, weekdays: [6, 7], window_start: "00:00", window_minutes: 1440 })).toBe("Every 2 weeks on weekends all day");
    expect(describeRule({ ...base, interval: 3, window_start: "22:00", window_minutes: 240 })).toBe("Every 3 days from 22:00 for 4 h");
    expect(describeRule({ ...base, freq: "monthly", month_day: -1, window_start: "09:00", window_minutes: 60 })).toBe("Monthly on the last day 09:00–10:00");
    expect(describeRule({ ...base, freq: "monthly", interval: 3, month_day: 22 })).toBe("Every 3 months on the 22nd 09:00–11:00");
    expect(describeRule({ ...base, window_start: "23:00", window_minutes: 60 })).toBe("Every day 23:00–24:00");
  });
  it("ordinals and durations", () => {
    expect([1, 2, 3, 4, 11, 12, 13, 21, 22, 23, 31].map(ordinal)).toEqual(["1st", "2nd", "3rd", "4th", "11th", "12th", "13th", "21st", "22nd", "23rd", "31st"]);
    expect([45, 60, 90, 1440, 2880].map(durText)).toEqual(["45 min", "1 h", "1 h 30 min", "1 d", "2 d"]);
    expect(windowText("", 1440)).toBe("all day");
  });
});

describe("previewOccurrences", () => {
  it("weekly", () => {
    expect(fmt({ ...base, freq: "weekly", weekdays: [1, 4] })).toEqual(["Mon 16 Nov 09:00–11:00", "Thu 19 Nov 09:00–11:00", "Mon 23 Nov 09:00–11:00"]);
  });
  it("every other week counts from the start week", () => {
    expect(fmt({ ...base, freq: "weekly", interval: 2, weekdays: [5] })).toEqual(["Fri 13 Nov 09:00–11:00", "Fri 27 Nov 09:00–11:00", "Fri 11 Dec 09:00–11:00"]);
  });
  it("monthly last day and the 31st in short months", () => {
    expect(fmt({ ...base, freq: "monthly", month_day: -1 })).toEqual(["Mon 30 Nov 09:00–11:00", "Thu 31 Dec 09:00–11:00", "Sun 31 Jan 09:00–11:00"]);
    expect(fmt({ ...base, freq: "monthly", month_day: 31 }, 2)).toEqual(["Mon 30 Nov 09:00–11:00", "Thu 31 Dec 09:00–11:00"]);
  });
  it("daily skips today when the window start has passed", () => {
    expect(fmt({ ...base, interval: 2, starts_on: Date.UTC(2026, 10, 12), window_start: "08:00" })).toEqual(["Sat 14 Nov 08:00–10:00", "Mon 16 Nov 08:00–10:00", "Wed 18 Nov 08:00–10:00"]);
  });
  it("uses the schedule's zone", () => {
    // 15:00 UTC is 10:00 in New York
    expect(fmt({ ...base, tz: "America/New_York", window_start: "11:00", window_minutes: 30 }, 1)).toEqual(["Thu 12 Nov 11:00–11:30"]);
    expect(fmt({ ...base, tz: "America/New_York", window_start: "09:00" }, 1)).toEqual(["Fri 13 Nov 09:00–11:00"]);
  });
  it("respects ends_on and max_count", () => {
    expect(fmt({ ...base, ends_on: Date.UTC(2026, 10, 14, 23, 0) }, 5)).toEqual(["Fri 13 Nov 09:00–11:00", "Sat 14 Nov 09:00–11:00"]);
    expect(fmt({ ...base, max_count: 1 }, 5)).toHaveLength(1);
  });
  it("formats all-day and overnight windows", () => {
    expect(fmtWallWindow(Date.UTC(2026, 10, 16), 1440)).toBe("Mon 16 Nov · all day");
    expect(fmtWallWindow(Date.UTC(2026, 10, 16, 22), 240)).toBe("Mon 16 Nov 22:00 → Tue 17 Nov 02:00");
    expect(fmtInstant(NOW, "UTC")).toBe("Thu 12 Nov 15:00");
  });
});

describe("form state", () => {
  it("builds the API rule, omitting what doesn't apply", () => {
    const s = { ...defaultRepeat(new Date(2026, 10, 12)), freq: "weekly" as const, weekdays: [4, 1], allDay: false, start: "09:00", len: 2, unit: "h" as const, tz: "UTC" };
    expect(repeatRule(s)).toEqual({ freq: "weekly", interval: 1, weekdays: [1, 4], tz: "UTC", window_start: "09:00", window_minutes: 120, lead_minutes: 0, overlap: "skip", paused: false });
    expect(repeatRule({ ...s, freq: "monthly", monthDay: -1, allDay: true, endMode: "count", count: 5 })).toMatchObject({ month_day: -1, window_start: "00:00", window_minutes: 1440, max_count: 5 });
    expect(repeatRule({ ...s, endMode: "date", endDate: "2026-12-01" }).ends_on).toBe(new Date(2026, 11, 1, 23, 59, 59).getTime());
  });
  it("defaults to today's weekday and day of month", () => {
    const d = defaultRepeat(new Date(2026, 10, 12));
    expect(d.weekdays).toEqual([4]);
    expect(d.monthDay).toBe(12);
    expect(d.freq).toBe("");
  });
  it("round-trips a stored schedule", () => {
    const r = { ...base, freq: "weekly" as const, weekdays: [1, 3], window_minutes: 90, lead_minutes: 15, overlap: "create", paused: true, max_count: 4 };
    const s = repeatFromRule(r);
    expect(s).toMatchObject({ freq: "weekly", allDay: false, start: "09:00", len: 90, unit: "m", lead: 15, endMode: "count", count: 4, overlap: "create", paused: true });
    expect(ruleOf(s, NOW)).toMatchObject({ freq: "weekly", weekdays: [1, 3], window_minutes: 90, max_count: 4, starts_on: NOW });
    expect(repeatFromRule({ ...r, window_start: "00:00", window_minutes: 1440 }).allDay).toBe(true);
  });
  it("flags problems", () => {
    const s = { ...defaultRepeat(), freq: "weekly" as const };
    expect(repeatProblem({ ...s, freq: "" })).toBeNull();
    expect(repeatProblem(s)).toBeNull();
    expect(repeatProblem({ ...s, weekdays: [] })).toMatch(/weekday/);
    expect(repeatProblem({ ...s, allDay: false, len: 2, unit: "m" })).toMatch(/5 minutes/);
    expect(repeatProblem({ ...s, endMode: "date", endDate: "" })).toMatch(/end date/);
    expect(repeatProblem({ ...s, interval: 0 })).toMatch(/1 to 365/);
  });
  it("names the field a typed value is wrong on, never taking another value instead", () => {
    const s = { ...defaultRepeat(), freq: "weekly" as const, allDay: false, start: "23:00", len: 1, unit: "h" as const };
    // the QA case: every 0, a window of 0, created -5 min early
    expect(repeatIssue({ ...s, interval: 0 })?.field).toBe("interval");
    expect(repeatIssue({ ...s, len: 0 })?.field).toBe("len");
    expect(repeatIssue({ ...s, lead: -5 })?.field).toBe("lead");
    // an empty box (NaN) or a fraction is wrong too
    expect(repeatIssue({ ...s, interval: NaN })?.field).toBe("interval");
    expect(repeatIssue({ ...s, interval: 1.5 })?.field).toBe("interval");
    expect(repeatIssue({ ...s, lead: NaN })?.field).toBe("lead");
    expect(repeatIssue({ ...s, len: NaN })?.field).toBe("len");
    expect(repeatIssue({ ...s, endMode: "count", count: 0 })?.field).toBe("count");
    // a start time that isn't one
    expect(repeatIssue({ ...s, start: "" })?.field).toBe("start");
    expect(repeatIssue({ ...s, start: "24:00" })?.field).toBe("start");
    expect(repeatIssue({ ...s, weekdays: [] })?.field).toBe("weekdays");
    // half an hour in hours is fine
    expect(repeatIssue({ ...s, len: 0.5 })).toBeNull();
    expect(repeatIssue(s)).toBeNull();
  });
  it("reads number boxes as typed", () => {
    expect(numIn("0")).toBe(0);
    expect(numIn("-5")).toBe(-5);
    expect(numIn("2.5")).toBe(2.5);
    expect(numIn("")).toBeNaN();
    expect(numIn("  ")).toBeNaN();
  });
});

describe("windowBadge", () => {
  const at = (h: number, m = 0, day = 17) => new Date(2026, 10, day, h, m).getTime();
  it("opens, closes, closed", () => {
    const t = { start_at: at(9), not_after: at(11) };
    expect(windowBadge(t, at(8))).toEqual({ text: "opens 09:00", state: "upcoming" });
    expect(windowBadge(t, at(10))).toEqual({ text: "closes 11:00", state: "open" });
    expect(windowBadge(t, at(12))).toEqual({ text: "window closed", state: "closed" });
  });
  it("hides when there's no window or the gig was missed", () => {
    expect(windowBadge({ start_at: at(9), not_after: null }, at(8))).toBeNull();
    expect(windowBadge({ start_at: at(9), not_after: at(11), missed_at: at(11) }, at(12))).toBeNull();
  });
  it("names the day when it isn't today", () => {
    expect(clock(at(9, 0, 18), at(8))).toBe("Wed 09:00");
    expect(clock(at(9, 5, 30), at(8))).toBe("30 Nov 09:05");
  });
  it("datetime-local round trip", () => {
    expect(toDateTimeInput(at(9, 5))).toBe("2026-11-17T09:05");
    expect(fromDateTimeInput("2026-11-17T09:05")).toBe(at(9, 5));
    expect(fromDateTimeInput("")).toBeNull();
  });
});

describe("time zone preference", () => {
  it("uses the preference when it's a real zone, else the browser's", async () => {
    const { setPreferredTZ, localTZ, browserTZ, validTZ } = await import("./recurrence");
    setPreferredTZ("Europe/Paris");
    expect(localTZ()).toBe("Europe/Paris");
    setPreferredTZ("Mars/Olympus");
    expect(localTZ()).toBe(browserTZ());
    setPreferredTZ(undefined);
    expect(localTZ()).toBe(browserTZ());
    expect(validTZ("UTC")).toBe(true);
  });
});

describe("end dates in the schedule's zone", () => {
  it("ends at 23:59:59 on that day where the schedule runs, whatever the browser's zone", async () => {
    const { instantOfWall, wallParts } = await import("./recurrence");
    const t = instantOfWall(2026, 3, 8, 23, 59, 59, "America/Los_Angeles"); // DST starts that day there
    const w = wallParts(t, "America/Los_Angeles");
    expect([w.y, w.m, w.d, w.hh, w.mm]).toEqual([2026, 3, 8, 23, 59]);
    const u = instantOfWall(2026, 11, 14, 23, 59, 59, "Asia/Tokyo");
    expect(new Date(u).toISOString()).toBe("2026-11-14T14:59:59.000Z");
  });
});
