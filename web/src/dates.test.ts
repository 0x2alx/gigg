import { describe, expect, it } from "vitest";
import { DEFAULT_DUE_TIME, DEFAULT_START_TIME, fillTime, fmtDue, joinDateTime, splitDateTime } from "./dates";

describe("date and time inputs", () => {
  it("fills the default time when only a date is picked", () => {
    expect(fillTime("2026-11-12", "", DEFAULT_START_TIME)).toBe("09:00");
    expect(fillTime("2026-11-12", "", DEFAULT_DUE_TIME)).toBe("17:00");
    expect(fillTime("2026-11-12", "08:15", DEFAULT_DUE_TIME)).toBe("08:15");
    expect(fillTime("", "08:15", DEFAULT_DUE_TIME)).toBe("");
  });
  it("joins local date and time into unix ms and back", () => {
    const ms = joinDateTime("2026-11-12", "", DEFAULT_DUE_TIME)!;
    expect(ms).toBe(new Date(2026, 10, 12, 17, 0).getTime());
    expect(splitDateTime(ms)).toEqual({ date: "2026-11-12", time: "17:00" });
    expect(joinDateTime("2026-11-12", "07:30", DEFAULT_DUE_TIME)).toBe(new Date(2026, 10, 12, 7, 30).getTime());
    expect(joinDateTime("", "07:30", DEFAULT_DUE_TIME)).toBeNull();
    expect(splitDateTime(null)).toEqual({ date: "", time: "" });
  });
  it("shows the time on due pills for today and tomorrow only", () => {
    const now = new Date(2026, 10, 12, 10, 0);
    expect(fmtDue(new Date(2026, 10, 12, 17, 0).getTime(), now)).toMatch(/^Today /);
    expect(fmtDue(new Date(2026, 10, 13, 9, 0).getTime(), now)).toMatch(/^Tomorrow /);
    expect(fmtDue(new Date(2026, 10, 20, 9, 0).getTime(), now)).not.toMatch(/Today|Tomorrow|09/);
    expect(fmtDue(null, now)).toBe("");
  });
});
